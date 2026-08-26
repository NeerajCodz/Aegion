package grpc

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aegion/aegion/internal/xlog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

var uuidTextPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Service struct {
	pb.UnimplementedAnalyticsServiceServer
	logger      *xlog.Logger
	store       Store
	syncManager SyncManager
	config      Config
}

type Store interface {
	QueryEvents(ctx context.Context, filters map[string]interface{}, pageSize int, pageToken string) ([]interface{}, int64, string, error)
	GetDashboard(ctx context.Context, id string) (map[string]interface{}, error)
	GetEvent(ctx context.Context, id string) (map[string]interface{}, error)
	ExecuteQuery(ctx context.Context, sql string, params []interface{}) ([]map[string]interface{}, error)
	CreateDashboard(ctx context.Context, dashboard map[string]interface{}) (string, error)
	UpdateDashboard(ctx context.Context, id string, dashboard map[string]interface{}) error
	GetHealthStatus(ctx context.Context) (map[string]interface{}, error)
	ExportData(ctx context.Context, format string, filters map[string]interface{}, maxRecords int64) ([]byte, error)
}

type SyncManager interface {
	GetSyncStatus(ctx context.Context) (map[string]interface{}, error)
	GetSyncLag(ctx context.Context) (int64, error)
}

type Config struct {
	MaxConcurrentStreams int
	KeepaliveTime        int
	KeepaliveTimeout     int
}

func NewService(log *xlog.Logger, store Store, syncManager SyncManager, config Config) *Service {
	return &Service{
		logger:      log,
		store:       store,
		syncManager: syncManager,
		config:      config,
	}
}

func (s *Service) QueryEvents(ctx context.Context, req *pb.QueryEventsRequest) (*pb.QueryEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	event := s.logger.Start(ctx, "analytics.query_events", xlog.WithKind(xlog.KindRequest)).
		Set("category", req.Category).
		Set("event_type", req.EventType)

	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}
	if req.EventType != "" {
		filters["event_type"] = req.EventType
	}

	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 100
	}
	event.Set("page_size", pageSize)

	events, totalCount, nextToken, err := s.store.QueryEvents(ctx, filters, pageSize, req.PageToken)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to query events")
	}

	event.Set("total_count", totalCount)
	pbEvents := make([]*pb.Event, 0, len(events))
	for i, e := range events {
		pbEvent, err := eventToProto(e)
		if err != nil {
			event.Set("event_index", i).Set("convert_error", err.Error())
			continue
		}
		pbEvents = append(pbEvents, pbEvent)
	}

	event.Success()
	_ = event.Emit()

	return &pb.QueryEventsResponse{
		Events:        pbEvents,
		TotalCount:    totalCount,
		NextPageToken: nextToken,
	}, nil
}

func (s *Service) GetDashboard(ctx context.Context, req *pb.GetDashboardRequest) (*pb.DashboardData, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "dashboard ID is required")
	}

	event := s.logger.Start(ctx, "analytics.get_dashboard", xlog.WithKind(xlog.KindRequest)).
		Set("dashboard_id", req.Id)

	dashboard, err := s.store.GetDashboard(ctx, req.Id)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.NotFound, "dashboard not found")
	}

	pbDashboard := dashboardToProto(dashboard)
	event.Set("dashboard_name", dashboard["name"])
	event.Success()
	_ = event.Emit()

	return &pb.DashboardData{
		Dashboard: pbDashboard,
		Events:    []*pb.Event{},
		Metrics:   []*pb.Metric{},
	}, nil
}

func (s *Service) GetHealthStatus(ctx context.Context, _ *pb.Empty) (*pb.HealthStatus, error) {
	event := s.logger.Start(ctx, "analytics.health_check", xlog.WithKind(xlog.KindSystem))

	health, err := s.store.GetHealthStatus(ctx)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to check health")
	}

	syncLag, _ := s.syncManager.GetSyncLag(ctx)
	event.Set("is_healthy", health["is_healthy"]).Set("sync_lag", syncLag)
	event.Success()
	_ = event.Emit()

	return healthStatusToProto(health, syncLag), nil
}

