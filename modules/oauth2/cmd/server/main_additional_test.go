package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"

	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainVersionFlagReturnsEarly(t *testing.T) {
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	os.Args = []string{"oauth2-server", "-version"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	main()
}

func TestAccessTokenValidator_MissingJTIAndIssuerMismatch(t *testing.T) {
	issuer := "https://issuer.example.com"
	keyPair, err := platformjwt.GenerateECKeyPair("validator-kid-extra")
	require.NoError(t, err)

	t.Run("token missing jti", func(t *testing.T) {
		token, err := platformjwt.Sign(platformjwt.Claims{
			Issuer:    issuer,
			Subject:   "identity-1",
			IssuedAt:  time.Now().UTC().Unix(),
			ExpiresAt: time.Now().UTC().Add(time.Minute).Unix(),
		}, keyPair.PrivateKey, keyPair.Algorithm, keyPair.KeyID)
		require.NoError(t, err)

		v := &accessTokenValidator{
			store:     &mockLookupStore{},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err = v.ValidateAccessToken(context.Background(), token)
		require.Error(t, err)
		assert.ErrorContains(t, err, "missing jti")
	})

	t.Run("issuer mismatch", func(t *testing.T) {
		token := signAccessTokenForTest(t, keyPair, issuer, "identity-1", "jti-issuer-mismatch", time.Now().UTC().Add(time.Minute))
		v := &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-issuer-mismatch",
					Subject:   "identity-1",
					Issuer:    "https://different.example.com",
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err := v.ValidateAccessToken(context.Background(), token)
		require.Error(t, err)
		assert.ErrorContains(t, err, "issuer mismatch")
	})
}

func TestOAuth2JWTSignerAdditionalPaths(t *testing.T) {
	keyPair, err := platformjwt.GenerateECKeyPair("signer-kid-extra")
	require.NoError(t, err)

	t.Run("SignIDToken works", func(t *testing.T) {
		signer := &oauth2JWTSigner{keyPair: keyPair}
		token, err := signer.SignIDToken(map[string]interface{}{
			"iss": "https://issuer.example.com",
			"sub": "identity-1",
			"iat": time.Now().UTC().Unix(),
			"exp": time.Now().UTC().Add(time.Minute).Unix(),
			"jti": "id-token-jti",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("nil receiver and nil keypair return config error", func(t *testing.T) {
		var nilSigner *oauth2JWTSigner
		_, err := nilSigner.SignAccessToken(map[string]interface{}{"iss": "https://issuer.example.com"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "not configured")

		signer := &oauth2JWTSigner{}
		_, err = signer.SignIDToken(map[string]interface{}{"iss": "https://issuer.example.com"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "not configured")
	})

	t.Run("invalid claims bubble up", func(t *testing.T) {
		signer := &oauth2JWTSigner{keyPair: keyPair}
		_, err := signer.SignAccessToken(map[string]interface{}{"iss": 123})
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid iss")
	})
}

func TestDisabledJWTAssertionValidator(t *testing.T) {
	v := &disabledJWTAssertionValidator{}
	claims, err := v.ValidateJWTAssertion(context.Background(), "assertion", "client-1")
	assert.Nil(t, claims)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not configured")
}

func TestMapClaimsToJWTClaimsAndNormalizers(t *testing.T) {
	t.Run("success with all known claims and custom", func(t *testing.T) {
		claims, err := mapClaimsToJWTClaims(map[string]interface{}{
			"iss":    "https://issuer.example.com",
			"sub":    "identity-1",
			"aud":    []interface{}{"aud-1"},
			"exp":    json.Number("1700000000"),
			"nbf":    int32(1700000001),
			"iat":    int(1700000002),
			"jti":    "jti-1",
			"sid":    "sid-1",
			"custom": true,
		})
		require.NoError(t, err)
		assert.Equal(t, "https://issuer.example.com", claims.Issuer)
		assert.Equal(t, "identity-1", claims.Subject)
		assert.Equal(t, "aud-1", claims.Audience)
		assert.Equal(t, int64(1700000000), claims.ExpiresAt)
		assert.Equal(t, int64(1700000001), claims.NotBefore)
		assert.Equal(t, int64(1700000002), claims.IssuedAt)
		assert.Equal(t, "jti-1", claims.JWTID)
		assert.Equal(t, "sid-1", claims.SessionID)
		assert.Equal(t, true, claims.Custom["custom"])
	})

	t.Run("invalid claim types return errors", func(t *testing.T) {
		cases := []struct {
			name  string
			input map[string]interface{}
			want  string
		}{
			{name: "iss invalid", input: map[string]interface{}{"iss": 1}, want: "invalid iss claim type"},
			{name: "sub invalid", input: map[string]interface{}{"sub": 1}, want: "invalid sub claim type"},
			{name: "aud invalid type", input: map[string]interface{}{"aud": true}, want: "invalid aud claim type"},
			{name: "aud invalid item", input: map[string]interface{}{"aud": []interface{}{1}}, want: "invalid aud claim item type"},
			{name: "exp invalid type", input: map[string]interface{}{"exp": "x"}, want: "invalid exp claim type"},
			{name: "exp invalid number", input: map[string]interface{}{"exp": json.Number("x")}, want: "invalid exp claim value"},
			{name: "nbf invalid type", input: map[string]interface{}{"nbf": "x"}, want: "invalid nbf claim type"},
			{name: "iat invalid type", input: map[string]interface{}{"iat": "x"}, want: "invalid iat claim type"},
			{name: "jti invalid", input: map[string]interface{}{"jti": 1}, want: "invalid jti claim type"},
			{name: "sid invalid", input: map[string]interface{}{"sid": 1}, want: "invalid sid claim type"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := mapClaimsToJWTClaims(tc.input)
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.want)
			})
		}
	})

	t.Run("normalizeAudienceClaim branches", func(t *testing.T) {
		aud, err := normalizeAudienceClaim("aud-1")
		require.NoError(t, err)
		assert.Equal(t, "aud-1", aud)

		aud, err = normalizeAudienceClaim([]string{})
		require.NoError(t, err)
		assert.Equal(t, "", aud)

		aud, err = normalizeAudienceClaim([]string{"aud-2", "aud-3"})
		require.NoError(t, err)
		assert.Equal(t, "aud-2", aud)

		aud, err = normalizeAudienceClaim([]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", aud)

		aud, err = normalizeAudienceClaim([]interface{}{"aud-4"})
		require.NoError(t, err)
		assert.Equal(t, "aud-4", aud)

		_, err = normalizeAudienceClaim([]interface{}{123})
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid aud claim item type")

		_, err = normalizeAudienceClaim(123)
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid aud claim type")
	})

	t.Run("normalizeUnixClaim branches", func(t *testing.T) {
		v, err := normalizeUnixClaim("exp", int64(10))
		require.NoError(t, err)
		assert.Equal(t, int64(10), v)

		v, err = normalizeUnixClaim("exp", int(11))
		require.NoError(t, err)
		assert.Equal(t, int64(11), v)

		v, err = normalizeUnixClaim("exp", int32(12))
		require.NoError(t, err)
		assert.Equal(t, int64(12), v)

		v, err = normalizeUnixClaim("exp", float64(13.9))
		require.NoError(t, err)
		assert.Equal(t, int64(13), v)

		v, err = normalizeUnixClaim("exp", json.Number("14"))
		require.NoError(t, err)
		assert.Equal(t, int64(14), v)

		_, err = normalizeUnixClaim("exp", json.Number("bad"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid exp claim value")

		_, err = normalizeUnixClaim("exp", "bad")
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid exp claim type")
	})
}

func TestNewOAuth2SigningKeyPairAdditionalPaths(t *testing.T) {
	resetSigningEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("AEGION_ENV", "")
		t.Setenv("APP_ENV", "")
		t.Setenv("ENV", "")
		t.Setenv(signingKeyIDEnv, "")
		t.Setenv(signingPrivateKeyB64Env, "")
		t.Setenv(signingPublicKeyB64Env, "")
	}

	t.Run("non-production generates ephemeral keypair", func(t *testing.T) {
		resetSigningEnv(t)
		keyPair, err := newOAuth2SigningKeyPair()
		require.NoError(t, err)
		require.NotNil(t, keyPair)
		assert.NotEmpty(t, keyPair.PrivateKey)
		assert.NotEmpty(t, keyPair.PublicKey)
		assert.NotEmpty(t, keyPair.Algorithm)
		assert.NoError(t, validateSigningKeyPair(keyPair))
	})

	t.Run("invalid private key encoding", func(t *testing.T) {
		resetSigningEnv(t)
		t.Setenv(signingPrivateKeyB64Env, "%%%not-base64%%%")
		t.Setenv(signingPublicKeyB64Env, base64.StdEncoding.EncodeToString([]byte("public")))

		_, err := newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "decode "+signingPrivateKeyB64Env)
	})

	t.Run("invalid public key encoding", func(t *testing.T) {
		resetSigningEnv(t)
		t.Setenv(signingPrivateKeyB64Env, base64.StdEncoding.EncodeToString([]byte("private")))
		t.Setenv(signingPublicKeyB64Env, "%%%not-base64%%%")

		_, err := newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "decode "+signingPublicKeyB64Env)
	})

	t.Run("mismatched static keypair fails validation", func(t *testing.T) {
		resetSigningEnv(t)
		kp1, err := platformjwt.GenerateECKeyPair("kp1")
		require.NoError(t, err)
		kp2, err := platformjwt.GenerateECKeyPair("kp2")
		require.NoError(t, err)

		t.Setenv(signingPrivateKeyB64Env, base64.StdEncoding.EncodeToString(kp1.PrivateKey))
		t.Setenv(signingPublicKeyB64Env, base64.StdEncoding.EncodeToString(kp2.PublicKey))

		_, err = newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "signing keypair verification failed")
	})
}

func TestValidateSigningKeyPairErrorsAndDecodingHelpers(t *testing.T) {
	t.Run("validateSigningKeyPair fails sign with invalid private key", func(t *testing.T) {
		err := validateSigningKeyPair(&platformjwt.KeyPair{
			Algorithm:  defaultSigningAlgorithm,
			KeyID:      "bad-private",
			PrivateKey: []byte("not-a-private-key"),
			PublicKey:  []byte("also-not-a-public-key"),
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "signing key self-test failed")
	})

	t.Run("validateSigningKeyPair fails verify with mismatched public key", func(t *testing.T) {
		kp1, err := platformjwt.GenerateECKeyPair("one")
		require.NoError(t, err)
		kp2, err := platformjwt.GenerateECKeyPair("two")
		require.NoError(t, err)

		err = validateSigningKeyPair(&platformjwt.KeyPair{
			Algorithm:  kp1.Algorithm,
			KeyID:      "mismatch",
			PrivateKey: kp1.PrivateKey,
			PublicKey:  kp2.PublicKey,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "signing keypair verification failed")
	})

	t.Run("decodeBase64Key supports multiple encodings and errors", func(t *testing.T) {
		plain := []byte("hello-world")
		for _, encoded := range []string{
			base64.StdEncoding.EncodeToString(plain),
			base64.RawStdEncoding.EncodeToString(plain),
			base64.URLEncoding.EncodeToString(plain),
			base64.RawURLEncoding.EncodeToString(plain),
		} {
			decoded, err := decodeBase64Key(encoded)
			require.NoError(t, err)
			assert.Equal(t, plain, decoded)
		}

		_, err := decodeBase64Key("%%%invalid%%%")
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid base64 key encoding")
	})

	t.Run("newStaticJWKSProvider returns build errors", func(t *testing.T) {
		_, err := newStaticJWKSProvider(&platformjwt.KeyPair{
			Algorithm:  defaultSigningAlgorithm,
			KeyID:      "no-public-key",
			PrivateKey: []byte("ignored"),
			PublicKey:  nil,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "build jwk")
	})
}
