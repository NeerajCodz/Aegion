package authtoken

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddlewareFailsClosedWhenGeneratorMissing(t *testing.T) {
	mw := Middleware(MiddlewareConfig{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/internal/registry/modules", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestMiddlewareSkipPathRequiresExactMatch(t *testing.T) {
	mw := Middleware(MiddlewareConfig{
		SkipPaths: []string{"/internal/health"},
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/internal/health/../health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestMiddlewareRejectsMissingAndInvalidTokens(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{Secret: []byte("middleware-secret"), TTL: time.Minute})
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	mw := Middleware(MiddlewareConfig{Generator: gen})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	missingReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token status %d, got %d", http.StatusUnauthorized, missingRec.Code)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	invalidReq.Header.Set(HeaderInternalToken, "invalid-token")
	invalidRec := httptest.NewRecorder()
	handler.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid token status %d, got %d", http.StatusUnauthorized, invalidRec.Code)
	}
}

func TestMiddlewareInjectsModuleIDContextAndRequireModuleID(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{Secret: []byte("middleware-secret-ok"), TTL: time.Minute})
	if err != nil {
		t.Fatalf("new generator: %v", err)
	}
	token, err := gen.Generate("admin")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	var moduleID string
	protected := RequireModuleID("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moduleID = ModuleIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	handler := Middleware(MiddlewareConfig{Generator: gen})(protected)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set(HeaderInternalToken, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected allowed module request status %d, got %d", http.StatusOK, rec.Code)
	}
	if moduleID != "admin" {
		t.Fatalf("expected module id in context, got %q", moduleID)
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	otherToken, err := gen.Generate("other")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	forbiddenReq.Header.Set(HeaderInternalToken, otherToken)
	forbiddenRec := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden status %d, got %d", http.StatusForbidden, forbiddenRec.Code)
	}
}

func TestModuleIDFromContextFallbacks(t *testing.T) {
	if got := ModuleIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty module id without context value, got %q", got)
	}
	ctx := context.WithValue(context.Background(), ContextKeyModuleID, 123)
	if got := ModuleIDFromContext(ctx); got != "" {
		t.Fatalf("expected empty module id for non-string context value, got %q", got)
	}
	ctx = context.WithValue(context.Background(), ContextKeyModuleID, "module-1")
	if got := ModuleIDFromContext(ctx); got != "module-1" {
		t.Fatalf("expected module-1 from context, got %q", got)
	}
}