func (s *Service) ExecuteQuery(ctx context.Context, req *pb.ExecuteQueryRequest) (*pb.QueryResult, error) {
	if req == nil || req.QueryId == "" {
		return nil, status.Error(codes.InvalidArgument, "query_id is required")
	}

	event := s.logger.Start(ctx, "analytics.execute_query", xlog.WithKind(xlog.KindRequest)).
		Set("query_id", req.QueryId).
		Set("page_size", int(req.PageSize))

	lookupSQL := fmt.Sprintf("SELECT sql FROM queries WHERE id = '%s' LIMIT 1", sanitizeSQLLiteral(req.QueryId))
	queryRows, err := s.store.ExecuteQuery(ctx, lookupSQL, nil)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to load saved query")
	}
	if len(queryRows) == 0 {
		event.Rejected(fmt.Errorf("query not found"))
		_ = event.Emit()
		return nil, status.Error(codes.NotFound, "query not found")
	}

	rawSQL := fmt.Sprintf("%v", queryRows[0]["sql"])
	if err := validateReadOnlySQL(rawSQL); err != nil {
		event.Set("error", err.Error()).Rejected(err)
		_ = event.Emit()
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	execSQL := applyQueryLimit(rawSQL, int(req.PageSize))
	rows, err := s.store.ExecuteQuery(ctx, execSQL, nil)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to execute query")
	}

	event.Set("row_count", len(rows))
	event.Success()
	_ = event.Emit()

	return &pb.QueryResult{
		Id:       req.QueryId,
		RowCount: int64(len(rows)),
	}, nil
}

func (s *Service) CreateDashboard(ctx context.Context, req *pb.CreateDashboardRequest) (*pb.Dashboard, error) {
	if req == nil || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	event := s.logger.Start(ctx, "analytics.create_dashboard", xlog.WithKind(xlog.KindRequest)).
		Set("name", req.Name)

	dashboardData := map[string]interface{}{
		"name":        req.Name,
		"description": req.Description,
		"config":      req.Config.AsMap(),
		"public":      req.Public,
	}

	dashboardID, err := s.store.CreateDashboard(ctx, dashboardData)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to create dashboard")
	}

	dashboardData["id"] = dashboardID
	event.Set("dashboard_id", dashboardID)
	event.Success()
	_ = event.Emit()

	return dashboardToProto(dashboardData), nil
}

func (s *Service) UpdateDashboard(ctx context.Context, req *pb.UpdateDashboardRequest) (*pb.Dashboard, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	event := s.logger.Start(ctx, "analytics.update_dashboard", xlog.WithKind(xlog.KindRequest)).
		Set("id", req.Id)

	dashboardData := map[string]interface{}{
		"name":        req.Name,
		"description": req.Description,
		"config":      req.Config.AsMap(),
		"public":      req.Public,
	}

	if err := s.store.UpdateDashboard(ctx, req.Id, dashboardData); err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, status.Error(codes.Internal, "failed to update dashboard")
	}

	dashboardData["id"] = req.Id
	event.Success()
	_ = event.Emit()

	return dashboardToProto(dashboardData), nil
}

func (s *Service) StreamEvents(req *pb.StreamEventsRequest, stream pb.AnalyticsService_StreamEventsServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	ctx := stream.Context()
	event := s.logger.Start(ctx, "analytics.stream_events", xlog.WithKind(xlog.KindRequest)).
		Set("category", req.Category).
		Set("include_historical", req.IncludeHistorical)

	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}

	sentCount := 0
	if req.IncludeHistorical {
		events, _, _, err := s.store.QueryEvents(ctx, filters, 1000, "")
		if err != nil {
			event.Set("error", err.Error()).Error(err)
			_ = event.Emit()
			return status.Error(codes.Internal, "failed to query historical events")
		}

		for _, e := range events {
			pbEvent, err := eventToProto(e)
			if err != nil {
				event.Set("convert_error", err.Error())
				continue
			}

			select {
			case <-ctx.Done():
				event.Set("sent_count", sentCount).Error(ctx.Err())
				_ = event.Emit()
				return ctx.Err()
			default:
				if err := stream.Send(pbEvent); err != nil {
					event.Set("error", err.Error()).Error(err)
					_ = event.Emit()
					return err
				}
				sentCount++
			}
		}
	}

	event.Set("sent_count", sentCount)
	event.Success()
	_ = event.Emit()
	return nil
}

func sanitizeSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func nullableUUID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !uuidTextPattern.MatchString(value) {
		return "NULL"
	}
	return "'" + sanitizeSQLLiteral(value) + "'"
}

