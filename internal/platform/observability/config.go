package observability

import (
	"os"
	"time"
)

// Config contains configuration for OpenTelemetry observability
type Config struct {
	// Service name for resource attributes
	ServiceName string `yaml:"service_name"`
	
	// Service version for resource attributes
	ServiceVersion string `yaml:"service_version"`
	
	// Deployment environment (e.g., "development", "staging", "production")
	Environment string `yaml:"environment"`
	
	// Service instance ID (defaults to hostname if not set)
	InstanceID string `yaml:"instance_id"`
	
	// OTLP endpoints
	TracesEndpoint  string `yaml:"traces_endpoint"`
	MetricsEndpoint string `yaml:"metrics_endpoint"`
	LogsEndpoint    string `yaml:"logs_endpoint"`
	
	// Headers for authentication (e.g., API keys)
	Headers map[string]string `yaml:"headers"`
	
	// Sampling configuration
	TraceSamplingRatio float64 `yaml:"trace_sampling_ratio"`
	
	// Export intervals
	MetricExportInterval time.Duration `yaml:"metric_export_interval"`
	TraceExportTimeout   time.Duration `yaml:"trace_export_timeout"`
	
	// Insecure connection (for development)
	Insecure bool `yaml:"insecure"`
	
	// Enable/disable telemetry components
	EnableTraces  bool `yaml:"enable_traces"`
	EnableMetrics bool `yaml:"enable_metrics"`
	EnableLogs    bool `yaml:"enable_logs"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	
	return &Config{
		ServiceName:          "aegion",
		ServiceVersion:       "v1.0.0",
		Environment:          getEnvOrDefault("AEGION_ENVIRONMENT", "development"),
		InstanceID:           getEnvOrDefault("AEGION_INSTANCE_ID", hostname),
		TracesEndpoint:       getEnvOrDefault("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318"),
		MetricsEndpoint:      getEnvOrDefault("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://localhost:4318"),
		LogsEndpoint:         getEnvOrDefault("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://localhost:4318"),
		Headers:              make(map[string]string),
		TraceSamplingRatio:   1.0, // 100% sampling for development
		MetricExportInterval: 30 * time.Second,
		TraceExportTimeout:   10 * time.Second,
		Insecure:             true, // Default to insecure for development
		EnableTraces:         true,
		EnableMetrics:        true,
		EnableLogs:           true,
	}
}

// ProductionConfig returns a configuration optimized for production
func ProductionConfig() *Config {
	cfg := DefaultConfig()
	cfg.Environment = "production"
	cfg.TraceSamplingRatio = 0.1 // 10% sampling for production
	cfg.Insecure = false
	return cfg
}

// getEnvOrDefault returns environment variable value or default if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}