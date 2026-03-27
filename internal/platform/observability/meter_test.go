package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestNewMeterWrapper(t *testing.T) {
	// Set up a no-op meter provider to avoid actual telemetry
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	assert.NotNil(t, meter)
	assert.NotNil(t, meter.meter)
	assert.NotNil(t, meter.httpRequestCount)
	assert.NotNil(t, meter.httpRequestDuration)
	assert.NotNil(t, meter.dbQueryCount)
	assert.NotNil(t, meter.dbQueryDuration)
}

func TestMeterWrapper_RecordHTTPRequest(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// Test recording HTTP request metrics
	meter.RecordHTTPRequest(
		ctx,
		"GET",
		"/api/v1/users",
		200,
		100*time.Millisecond,
		1024,  // request size
		2048,  // response size
	)
	
	// Test with zero sizes
	meter.RecordHTTPRequest(
		ctx,
		"POST",
		"/api/v1/users",
		201,
		50*time.Millisecond,
		0,  // no request size
		0,  // no response size
	)
	
	// No assertions here since we're using no-op meter
	// In real implementation, we'd verify metrics were recorded
}

func TestMeterWrapper_RecordDatabaseQuery(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// Test successful query
	meter.RecordDatabaseQuery(
		ctx,
		"SELECT",
		"users",
		25*time.Millisecond,
		true,
	)
	
	// Test failed query
	meter.RecordDatabaseQuery(
		ctx,
		"INSERT",
		"users",
		100*time.Millisecond,
		false,
	)
}

func TestMeterWrapper_SetDatabaseConnections(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.SetDatabaseConnections(ctx, 10)
	meter.SetDatabaseConnections(ctx, 5)  // Should update the gauge
}

func TestMeterWrapper_SetActiveUsers(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.SetActiveUsers(ctx, 100)
	meter.SetActiveUsers(ctx, 150)
}

func TestMeterWrapper_SetActiveSessions(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.SetActiveSessions(ctx, 50)
	meter.SetActiveSessions(ctx, 75)
}

func TestMeterWrapper_SetModuleCount(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.SetModuleCount(ctx, 5, 4)  // 5 total, 4 healthy
	meter.SetModuleCount(ctx, 6, 5)  // Updated counts
}

func TestMeterWrapper_RecordModuleHealth(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// Module becomes healthy
	meter.RecordModuleHealth(ctx, "user-management", true)
	
	// Module becomes unhealthy
	meter.RecordModuleHealth(ctx, "user-management", false)
}

func TestMeterWrapper_SetSystemMetrics(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.SetSystemMetrics(
		ctx,
		1024*1024*512, // 512 MB memory
		45.5,          // 45.5% CPU
		1000,          // 1000 goroutines
	)
}

func TestMeterWrapper_RecordError(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.RecordError(ctx, "http", "timeout")
	meter.RecordError(ctx, "database", "connection_failed")
	meter.RecordError(ctx, "auth", "invalid_token")
}

func TestMeterWrapper_RecordBusinessMetric(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	meter.RecordBusinessMetric(ctx, "user_registrations", 1)
	meter.RecordBusinessMetric(ctx, "login_attempts", 5)
}

func TestMeterWrapper_CreateCustomCounter(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	counter, err := meter.CreateCustomCounter(
		"custom_events_total",
		"Total number of custom events",
		"1",
	)
	require.NoError(t, err)
	assert.NotNil(t, counter)
}

func TestMeterWrapper_CreateCustomHistogram(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	histogram, err := meter.CreateCustomHistogram(
		"custom_duration_seconds",
		"Duration of custom operations",
		"s",
		[]float64{0.1, 0.5, 1.0, 2.0, 5.0},
	)
	require.NoError(t, err)
	assert.NotNil(t, histogram)
	
	// Test with nil boundaries
	histogram2, err := meter.CreateCustomHistogram(
		"custom_duration2_seconds",
		"Duration of custom operations 2",
		"s",
		nil,
	)
	require.NoError(t, err)
	assert.NotNil(t, histogram2)
}

func TestMeterWrapper_CreateCustomGauge(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	gauge, err := meter.CreateCustomGauge(
		"custom_queue_size",
		"Size of custom queue",
		"1",
	)
	require.NoError(t, err)
	assert.NotNil(t, gauge)
}

func TestMeterWrapper_HTTPMetricsIntegration(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// Simulate a series of HTTP requests
	requests := []struct {
		method       string
		path         string
		statusCode   int
		duration     time.Duration
		requestSize  int64
		responseSize int64
	}{
		{"GET", "/api/v1/users", 200, 50 * time.Millisecond, 0, 1024},
		{"POST", "/api/v1/users", 201, 100 * time.Millisecond, 512, 256},
		{"GET", "/api/v1/users/123", 404, 25 * time.Millisecond, 0, 64},
		{"PUT", "/api/v1/users/123", 500, 200 * time.Millisecond, 256, 128},
	}
	
	for _, req := range requests {
		meter.RecordHTTPRequest(
			ctx,
			req.method,
			req.path,
			req.statusCode,
			req.duration,
			req.requestSize,
			req.responseSize,
		)
	}
}

func TestMeterWrapper_DatabaseMetricsIntegration(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	ctx := context.Background()
	
	// Simulate database operations
	operations := []struct {
		operation string
		table     string
		duration  time.Duration
		success   bool
	}{
		{"SELECT", "users", 10 * time.Millisecond, true},
		{"INSERT", "users", 50 * time.Millisecond, true},
		{"UPDATE", "users", 30 * time.Millisecond, true},
		{"DELETE", "sessions", 25 * time.Millisecond, true},
		{"SELECT", "users", 100 * time.Millisecond, false}, // timeout
	}
	
	for _, op := range operations {
		meter.RecordDatabaseQuery(
			ctx,
			op.operation,
			op.table,
			op.duration,
			op.success,
		)
	}
}