package service

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestJWTAdditionalBranches(t *testing.T) {
	ctx := context.Background()

	makeToken := func(headerJSON, payloadJSON string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(headerJSON)) + "." +
			base64.RawURLEncoding.EncodeToString([]byte(payloadJSON)) + ".sig"
	}

	t.Run("verifyAndDecodeIDToken propagates jwks fetch error", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
		})
		token := makeToken(`{"alg":"ES256","kid":"k1"}`, `{"sub":"u1"}`)
		if _, err := svc.verifyAndDecodeIDToken(ctx, token, "cid", "iss", "https://provider.example.com/jwks"); err == nil {
			t.Fatal("expected jwks fetch error")
		}
	})

	t.Run("verifyAndDecodeIDToken skips mismatched alg and key conversion errors", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/jwks-mismatch" {
					return jsonResponse(http.StatusOK, `{"keys":[{"kid":"k1","kty":"EC","alg":"RS256","crv":"P-256","x":"AQAB","y":"AQAB"}]}`), nil
				}
				return jsonResponse(http.StatusOK, `{"keys":[{"kid":"k1","kty":"oct","alg":"ES256"}]}`), nil
			}),
		})

		token := makeToken(`{"alg":"ES256","kid":"k1"}`, `{"sub":"u1"}`)
		if _, err := svc.verifyAndDecodeIDToken(ctx, token, "cid", "iss", "https://provider.example.com/jwks-mismatch"); err == nil || !strings.Contains(err.Error(), "provider jwk not found") {
			t.Fatalf("expected jwk mismatch error, got %v", err)
		}
		if _, err := svc.verifyAndDecodeIDToken(ctx, token, "cid", "iss", "https://provider.example.com/jwks-invalid"); err == nil || !strings.Contains(err.Error(), "unsupported jwk type") {
			t.Fatalf("expected unsupported jwk type error, got %v", err)
		}
	})

	t.Run("verifyAndDecodeIDToken returns verify error for invalid signature", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(http.StatusOK, `{"keys":[{"kid":"k1","kty":"EC","alg":"ES256","crv":"P-256","x":"AQAB","y":"AQAB"}]}`), nil
			}),
		})
		token := makeToken(`{"alg":"ES256","kid":"k1"}`, `{"sub":"u1","iss":"iss","aud":"cid"}`)
		if _, err := svc.verifyAndDecodeIDToken(ctx, token, "cid", "iss", "https://provider.example.com/jwks"); err == nil {
			t.Fatal("expected signature verification error")
		}
	})

	t.Run("fetchJWKS handles do and decode failures", func(t *testing.T) {
		svc := New(store.New()).WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/decode" {
					return jsonResponse(http.StatusOK, `{`), nil
				}
				return nil, context.Canceled
			}),
		})
		if _, err := svc.fetchJWKS(ctx, "https://provider.example.com/fail"); err == nil {
			t.Fatal("expected transport failure")
		}
		if _, err := svc.fetchJWKS(ctx, "https://provider.example.com/decode"); err == nil {
			t.Fatal("expected decode failure")
		}
	})

	t.Run("jwt parsing helpers handle invalid json payloads", func(t *testing.T) {
		if _, err := parseJWTHeader(makeToken(`{`, `{"sub":"u1"}`)); err == nil {
			t.Fatal("expected parseJWTHeader json error")
		}
		if _, err := parseJWTPayload(makeToken(`{"alg":"none"}`, `{`)); err == nil {
			t.Fatal("expected parseJWTPayload json error")
		}
	})

	t.Run("jwk conversion handles decode errors and rsa success", func(t *testing.T) {
		if _, err := jwkToVerifyKey(jwk{Kty: "EC", Crv: "P-256", X: "AQAB", Y: "~"}); err == nil {
			t.Fatal("expected EC Y decode error")
		}
		if _, err := jwkToVerifyKey(jwk{Kty: "RSA", N: "~", E: "AQAB"}); err == nil {
			t.Fatal("expected RSA modulus decode error")
		}
		key, err := jwkToVerifyKey(jwk{Kty: "RSA", N: "AQID", E: "AQAB"})
		if err != nil {
			t.Fatalf("expected RSA key conversion success, got %v", err)
		}
		if len(key) == 0 {
			t.Fatal("expected non-empty RSA public key bytes")
		}
	})
}
