package router

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/session"
	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
)

var (
	ErrModuleUnavailable = errors.New("module unavailable")
	ErrModuleTimeout     = errors.New("module request timeout")
	ErrNoHealthyEndpoint = errors.New("no healthy endpoint for module")
	ErrPolicyDenied      = errors.New("policy denied request")
)

// PolicyChecker evaluates authorization decisions for proxied module requests.
type PolicyChecker interface {
	Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error)
}

// ModuleProxyConfig configures the module proxy.
type ModuleProxyConfig struct {
	Registry      *registry.Registry
	ModuleID      string
	InternalToken string
	SessionSecret []byte
	Timeout       time.Duration
	PreserveHost  bool

	StripInboundIdentityHeaders bool
	IdentitySigningSecret       []byte
	IdentitySignatureHeader     string
	SignedIdentityHeaders       []string

	PolicyChecker PolicyChecker
	RequirePolicy bool
	PolicyModel   string
	Logger        zerolog.Logger
}

// ModuleProxy forwards requests to module containers.
type ModuleProxy struct {
	config    ModuleProxyConfig
	logger    zerolog.Logger
	transport *http.Transport
	now       func() time.Time
}

type policyDenyError struct {
	response *policypb.CheckResponse
}

func (e *policyDenyError) Error() string {
	if e == nil || e.response == nil {
		return ErrPolicyDenied.Error()
	}
	reason := strings.TrimSpace(e.response.GetDenyReason())
	if reason == "" {
		reason = "policy_denied"
	}
	return reason
}

// NewModuleProxy creates a new module proxy.
func NewModuleProxy(cfg ModuleProxyConfig) *ModuleProxy {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.IdentitySignatureHeader == "" {
		cfg.IdentitySignatureHeader = "X-Aegion-Signature"
	}
	if len(cfg.SignedIdentityHeaders) == 0 {
		cfg.SignedIdentityHeaders = []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"}
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
		now: func() time.Time {
			return time.Now().UTC()
		},
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

	decision, err := p.authorize(ctx, r)
	if err != nil {
		p.handlePolicyError(w, r, err, requestID)
		return
	}
	if decision != nil {
		p.logger.Debug().
			Str("request_id", requestID).
			Str("policy_model", decision.GetModelUsed()).
			Strs("policy_eval_path", decision.GetEvalPath()).
			Msg("module proxy policy allow")
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

func (p *ModuleProxy) authorize(ctx context.Context, r *http.Request) (*policypb.CheckResponse, error) {
	if p.config.PolicyChecker == nil {
		if p.config.RequirePolicy {
			return nil, &policyDenyError{response: requiredPolicyDenyResponse("policy_unavailable")}
		}
		return nil, nil
	}

	checkReq := p.buildPolicyCheckRequest(r)
	resp, err := p.config.PolicyChecker.Check(ctx, checkReq)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, &policyDenyError{response: requiredPolicyDenyResponse("policy_no_decision")}
	}
	if !resp.GetAllowed() {
		return resp, &policyDenyError{response: resp}
	}
	return resp, nil
}

func requiredPolicyDenyResponse(reason string) *policypb.CheckResponse {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "policy_denied"
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "default",
		DenyReason: reason,
		EvalPath:   []string{"default:deny", "policy:" + reason},
	}
}

func (p *ModuleProxy) buildPolicyCheckRequest(r *http.Request) *policypb.CheckRequest {
	subject := "anonymous"
	if sess := session.FromContext(r.Context()); sess != nil {
		subject = "user:" + sess.IdentityID.String()
	}

	resourcePath := strings.TrimPrefix(r.URL.Path, "/")
	if resourcePath == "" {
		resourcePath = "_root"
	}

	extra := map[string]string{
		"module_id":   p.config.ModuleID,
		"path":        r.URL.Path,
		"http_method": r.Method,
	}
	if requestID := GetRequestID(r.Context()); requestID != "" {
		extra["request_id"] = requestID
	}
	if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
		extra["user_agent"] = userAgent
	}

	clientIP := requestContextIPFromContext(r.Context())
	if clientIP == "" {
		clientIP = getClientIP(r)
	}

	return &policypb.CheckRequest{
		Subject:      subject,
		Resource:     p.config.ModuleID + ":" + resourcePath,
		ResourceType: "module:" + p.config.ModuleID,
		Action:       policyActionFromMethod(r.Method),
		Model:        normalizePolicyModel(p.config.PolicyModel),
		Context: &policypb.Context{
			Ip:       clientIP,
			TenantId: strings.TrimSpace(r.Header.Get("X-Aegion-Tenant-ID")),
			Extra:    extra,
		},
	}
}

func policyActionFromMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "read"
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return "write"
	default:
		return strings.ToLower(strings.TrimSpace(method))
	}
}

func normalizePolicyModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch model {
	case "", "default", "rbac", "abac", "rebac":
		return model
	default:
		return ""
	}
}

type requestContextIPKey struct{}

// WithRequestContextIP attaches a best-effort client IP for policy context evaluation.
func WithRequestContextIP(ctx context.Context, ip string) context.Context {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ctx
	}
	return context.WithValue(ctx, requestContextIPKey{}, ip)
}

