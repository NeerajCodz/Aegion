package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/session"
)

var (
	ErrNoRuleMatched     = errors.New("no rule matched")
	ErrUpstreamNotFound  = errors.New("upstream not found")
	ErrUpstreamUnhealthy = errors.New("upstream is unhealthy")
	ErrRequestTimeout    = errors.New("request timeout")
)

// responseWriter is a wrapper that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Proxy represents the main API gateway proxy.
type Proxy struct {
	config      *Config
	transport   http.RoundTripper
	rules       *RuleEngine
	limiter     *RateLimiter
	breakers    map[string]*CircuitBreaker
	breakersMux sync.RWMutex
	logger      zerolog.Logger
	
	// Health checking
	healthCheckers map[string]*HealthChecker
	healthMux      sync.RWMutex
}

// NewProxy creates a new proxy instance.
func NewProxy(config Config, rules *RuleEngine, logger zerolog.Logger) *Proxy {
	if rules == nil {
		rules = NewRuleEngine([]Rule{})
	}

	// Create HTTP transport
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   config.Transport.DialTimeout,
			KeepAlive: config.Transport.KeepAlive,
		}).DialContext,
		MaxIdleConns:          config.Transport.MaxIdleConns,
		MaxIdleConnsPerHost:   config.Transport.MaxIdleConnsPerHost,
		IdleConnTimeout:       config.Transport.IdleConnTimeout,
		TLSHandshakeTimeout:   config.Transport.TLSHandshakeTimeout,
		ExpectContinueTimeout: config.Transport.ExpectContinueTimeout,
	}

	proxy := &Proxy{
		config:         &config,
		transport:      transport,
		rules:          rules,
		breakers:       make(map[string]*CircuitBreaker),
		logger:         logger.With().Str("component", "proxy").Logger(),
		healthCheckers: make(map[string]*HealthChecker),
	}

	// Initialize rate limiter if configured
	if config.EnableHealthChecks {
		globalRateLimit := DefaultRateLimitConfig()
		proxy.limiter = NewRateLimiter(*globalRateLimit, NewMemoryStore())
	}

	// Initialize circuit breakers for each upstream
	for name, upstream := range config.Upstreams {
		cbConfig := DefaultCircuitBreakerConfig()
		if upstream.CircuitBreaker != nil {
			cbConfig = upstream.CircuitBreaker
		}
		proxy.breakers[name] = NewCircuitBreaker(*cbConfig)

		// Initialize health checker if enabled
		if config.EnableHealthChecks {
			proxy.healthCheckers[name] = NewHealthChecker(HealthCheckerConfig{
				URL:      upstream.URL + upstream.HealthCheck,
				Interval: config.HealthCheckInterval,
				Timeout:  5 * time.Second,
				Logger:   logger,
			})
		}
	}

	// Start health checkers
	if config.EnableHealthChecks {
		proxy.startHealthCheckers()
	}

	return proxy
}

// ServeHTTP implements http.Handler interface.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := p.getOrCreateRequestID(r)
	start := time.Now()

	// Add request ID to context and response headers
	ctx = context.WithValue(ctx, "request_id", requestID)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	p.logger.Debug().
		Str("request_id", requestID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Msg("received request")

	// Apply request timeout
	if p.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()
		r = r.WithContext(ctx)
	}

	// Find matching rule
	rule, matched := p.rules.Match(r)
	if !matched {
		p.handleError(w, r, ErrNoRuleMatched, http.StatusNotFound, start)
		return
	}

	// Check rate limiting
	if p.limiter != nil {
		// Use rule-specific rate limit if available
		if rule.RateLimit != nil {
			limiter := NewRateLimiter(*rule.RateLimit, NewMemoryStore())
			if allowed, waitTime, err := limiter.Allow(r); !allowed {
				p.handleRateLimitExceeded(w, r, waitTime, err, start)
				return
			}
		} else {
			// Use global rate limiter
			if allowed, waitTime, err := p.limiter.Allow(r); !allowed {
				p.handleRateLimitExceeded(w, r, waitTime, err, start)
				return
			}
		}
	}

	// Check access control
	sess := session.FromContext(ctx)
	if err := p.rules.CheckAccess(r, rule, sess); err != nil {
		p.handleAccessError(w, r, err, start)
		return
	}

	// Get target upstream
	upstream, exists := p.config.Upstreams[rule.Target]
	if !exists {
		p.handleError(w, r, ErrUpstreamNotFound, http.StatusBadGateway, start)
		return
	}

	// Check circuit breaker
	breaker := p.getCircuitBreaker(rule.Target)
	if !breaker.Allow() {
		p.handleError(w, r, ErrUpstreamUnhealthy, http.StatusServiceUnavailable, start)
		return
	}

	// Parse upstream URL
	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		breaker.RecordFailure()
		p.handleError(w, r, fmt.Errorf("invalid upstream URL: %w", err), http.StatusInternalServerError, start)
		return
	}

	// Forward request
	if err := p.Forward(targetURL, w, r, rule, upstream); err != nil {
		breaker.RecordFailure()
		p.handleProxyError(w, r, err, start)
		return
	}

	// Record success
	breaker.RecordSuccess()

	p.logger.Debug().
		Str("request_id", requestID).
		Str("rule_id", rule.ID).
		Str("target", rule.Target).
		Dur("duration", time.Since(start)).
		Msg("request completed successfully")
}