func validateReadOnlySQL(query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return fmt.Errorf("saved query SQL is empty")
	}
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("saved query must contain exactly one statement")
	}

	upper := strings.ToUpper(trimmed)
	if !(strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "WITH ")) {
		return fmt.Errorf("saved query must be read-only")
	}

	disallowedPattern := regexp.MustCompile(`\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|TRUNCATE|ATTACH|DETACH)\b`)
	if disallowedPattern.FindStringIndex(upper) != nil {
		return fmt.Errorf("saved query contains disallowed statement")
	}

	return nil
}

func applyQueryLimit(query string, pageSize int) string {
	if pageSize <= 0 {
		return query
	}

	upper := strings.ToUpper(query)
	if strings.Contains(upper, " LIMIT ") {
		return query
	}

	return fmt.Sprintf("%s LIMIT %d", strings.TrimSpace(query), pageSize)
}

func (s *Service) ExportData(req *pb.ExportDataRequest, stream pb.AnalyticsService_ExportDataServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	ctx := stream.Context()
	event := s.logger.Start(ctx, "analytics.export_data", xlog.WithKind(xlog.KindRequest)).
		Set("format", req.Format.String()).
		Set("max_records", req.MaxRecords)

	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}

	formatStr := "json"
	switch req.Format {
	case pb.ExportFormat_CSV:
		formatStr = "csv"
	case pb.ExportFormat_PARQUET:
		formatStr = "parquet"
	}

	data, err := s.store.ExportData(ctx, formatStr, filters, req.MaxRecords)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return status.Error(codes.Internal, "failed to export data")
	}

	chunkSize := 64 * 1024
	sentBytes := 0
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		chunk := &pb.DataChunk{
			Data:     data[i:end],
			Sequence: int32(i / chunkSize),
			IsFinal:  end >= len(data),
		}

		select {
		case <-ctx.Done():
			event.Set("sent_bytes", sentBytes).Error(ctx.Err())
			_ = event.Emit()
			return ctx.Err()
		default:
			if err := stream.Send(chunk); err != nil {
				event.Set("error", err.Error()).Error(err)
				_ = event.Emit()
				return err
			}
			sentBytes += len(chunk.Data)
		}
	}

	event.Set("total_bytes", sentBytes)
	event.Success()
	_ = event.Emit()
	return nil
}

func (s *Service) BatchQuery(stream pb.AnalyticsService_BatchQueryServer) error {
	ctx := stream.Context()
	event := s.logger.Start(ctx, "analytics.batch_query", xlog.WithKind(xlog.KindRequest))

	results := make(chan *pb.QueryResult)
	errors := make(chan error)

	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				errors <- err
				return
			}
			if req == nil {
				continue
			}

			result := &pb.QueryResult{
				Id:       req.RequestId,
				RowCount: 0,
			}

			select {
			case results <- result:
			case <-ctx.Done():
				return
			}
		}
	}()

	sentCount := 0
	for {
		select {
		case result, ok := <-results:
			if !ok {
				event.Set("sent_count", sentCount)
				event.Success()
				_ = event.Emit()
				return nil
			}
			if err := stream.Send(result); err != nil {
				event.Set("error", err.Error()).Error(err)
				_ = event.Emit()
				return err
			}
			sentCount++
		case err := <-errors:
			event.Set("error", err.Error()).Error(err)
			_ = event.Emit()
			return status.Error(codes.Internal, "error processing batch query")
		case <-ctx.Done():
			event.Set("sent_count", sentCount).Error(ctx.Err())
			_ = event.Emit()
			return ctx.Err()
		}
	}
}

func eventToProto(data interface{}) (*pb.Event, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid event data type")
	}

	return &pb.Event{
		Id:        toString(m["id"]),
		Category:  toString(m["category"]),
		EventType: toString(m["event_type"]),
	}, nil
}

func dashboardToProto(data map[string]interface{}) *pb.Dashboard {
	return &pb.Dashboard{
		Id:          toString(data["id"]),
		Name:        toString(data["name"]),
		Description: toString(data["description"]),
		OwnerId:     toString(data["owner_id"]),
		Public:      toBool(data["public"]),
	}
}

func healthStatusToProto(data map[string]interface{}, syncLag int64) *pb.HealthStatus {
	return &pb.HealthStatus{
		IsHealthy:      toBool(data["is_healthy"]),
		Status:         toString(data["status"]),
		Duckdb:         toBool(data["duckdb"]),
		Storage:        toBool(data["storage"]),
		Migrations:     toBool(data["migrations"]),
		SyncLagSeconds: syncLag,
	}
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func toBool(v interface{}) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
