package proxy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/core/session"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/trustedproxy"
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
	config          *Config
	transport       http.RoundTripper
	rules           *RuleEngine
	limiter         *RateLimiter
	ruleLimiters    map[string]*RateLimiter
	ruleLimitersMux sync.RWMutex
	breakers        map[string]*CircuitBreaker
	breakersMux     sync.RWMutex
	logger          *slog.Logger

	// Health checking
	healthCheckers map[string]*HealthChecker
	healthMux      sync.RWMutex
}

// NewProxy creates a new proxy instance.
func NewProxy(config Config, rules *RuleEngine, logger *slog.Logger) *Proxy {
	if rules == nil {
		rules = NewRuleEngine([]Rule{})
	}

	if logger == nil {
		logger = slog.Default()
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
		ruleLimiters:   make(map[string]*RateLimiter),
		breakers:       make(map[string]*CircuitBreaker),
		logger:         logger.With("component", "proxy"),
		healthCheckers: make(map[string]*HealthChecker),
	}

	// Initialize global rate limiter.
	globalRateLimit := DefaultRateLimitConfig()
	globalRateLimit.TrustForwardedHeaders = config.TrustForwardedHeaders
	proxy.limiter = NewRateLimiter(*globalRateLimit, NewMemoryStore())

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
				URL:            upstream.URL + upstream.HealthCheck,
				Interval:       config.HealthCheckInterval,
				Timeout:        config.Transport.DialTimeout,
				Logger:         logger,
				ExpectedBody:   upstream.HealthCheckExpectedBody,
				ExpectedStatus: http.StatusOK,
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
	ctx = withRequestID(ctx, requestID)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", requestID)

	p.logger.DebugContext(ctx, "received request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
	)

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
	if rule.RateLimit != nil {
		ruleLimiter := p.getRuleLimiter(rule)
		if allowed, waitTime, err := ruleLimiter.Allow(r); !allowed {
			p.handleRateLimitExceeded(w, r, waitTime, err, start)
			return
		}
	} else if p.limiter != nil {
		if allowed, waitTime, err := p.limiter.Allow(r); !allowed {
			p.handleRateLimitExceeded(w, r, waitTime, err, start)
			return
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

	p.logger.DebugContext(ctx, "request completed successfully",
		"rule_id", rule.ID,
		"target", rule.Target,
		"duration", time.Since(start),
	)
}

// Forward proxies the request to the specified target URL.
func (p *Proxy) Forward(target *url.URL, w http.ResponseWriter, r *http.Request, rule *Rule, upstream Upstream) error {
	// Apply path rewriting
	originalPath := r.URL.Path
	if rule.Rewrite != nil {
		r.URL.Path = rule.ApplyRewrite(r.URL.Path)
	}

	// Create reverse proxy
	var responseStatus int
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Set target URL
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if p.config.StripInboundIdentityHeaders {
				p.stripInboundIdentityHeaders(req)
			}

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
				p.signIdentityHeaders(req)
			}

			// Add forwarded headers
			p.addForwardedHeaders(req, r)

			// Preserve request ID
			if requestID := getRequestIDFromContext(req.Context()); requestID != "" {
				req.Header.Set("X-Request-ID", requestID)
			}

			p.logger.DebugContext(req.Context(), "forwarding request",
				"original_path", originalPath,
				"rewritten_path", req.URL.Path,
				"target_url", req.URL.String(),
			)
		},
		Transport: p.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Transport errors or timeouts will be handled here
			if r.Context().Err() == context.DeadlineExceeded {
				responseStatus = http.StatusGatewayTimeout
			} else {
				responseStatus = http.StatusBadGateway
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			responseStatus = resp.StatusCode
			return nil
		},
	}

	proxy.ServeHTTP(w, r)

	// Check if the context was cancelled (timeout)
	if r.Context().Err() == context.DeadlineExceeded {
		return fmt.Errorf("request timeout")
	}

	// Check if the response indicates failure
	if responseStatus >= 500 {
		return fmt.Errorf("upstream error: status %d", responseStatus)
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

// getRuleLimiter gets or creates a stable limiter for a rule.
func (p *Proxy) getRuleLimiter(rule *Rule) *RateLimiter {
	if rule == nil || rule.RateLimit == nil {
		return nil
	}

	key := rule.ID
	if key == "" {
		key = rule.Path + "|" + rule.Target + "|" + strconv.Itoa(rule.Priority)
	}

	p.ruleLimitersMux.RLock()
	limiter, exists := p.ruleLimiters[key]
	p.ruleLimitersMux.RUnlock()
	if exists {
		return limiter
	}

	p.ruleLimitersMux.Lock()
	defer p.ruleLimitersMux.Unlock()

	if limiter, exists = p.ruleLimiters[key]; exists {
		return limiter
	}

	ruleRateLimit := *rule.RateLimit
	if p.config.TrustForwardedHeaders {
		ruleRateLimit.TrustForwardedHeaders = true
	}
	limiter = NewRateLimiter(ruleRateLimit, NewMemoryStore())
	p.ruleLimiters[key] = limiter
	return limiter
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

func (p *Proxy) stripInboundIdentityHeaders(req *http.Request) {
	for _, header := range p.trustedIdentityHeaders() {
		req.Header.Del(header)
	}
	if p.config.IdentitySignatureHeader != "" {
		req.Header.Del(p.config.IdentitySignatureHeader)
	}
}

func (p *Proxy) signIdentityHeaders(req *http.Request) {
	secret := strings.TrimSpace(p.config.IdentitySigningSecret)
	if secret == "" {
		return
	}

	signatureHeader := p.config.IdentitySignatureHeader
	if signatureHeader == "" {
		signatureHeader = "X-Aegion-Signature"
	}
	signed, err := platformcrypto.SignIdentityHeaders([]byte(secret), req.Header, p.identityHeaders(), time.Now().UTC())
	if err != nil || signed == "" {
		return
	}
	req.Header.Set(signatureHeader, signed)
}

func (p *Proxy) identityHeaders() []string {
	if len(p.config.SignedIdentityHeaders) > 0 {
		return p.config.SignedIdentityHeaders
	}
	return p.trustedIdentityHeaders()
}

func (p *Proxy) trustedIdentityHeaders() []string {
	return []string{
		"X-Aegion-Session-ID",
		"X-Aegion-Identity-ID",
		"X-Aegion-AAL",
		"X-Aegion-Authenticated-At",
		"X-Aegion-Impersonation",
		"X-Aegion-Impersonator-ID",
	}
}

// addForwardedHeaders adds standard forwarded headers.
func (p *Proxy) addForwardedHeaders(req, original *http.Request) {
	if req == nil || original == nil {
		return
	}

	clientIP := trustedproxy.RemoteIP(original.RemoteAddr)
	if clientIP == "" {
		clientIP = trustedproxy.ClientIP(original, p.config.TrustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS")
	}

	if prior := trustedproxy.PriorForwardedFor(original, p.config.TrustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS"); prior != "" {
		if clientIP != "" {
			req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			req.Header.Set("X-Forwarded-For", prior)
		}
	} else if clientIP == "" {
		req.Header.Del("X-Forwarded-For")
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	req.Header.Set("X-Forwarded-Proto", trustedproxy.ForwardedProto(original, p.config.TrustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS"))
	req.Header.Set("X-Forwarded-Host", trustedproxy.ForwardedHost(original, p.config.TrustForwardedHeaders, "AEGION_TRUSTED_PROXY_CIDRS"))
}

// startHealthCheckers starts health checking for all configured upstreams.
func (p *Proxy) startHealthCheckers() {
	p.healthMux.Lock()
	defer p.healthMux.Unlock()

	for name, checker := range p.healthCheckers {
		p.logger.Info("starting health checker", "upstream", name)
		go checker.Start()
	}
}

// Close shuts down background components started by the proxy.
func (p *Proxy) Close() {
	p.healthMux.Lock()
	defer p.healthMux.Unlock()

	for name, checker := range p.healthCheckers {
		checker.Stop()
		delete(p.healthCheckers, name)
		p.logger.Info("stopped health checker", "upstream", name)
	}

	p.ruleLimitersMux.Lock()
	p.ruleLimiters = make(map[string]*RateLimiter)
	p.ruleLimitersMux.Unlock()

	if transport, ok := p.transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

// getOrCreateRequestID gets the request ID from headers or creates a new one.
func (p *Proxy) getOrCreateRequestID(r *http.Request) string {
	// Check if request ID already exists
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}

	// Generate new request ID
	b, err := platformcrypto.RandomBytes(8)
	if err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// Error handling methods

func (p *Proxy) handleError(w http.ResponseWriter, r *http.Request, err error, statusCode int, start time.Time) {
	ctx := r.Context()
	requestID := getRequestIDFromContext(ctx)
	duration := time.Since(start)

	p.logger.ErrorContext(ctx, "proxy error",
		"error", err,
		"status", statusCode,
		"duration", duration,
	)

	p.writeErrorResponse(w, statusCode, err.Error(), requestID)
}

func (p *Proxy) handleRateLimitExceeded(w http.ResponseWriter, r *http.Request, waitTime time.Duration, err error, start time.Time) {
	ctx := r.Context()
	requestID := getRequestIDFromContext(ctx)

	limit := 100
	remaining := 0
	if p.limiter != nil {
		limit = p.limiter.Limit()
	}

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("Retry-After", strconv.Itoa(int(waitTime.Seconds())))

	p.logger.WarnContext(ctx, "rate limit exceeded",
		"wait_time", waitTime,
		"duration", time.Since(start),
	)

	p.writeErrorResponse(w, http.StatusTooManyRequests, "Rate limit exceeded", requestID)
}

func (p *Proxy) handleAccessError(w http.ResponseWriter, r *http.Request, err error, start time.Time) {
	ctx := r.Context()
	requestID := getRequestIDFromContext(ctx)
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

	p.logger.WarnContext(ctx, "access denied",
		"error", err,
		"status", statusCode,
		"duration", time.Since(start),
	)

	p.writeErrorResponse(w, statusCode, message, requestID)
}

func (p *Proxy) handleProxyError(w http.ResponseWriter, r *http.Request, err error, start time.Time) {
	ctx := r.Context()
	requestID := getRequestIDFromContext(ctx)
	statusCode := http.StatusBadGateway
	message := "Upstream error"

	if r.Context().Err() == context.DeadlineExceeded {
		statusCode = http.StatusGatewayTimeout
		message = "Request timeout"
	}

	p.logger.ErrorContext(ctx, "proxy error",
		"error", err,
		"status", statusCode,
		"duration", time.Since(start),
	)

	p.writeErrorResponse(w, statusCode, message, requestID)
}

func (p *Proxy) writeErrorResponse(w http.ResponseWriter, statusCode int, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]any{
		"error": map[string]any{
			"code":       statusCode,
			"message":    message,
			"request_id": requestID,
		},
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		p.logger.Error("failed to write error response", "error", err)
	}
}