// Forward proxies the request to the specified target URL.
func (p *Proxy) Forward(target *url.URL, w http.ResponseWriter, r *http.Request, rule *Rule, upstream Upstream) error {
	// Apply path rewriting
	originalPath := r.URL.Path
	if rule.Rewrite != nil {
		r.URL.Path = rule.ApplyRewrite(r.URL.Path)
	}

	// Create a response writer that captures the status code
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Set target URL
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			// Preserve query parameters
			req.URL.RawQuery = r.URL.RawQuery

			// Add custom headers from rule
			for key, value := range rule.Headers {
				req.Header.Set(key, value)
			}

			// Add custom headers from upstream
			for key, value := range upstream.Headers {
				req.Header.Set(key, value)
			}

			// Inject session headers for authenticated requests
			if sess := session.FromContext(req.Context()); sess != nil {
				p.injectSessionHeaders(req, sess)
			}

			// Add forwarded headers
			p.addForwardedHeaders(req, r)

			// Preserve request ID
			if requestID := req.Context().Value("request_id"); requestID != nil {
				req.Header.Set("X-Request-ID", requestID.(string))
			}

			p.logger.Debug().
				Str("request_id", getRequestIDFromContext(req.Context())).
				Str("original_path", originalPath).
				Str("rewritten_path", req.URL.Path).
				Str("target_url", req.URL.String()).
				Msg("forwarding request")
		},
		Transport: p.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Transport errors or timeouts will be handled here
			if r.Context().Err() == context.DeadlineExceeded {
				rw.statusCode = http.StatusGatewayTimeout
			} else {
				rw.statusCode = http.StatusBadGateway
			}
		},
	}

	proxy.ServeHTTP(rw, r)

	// Check if the context was cancelled (timeout)
	if r.Context().Err() == context.DeadlineExceeded {
		return fmt.Errorf("request timeout")
	}

	// Check if the response indicates failure
	if rw.statusCode >= 500 {
		return fmt.Errorf("upstream error: status %d", rw.statusCode)
	}

	return nil
}

// getCircuitBreaker gets or creates a circuit breaker for the given upstream.
func (p *Proxy) getCircuitBreaker(upstreamName string) *CircuitBreaker {
	p.breakersMux.RLock()
	breaker, exists := p.breakers[upstreamName]
	p.breakersMux.RUnlock()

	if !exists {
		p.breakersMux.Lock()
		// Double-check after acquiring write lock
		if breaker, exists = p.breakers[upstreamName]; !exists {
			config := DefaultCircuitBreakerConfig()
			if upstream, ok := p.config.Upstreams[upstreamName]; ok && upstream.CircuitBreaker != nil {
				config = upstream.CircuitBreaker
			}
			breaker = NewCircuitBreaker(*config)
			p.breakers[upstreamName] = breaker
		}
		p.breakersMux.Unlock()
	}

	return breaker
}

// injectSessionHeaders adds session information to the request headers.
func (p *Proxy) injectSessionHeaders(req *http.Request, sess *session.Session) {
	req.Header.Set("X-Aegion-Session-ID", sess.ID.String())
	req.Header.Set("X-Aegion-Identity-ID", sess.IdentityID.String())
	req.Header.Set("X-Aegion-AAL", string(sess.AAL))
	req.Header.Set("X-Aegion-Authenticated-At", sess.AuthenticatedAt.Format(time.RFC3339))
	
	if sess.IsImpersonation && sess.ImpersonatorID != nil {
		req.Header.Set("X-Aegion-Impersonation", "true")
		req.Header.Set("X-Aegion-Impersonator-ID", sess.ImpersonatorID.String())
	}
}

