package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

// MockStore implements Store interface for testing
type MockStore struct {
	events     []interface{}
	dashboards map[string]map[string]interface{}
	queryRows  map[string][]map[string]interface{}
	queryErr   error
}

func NewMockStore() *MockStore {
	return &MockStore{
		events:     []interface{}{},
		dashboards: make(map[string]map[string]interface{}),
		queryRows:  make(map[string][]map[string]interface{}),
	}
}

func (m *MockStore) QueryEvents(ctx context.Context, filters map[string]interface{}, pageSize int, pageToken string) ([]interface{}, int64, string, error) {
	return m.events, int64(len(m.events)), "", nil
}

func (m *MockStore) GetDashboard(ctx context.Context, id string) (map[string]interface{}, error) {
	if dashboard, ok := m.dashboards[id]; ok {
		return dashboard, nil
	}
	return nil, nil
}

func (m *MockStore) GetEvent(ctx context.Context, id string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockStore) ExecuteQuery(ctx context.Context, sql string, params []interface{}) ([]map[string]interface{}, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	if rows, ok := m.queryRows[sql]; ok {
		return rows, nil
	}
	return []map[string]interface{}{}, nil
}

func (m *MockStore) CreateDashboard(ctx context.Context, dashboard map[string]interface{}) (string, error) {
	id := "test-dashboard-1"
	dashboard["id"] = id
	m.dashboards[id] = dashboard
	return id, nil
}

func (m *MockStore) UpdateDashboard(ctx context.Context, id string, dashboard map[string]interface{}) error {
	if _, ok := m.dashboards[id]; ok {
		m.dashboards[id] = dashboard
		m.dashboards[id]["id"] = id
		return nil
	}
	return nil
}

func (m *MockStore) GetHealthStatus(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"is_healthy": true,
		"status":     "healthy",
		"duckdb":     true,
		"storage":    true,
		"migrations": true,
	}, nil
}

func (m *MockStore) ExportData(ctx context.Context, format string, filters map[string]interface{}, maxRecords int64) ([]byte, error) {
	return []byte(`[{"id":"1","name":"test"}]`), nil
}

// MockSyncManager implements SyncManager interface for testing
type MockSyncManager struct{}

func (m *MockSyncManager) GetSyncStatus(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockSyncManager) GetSyncLag(ctx context.Context) (int64, error) {
	return 100, nil
}

// TestQueryEvents tests the QueryEvents RPC
func TestQueryEvents(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	req := &pb.QueryEventsRequest{
		PageSize:  50,
		Category:  "test",
		EventType: "click",
	}

	resp, err := service.QueryEvents(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int64(0), resp.TotalCount)
}

