package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/session"
)

type failingJSONResponseWriter struct {
	header http.Header
	status int
}

func (w *failingJSONResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingJSONResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *failingJSONResponseWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestAuthMiddleware_AdditionalCoverageBranches(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	t.Run("optional middleware allows expired session", func(t *testing.T) {
		am := NewAuthMiddleware(nil, logger, true)
		expired := &session.Session{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			Active:     true,
			ExpiresAt:  time.Now().UTC().Add(-time.Minute),
		}
		am.getFromRequest = func(context.Context, *http.Request) (*session.Session, error) {
			return expired, nil
		}

		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodGet, "/protected", nil).WithContext(withRequestID(context.Background(), "req-expired-optional"))
		rec := httptest.NewRecorder()
		am.Middleware(next).ServeHTTP(rec, req)

		if !nextCalled || rec.Code != http.StatusNoContent {
			t.Fatalf("expected optional middleware to continue, next=%v status=%d", nextCalled, rec.Code)
		}
	})

	t.Run("resolveSession uses session manager fallback path", func(t *testing.T) {
		am := NewAuthMiddleware(&session.Manager{}, logger, false)
		_, err := am.resolveSession(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
		if !errors.Is(err, session.ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("handleAuthError default branch with write failure", func(t *testing.T) {
		am := NewAuthMiddleware(nil, logger, false)
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil).WithContext(withRequestID(context.Background(), "req-auth-default"))
		w := &failingJSONResponseWriter{}

		am.handleAuthError(w, req, errors.New("unexpected-auth-error"))
		if w.status != http.StatusUnauthorized {
			t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.status)
		}
	})

	t.Run("require capabilities middleware covers deny and allow branches", func(t *testing.T) {
		sess := &session.Session{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			AAL:        session.AAL1,
		}

		denyCalled := false
		denyNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			denyCalled = true
		})
		denyReq := httptest.NewRequest(http.MethodGet, "/api/admin", nil).WithContext(session.WithSession(context.Background(), sess))
		denyRec := httptest.NewRecorder()
		RequireCapabilities("admin:write")(denyNext).ServeHTTP(denyRec, denyReq)
		if denyCalled {
			t.Fatalf("expected deny path to block downstream handler")
		}
		if denyRec.Code != http.StatusForbidden {
			t.Fatalf("expected forbidden status for deny path, got %d", denyRec.Code)
		}

		allowCalled := false
		allowNext := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowCalled = true
			w.WriteHeader(http.StatusOK)
		})
		allowReq := httptest.NewRequest(http.MethodGet, "/api/profile", nil).WithContext(session.WithSession(context.Background(), sess))
		allowRec := httptest.NewRecorder()
		RequireCapabilities()(allowNext).ServeHTTP(allowRec, allowReq)
		if !allowCalled || allowRec.Code != http.StatusOK {
			t.Fatalf("expected allow path to continue, called=%v status=%d", allowCalled, allowRec.Code)
		}
	})
}

func TestCircuitBreaker_AdditionalCoverageBranches(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          time.Second,
		SuccessThreshold: 1,
	})

	cb.state = State(99)
	if cb.Allow() {
		t.Fatalf("expected unknown state to reject request")
	}

	cb.state = StateOpen
	cb.RecordSuccess()
	if cb.GetState() != StateHalfOpen {
		t.Fatalf("expected open-state success to transition to half-open, got %s", cb.GetState())
	}
}