func requestContextIPFromContext(ctx context.Context) string {
	v := ctx.Value(requestContextIPKey{})
	ip, _ := v.(string)
	ip = strings.TrimSpace(ip)
	host := ip
	if parsedHost, _, err := net.SplitHostPort(ip); err == nil {
		host = parsedHost
	}
	return strings.Trim(host, "[]")
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
		if p.config.PreserveHost && originalReq.Host != "" {
			req.Host = originalReq.Host
		} else {
			req.Host = target.Host
		}

		if p.config.StripInboundIdentityHeaders {
			p.stripIdentityHeaders(req)
		}

		// Inject internal token
		if p.config.InternalToken != "" {
			req.Header.Set("X-Aegion-Internal-Token", p.config.InternalToken)
		}

		// Inject session headers if session exists
		p.injectSessionHeaders(req)

		// Inject identity headers for upstream compatibility
		p.injectIdentityHeaders(req)

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
			Bool("preserve_host", p.config.PreserveHost).
			Bool("identity_headers_signed", len(p.config.IdentitySigningSecret) > 0).
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

func (p *ModuleProxy) stripIdentityHeaders(req *http.Request) {
	for _, header := range []string{
		"X-User-ID",
		"X-User-Email",
		"X-User-Roles",
		"X-User-Session-ID",
		"X-User-AAL",
		p.config.IdentitySignatureHeader,
	} {
		req.Header.Del(header)
	}
}

func (p *ModuleProxy) injectIdentityHeaders(req *http.Request) {
	sess := session.FromContext(req.Context())
	if sess == nil {
		return
	}

	req.Header.Set("X-User-ID", sess.IdentityID.String())
	req.Header.Set("X-User-Session-ID", sess.ID.String())
	req.Header.Set("X-User-AAL", string(sess.AAL))

	if len(p.config.IdentitySigningSecret) == 0 {
		return
	}

	timestamp := p.now().Unix()
	canonical := p.canonicalIdentityHeaders(req)
	payload := strconv.FormatInt(timestamp, 10) + "." + canonical

	mac := hmac.New(sha256.New, p.config.IdentitySigningSecret)
	_, _ = mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set(p.config.IdentitySignatureHeader, fmt.Sprintf("t=%d,v1=%s", timestamp, signature))
}

func (p *ModuleProxy) canonicalIdentityHeaders(req *http.Request) string {
	parts := make([]string, 0, len(p.config.SignedIdentityHeaders))
	for _, header := range p.config.SignedIdentityHeaders {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		parts = append(parts, strings.ToLower(header)+":"+strings.TrimSpace(req.Header.Get(header)))
	}
	return strings.Join(parts, "\n")
}

// addForwardedHeaders adds X-Forwarded-* headers.
func (p *ModuleProxy) addForwardedHeaders(req, originalReq *http.Request) {
	clientIP := originalReq.RemoteAddr
	if host, _, err := net.SplitHostPort(strings.TrimSpace(originalReq.RemoteAddr)); err == nil {
		clientIP = strings.Trim(host, "[]")
	}
	if clientIP == "" {
		clientIP = getClientIP(originalReq)
	}

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
		return nil, fmt.Errorf("%w: %w", ErrModuleUnavailable, err)
	}

	// Check module health
	if module.Status != registry.StatusHealthy && module.Status != registry.StatusStarting {
		return nil, fmt.Errorf("%w: module status is %s", ErrModuleUnavailable, module.Status)
	}

	// Find HTTP endpoint
	for _, ep := range module.Endpoints {
		if ep.Type == registry.EndpointHTTP {
			target, parseErr := url.Parse(ep.URL)
			if parseErr != nil {
				return nil, parseErr
			}
			return target, nil
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

	switch {
	case errors.Is(err, ErrModuleTimeout):
		status = http.StatusGatewayTimeout
		message = "Module request timeout"
	case errors.Is(err, registry.ErrModuleNotFound):
		status = http.StatusNotFound
		message = "module not found"
	case errors.Is(err, ErrNoHealthyEndpoint):
		status = http.StatusBadGateway
		message = "no HTTP endpoint available"
	default:
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			status = http.StatusBadGateway
			message = "invalid module endpoint"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":       status,
			"message":    message,
			"request_id": requestID,
		},
	})
}

func (p *ModuleProxy) handlePolicyError(w http.ResponseWriter, r *http.Request, err error, requestID string) {
	status := http.StatusBadGateway
	message := "Policy evaluation failed"
	modelUsed := ""
	evalPath := []string{}

	var denyErr *policyDenyError
	if errors.As(err, &denyErr) && denyErr != nil && denyErr.response != nil {
		status = http.StatusForbidden
		message = denyErr.Error()
		modelUsed = denyErr.response.GetModelUsed()
		evalPath = denyErr.response.GetEvalPath()
	}

	p.logger.Warn().
		Str("request_id", requestID).
		Err(err).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("policy_model", modelUsed).
		Strs("policy_eval_path", evalPath).
		Msg("module proxy policy decision")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":        status,
			"message":     message,
			"request_id":  requestID,
			"deny_reason": message,
			"model_used":  modelUsed,
			"eval_path":   evalPath,
		},
	})
}

// logResponse logs the proxied response.
func (p *ModuleProxy) logResponse(resp *http.Response, requestID string, start time.Time) {
	duration := time.Since(start)
	upstreamHost := ""
	upstreamPath := ""
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		upstreamHost = resp.Request.URL.Host
		upstreamPath = resp.Request.URL.Path
	}

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
		Str("upstream", upstreamHost).
		Str("upstream_path", upstreamPath).
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
