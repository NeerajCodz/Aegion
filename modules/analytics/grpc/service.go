package grpc

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

// Service implements the gRPC AnalyticsService.
type Service struct {
	pb.UnimplementedAnalyticsServiceServer
	logger      zerolog.Logger
	store       Store
	syncManager SyncManager
	config      Config
}

// Store interface provides analytics data operations.
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

// SyncManager interface provides sync operations.
type SyncManager interface {
	GetSyncStatus(ctx context.Context) (map[string]interface{}, error)
	GetSyncLag(ctx context.Context) (int64, error)
}

// Config holds gRPC service configuration.
type Config struct {
	MaxConcurrentStreams int
	KeepaliveTime        int
	KeepaliveTimeout     int
}

// NewService creates a new gRPC analytics service.
func NewService(logger zerolog.Logger, store Store, syncManager SyncManager, config Config) *Service {
	return &Service{
		logger:      logger,
		store:       store,
		syncManager: syncManager,
		config:      config,
	}
}

// QueryEvents implements the QueryEvents RPC.
func (s *Service) QueryEvents(ctx context.Context, req *pb.QueryEventsRequest) (*pb.QueryEventsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	s.logger.Debug().
		Str("category", req.Category).
		Str("event_type", req.EventType).
		Msg("Querying events")

	// Build filters map
	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}
	if req.EventType != "" {
		filters["event_type"] = req.EventType
	}

	// Set default page size
	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = 100
	}

	// Query events
	events, totalCount, nextToken, err := s.store.QueryEvents(ctx, filters, pageSize, req.PageToken)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to query events")
		return nil, status.Error(codes.Internal, "failed to query events")
	}

	// Convert to protobuf events
	pbEvents := make([]*pb.Event, len(events))
	for i, e := range events {
		pbEvent, err := eventToProto(e)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to convert event")
			continue
		}
		pbEvents[i] = pbEvent
	}

	return &pb.QueryEventsResponse{
		Events:        pbEvents,
		TotalCount:    totalCount,
		NextPageToken: nextToken,
	}, nil
}

// GetDashboard implements the GetDashboard RPC.
func (s *Service) GetDashboard(ctx context.Context, req *pb.GetDashboardRequest) (*pb.DashboardData, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "dashboard ID is required")
	}

	s.logger.Debug().Str("dashboard_id", req.Id).Msg("Getting dashboard")

	dashboard, err := s.store.GetDashboard(ctx, req.Id)
	if err != nil {
		s.logger.Error().Err(err).Str("dashboard_id", req.Id).Msg("Failed to get dashboard")
		return nil, status.Error(codes.NotFound, "dashboard not found")
	}

	pbDashboard := dashboardToProto(dashboard)

	return &pb.DashboardData{
		Dashboard: pbDashboard,
		Events:    []*pb.Event{},
		Metrics:   []*pb.Metric{},
	}, nil
}

// GetHealthStatus implements the GetHealthStatus RPC.
func (s *Service) GetHealthStatus(ctx context.Context, _ *pb.Empty) (*pb.HealthStatus, error) {
	s.logger.Debug().Msg("Checking health status")

	health, err := s.store.GetHealthStatus(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to get health status")
		return nil, status.Error(codes.Internal, "failed to check health")
	}

	syncLag, _ := s.syncManager.GetSyncLag(ctx)

	return healthStatusToProto(health, syncLag), nil
}

// ExecuteQuery implements the ExecuteQuery RPC.
func (s *Service) ExecuteQuery(ctx context.Context, req *pb.ExecuteQueryRequest) (*pb.QueryResult, error) {
	if req == nil || req.QueryId == "" {
		return nil, status.Error(codes.InvalidArgument, "query_id is required")
	}

	s.logger.Debug().
		Str("query_id", req.QueryId).
		Int("page_size", int(req.PageSize)).
		Msg("Executing query")

	lookupSQL := fmt.Sprintf("SELECT sql FROM queries WHERE id = '%s' LIMIT 1", sanitizeSQLLiteral(req.QueryId))
	queryRows, err := s.store.ExecuteQuery(ctx, lookupSQL, nil)
	if err != nil {
		s.logger.Error().Err(err).Str("query_id", req.QueryId).Msg("Failed to load saved query")
		return nil, status.Error(codes.Internal, "failed to load saved query")
	}
	if len(queryRows) == 0 {
		return nil, status.Error(codes.NotFound, "query not found")
	}

	rawSQL := fmt.Sprintf("%v", queryRows[0]["sql"])
	if err := validateReadOnlySQL(rawSQL); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	execSQL := applyQueryLimit(rawSQL, int(req.PageSize))
	rows, err := s.store.ExecuteQuery(ctx, execSQL, nil)
	if err != nil {
		s.logger.Error().Err(err).Str("query_id", req.QueryId).Msg("Failed to execute saved query")
		return nil, status.Error(codes.Internal, "failed to execute query")
	}

	return &pb.QueryResult{
		Id:       req.QueryId,
		RowCount: int64(len(rows)),
	}, nil
}

// CreateDashboard implements the CreateDashboard RPC.
func (s *Service) CreateDashboard(ctx context.Context, req *pb.CreateDashboardRequest) (*pb.Dashboard, error) {
	if req == nil || req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	s.logger.Debug().Str("name", req.Name).Msg("Creating dashboard")

	dashboardData := map[string]interface{}{
		"name":        req.Name,
		"description": req.Description,
		"config":      req.Config.AsMap(),
		"public":      req.Public,
	}

	dashboardID, err := s.store.CreateDashboard(ctx, dashboardData)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create dashboard")
		return nil, status.Error(codes.Internal, "failed to create dashboard")
	}

	dashboardData["id"] = dashboardID
	return dashboardToProto(dashboardData), nil
}

