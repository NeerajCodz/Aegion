package jwt

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJWT_RoundTripAndJWK(t *testing.T) {
	kp, err := GenerateECKeyPair("kid-coverage")
	if err != nil {
		t.Fatalf("GenerateECKeyPair failed: %v", err)
	}

	now := time.Now().Unix()
	token, err := Sign(Claims{
		Issuer:    "aegion",
		Subject:   "user-123",
		Audience:  "api",
		IssuedAt:  now,
		ExpiresAt: now + 3600,
		Custom: map[string]interface{}{
			"role": "admin",
		},
	}, kp.PrivateKey, kp.Algorithm, kp.KeyID)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	verified, err := Verify(token, kp.PublicKey, kp.Algorithm, VerifyOptions{
		Issuer:   "aegion",
		Audience: "api",
	})
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if verified.KeyID == "" {
		t.Fatalf("expected key ID from verified token")
	}

	jwk, err := ToJWK(kp.Algorithm, kp.KeyID, kp.PublicKey)
	if err != nil {
		t.Fatalf("ToJWK failed: %v", err)
	}
	if !strings.Contains(jwk, "\"kid\"") {
		t.Fatalf("expected JWK to include kid, got %s", jwk)
	}
}

func TestJWT_SignMarshalError(t *testing.T) {
	_, err := Sign(Claims{
		Custom: map[string]interface{}{
			"bad": func() {},
		},
	}, []byte("x"), "ES256", "kid")
	if err == nil {
		t.Fatalf("expected Sign to fail when claims JSON cannot be marshaled")
	}
}

func TestJWT_VerifyErrorMappings(t *testing.T) {
	kp, err := GenerateECKeyPair("kid-errors")
	if err != nil {
		t.Fatalf("GenerateECKeyPair failed: %v", err)
	}

	now := time.Now().Unix()

	expiredToken, err := Sign(Claims{
		Issuer:    "aegion",
		Audience:  "api",
		IssuedAt:  now - 120,
		ExpiresAt: now - 60,
	}, kp.PrivateKey, kp.Algorithm, kp.KeyID)
	if err != nil {
		t.Fatalf("Sign expired token failed: %v", err)
	}
	if _, err := Verify(expiredToken, kp.PublicKey, kp.Algorithm, VerifyOptions{
		Issuer:   "aegion",
		Audience: "api",
	}); err == nil || (!errors.Is(err, ErrTokenExpired) && !errors.Is(err, ErrVerifyFailed)) {
		t.Fatalf("expected expired token verification error, got %v", err)
	}

	notYetValidToken, err := Sign(Claims{
		Issuer:    "aegion",
		Audience:  "api",
		IssuedAt:  now,
		NotBefore: now + 3600,
		ExpiresAt: now + 7200,
	}, kp.PrivateKey, kp.Algorithm, kp.KeyID)
	if err != nil {
		t.Fatalf("Sign not-yet-valid token failed: %v", err)
	}
	if _, err := Verify(notYetValidToken, kp.PublicKey, kp.Algorithm, VerifyOptions{
		Issuer:   "aegion",
		Audience: "api",
	}); err == nil || (!errors.Is(err, ErrTokenNotYetValid) && !errors.Is(err, ErrVerifyFailed)) {
		t.Fatalf("expected not-yet-valid token verification error, got %v", err)
	}

	validToken, err := Sign(Claims{
		Issuer:    "aegion",
		Audience:  "api",
		IssuedAt:  now,
		ExpiresAt: now + 3600,
	}, kp.PrivateKey, kp.Algorithm, kp.KeyID)
	if err != nil {
		t.Fatalf("Sign valid token failed: %v", err)
	}
	if _, err := Verify(validToken, kp.PublicKey, "RS256", VerifyOptions{
		Issuer:   "aegion",
		Audience: "api",
	}); err == nil || (!errors.Is(err, ErrInvalidAlg) && !errors.Is(err, ErrVerifyFailed)) {
		t.Fatalf("expected invalid algorithm error, got %v", err)
	}

	if _, err := Verify("not-a-jwt", kp.PublicKey, kp.Algorithm, VerifyOptions{}); err == nil || (!errors.Is(err, ErrInvalidToken) && !errors.Is(err, ErrVerifyFailed)) {
		t.Fatalf("expected invalid token error, got %v", err)
	}
}
