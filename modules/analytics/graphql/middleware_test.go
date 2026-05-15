package graphql

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/analytics/rbac"
)

var graphQLTestKeyPair *platformjwt.KeyPair

func TestAuthMiddleware_RequiresAuthForProtectedField(t *testing.T) {
	handler := AuthMiddleware(xlog.New(xlog.Config{}), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := AuthMiddleware(xlog.New(xlog.Config{}), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	expectedToken := testJWT("user-1")
	handler := AuthMiddleware(xlog.New(xlog.Config{}), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value("userID").(string)
		token, _ := r.Context().Value("token").(string)
		role, _ := r.Context().Value("role").(string)
		if userID != "user-1" {
			t.Fatalf("expected user-1, got %q", userID)
		}
		if token != expectedToken {
			t.Fatalf("expected token in context, got %q", token)
		}
		if role != "" {
			t.Fatalf("expected empty role for token without role claim, got %q", role)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+expectedToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", w.Code)
	}
}

func TestAuthMiddleware_RejectsSessionTokenHeader(t *testing.T) {
	handler := AuthMiddleware(xlog.New(xlog.Config{}), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-Token", "user-2:session-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", w.Code)
	}
}

func TestAuthMiddleware_SetsRoleFromTokenClaim(t *testing.T) {
	handler := AuthMiddleware(xlog.New(xlog.Config{}), map[string]bool{"events": true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value("userID").(string)
		role, _ := r.Context().Value("role").(string)
		manager := rbac.FromContext(r.Context())
		resolvedRole, err := manager.GetUserRole(userID)
		if err != nil {
			t.Fatalf("unexpected role lookup error: %v", err)
		}
		if userID != "admin-1" {
			t.Fatalf("expected admin-1, got %q", userID)
		}
		if role != "admin" {
			t.Fatalf("expected admin role in context, got %q", role)
		}
		if resolvedRole != rbac.RoleAdmin {
			t.Fatalf("expected rbac manager admin role, got %q", resolvedRole)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query { events { totalCount } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testJWTWithRole("admin-1", "admin"))
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
	userID, err := validateGraphQLToken(testJWT("user-3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "user-3" {
		t.Fatalf("expected user-3, got %q", userID)
	}
}

func testJWT(sub string) string {
	return testJWTWithRole(sub, "")
}

func testJWTWithRole(sub string, role string) string {
	claims := platformjwt.Claims{
		Subject:   sub,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if role != "" {
		claims.Custom = map[string]interface{}{"role": role}
	}
	token, err := platformjwt.Sign(claims, graphQLTestKeyPair.PrivateKey, graphQLTestKeyPair.Algorithm, graphQLTestKeyPair.KeyID)
	if err != nil {
		panic(err)
	}
	return token
}

func init() {
	keyPair, err := platformjwt.GenerateECKeyPair("test-key")
	if err != nil {
		panic(err)
	}
	graphQLTestKeyPair = keyPair
	if err := setGraphQLTestJWTKey(keyPair.PublicKey); err != nil {
		panic(err)
	}
}

func setGraphQLTestJWTKey(publicKey []byte) error {
	return os.Setenv("AEGION_ANALYTICS_REST_JWT_PUBLIC_KEY_BASE64", base64.StdEncoding.EncodeToString(publicKey))
}