// UpdateDashboard implements the UpdateDashboard RPC.
func (s *Service) UpdateDashboard(ctx context.Context, req *pb.UpdateDashboardRequest) (*pb.Dashboard, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	s.logger.Debug().Str("id", req.Id).Msg("Updating dashboard")

	dashboardData := map[string]interface{}{
		"name":        req.Name,
		"description": req.Description,
		"config":      req.Config.AsMap(),
		"public":      req.Public,
	}

	if err := s.store.UpdateDashboard(ctx, req.Id, dashboardData); err != nil {
		s.logger.Error().Err(err).Str("id", req.Id).Msg("Failed to update dashboard")
		return nil, status.Error(codes.Internal, "failed to update dashboard")
	}

	dashboardData["id"] = req.Id
	return dashboardToProto(dashboardData), nil
}

// StreamEvents implements the StreamEvents RPC for server-side streaming.
func (s *Service) StreamEvents(req *pb.StreamEventsRequest, stream grpc.ServerStream) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	ctx := stream.Context()
	s.logger.Debug().
		Str("category", req.Category).
		Bool("include_historical", req.IncludeHistorical).
		Msg("Streaming events")

	// Build filters
	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}

	// Stream existing events if requested
	if req.IncludeHistorical {
		events, _, _, err := s.store.QueryEvents(ctx, filters, 1000, "")
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to query historical events")
			return status.Error(codes.Internal, "failed to query historical events")
		}

		for _, e := range events {
			pbEvent, err := eventToProto(e)
			if err != nil {
				s.logger.Warn().Err(err).Msg("Failed to convert event")
				continue
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				if err := stream.SendMsg(pbEvent); err != nil {
					s.logger.Error().Err(err).Msg("Failed to send event")
					return err
				}
			}
		}
	}

	// In a real implementation, subscribe to live events
	// For now, we don't have a live subscription mechanism; end the stream once
	// any historical events are sent.
	s.logger.Debug().Msg("Event streaming complete (no live subscription configured)")
	return nil
}

func sanitizeSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func validateReadOnlySQL(query string) error {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return fmt.Errorf("saved query SQL is empty")
	}

	upper := strings.ToUpper(trimmed)
	if !(strings.HasPrefix(upper, "SELECT ") || strings.HasPrefix(upper, "WITH ")) {
		return fmt.Errorf("saved query must be read-only")
	}

	disallowed := []string{
		" INSERT ",
		" UPDATE ",
		" DELETE ",
		" DROP ",
		" ALTER ",
		" CREATE ",
		" TRUNCATE ",
		" ATTACH ",
		" DETACH ",
	}
	padded := " " + upper + " "
	for _, keyword := range disallowed {
		if strings.Contains(padded, keyword) {
			return fmt.Errorf("saved query contains disallowed statement")
		}
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

// ExportData implements the ExportData RPC for streaming exports.
func (s *Service) ExportData(req *pb.ExportDataRequest, stream grpc.ServerStream) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	ctx := stream.Context()
	s.logger.Debug().
		Str("format", req.Format.String()).
		Int64("max_records", req.MaxRecords).
		Msg("Starting data export")

	// Build filters
	filters := make(map[string]interface{})
	if req.Category != "" {
		filters["category"] = req.Category
	}

	// Get format string
	formatStr := "json"
	switch req.Format {
	case pb.ExportFormat_CSV:
		formatStr = "csv"
	case pb.ExportFormat_PARQUET:
		formatStr = "parquet"
	}

	// Export data
	data, err := s.store.ExportData(ctx, formatStr, filters, req.MaxRecords)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to export data")
		return status.Error(codes.Internal, "failed to export data")
	}

	// Stream data in chunks
	chunkSize := 64 * 1024 // 64KB chunks
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
			return ctx.Err()
		default:
			if err := stream.SendMsg(chunk); err != nil {
				s.logger.Error().Err(err).Msg("Failed to send data chunk")
				return err
			}
		}
	}

	s.logger.Debug().Int("bytes_sent", len(data)).Msg("Data export complete")
	return nil
}

// BatchQuery implements the BatchQuery RPC for bidirectional streaming.
func (s *Service) BatchQuery(stream grpc.ServerStream) error {
	ctx := stream.Context()
	s.logger.Debug().Msg("Starting batch query processing")

	results := make(chan *pb.QueryResult)
	errors := make(chan error)

	go func() {
		for {
			req := &pb.QueryBatch{}
			err := stream.RecvMsg(req)
			if err == io.EOF {
				close(results)
				close(errors)
				return
			}
			if err != nil {
				errors <- err
				return
			}

			// Process query
			s.logger.Debug().
				Str("request_id", req.RequestId).
				Str("query_id", req.QueryId).
				Msg("Processing batch query")

			// In a real implementation, fetch and execute saved query
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

	// Send results back to client
	for {
		select {
		case result, ok := <-results:
			if !ok {
				return nil
			}
			if err := stream.SendMsg(result); err != nil {
				s.logger.Error().Err(err).Msg("Failed to send query result")
				return err
			}
		case err := <-errors:
			s.logger.Error().Err(err).Msg("Error processing batch query")
			return status.Error(codes.Internal, "error processing batch query")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Helper functions to convert between Go and protobuf types

func eventToProto(data interface{}) (*pb.Event, error) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid event data type")
	}

	// Convert timestamps if needed
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
		IsHealthy:       toBool(data["is_healthy"]),
		Status:          toString(data["status"]),
		Duckdb:          toBool(data["duckdb"]),
		Storage:         toBool(data["storage"]),
		Migrations:      toBool(data["migrations"]),
		SyncLagSeconds:  syncLag,
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
