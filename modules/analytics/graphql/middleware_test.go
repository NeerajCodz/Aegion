package graphql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestAuthMiddleware_RequiresAuthForProtectedField(t *testing.T) {
	handler := AuthMiddleware(zerolog.Nop(), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", w.Code)
	}
}

func TestAuthMiddleware_AllowsPublicQueryWithoutAuth(t *testing.T) {
	handler := AuthMiddleware(zerolog.Nop(), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value("userID").(string); ok {
			t.Fatal("expected no user context for unauthenticated public query")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { health { status } }"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", w.Code)
	}
}

func TestAuthMiddleware_SetsUserContextFromBearerToken(t *testing.T) {
	handler := AuthMiddleware(zerolog.Nop(), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value("userID").(string)
		token, _ := r.Context().Value("token").(string)
		if userID != "user-1" {
			t.Fatalf("expected user-1, got %q", userID)
		}
		if token != "user-1:session-token" {
			t.Fatalf("expected token in context, got %q", token)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer user-1:session-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", w.Code)
	}
}

func TestAuthMiddleware_SupportsSessionTokenHeader(t *testing.T) {
	handler := AuthMiddleware(zerolog.Nop(), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value("userID").(string)
		if userID != "user-2" {
			t.Fatalf("expected user-2, got %q", userID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Token", "user-2:session-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", w.Code)
	}
}

func TestAuthDirectiveHandler_RequiresTokenWhenRequested(t *testing.T) {
	err := AuthDirectiveHandler(context.WithValue(context.Background(), "userID", "user-1"), func() error { return nil }, &DirectiveContext{
		FieldName: "events",
		Arguments: map[string]interface{}{"required": true},
	})
	if err == nil {
		t.Fatal("expected directive auth error")
	}
}

func TestValidateGraphQLToken(t *testing.T) {
	userID, err := validateGraphQLToken("user-3:anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "user-3" {
		t.Fatalf("expected user-3, got %q", userID)
	}
}
