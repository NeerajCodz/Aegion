package proxy

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// HealthStatus represents the health status of an upstream.
type HealthStatus int

const (
	HealthStatusUnknown HealthStatus = iota
	HealthStatusHealthy
	HealthStatusUnhealthy
)

// String returns the string representation of the health status.
func (hs HealthStatus) String() string {
	switch hs {
	case HealthStatusHealthy:
		return "healthy"
	case HealthStatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// HealthCheckerConfig configures a health checker.
type HealthCheckerConfig struct {
	// URL to check for health
	URL string

	// Interval between health checks
	Interval time.Duration

	// Timeout for health check requests
	Timeout time.Duration

	// Logger for health check events
	Logger zerolog.Logger

	// ExpectedStatus is the HTTP status code expected for a healthy response
	ExpectedStatus int

	// ExpectedBody is the expected response body (optional)
	ExpectedBody string

	// Method is the HTTP method to use (default: GET)
	Method string

	// Headers to send with health check requests
	Headers map[string]string
}

// HealthChecker performs periodic health checks on an upstream service.
type HealthChecker struct {
	config    HealthCheckerConfig
	client    *http.Client
	logger    zerolog.Logger
	
	status    HealthStatus
	lastCheck time.Time
	lastError error
	checkCount int64
	failureCount int64
	
	mutex   sync.RWMutex
	stop    chan struct{}
	stopped chan struct{}
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(config HealthCheckerConfig) *HealthChecker {
	// Set defaults
	if config.Interval == 0 {
		config.Interval = 30 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.ExpectedStatus == 0 {
		config.ExpectedStatus = http.StatusOK
	}
	if config.Method == "" {
		config.Method = "GET"
	}

	client := &http.Client{
		Timeout: config.Timeout,
		// Don't follow redirects for health checks
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HealthChecker{
		config:  config,
		client:  client,
		logger:  config.Logger.With().Str("component", "health-checker").Logger(),
		status:  HealthStatusUnknown,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Start begins health checking in a separate goroutine.
func (hc *HealthChecker) Start() {
	defer close(hc.stopped)
	
	ticker := time.NewTicker(hc.config.Interval)
	defer ticker.Stop()

	// Perform initial check immediately
	hc.performCheck()

	for {
		select {
		case <-hc.stop:
			hc.logger.Info().Str("url", hc.config.URL).Msg("stopping health checker")
			return
		case <-ticker.C:
			hc.performCheck()
		}
	}
}

// Stop stops the health checker.
func (hc *HealthChecker) Stop() {
	close(hc.stop)
	<-hc.stopped
}

// GetStatus returns the current health status.
func (hc *HealthChecker) GetStatus() HealthStatus {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()
	return hc.status
}

// GetMetrics returns health check metrics.
func (hc *HealthChecker) GetMetrics() HealthMetrics {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()

	successRate := float64(0)
	if hc.checkCount > 0 {
		successRate = float64(hc.checkCount-hc.failureCount) / float64(hc.checkCount)
	}

	return HealthMetrics{
		Status:       hc.status,
		LastCheck:    hc.lastCheck,
		LastError:    hc.lastError,
		CheckCount:   hc.checkCount,
		FailureCount: hc.failureCount,
		SuccessRate:  successRate,
	}
}

// performCheck performs a single health check.
func (hc *HealthChecker) performCheck() {
	now := time.Now()
	
	hc.mutex.Lock()
	hc.checkCount++
	hc.lastCheck = now
	hc.mutex.Unlock()

	// Create request
	ctx, cancel := context.WithTimeout(context.Background(), hc.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, hc.config.Method, hc.config.URL, nil)
	if err != nil {
		hc.recordFailure(err)
		return
	}

	// Add custom headers
	for key, value := range hc.config.Headers {
		req.Header.Set(key, value)
	}

	// Set user agent
	req.Header.Set("User-Agent", "Aegion-Proxy-HealthChecker/1.0")

	// Perform request
	resp, err := hc.client.Do(req)
	if err != nil {
		hc.recordFailure(err)
		return
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != hc.config.ExpectedStatus {
		hc.recordFailure(err)
		return
	}

	// TODO: Check response body if ExpectedBody is configured

	// Health check successful
	hc.recordSuccess()
}

// recordSuccess records a successful health check.
func (hc *HealthChecker) recordSuccess() {
	hc.mutex.Lock()
	previousStatus := hc.status
	hc.status = HealthStatusHealthy
	hc.lastError = nil
	hc.mutex.Unlock()

	// Log status change
	if previousStatus != HealthStatusHealthy {
		hc.logger.Info().
			Str("url", hc.config.URL).
			Str("previous_status", previousStatus.String()).
			Msg("upstream is now healthy")
	} else {
		hc.logger.Debug().
			Str("url", hc.config.URL).
			Msg("health check successful")
	}
}

// recordFailure records a failed health check.
func (hc *HealthChecker) recordFailure(err error) {
	hc.mutex.Lock()
	previousStatus := hc.status
	hc.status = HealthStatusUnhealthy
	hc.lastError = err
	hc.failureCount++
	hc.mutex.Unlock()

	// Log status change or failure
	if previousStatus != HealthStatusUnhealthy {
		hc.logger.Warn().
			Str("url", hc.config.URL).
			Err(err).
			Str("previous_status", previousStatus.String()).
			Msg("upstream is now unhealthy")
	} else {
		hc.logger.Debug().
			Str("url", hc.config.URL).
			Err(err).
			Msg("health check failed")
	}
}

// HealthMetrics holds health check metrics.
type HealthMetrics struct {
	Status       HealthStatus `json:"status"`
	LastCheck    time.Time    `json:"last_check"`
	LastError    error        `json:"last_error,omitempty"`
	CheckCount   int64        `json:"check_count"`
	FailureCount int64        `json:"failure_count"`
	SuccessRate  float64      `json:"success_rate"`
}

// IsHealthy returns true if the upstream is healthy.
func (hm HealthMetrics) IsHealthy() bool {
	return hm.Status == HealthStatusHealthy
}

// UpstreamHealth represents the overall health status of an upstream.
type UpstreamHealth struct {
	Name           string           `json:"name"`
	URL            string           `json:"url"`
	Health         HealthMetrics    `json:"health"`
	CircuitBreaker CircuitBreakerMetrics `json:"circuit_breaker,omitempty"`
}

// GetUpstreamHealth returns health information for all upstreams.
func (p *Proxy) GetUpstreamHealth() []UpstreamHealth {
	var health []UpstreamHealth

	p.healthMux.RLock()
	defer p.healthMux.RUnlock()

	for name, checker := range p.healthCheckers {
		upstream := p.config.Upstreams[name]
		
		uh := UpstreamHealth{
			Name:   name,
			URL:    upstream.URL,
			Health: checker.GetMetrics(),
		}

		// Add circuit breaker metrics if available
		if breaker, exists := p.breakers[name]; exists {
			uh.CircuitBreaker = breaker.GetMetrics()
		}

		health = append(health, uh)
	}

	return health
}

// IsUpstreamHealthy checks if a specific upstream is healthy.
func (p *Proxy) IsUpstreamHealthy(name string) bool {
	p.healthMux.RLock()
	checker, exists := p.healthCheckers[name]
	p.healthMux.RUnlock()

	if !exists {
		return true // Assume healthy if no health checker configured
	}

	return checker.GetStatus() == HealthStatusHealthy
}