package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MeterWrapper wraps OpenTelemetry meter with common metrics
type MeterWrapper struct {
	meter metric.Meter

	// HTTP metrics
	httpRequestCount    metric.Int64Counter
	httpRequestDuration metric.Float64Histogram
	httpRequestSize     metric.Int64Histogram
	httpResponseSize    metric.Int64Histogram

	// Database metrics
	dbConnectionCount   metric.Int64UpDownCounter
	dbQueryDuration     metric.Float64Histogram
	dbQueryCount        metric.Int64Counter

	// Application metrics
	activeUsers         metric.Int64UpDownCounter
	activeSessions      metric.Int64UpDownCounter
	moduleCount         metric.Int64UpDownCounter
	moduleHealthy       metric.Int64UpDownCounter

	// System metrics
	memoryUsage         metric.Int64UpDownCounter
	cpuUsage            metric.Float64UpDownCounter
	goroutineCount      metric.Int64UpDownCounter
}

// NewMeterWrapper creates a new meter wrapper with common metrics
func NewMeterWrapper(serviceName string) (*MeterWrapper, error) {
	meter := otel.Meter(serviceName)
	
	mw := &MeterWrapper{
		meter: meter,
	}

	if err := mw.initMetrics(); err != nil {
		return nil, err
	}

	return mw, nil
}

