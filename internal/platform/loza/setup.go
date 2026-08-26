// Package loza configures Aegion's process-wide Loza event pipeline.
package loza

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/config"
	lozasdk "github.com/astraive/loza/sdks/go"
)

var processState struct {
	sync.Mutex
	configured bool
}

// Initialize configures Loza once for a process and returns the configured logger
// plus a shutdown function that drains the SDK within the supplied context.
func Initialize(cfg config.LozaConfig, service, version string) (*lozasdk.Logger, func(context.Context) error, error) {
	processState.Lock()
	defer processState.Unlock()

	if processState.configured {
		return lozasdk.Default(), func(ctx context.Context) error { return lozasdk.Flush(ctx) }, nil
	}

	endpoint := strings.TrimSpace(firstNonEmpty(os.Getenv("AEGION_LOZA_COLLECTOR_URL"), cfg.CollectorURL))
	if endpoint == "" {
		return nil, nil, fmt.Errorf("loza collector endpoint is required")
	}
	environment := firstNonEmpty(cfg.Environment, os.Getenv("AEGION_ENV"), "development")
	apiKey, err := readAPIKey()
	if err != nil {
		return nil, nil, err
	}
	if isProduction(environment) {
		if apiKey == "" {
			return nil, nil, fmt.Errorf("AEGION_LOZA_API_KEY or AEGION_LOZA_API_KEY_FILE is required in production")
		}
		if err := validateSecureEndpoint(endpoint, cfg.Insecure); err != nil {
			return nil, nil, err
		}
	}

	collector, err := lozasdk.CollectorSink(lozasdk.CollectorSinkConfig{
		Endpoint:          endpoint,
		Headers:           authorizationHeader(apiKey),
		Insecure:          cfg.Insecure,
		MaxRetries:        cfg.MaxRetries,
		MaxBackoff:        time.Duration(cfg.MaxBackoff),
		Timeout:           time.Duration(cfg.Timeout),
		ConnectionTimeout: time.Duration(cfg.ConnectionTimeout),
		SDKName:           "aegion",
		SDKVersion:        version,
		Service:           service,
		EnableCompression: cfg.EnableCompression,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("creating Loza collector sink: %w", err)
	}

	base := lozasdk.Production()
	base.Service = service
	base.Alias = firstNonEmpty(cfg.CollectorName, "aegion")
	base.Version = version
	base.Environment = environment
	base.CollectorURL = endpoint
	base.CollectorName = base.Alias
	base.APIKey = ""
	base.Sink = collector
	base.Sinks = nil
	base.Async.Enabled = true
	base.Async.QueueSize = firstPositive(cfg.QueueSize, 8192)
	base.Async.Workers = 4
	base.Async.FlushInterval = time.Duration(cfg.FlushInterval)
	base.Async.Backpressure = lozasdk.Block
	base.BatchSize = firstPositive(cfg.BatchSize, 100)
	base.FlushInterval = time.Duration(cfg.FlushInterval)
	base.MaxRetries = cfg.MaxRetries
	base.MaxBackoff = time.Duration(cfg.MaxBackoff)
	base.Timeout = time.Duration(cfg.Timeout)
	base.ConnectionTimeout = time.Duration(cfg.ConnectionTimeout)
	base.EnableCompression = cfg.EnableCompression
	base.Redactor = lozasdk.ComposeRedactors(lozasdk.DefaultRedactor(), lozasdk.Redact(cfg.RedactFields...))
	base.OTelBridge = true

	logger, err := lozasdk.New(base)
	if err != nil {
		_ = collector.Close(context.Background())
		return nil, nil, fmt.Errorf("creating Loza logger: %w", err)
	}
	lozasdk.SetDefault(logger)
	processState.configured = true

	return logger, func(ctx context.Context) error {
		return logger.Shutdown(ctx)
	}, nil
}

func readAPIKey() (string, error) {
	path := strings.TrimSpace(os.Getenv("AEGION_LOZA_API_KEY_FILE"))
	if path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- deployment-owned secret-file path.
		if err != nil {
			return "", fmt.Errorf("reading AEGION_LOZA_API_KEY_FILE: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("AEGION_LOZA_API_KEY_FILE is empty")
		}
		return key, nil
	}
	return strings.TrimSpace(os.Getenv("AEGION_LOZA_API_KEY")), nil
}

func validateSecureEndpoint(endpoint string, insecure bool) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("loza collector URL must be an absolute URL")
	}
	if strings.EqualFold(parsed.Scheme, "http") && !insecure {
		return fmt.Errorf("production Loza collector URL must use HTTPS")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("loza collector URL must use HTTP or HTTPS")
	}
	return nil
}

func authorizationHeader(apiKey string) map[string]string {
	if apiKey == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + apiKey}
}

func isProduction(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "production", "prod":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstPositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
