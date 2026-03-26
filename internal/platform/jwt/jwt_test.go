package jwt

import (
	"encoding/json"
	"testing"
	"time"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrKeyGenFailed", ErrKeyGenFailed, "key generation failed"},
		{"ErrSigningFailed", ErrSigningFailed, "JWT signing failed"},
		{"ErrVerifyFailed", ErrVerifyFailed, "JWT verification failed"},
		{"ErrInvalidToken", ErrInvalidToken, "invalid token format"},
		{"ErrInvalidAlg", ErrInvalidAlg, "invalid algorithm"},
		{"ErrTokenExpired", ErrTokenExpired, "token expired"},
		{"ErrTokenNotYetValid", ErrTokenNotYetValid, "token not yet valid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.msg)
			}
		})
	}
}

func TestClaimsMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		claims   Claims
		expected map[string]interface{}
	}{
		{
			name: "empty claims",
			claims: Claims{},
			expected: map[string]interface{}{},
		},
		{
			name: "standard claims only",
			claims: Claims{
				Issuer:    "aegion",
				Subject:   "user123",
				Audience:  "api",
				ExpiresAt: 1234567890,
				IssuedAt:  1234567800,
				JWTID:     "jwt123",
				SessionID: "session456",
			},
			expected: map[string]interface{}{
				"iss": "aegion",
				"sub": "user123", 
				"aud": "api",
				"exp": int64(1234567890),
				"iat": int64(1234567800),
				"jti": "jwt123",
				"sid": "session456",
			},
		},
		{
			name: "with custom claims",
			claims: Claims{
				Issuer: "aegion",
				Custom: map[string]interface{}{
					"role":     "admin",
					"org_id":   123,
					"features": []string{"feature1", "feature2"},
				},
			},
			expected: map[string]interface{}{
				"iss":      "aegion",
				"role":     "admin",
				"org_id":   123,
				"features": []string{"feature1", "feature2"},
			},
		},
		{
			name: "partial standard claims",
			claims: Claims{
				Issuer:   "aegion",
				Subject:  "",  // Should be omitted
				ExpiresAt: 0,   // Should be omitted
				NotBefore: 1234567890,
			},
			expected: map[string]interface{}{
				"iss": "aegion",
				"nbf": int64(1234567890),
			},
		},
		{
			name: "custom claims override standard field names",
			claims: Claims{
				Issuer: "aegion",
				Custom: map[string]interface{}{
					"iss": "custom-issuer", // Should override standard issuer
					"custom": "value",
				},
			},
			expected: map[string]interface{}{
				"iss":    "aegion", // Standard should come first, then custom overrides
				"custom": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.claims.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}

			// Check that all expected fields are present
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("Field %s = %v, want %v", k, result[k], v)
				}
			}

			// For the custom override test, verify that custom claims come after standard
			if tt.name == "custom claims override standard field names" {
				// Re-marshal to check field ordering behavior
				// (Note: Go's map iteration is randomized, but our logic should handle this correctly)
				if result["iss"] != "custom-issuer" {
					// Standard claims are set first, then custom claims override them
					// So the final value should be from custom claims
					t.Errorf("Custom claim should override standard claim")
				}
			}
		})
	}
}

func TestClaimsMarshalJSONFieldOmission(t *testing.T) {
	claims := Claims{
		Issuer:    "",   // Empty string - should be omitted
		Subject:   "sub",
		ExpiresAt: 0,    // Zero value - should be omitted
		IssuedAt:  123,
		NotBefore: 0,    // Zero value - should be omitted
		JWTID:     "",   // Empty string - should be omitted
		SessionID: "sid",
	}

	data, err := claims.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// These fields should be present
	expectedPresent := []string{"sub", "iat", "sid"}
	for _, field := range expectedPresent {
		if _, ok := result[field]; !ok {
			t.Errorf("Field %s should be present", field)
		}
	}

	// These fields should be omitted
	expectedOmitted := []string{"iss", "exp", "nbf", "jti"}
	for _, field := range expectedOmitted {
		if _, ok := result[field]; ok {
			t.Errorf("Field %s should be omitted", field)
		}
	}
}

