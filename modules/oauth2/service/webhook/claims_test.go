package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimsHookClient_InjectClaims_NoWebhook(t *testing.T) {
	client := NewClaimsHookClient("", "", time.Second)

	claims := map[string]interface{}{"sub": "u1"}
	out, err := client.InjectClaims(context.Background(), &ClaimsRequest{
		IdentityID: "u1",
		ClientID:   "c1",
		TokenType:  "access",
		Claims:     claims,
	})
	require.NoError(t, err)
	assert.Equal(t, claims, out)

	out, err = client.InjectClaims(context.Background(), &ClaimsRequest{})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestClaimsHookClient_InjectClaims_HTTPScenarios(t *testing.T) {
	t.Run("success and signature header", func(t *testing.T) {
		var gotSignature string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotSignature = r.Header.Get("X-Aegion-Signature")
			require.Equal(t, "application/json", r.Header.Get("Content-Type"))
			require.Equal(t, "Aegion-OAuth2/1.0", r.Header.Get("User-Agent"))
			_ = json.NewEncoder(w).Encode(ClaimsResponse{
				Claims: map[string]interface{}{"role": "admin"},
			})
		}))
		defer srv.Close()

		client := NewClaimsHookClient(srv.URL, "secret", time.Second)
		out, err := client.InjectClaims(context.Background(), &ClaimsRequest{
			IdentityID: "u1",
			ClientID:   "c1",
			TokenType:  "access",
			Scopes:     []string{"openid"},
			Claims:     map[string]interface{}{"sub": "u1"},
		})
		require.NoError(t, err)
		assert.Equal(t, "admin", out["role"])
		assert.NotEmpty(t, gotSignature)
	})

	t.Run("status decode and response error", func(t *testing.T) {
		badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("downstream failed"))
		}))
		defer badStatus.Close()
		client := NewClaimsHookClient(badStatus.URL, "", time.Second)
		_, err := client.InjectClaims(context.Background(), &ClaimsRequest{})
		assert.ErrorContains(t, err, "status 502")

		badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{"))
		}))
		defer badJSON.Close()
		client = NewClaimsHookClient(badJSON.URL, "", time.Second)
		_, err = client.InjectClaims(context.Background(), &ClaimsRequest{})
		assert.ErrorContains(t, err, "decode")

		withError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			msg := "hook failed"
			_ = json.NewEncoder(w).Encode(ClaimsResponse{
				Claims: map[string]interface{}{},
				Error:  &msg,
			})
		}))
		defer withError.Close()
		client = NewClaimsHookClient(withError.URL, "", time.Second)
		_, err = client.InjectClaims(context.Background(), &ClaimsRequest{})
		assert.ErrorContains(t, err, "hook failed")
	})
}

func TestClaimsHookClient_Signature(t *testing.T) {
	client := NewClaimsHookClient("https://example.test", "secret", time.Second)
	payload := []byte(`{"x":1}`)
	signature, err := client.signPayload(payload)
	require.NoError(t, err)
	assert.True(t, client.VerifySignature(payload, signature))
	assert.False(t, client.VerifySignature(payload, "deadbeef"))
}

func TestMergeClaimsHTTP(t *testing.T) {
	base := map[string]interface{}{
		"sub":  "identity-1",
		"name": "John",
	}

	t.Run("nil hook passthrough", func(t *testing.T) {
		got, err := MergeClaimsHTTP("identity-1", "client-1", "id", []string{"openid"}, base, nil)
		require.NoError(t, err)
		assert.Equal(t, base, got)
	})

	t.Run("merge custom but keep reserved", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ClaimsResponse{
				Claims: map[string]interface{}{
					"tenant_id": "tenant-1",
					"sub":       "override-not-allowed",
					"scope":     "admin",
					"client_id": "attacker-client",
				},
			})
		}))
		defer srv.Close()

		hook := NewClaimsHookClient(srv.URL, "", time.Second)
		baseClaims := map[string]interface{}{"sub": "identity-1", "iss": "issuer", "scope": "read", "client_id": "client-1"}
		got, err := MergeClaimsHTTP("identity-1", "client-1", "access", []string{"openid"}, baseClaims, hook)
		require.NoError(t, err)
		assert.Equal(t, "identity-1", got["sub"])
		assert.Equal(t, "issuer", got["iss"])
		assert.Equal(t, "read", got["scope"])
		assert.Equal(t, "client-1", got["client_id"])
		assert.Equal(t, "tenant-1", got["tenant_id"])
	})

	t.Run("hook errors are non fatal", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		hook := NewClaimsHookClient(srv.URL, "", time.Second)
		baseClaims := map[string]interface{}{"sub": "identity-1"}
		got, err := MergeClaimsHTTP("identity-1", "client-1", "access", nil, baseClaims, hook)
		assert.ErrorContains(t, err, "non-fatal")
		assert.Equal(t, "identity-1", got["sub"])
	})
}

func TestIsReservedClaim(t *testing.T) {
	assert.True(t, isReservedClaim("sub"))
	assert.True(t, isReservedClaim("at_hash"))
	assert.True(t, isReservedClaim("scope"))
	assert.True(t, isReservedClaim("client_id"))
	assert.False(t, isReservedClaim("tenant_id"))
}
