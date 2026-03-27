package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	
	assert.Equal(t, "aegion", cfg.ServiceName)
	assert.NotEmpty(t, cfg.ServiceVersion)
	assert.NotEmpty(t, cfg.Environment)
	assert.NotEmpty(t, cfg.InstanceID)
	assert.True(t, cfg.EnableTraces)
	assert.True(t, cfg.EnableMetrics)
	assert.True(t, cfg.EnableLogs)
	assert.Equal(t, 1.0, cfg.TraceSamplingRatio)
	assert.True(t, cfg.Insecure)
}

func TestProductionConfig(t *testing.T) {
	cfg := ProductionConfig()
	
	assert.Equal(t, "production", cfg.Environment)
	assert.Equal(t, 0.1, cfg.TraceSamplingRatio)
	assert.False(t, cfg.Insecure)
}

func TestConfigEnvironmentVariables(t *testing.T) {
	// Set test environment variables
	t.Setenv("AEGION_ENVIRONMENT", "test")
	t.Setenv("AEGION_INSTANCE_ID", "test-instance-123")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://test:4318/v1/traces")
	
	cfg := DefaultConfig()
	
	assert.Equal(t, "test", cfg.Environment)
	assert.Equal(t, "test-instance-123", cfg.InstanceID)
	assert.Equal(t, "http://test:4318/v1/traces", cfg.TracesEndpoint)
}

func TestProvider_NewProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableTraces = false  // Disable for testing
	cfg.EnableMetrics = false
	cfg.EnableLogs = false
	
	provider, err := NewProvider(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	
	assert.Equal(t, cfg, provider.config)
	assert.NotNil(t, provider.resource)
	
	// Test shutdown
	err = provider.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestProvider_NewProviderWithNilConfig(t *testing.T) {
	provider, err := NewProvider(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, provider)
	
	// Should use default config
	assert.NotNil(t, provider.config)
	assert.Equal(t, "aegion", provider.config.ServiceName)
	
	err = provider.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestProvider_ResourceAttributes(t *testing.T) {
	cfg := &Config{
		ServiceName:    "test-service",
		ServiceVersion: "v1.2.3",
		Environment:    "testing",
		InstanceID:     "test-instance",
		EnableTraces:   false,
		EnableMetrics:  false,
		EnableLogs:     false,
	}
	
	provider, err := NewProvider(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	
	// Check resource attributes
	attrs := provider.resource.Attributes()
	hasServiceName := false
	hasServiceVersion := false
	hasEnvironment := false
	hasInstanceID := false
	
	for _, attr := range attrs {
		switch attr.Key {
		case "service.name":
			hasServiceName = true
			assert.Equal(t, "test-service", attr.Value.AsString())
		case "service.version":
			hasServiceVersion = true
			assert.Equal(t, "v1.2.3", attr.Value.AsString())
		case "deployment.environment":
			hasEnvironment = true
			assert.Equal(t, "testing", attr.Value.AsString())
		case "service.instance.id":
			hasInstanceID = true
			assert.Equal(t, "test-instance", attr.Value.AsString())
		}
	}
	
	assert.True(t, hasServiceName, "Missing service.name attribute")
	assert.True(t, hasServiceVersion, "Missing service.version attribute") 
	assert.True(t, hasEnvironment, "Missing deployment.environment attribute")
	assert.True(t, hasInstanceID, "Missing service.instance.id attribute")
	
	err = provider.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestProvider_EnabledFlags(t *testing.T) {
	tests := []struct {
		name           string
		enableTraces   bool
		enableMetrics  bool
		enableLogs     bool
	}{
		{
			name:           "all enabled",
			enableTraces:   true,
			enableMetrics:  true,
			enableLogs:     true,
		},
		{
			name:           "traces only",
			enableTraces:   true,
			enableMetrics:  false,
			enableLogs:     false,
		},
		{
			name:           "metrics only",
			enableTraces:   false,
			enableMetrics:  true,
			enableLogs:     false,
		},
		{
			name:           "logs only",
			enableTraces:   false,
			enableMetrics:  false,
			enableLogs:     true,
		},
		{
			name:           "all disabled",
			enableTraces:   false,
			enableMetrics:  false,
			enableLogs:     false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.EnableTraces = tt.enableTraces
			cfg.EnableMetrics = tt.enableMetrics
			cfg.EnableLogs = tt.enableLogs
			
			provider, err := NewProvider(context.Background(), cfg)
			require.NoError(t, err)
			require.NotNil(t, provider)
			
			assert.Equal(t, tt.enableTraces, provider.IsTracingEnabled())
			assert.Equal(t, tt.enableMetrics, provider.IsMetricsEnabled())
			assert.Equal(t, tt.enableLogs, provider.IsLoggingEnabled())
			
			if tt.enableTraces {
				assert.NotNil(t, provider.Tracer)
			}
			
			if tt.enableMetrics {
				assert.NotNil(t, provider.Meter)
			}
			
			err = provider.Shutdown(context.Background())
			assert.NoError(t, err)
		})
	}
}

func TestProvider_ShutdownTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableTraces = false
	cfg.EnableMetrics = false
	cfg.EnableLogs = false
	
	provider, err := NewProvider(context.Background(), cfg)
	require.NoError(t, err)
	
	// Test shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	
	err = provider.Shutdown(ctx)
	// Should complete even with short timeout since no components are enabled
	assert.NoError(t, err)
}