// addForwardedHeaders adds standard forwarded headers.
func (p *Proxy) addForwardedHeaders(req, original *http.Request) {
	clientIP := getClientIP(original)
	
	// X-Forwarded-For
	if prior := original.Header.Get("X-Forwarded-For"); prior != "" {
		req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	// X-Forwarded-Proto
	if proto := original.Header.Get("X-Forwarded-Proto"); proto != "" {
		req.Header.Set("X-Forwarded-Proto", proto)
	} else if original.TLS != nil {
		req.Header.Set("X-Forwarded-Proto", "https")
	} else {
		req.Header.Set("X-Forwarded-Proto", "http")
	}

	// X-Forwarded-Host
	if host := original.Header.Get("X-Forwarded-Host"); host != "" {
		req.Header.Set("X-Forwarded-Host", host)
	} else {
		req.Header.Set("X-Forwarded-Host", original.Host)
	}
}

// startHealthCheckers starts health checking for all configured upstreams.
func (p *Proxy) startHealthCheckers() {
	p.healthMux.Lock()
	defer p.healthMux.Unlock()

	for name, checker := range p.healthCheckers {
		go func(name string, checker *HealthChecker) {
			p.logger.Info().Str("upstream", name).Msg("starting health checker")
			checker.Start()
		}(name, checker)
	}
}

// getOrCreateRequestID gets the request ID from headers or creates a new one.
func (p *Proxy) getOrCreateRequestID(r *http.Request) string {
	// Check if request ID already exists
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}

	// Generate new request ID
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Error handling methods

func (p *Proxy) handleError(w http.ResponseWriter, r *http.Request, err error, statusCode int, start time.Time) {
	requestID := getRequestIDFromContext(r.Context())
	duration := time.Since(start)

	p.logger.Error().
		Str("request_id", requestID).
		Err(err).
		Int("status", statusCode).
		Dur("duration", duration).
		Msg("proxy error")

	p.writeErrorResponse(w, statusCode, err.Error(), requestID)
}

func (p *Proxy) handleRateLimitExceeded(w http.ResponseWriter, r *http.Request, waitTime time.Duration, err error, start time.Time) {
	requestID := getRequestIDFromContext(r.Context())
	
	// Add rate limit headers
	w.Header().Set("X-RateLimit-Limit", "100") // This should come from config
	w.Header().Set("X-RateLimit-Remaining", "0")
	w.Header().Set("Retry-After", strconv.Itoa(int(waitTime.Seconds())))

	p.logger.Warn().
		Str("request_id", requestID).
		Dur("wait_time", waitTime).
		Dur("duration", time.Since(start)).
		Msg("rate limit exceeded")

	p.writeErrorResponse(w, http.StatusTooManyRequests, "Rate limit exceeded", requestID)
}

func (p *Proxy) handleAccessError(w http.ResponseWriter, r *http.Request, err error, start time.Time) {
	requestID := getRequestIDFromContext(r.Context())
	var statusCode int
	var message string

	switch err {
	case ErrAuthenticationRequired:
		statusCode = http.StatusUnauthorized
		message = "Authentication required"
	case ErrInsufficientPrivileges:
		statusCode = http.StatusForbidden
		message = "Insufficient privileges"
	default:
		statusCode = http.StatusForbidden
		message = "Access denied"
	}

	p.logger.Warn().
		Str("request_id", requestID).
		Err(err).
		Int("status", statusCode).
		Dur("duration", time.Since(start)).
		Msg("access denied")

	p.writeErrorResponse(w, statusCode, message, requestID)
}

func (p *Proxy) handleProxyError(w http.ResponseWriter, r *http.Request, err error, start time.Time) {
	requestID := getRequestIDFromContext(r.Context())
	statusCode := http.StatusBadGateway
	message := "Upstream error"

	if r.Context().Err() == context.DeadlineExceeded {
		statusCode = http.StatusGatewayTimeout
		message = "Request timeout"
	}

	p.logger.Error().
		Str("request_id", requestID).
		Err(err).
		Int("status", statusCode).
		Dur("duration", time.Since(start)).
		Msg("proxy error")

	p.writeErrorResponse(w, statusCode, message, requestID)
}

func (p *Proxy) writeErrorResponse(w http.ResponseWriter, statusCode int, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := fmt.Sprintf(
		`{"error":{"code":%d,"message":"%s","request_id":"%s"}}`,
		statusCode, message, requestID,
	)
	w.Write([]byte(response))
}