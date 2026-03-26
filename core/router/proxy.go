package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
)

var (
	ErrModuleUnavailable = errors.New("module unavailable")
	ErrModuleTimeout     = errors.New("module request timeout")
	ErrNoHealthyEndpoint = errors.New("no healthy endpoint for module")
)

// ModuleProxyConfig configures the module proxy.
type ModuleProxyConfig struct {
	Registry      *registry.Registry
	ModuleID      string
	InternalToken string
	SessionSecret []byte
	Timeout       time.Duration
	Logger        zerolog.Logger
}

// ModuleProxy forwards requests to module containers.
type ModuleProxy struct {
	config    ModuleProxyConfig
	logger    zerolog.Logger
	transport *http.Transport
}

// NewModuleProxy creates a new module proxy.
func NewModuleProxy(cfg ModuleProxyConfig) *ModuleProxy {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConnsPerHost:   10,
	}

	return &ModuleProxy{
		config:    cfg,
		logger:    cfg.Logger.With().Str("module", cfg.ModuleID).Logger(),
		transport: transport,
	}
}

// ServeHTTP implements http.Handler.
func (p *ModuleProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestID := GetRequestID(ctx)
	start := time.Now()

	// Get module endpoint
	targetURL, err := p.getModuleEndpoint(ctx)
	if err != nil {
		p.handleError(w, r, err, requestID)
		return
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director:  p.director(targetURL, r),
		Transport: p.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.handleProxyError(w, r, err, requestID)
		},
		ModifyResponse: func(resp *http.Response) error {
			p.logResponse(resp, requestID, start)
			return nil
		},
	}

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()

	proxy.ServeHTTP(w, r.WithContext(ctx))
}

// director returns a function that modifies the request before proxying.
func (p *ModuleProxy) director(target *url.URL, originalReq *http.Request) func(*http.Request) {
	return func(req *http.Request) {
		// Set target
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host

		// Preserve the path after the mount point
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		req.URL.RawQuery = originalReq.URL.RawQuery

		// Set Host header
		req.Host = target.Host

		// Inject internal token
		if p.config.InternalToken != "" {
			req.Header.Set("X-Aegion-Internal-Token", p.config.InternalToken)
		}

		// Inject session headers if session exists
		p.injectSessionHeaders(req)

		// Preserve request ID
		if requestID := GetRequestID(req.Context()); requestID != "" {
			req.Header.Set("X-Request-ID", requestID)
		}

		// Add forwarded headers
		p.addForwardedHeaders(req, originalReq)

		p.logger.Debug().
			Str("request_id", GetRequestID(req.Context())).
			Str("method", req.Method).
			Str("target", req.URL.String()).
			Msg("proxying request to module")
	}
}

// injectSessionHeaders adds signed session context headers.
func (p *ModuleProxy) injectSessionHeaders(req *http.Request) {
	ctx := req.Context()
	sess := session.FromContext(ctx)
	if sess == nil {
		return
	}

	session.InjectHeaders(req, sess, p.config.SessionSecret)
}

// addForwardedHeaders adds X-Forwarded-* headers.
func (p *ModuleProxy) addForwardedHeaders(req, originalReq *http.Request) {
	clientIP := getClientIP(originalReq)

	if prior := originalReq.Header.Get("X-Forwarded-For"); prior != "" {
		req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	} else {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	if proto := originalReq.Header.Get("X-Forwarded-Proto"); proto != "" {
		req.Header.Set("X-Forwarded-Proto", proto)
	} else if originalReq.TLS != nil {
		req.Header.Set("X-Forwarded-Proto", "https")
	} else {
		req.Header.Set("X-Forwarded-Proto", "http")
	}

	if host := originalReq.Header.Get("X-Forwarded-Host"); host != "" {
		req.Header.Set("X-Forwarded-Host", host)
	} else {
		req.Header.Set("X-Forwarded-Host", originalReq.Host)
	}
}

// getModuleEndpoint retrieves the HTTP endpoint for a module.
func (p *ModuleProxy) getModuleEndpoint(ctx context.Context) (*url.URL, error) {
	if p.config.Registry == nil {
		return nil, ErrModuleUnavailable
	}

	module, err := p.config.Registry.GetModule(p.config.ModuleID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModuleUnavailable, err)
	}

	// Check module health
	if module.Status != registry.StatusHealthy && module.Status != registry.StatusStarting {
		return nil, fmt.Errorf("%w: module status is %s", ErrModuleUnavailable, module.Status)
	}

	// Find HTTP endpoint
	for _, ep := range module.Endpoints {
		if ep.Type == registry.EndpointHTTP {
			return url.Parse(ep.URL)
		}
	}

	return nil, ErrNoHealthyEndpoint
}

// handleError handles proxy setup errors.
func (p *ModuleProxy) handleError(w http.ResponseWriter, r *http.Request, err error, requestID string) {
	p.logger.Error().
		Str("request_id", requestID).
		Err(err).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("module proxy error")

	status := http.StatusServiceUnavailable
	message := "Module temporarily unavailable"

	if errors.Is(err, ErrModuleTimeout) {
		status = http.StatusGatewayTimeout
		message = "Module request timeout"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":       status,
			"message":    message,
			"request_id": requestID,
		},
	})
}

// handleProxyError handles errors during proxying.
func (p *ModuleProxy) handleProxyError(w http.ResponseWriter, r *http.Request, err error, requestID string) {
	p.logger.Error().
		Str("request_id", requestID).
		Err(err).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("proxy transport error")

	status := http.StatusBadGateway
	message := "Error communicating with module"

	// Check for context deadline exceeded
	if r.Context().Err() == context.DeadlineExceeded {
		status = http.StatusGatewayTimeout
		message = "Module request timeout"
	}

	// Check for connection refused
	if isConnectionRefused(err) {
		status = http.StatusServiceUnavailable
		message = "Module is not responding"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":       status,
			"message":    message,
			"request_id": requestID,
		},
	})
}

// logResponse logs the proxied response.
func (p *ModuleProxy) logResponse(resp *http.Response, requestID string, start time.Time) {
	duration := time.Since(start)

	var event *zerolog.Event
	switch {
	case resp.StatusCode >= 500:
		event = p.logger.Error()
	case resp.StatusCode >= 400:
		event = p.logger.Warn()
	default:
		event = p.logger.Debug()
	}

	event.
		Str("request_id", requestID).
		Int("status", resp.StatusCode).
		Dur("duration", duration).
		Msg("module response")
}

// Helper functions

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "dial tcp")
}