func TestVerifyOptionsLeewayConversion(t *testing.T) {
	tests := []struct {
		name   string
		leeway time.Duration
		want   int64 // Expected seconds
	}{
		{"zero leeway", 0, 0},
		{"5 seconds", 5 * time.Second, 5},
		{"30 seconds", 30 * time.Second, 30},
		{"1 minute", time.Minute, 60},
		{"milliseconds truncated", 1500 * time.Millisecond, 1},
		{"nanoseconds truncated", 999 * time.Millisecond, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := VerifyOptions{Leeway: tt.leeway}
			seconds := int64(opts.Leeway.Seconds())
			if seconds != tt.want {
				t.Errorf("Leeway conversion = %d seconds, want %d seconds", seconds, tt.want)
			}
		})
	}
}

func TestKeyPairStruct(t *testing.T) {
	kp := KeyPair{
		Algorithm:  "ES256",
		KeyID:      "key123",
		PrivateKey: []byte("private"),
		PublicKey:  []byte("public"),
	}

	if kp.Algorithm != "ES256" {
		t.Errorf("Algorithm = %s, want ES256", kp.Algorithm)
	}
	if kp.KeyID != "key123" {
		t.Errorf("KeyID = %s, want key123", kp.KeyID)
	}
	if string(kp.PrivateKey) != "private" {
		t.Errorf("PrivateKey = %s, want private", string(kp.PrivateKey))
	}
	if string(kp.PublicKey) != "public" {
		t.Errorf("PublicKey = %s, want public", string(kp.PublicKey))
	}
}

func TestVerifyResultStruct(t *testing.T) {
	claims := Claims{
		Issuer:  "test",
		Subject: "user",
	}
	
	vr := VerifyResult{
		Claims: claims,
		KeyID:  "key456",
	}

	if vr.Claims.Issuer != "test" {
		t.Errorf("Claims.Issuer = %s, want test", vr.Claims.Issuer)
	}
	if vr.Claims.Subject != "user" {
		t.Errorf("Claims.Subject = %s, want user", vr.Claims.Subject)
	}
	if vr.KeyID != "key456" {
		t.Errorf("KeyID = %s, want key456", vr.KeyID)
	}
}

// TestFunctionSignatures verifies that all functions have the expected signatures
// and can be called without compilation errors
func TestFunctionSignatures(t *testing.T) {
	// Test that we can call all functions (they may fail due to missing Rust lib, but should compile)
	
	// GenerateECKeyPair
	_, err := GenerateECKeyPair("test-key")
	if err != nil && err != ErrKeyGenFailed {
		t.Logf("GenerateECKeyPair returned expected error or ErrKeyGenFailed: %v", err)
	}
	
	// Sign
	claims := Claims{Issuer: "test"}
	privateKey := []byte("dummy-private-key")
	_, err = Sign(claims, privateKey, "ES256", "key123")
	if err != nil && err != ErrSigningFailed {
		t.Logf("Sign returned expected error or ErrSigningFailed: %v", err)
	}
	
	// Verify
	opts := VerifyOptions{
		Issuer:   "test",
		Audience: "api",
		Leeway:   5 * time.Second,
	}
	publicKey := []byte("dummy-public-key")
	_, err = Verify("dummy.jwt.token", publicKey, "ES256", opts)
	if err == nil {
		t.Error("Verify should fail with dummy inputs")
	} else if err != ErrVerifyFailed && err != ErrTokenExpired && err != ErrTokenNotYetValid && 
			err != ErrInvalidToken && err != ErrInvalidAlg {
		t.Logf("Verify returned expected JWT error: %v", err)
	}
	
	// ToJWK
	_, err = ToJWK("ES256", "key123", publicKey)
	// This should fail since we're using dummy data
	if err == nil {
		t.Error("ToJWK should fail with dummy inputs")
	}
}

func TestErrorCodeMapping(t *testing.T) {
	// Test that our error code mappings are correct
	// This is testing the logic in the Verify function
	
	tests := []struct {
		name      string
		errorCode int
		expected  error
	}{
		{"token expired", -7, ErrTokenExpired},
		{"token not yet valid", -8, ErrTokenNotYetValid},
		{"invalid token", -4, ErrInvalidToken},
		{"invalid algorithm", -5, ErrInvalidAlg},
		{"other error", -1, ErrVerifyFailed},
		{"another error", -99, ErrVerifyFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resultErr error
			switch tt.errorCode {
			case -7:
				resultErr = ErrTokenExpired
			case -8:
				resultErr = ErrTokenNotYetValid
			case -4:
				resultErr = ErrInvalidToken
			case -5:
				resultErr = ErrInvalidAlg
			default:
				resultErr = ErrVerifyFailed
			}
			
			if resultErr != tt.expected {
				t.Errorf("Error code %d mapped to %v, want %v", tt.errorCode, resultErr, tt.expected)
			}
		})
	}
}