// initMetrics initializes all common metrics
func (m *MeterWrapper) initMetrics() error {
	var err error

	// HTTP metrics
	m.httpRequestCount, err = m.meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	m.httpRequestDuration, err = m.meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0),
	)
	if err != nil {
		return err
	}

	m.httpRequestSize, err = m.meter.Int64Histogram(
		"http_request_size_bytes",
		metric.WithDescription("Size of HTTP request in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000),
	)
	if err != nil {
		return err
	}

	m.httpResponseSize, err = m.meter.Int64Histogram(
		"http_response_size_bytes",
		metric.WithDescription("Size of HTTP response in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(100, 1000, 10000, 100000, 1000000),
	)
	if err != nil {
		return err
	}

	// Database metrics
	m.dbConnectionCount, err = m.meter.Int64UpDownCounter(
		"db_connections_active",
		metric.WithDescription("Number of active database connections"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	m.dbQueryDuration, err = m.meter.Float64Histogram(
		"db_query_duration_seconds",
		metric.WithDescription("Duration of database queries"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0),
	)
	if err != nil {
		return err
	}

	m.dbQueryCount, err = m.meter.Int64Counter(
		"db_queries_total",
		metric.WithDescription("Total number of database queries"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// Application metrics
	m.activeUsers, err = m.meter.Int64UpDownCounter(
		"active_users",
		metric.WithDescription("Number of active users"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	m.activeSessions, err = m.meter.Int64UpDownCounter(
		"active_sessions",
		metric.WithDescription("Number of active sessions"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	m.moduleCount, err = m.meter.Int64UpDownCounter(
		"modules_total",
		metric.WithDescription("Total number of registered modules"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	m.moduleHealthy, err = m.meter.Int64UpDownCounter(
		"modules_healthy",
		metric.WithDescription("Number of healthy modules"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	// System metrics
	m.memoryUsage, err = m.meter.Int64UpDownCounter(
		"memory_usage_bytes",
		metric.WithDescription("Current memory usage in bytes"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return err
	}

	m.cpuUsage, err = m.meter.Float64UpDownCounter(
		"cpu_usage_percent",
		metric.WithDescription("Current CPU usage percentage"),
		metric.WithUnit("%"),
	)
	if err != nil {
		return err
	}

	m.goroutineCount, err = m.meter.Int64UpDownCounter(
		"goroutines_total",
		metric.WithDescription("Number of goroutines"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return err
	}

	return nil
}

// RecordHTTPRequest records metrics for an HTTP request
func (m *MeterWrapper) RecordHTTPRequest(ctx context.Context, method, path string, statusCode int, duration time.Duration, requestSize, responseSize int64) {
	attrs := []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status_code", statusCode),
	}

	m.httpRequestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.httpRequestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
	
	if requestSize > 0 {
		m.httpRequestSize.Record(ctx, requestSize, metric.WithAttributes(attrs...))
	}
	
	if responseSize > 0 {
		m.httpResponseSize.Record(ctx, responseSize, metric.WithAttributes(attrs...))
	}
}

// RecordDatabaseQuery records metrics for a database query
func (m *MeterWrapper) RecordDatabaseQuery(ctx context.Context, operation, table string, duration time.Duration, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("table", table),
		attribute.Bool("success", success),
	}

	m.dbQueryCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.dbQueryDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
}

// SetDatabaseConnections sets the current number of active database connections
func (m *MeterWrapper) SetDatabaseConnections(ctx context.Context, count int64) {
	m.dbConnectionCount.Add(ctx, count, metric.WithAttributes())
}

// SetActiveUsers sets the current number of active users
func (m *MeterWrapper) SetActiveUsers(ctx context.Context, count int64) {
	m.activeUsers.Add(ctx, count, metric.WithAttributes())
}

// SetActiveSessions sets the current number of active sessions
func (m *MeterWrapper) SetActiveSessions(ctx context.Context, count int64) {
	m.activeSessions.Add(ctx, count, metric.WithAttributes())
}

// SetModuleCount sets the total number of registered modules
func (m *MeterWrapper) SetModuleCount(ctx context.Context, total, healthy int64) {
	m.moduleCount.Add(ctx, total, metric.WithAttributes())
	m.moduleHealthy.Add(ctx, healthy, metric.WithAttributes())
}

// RecordModuleHealth records module health change
func (m *MeterWrapper) RecordModuleHealth(ctx context.Context, moduleID string, healthy bool) {
	attrs := []attribute.KeyValue{
		attribute.String("module_id", moduleID),
	}

	var delta int64
	if healthy {
		delta = 1
	} else {
		delta = -1
	}

	m.moduleHealthy.Add(ctx, delta, metric.WithAttributes(attrs...))
}

// SetSystemMetrics sets current system resource usage
func (m *MeterWrapper) SetSystemMetrics(ctx context.Context, memoryBytes int64, cpuPercent float64, goroutines int64) {
	m.memoryUsage.Add(ctx, memoryBytes, metric.WithAttributes())
	m.cpuUsage.Add(ctx, cpuPercent, metric.WithAttributes())
	m.goroutineCount.Add(ctx, goroutines, metric.WithAttributes())
}

// RecordError records an error counter
func (m *MeterWrapper) RecordError(ctx context.Context, component, errorType string) {
	// Create an error counter dynamically if needed
	errorCounter, _ := m.meter.Int64Counter(
		"errors_total",
		metric.WithDescription("Total number of errors"),
		metric.WithUnit("1"),
	)

	attrs := []attribute.KeyValue{
		attribute.String("component", component),
		attribute.String("error_type", errorType),
	}

	errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordBusinessMetric records a custom business metric
func (m *MeterWrapper) RecordBusinessMetric(ctx context.Context, name string, value int64, attrs ...attribute.KeyValue) {
	// Create a counter dynamically for business metrics
	counter, _ := m.meter.Int64Counter(
		"business_"+name+"_total",
		metric.WithDescription("Business metric: "+name),
		metric.WithUnit("1"),
	)

	counter.Add(ctx, value, metric.WithAttributes(attrs...))
}

// CreateCustomCounter creates a custom counter metric
func (m *MeterWrapper) CreateCustomCounter(name, description, unit string) (metric.Int64Counter, error) {
	return m.meter.Int64Counter(name, metric.WithDescription(description), metric.WithUnit(unit))
}

// CreateCustomHistogram creates a custom histogram metric
func (m *MeterWrapper) CreateCustomHistogram(name, description, unit string, boundaries []float64) (metric.Float64Histogram, error) {
	opts := []metric.Float64HistogramOption{
		metric.WithDescription(description),
		metric.WithUnit(unit),
	}
	
	if len(boundaries) > 0 {
		opts = append(opts, metric.WithExplicitBucketBoundaries(boundaries...))
	}

	return m.meter.Float64Histogram(name, opts...)
}

// CreateCustomGauge creates a custom gauge (up/down counter) metric
func (m *MeterWrapper) CreateCustomGauge(name, description, unit string) (metric.Int64UpDownCounter, error) {
	return m.meter.Int64UpDownCounter(name, metric.WithDescription(description), metric.WithUnit(unit))
}