func TestProxy_AdditionalCoverageBranches(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := newProxyForTest(t, DefaultConfig(), nil, logger)

	if limiter := proxy.getRuleLimiter(nil); limiter != nil {
		t.Fatalf("expected nil limiter for nil rule")
	}
	if limiter := proxy.getRuleLimiter(&Rule{}); limiter != nil {
		t.Fatalf("expected nil limiter for rule without rate limit")
	}

	rule := &Rule{
		ID:       "",
		Path:     "/path",
		Target:   "upstream",
		Priority: 9,
		RateLimit: &RateLimitConfig{
			RequestsPerSecond: 1,
			ByIP:              true,
		},
	}
	if limiter := proxy.getRuleLimiter(rule); limiter == nil {
		t.Fatalf("expected limiter for rate-limited rule")
	}
	if _, ok := proxy.ruleLimiters["/path|upstream|9"]; !ok {
		t.Fatalf("expected synthesized rule-limiter key to be present")
	}

	noCanonicalCfg := DefaultConfig()
	noCanonicalCfg.IdentitySigningSecret = "secret"
	noCanonicalCfg.SignedIdentityHeaders = []string{"X-Empty"}
	noCanonicalProxy := newProxyForTest(t, noCanonicalCfg, nil, logger)
	noCanonicalReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	noCanonicalProxy.signIdentityHeaders(noCanonicalReq)
	if sig := noCanonicalReq.Header.Get("X-Aegion-Signature"); sig != "" {
		t.Fatalf("expected no signature when canonical headers are empty, got %q", sig)
	}

	defaultSigCfg := DefaultConfig()
	defaultSigCfg.IdentitySigningSecret = "secret"
	defaultSigCfg.IdentitySignatureHeader = ""
	defaultSigCfg.SignedIdentityHeaders = nil
	defaultSigCfg.TrustForwardedHeaders = true
	defaultSigProxy := newProxyForTest(t, defaultSigCfg, nil, logger)
	defaultSigReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	defaultSigReq.Header.Set("X-Aegion-Identity-ID", "identity-1")
	defaultSigProxy.signIdentityHeaders(defaultSigReq)
	if sig := defaultSigReq.Header.Get("X-Aegion-Signature"); sig == "" {
		t.Fatalf("expected default signature header to be populated")
	}
	if len(defaultSigProxy.identityHeaders()) == 0 {
		t.Fatalf("expected default identity headers list")
	}

	original := httptest.NewRequest(http.MethodGet, "http://edge.local/resource", nil)
	original.RemoteAddr = "198.51.100.10:1234"
	original.Host = "edge.local"
	original.Header.Set("X-Real-IP", "203.0.113.9")
	forwarded := httptest.NewRequest(http.MethodGet, "http://upstream.local/resource", nil)
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "198.51.100.0/24")
	defaultSigProxy.addForwardedHeaders(forwarded, original)
	if got := forwarded.Header.Get("X-Forwarded-For"); got != "198.51.100.10" {
		t.Fatalf("expected fallback forwarded-for IP, got %q", got)
	}
}

func TestRateLimiterAndRules_AdditionalCoverageBranches(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: 1,
	}, NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	allowed, wait, err := limiter.Allow(req)
	if err != nil {
		t.Fatalf("Allow returned error: %v", err)
	}
	if !allowed || wait != 0 {
		t.Fatalf("expected request allowed with no configured keys, allowed=%v wait=%v", allowed, wait)
	}

	if !matchesPattern("[", "[") {
		t.Fatalf("expected invalid glob fallback to exact match")
	}
}

func TestProxyLimiterConcurrency_AdditionalCoverageBranches(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := newProxyForTest(t, DefaultConfig(), nil, logger)

	rule := &Rule{
		ID:       "concurrent-rule",
		Path:     "/concurrent",
		Target:   "upstream",
		Priority: 1,
		RateLimit: &RateLimitConfig{
			RequestsPerSecond: 5,
			ByIP:              true,
		},
	}

	// Stress concurrent creation so one goroutine may hit the second existence check branch.
	for i := 0; i < 50; i++ {
		proxy.ruleLimitersMux.Lock()
		proxy.ruleLimiters = make(map[string]*RateLimiter)
		proxy.ruleLimitersMux.Unlock()

		start := make(chan struct{})
		var wg sync.WaitGroup
		limiters := make([]*RateLimiter, 2)

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			limiters[0] = proxy.getRuleLimiter(rule)
		}()
		go func() {
			defer wg.Done()
			<-start
			limiters[1] = proxy.getRuleLimiter(rule)
		}()

		close(start)
		wg.Wait()

		if limiters[0] == nil || limiters[1] == nil {
			t.Fatalf("expected non-nil limiters under concurrent creation")
		}
	}
}
