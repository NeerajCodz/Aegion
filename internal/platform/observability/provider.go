package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider manages OpenTelemetry providers and handles graceful shutdown
type Provider struct {
	config *Config

	// Resource attributes
	resource *resource.Resource

	// Providers
	traceProvider  *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *log.LoggerProvider

	// Public interfaces
	Tracer trace.Tracer
	Meter  metric.Meter

	// Shutdown functions
	shutdownFuncs []func(context.Context) error
}

// NewProvider creates a new OpenTelemetry provider with the given configuration
func NewProvider(ctx context.Context, config *Config) (*Provider, error) {
	if config == nil {
		config = DefaultConfig()
	}

	provider := &Provider{
		config:        config,
		shutdownFuncs: make([]func(context.Context) error, 0),
	}

	// Create resource with service attributes
	var err error
	provider.resource, err = provider.createResource()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Initialize tracing if enabled
	if config.EnableTraces {
		if err := provider.initTracing(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize tracing: %w", err)
		}
	}

	// Initialize metrics if enabled
	if config.EnableMetrics {
		if err := provider.initMetrics(ctx); err != nil {
			provider.Shutdown(ctx) // Clean up what we've created so far
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
	}

	// Initialize logging if enabled
	if config.EnableLogs {
		if err := provider.initLogging(ctx); err != nil {
			provider.Shutdown(ctx) // Clean up what we've created so far
			return nil, fmt.Errorf("failed to initialize logging: %w", err)
		}
	}

	// Set up global propagators for trace context
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider, nil
}

// createResource creates the OpenTelemetry resource with service attributes
func (p *Provider) createResource() (*resource.Resource, error) {
	return resource.NewWithAttributes(
		"",
		attribute.String("service.name", p.config.ServiceName),
		attribute.String("service.version", p.config.ServiceVersion),
		attribute.String("deployment.environment", p.config.Environment),
		attribute.String("service.instance.id", p.config.InstanceID),
	), nil
}

// initTracing initializes the trace provider and exporter
func (p *Provider) initTracing(ctx context.Context) error {
	// Create OTLP HTTP trace exporter
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(p.config.TracesEndpoint),
		otlptracehttp.WithTimeout(p.config.TraceExportTimeout),
	}

	// Add headers if configured
	if len(p.config.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(p.config.Headers))
	}

	// Use insecure connection if configured
	if p.config.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	traceExporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create trace provider with batch processor and sampling
	p.traceProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(p.resource),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(p.config.TraceSamplingRatio)),
	)

	// Set global trace provider
	otel.SetTracerProvider(p.traceProvider)

	// Create tracer for this service
	p.Tracer = p.traceProvider.Tracer(
		p.config.ServiceName,
		trace.WithInstrumentationVersion(p.config.ServiceVersion),
	)

	// Add shutdown function
	p.shutdownFuncs = append(p.shutdownFuncs, p.traceProvider.Shutdown)

	return nil
}

// initMetrics initializes the meter provider and exporter
func (p *Provider) initMetrics(ctx context.Context) error {
	// Create OTLP HTTP metric exporter
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(p.config.MetricsEndpoint),
	}

	// Add headers if configured
	if len(p.config.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(p.config.Headers))
	}

	// Use insecure connection if configured
	if p.config.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	metricExporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Create meter provider with periodic reader
	p.meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(p.config.MetricExportInterval),
		)),
		sdkmetric.WithResource(p.resource),
	)

	// Set global meter provider
	otel.SetMeterProvider(p.meterProvider)

	// Create meter for this service
	p.Meter = p.meterProvider.Meter(
		p.config.ServiceName,
		metric.WithInstrumentationVersion(p.config.ServiceVersion),
	)

	// Add shutdown function
	p.shutdownFuncs = append(p.shutdownFuncs, p.meterProvider.Shutdown)

	return nil
}

// initLogging initializes the logger provider and exporter
func (p *Provider) initLogging(ctx context.Context) error {
	// Create OTLP HTTP log exporter
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(p.config.LogsEndpoint),
	}

	// Add headers if configured
	if len(p.config.Headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(p.config.Headers))
	}

	// Use insecure connection if configured
	if p.config.Insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	logExporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create log exporter: %w", err)
	}

	// Create logger provider with batch processor
	p.loggerProvider = log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithResource(p.resource),
	)

	// Set global logger provider
	global.SetLoggerProvider(p.loggerProvider)

	// Add shutdown function
	p.shutdownFuncs = append(p.shutdownFuncs, p.loggerProvider.Shutdown)

	return nil
}

// Shutdown gracefully shuts down all OpenTelemetry providers
func (p *Provider) Shutdown(ctx context.Context) error {
	// Create a context with timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var lastErr error
	for _, shutdown := range p.shutdownFuncs {
		if err := shutdown(shutdownCtx); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// IsTracingEnabled returns whether tracing is enabled
func (p *Provider) IsTracingEnabled() bool {
	return p.config.EnableTraces
}

// IsMetricsEnabled returns whether metrics are enabled
func (p *Provider) IsMetricsEnabled() bool {
	return p.config.EnableMetrics
}

// IsLoggingEnabled returns whether logging is enabled
func (p *Provider) IsLoggingEnabled() bool {
	return p.config.EnableLogs
}