// TestQueryEventsNilRequest tests QueryEvents with nil request
func TestQueryEventsNilRequest(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	resp, err := service.QueryEvents(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestGetDashboard tests the GetDashboard RPC
func TestGetDashboard(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	// First create a dashboard
	store.dashboards["dash-1"] = map[string]interface{}{
		"id":          "dash-1",
		"name":        "Test Dashboard",
		"description": "A test dashboard",
		"owner_id":    "user-1",
		"public":      true,
	}

	req := &pb.GetDashboardRequest{
		Id: "dash-1",
	}

	resp, err := service.GetDashboard(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "dash-1", resp.Dashboard.Id)
	assert.Equal(t, "Test Dashboard", resp.Dashboard.Name)
}

// TestGetHealthStatus tests the GetHealthStatus RPC
func TestGetHealthStatus(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	req := &pb.Empty{}

	resp, err := service.GetHealthStatus(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.IsHealthy)
	assert.Equal(t, "healthy", resp.Status)
	assert.Equal(t, int64(100), resp.SyncLagSeconds)
}

// TestCreateDashboard tests the CreateDashboard RPC
func TestCreateDashboard(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	config_struct, err := structpb.NewStruct(map[string]interface{}{
		"chart_type": "line",
		"period":     "day",
	})
	require.NoError(t, err)

	req := &pb.CreateDashboardRequest{
		Name:        "New Dashboard",
		Description: "A new dashboard",
		Config:      config_struct,
		Public:      true,
	}

	resp, err := service.CreateDashboard(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "New Dashboard", resp.Name)
	assert.True(t, resp.Public)
}

// TestUpdateDashboard tests the UpdateDashboard RPC
func TestUpdateDashboard(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	// Create dashboard first
	store.dashboards["dash-1"] = map[string]interface{}{
		"id":       "dash-1",
		"name":     "Old Dashboard",
		"owner_id": "user-1",
		"public":   false,
	}

	config_struct, err := structpb.NewStruct(map[string]interface{}{
		"chart_type": "bar",
	})
	require.NoError(t, err)

	req := &pb.UpdateDashboardRequest{
		Id:          "dash-1",
		Name:        "Updated Dashboard",
		Description: "Updated description",
		Config:      config_struct,
		Public:      true,
	}

	resp, err := service.UpdateDashboard(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Updated Dashboard", resp.Name)
}

func TestExecuteQuery(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	store.queryRows["SELECT sql FROM queries WHERE id = 'query-1' LIMIT 1"] = []map[string]interface{}{
		{"sql": "SELECT id, name FROM analytics_events"},
	}
	store.queryRows["SELECT id, name FROM analytics_events LIMIT 25"] = []map[string]interface{}{
		{"id": "1", "name": "event1"},
		{"id": "2", "name": "event2"},
	}

	service := NewService(logger, store, syncManager, config)

	resp, err := service.ExecuteQuery(context.Background(), &pb.ExecuteQueryRequest{
		QueryId:  "query-1",
		PageSize: 25,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "query-1", resp.Id)
	assert.Equal(t, int64(2), resp.RowCount)
}

func TestExecuteQuery_NotFound(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	resp, err := service.ExecuteQuery(context.Background(), &pb.ExecuteQueryRequest{
		QueryId: "missing-query",
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestExecuteQuery_RejectsMutatingSQL(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	store.queryRows["SELECT sql FROM queries WHERE id = 'query-bad' LIMIT 1"] = []map[string]interface{}{
		{"sql": "DELETE FROM analytics_events"},
	}

	service := NewService(logger, store, syncManager, config)

	resp, err := service.ExecuteQuery(context.Background(), &pb.ExecuteQueryRequest{
		QueryId: "query-bad",
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestStreamEvents tests server streaming
func TestStreamEvents(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	req := &pb.StreamEventsRequest{
		Category:           "test",
		IncludeHistorical:  true,
	}

	// Create a mock stream
	stream := &mockServerStream{
		ctx:      context.Background(),
		messages: make([]interface{}, 0),
	}

	err := service.StreamEvents(req, stream)
	require.NoError(t, err)
	assert.Len(t, stream.messages, 0)
}

// TestExportData tests server streaming export
func TestExportData(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	req := &pb.ExportDataRequest{
		Format:    pb.ExportFormat_JSON,
		MaxRecords: 100,
	}

	stream := &mockServerStream{
		ctx:      context.Background(),
		messages: make([]interface{}, 0),
	}

	err := service.ExportData(req, stream)
	require.NoError(t, err)
	assert.Greater(t, len(stream.messages), 0)
}

// Mock ServerStream for testing
type mockServerStream struct {
	ctx      context.Context
	messages []interface{}
}

func (s *mockServerStream) SetHeader(md metadata.MD) error  { return nil }
func (s *mockServerStream) SendHeader(md metadata.MD) error { return nil }
func (s *mockServerStream) SetTrailer(md metadata.MD)       {}
func (s *mockServerStream) Context() context.Context              { return s.ctx }
func (s *mockServerStream) SendMsg(m interface{}) error {
	s.messages = append(s.messages, m)
	return nil
}
func (s *mockServerStream) RecvMsg(m interface{}) error {
	return nil
}

// TestServer tests the gRPC server startup
func TestServer(t *testing.T) {
	logger := zerolog.New(nil)
	store := NewMockStore()
	syncManager := &MockSyncManager{}
	config := Config{}

	service := NewService(logger, store, syncManager, config)

	serverCfg := ServerConfig{
		Port:   0, // Use random port
		Logger: logger,
	}

	server, err := NewServer(serverCfg, service)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.True(t, server.IsRunning())

	// Cleanup
	_ = server.Stop(1 * time.Second)
}
