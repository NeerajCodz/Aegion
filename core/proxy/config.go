package proxy

import (
	"time"
)

// Config holds the proxy configuration.
type Config struct {
	// Upstreams maps upstream names to their configurations
	Upstreams map[string]Upstream `json:"upstreams" yaml:"upstreams"`

	// DefaultTarget is the fallback upstream when no rule matches
	DefaultTarget string `json:"default_target" yaml:"default_target"`

	// Timeout for upstream requests
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// MaxRetries for failed upstream requests
	MaxRetries int `json:"max_retries" yaml:"max_retries"`

	// RetryBackoff delay between retries
	RetryBackoff time.Duration `json:"retry_backoff" yaml:"retry_backoff"`

	// EnableHealthChecks whether to perform health checks
	EnableHealthChecks bool `json:"enable_health_checks" yaml:"enable_health_checks"`

	// HealthCheckInterval how often to check upstream health
	HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`

	// Transport settings for HTTP client
	Transport TransportConfig `json:"transport" yaml:"transport"`
}

// Upstream defines a backend service configuration.
type Upstream struct {
	// URL is the upstream service endpoint
	URL string `json:"url" yaml:"url"`

	// HealthCheck endpoint path for health checks
	HealthCheck string `json:"health_check" yaml:"health_check"`

	// Weight for load balancing (not implemented yet, future extension)
	Weight int `json:"weight" yaml:"weight"`

	// CircuitBreaker configuration
	CircuitBreaker *CircuitBreakerConfig `json:"circuit_breaker,omitempty" yaml:"circuit_breaker,omitempty"`

	// Timeout override for this specific upstream
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// MaxConnections limit for this upstream
	MaxConnections int `json:"max_connections" yaml:"max_connections"`

	// Headers to add to requests to this upstream
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// TransportConfig holds HTTP transport settings.
type TransportConfig struct {
	// MaxIdleConns controls the maximum number of idle (keep-alive) connections
	MaxIdleConns int `json:"max_idle_conns" yaml:"max_idle_conns"`

	// MaxIdleConnsPerHost controls the maximum idle connections per-host
	MaxIdleConnsPerHost int `json:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host"`

	// IdleConnTimeout is the maximum amount of time an idle connection will remain idle
	IdleConnTimeout time.Duration `json:"idle_conn_timeout" yaml:"idle_conn_timeout"`

	// TLSHandshakeTimeout specifies the maximum amount of time waiting to wait for a TLS handshake
	TLSHandshakeTimeout time.Duration `json:"tls_handshake_timeout" yaml:"tls_handshake_timeout"`

	// ExpectContinueTimeout specifies the amount of time to wait for a server's first response headers
	ExpectContinueTimeout time.Duration `json:"expect_continue_timeout" yaml:"expect_continue_timeout"`

	// DialTimeout controls how long to wait for a dial to complete
	DialTimeout time.Duration `json:"dial_timeout" yaml:"dial_timeout"`

	// KeepAlive specifies the interval between keep-alive probes for an active network connection
	KeepAlive time.Duration `json:"keep_alive" yaml:"keep_alive"`
}

// CircuitBreakerConfig defines circuit breaker parameters.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures required to open the circuit
	FailureThreshold int `json:"failure_threshold" yaml:"failure_threshold"`

	// Timeout is how long to wait before attempting to close the circuit
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// SuccessThreshold is the number of successes required to close the circuit
	SuccessThreshold int `json:"success_threshold" yaml:"success_threshold"`
}

// RateLimitConfig defines rate limiting parameters.
type RateLimitConfig struct {
	// RequestsPerSecond allowed
	RequestsPerSecond int `json:"requests_per_second" yaml:"requests_per_second"`

	// BurstSize for token bucket algorithm
	BurstSize int `json:"burst_size" yaml:"burst_size"`

	// ByIP enables per-IP rate limiting
	ByIP bool `json:"by_ip" yaml:"by_ip"`

	// ByUser enables per-user rate limiting
	ByUser bool `json:"by_user" yaml:"by_user"`

	// ByPath enables per-path rate limiting
	ByPath bool `json:"by_path" yaml:"by_path"`
}

// DefaultConfig returns sensible proxy defaults.
func DefaultConfig() Config {
	return Config{
		Upstreams:               make(map[string]Upstream),
		Timeout:                 30 * time.Second,
		MaxRetries:              3,
		RetryBackoff:            time.Second,
		EnableHealthChecks:      true,
		HealthCheckInterval:     30 * time.Second,
		Transport: TransportConfig{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			DialTimeout:           5 * time.Second,
			KeepAlive:             30 * time.Second,
		},
	}
}

// DefaultCircuitBreakerConfig returns default circuit breaker settings.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold: 5,
		Timeout:          60 * time.Second,
		SuccessThreshold: 3,
	}
}

// DefaultRateLimitConfig returns default rate limiting settings.
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         200,
		ByIP:              true,
		ByUser:            false,
		ByPath:            false,
	}
}