package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/modules/oauth2/handler"
	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, 8083, cfg.Server.Port)
	assert.Equal(t, int32(20), cfg.Database.MaxConns)
	assert.Equal(t, int32(2), cfg.Database.MinConns)
	assert.Equal(t, "http://localhost:8083", cfg.OAuth2.Issuer)
	assert.Equal(t, "http://localhost:8083", cfg.OAuth2.BaseURL)
	assert.Equal(t, 10*time.Minute, cfg.OAuth2.DeviceCodeTTL)
	assert.Equal(t, 5, cfg.OAuth2.DevicePollInterval)
	assert.Equal(t, "http://localhost:8083/oauth2/device/verify", cfg.OAuth2.DeviceVerificationURI)
}

func TestApplyDefaults_TrimBaseURL(t *testing.T) {
	cfg := &Config{}
	cfg.OAuth2.Issuer = "https://issuer.example.com"
	cfg.OAuth2.BaseURL = "https://issuer.example.com/"
	applyDefaults(cfg)
	assert.Equal(t, "https://issuer.example.com", cfg.OAuth2.BaseURL)
}

type mockLookupStore struct {
	token *store.AccessToken
	err   error
	last  string
}

func (m *mockLookupStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	m.last = jti
	if m.err != nil {
		return nil, m.err
	}
	if m.token != nil {
		return m.token, nil
	}
	return nil, store.ErrNotFound
}

func signAccessTokenForTest(t *testing.T, keyPair *platformjwt.KeyPair, issuer, subject, jti string, expiresAt time.Time) string {
	t.Helper()
	token, err := platformjwt.Sign(platformjwt.Claims{
		Issuer:    issuer,
		Subject:   subject,
		JWTID:     jti,
		IssuedAt:  time.Now().UTC().Unix(),
		ExpiresAt: expiresAt.Unix(),
	}, keyPair.PrivateKey, keyPair.Algorithm, keyPair.KeyID)
	require.NoError(t, err)
	return token
}

func TestAccessTokenValidator(t *testing.T) {
	issuer := "https://issuer.example.com"
	keyPair, err := platformjwt.GenerateECKeyPair("validator-kid")
	require.NoError(t, err)
	validToken := signAccessTokenForTest(t, keyPair, issuer, "identity-1", "jti-1", time.Now().UTC().Add(time.Minute))

	t.Run("success", func(t *testing.T) {
		lookup := &mockLookupStore{
			token: &store.AccessToken{
				JTI:        "jti-1",
				ClientID:   "client-1",
				IdentityID: "identity-1",
				Scopes:     []string{"openid"},
				Subject:    "identity-1",
				Issuer:     issuer,
				ExpiresAt:  time.Now().UTC().Add(time.Minute),
			},
		}
		v := &accessTokenValidator{
			store:     lookup,
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		got, err := v.ValidateAccessToken(context.Background(), validToken)
		require.NoError(t, err)
		assert.Equal(t, "identity-1", got.IdentityID)
		assert.Equal(t, "jti-1", lookup.last)
	})

	t.Run("invalid signature and claim mismatch errors", func(t *testing.T) {
		otherKeyPair, err := platformjwt.GenerateECKeyPair("other-kid")
		require.NoError(t, err)
		tampered := signAccessTokenForTest(t, otherKeyPair, issuer, "identity-1", "jti-1", time.Now().UTC().Add(time.Minute))

		v := &accessTokenValidator{
			store:     &mockLookupStore{},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err = v.ValidateAccessToken(context.Background(), tampered)
		assert.Error(t, err)

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					Subject:   "different",
					Issuer:    issuer,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err = v.ValidateAccessToken(context.Background(), validToken)
		assert.ErrorContains(t, err, "subject mismatch")
	})

	t.Run("store and inactive errors", func(t *testing.T) {
		v := &accessTokenValidator{
			store:     &mockLookupStore{err: errors.New("db down")},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err := v.ValidateAccessToken(context.Background(), validToken)
		assert.ErrorContains(t, err, "db down")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					Revoked:   true,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err = v.ValidateAccessToken(context.Background(), validToken)
		assert.ErrorContains(t, err, "inactive")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					ExpiresAt: time.Now().UTC().Add(-time.Second),
				},
			},
			publicKey: keyPair.PublicKey,
			algorithm: keyPair.Algorithm,
			issuer:    issuer,
		}
		_, err = v.ValidateAccessToken(context.Background(), validToken)
		assert.ErrorContains(t, err, "inactive")
	})
}

func TestNewOAuth2SigningKeyPair(t *testing.T) {
	t.Run("loads configured key pair from env", func(t *testing.T) {
		source, err := platformjwt.GenerateECKeyPair("source-kid")
		require.NoError(t, err)

		t.Setenv(signingKeyIDEnv, "configured-kid")
		t.Setenv(signingPrivateKeyB64Env, base64.StdEncoding.EncodeToString(source.PrivateKey))
		t.Setenv(signingPublicKeyB64Env, base64.StdEncoding.EncodeToString(source.PublicKey))

		got, err := newOAuth2SigningKeyPair()
		require.NoError(t, err)
		assert.Equal(t, "configured-kid", got.KeyID)
		assert.Equal(t, defaultSigningAlgorithm, got.Algorithm)
		assert.Equal(t, source.PrivateKey, got.PrivateKey)
		assert.Equal(t, source.PublicKey, got.PublicKey)
	})

	t.Run("rejects partial key configuration", func(t *testing.T) {
		t.Setenv(signingPrivateKeyB64Env, "Zm9v")
		t.Setenv(signingPublicKeyB64Env, "")

		_, err := newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "must be set together")
	})

	t.Run("rejects ephemeral keys in production", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "production")
		t.Setenv(signingPrivateKeyB64Env, "")
		t.Setenv(signingPublicKeyB64Env, "")
		_, err := newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "production requires static OAuth2 signing keys")
	})

	t.Run("rejects ephemeral keys when AEGION_ENVIRONMENT indicates production", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "")
		t.Setenv("APP_ENV", "")
		t.Setenv("ENV", "")
		t.Setenv("AEGION_ENVIRONMENT", "production")
		t.Setenv(signingPrivateKeyB64Env, "")
		t.Setenv(signingPublicKeyB64Env, "")
		_, err := newOAuth2SigningKeyPair()
		require.Error(t, err)
		assert.ErrorContains(t, err, "production requires static OAuth2 signing keys")
	})
}

func TestOAuth2JWTSignerAndJWKSProvider(t *testing.T) {
	keyPair, err := platformjwt.GenerateECKeyPair("runtime-kid")
	require.NoError(t, err)

	signer := &oauth2JWTSigner{keyPair: keyPair}
	token, err := signer.SignAccessToken(map[string]interface{}{
		"iss":   "https://issuer.example.com",
		"sub":   "identity-1",
		"aud":   []string{"aud-1", "aud-2"},
		"iat":   time.Now().UTC().Unix(),
		"exp":   time.Now().UTC().Add(time.Minute).Unix(),
		"jti":   "jti-1",
		"scope": "openid profile",
	})
	require.NoError(t, err)

	verified, err := platformjwt.Verify(token, keyPair.PublicKey, keyPair.Algorithm, platformjwt.VerifyOptions{
		Issuer: "https://issuer.example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "jti-1", verified.Claims.JWTID)
	assert.Equal(t, "aud-1", verified.Claims.Audience)

	provider, err := newStaticJWKSProvider(keyPair)
	require.NoError(t, err)
	keys, err := provider.GetPublicKeys(context.Background())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, keyPair.KeyID, keys[0].KID)
	assert.Equal(t, keyPair.Algorithm, keys[0].ALG)
	assert.Equal(t, "sig", keys[0].USE)
}

func TestUserInfoProviderAndHelpers(t *testing.T) {
	p := &userInfoProvider{}
	info, err := p.GetUserInfo(context.Background(), "identity-1", []string{"openid", "profile", "email"})
	require.NoError(t, err)
	assert.Equal(t, "identity-1", info.Sub)
	assert.Nil(t, info.Name)
	assert.Nil(t, info.Email)

	assert.True(t, containsScope([]string{"a", "b"}, "b"))
	assert.False(t, containsScope([]string{"a", "b"}, "c"))
}

func TestRegisterRoutesAndServer(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	h := &handler.OAuth2Handler{}

	srv := newHTTPServer(cfg, h)
	require.NotNil(t, srv)
	assert.Contains(t, srv.Addr, ":8083")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	srv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "\"status\":\"ok\"")
}

func TestGetEnv(t *testing.T) {
	const key = "AEGION_OAUTH2_TEST_ENV"
	_ = os.Unsetenv(key)
	assert.Equal(t, "fallback", getEnv(key, "fallback"))

	require.NoError(t, os.Setenv(key, "value"))
	defer func() {
		_ = os.Unsetenv(key)
	}()
	assert.Equal(t, "value", getEnv(key, "fallback"))
}

func TestLoadConfig(t *testing.T) {
	t.Run("returns read errors", func(t *testing.T) {
		_, err := loadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "read config")
	})

	t.Run("returns parse errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.yaml")
		require.NoError(t, os.WriteFile(path, []byte("server: [invalid"), 0o644))

		_, err := loadConfig(path)
		require.Error(t, err)
		assert.ErrorContains(t, err, "parse config")
	})

	t.Run("expands env vars and applies defaults", func(t *testing.T) {
		const dbEnvKey = "AEGION_OAUTH2_TEST_DATABASE_URL"
		require.NoError(t, os.Setenv(dbEnvKey, "postgres://demo:demo@127.0.0.1:5432/demo?sslmode=disable"))
		t.Cleanup(func() {
			_ = os.Unsetenv(dbEnvKey)
		})

		configBody := `
database:
  url: ${AEGION_OAUTH2_TEST_DATABASE_URL}
server:
  port: 9100
oauth2:
  issuer: https://issuer.example.com/
`
		path := filepath.Join(t.TempDir(), "oauth2.yaml")
		require.NoError(t, os.WriteFile(path, []byte(configBody), 0o644))

		cfg, err := loadConfig(path)
		require.NoError(t, err)
		assert.Equal(t, "postgres://demo:demo@127.0.0.1:5432/demo?sslmode=disable", cfg.Database.URL)
		assert.Equal(t, 9100, cfg.Server.Port)
		assert.Equal(t, "0.0.0.0", cfg.Server.Address)
		assert.Equal(t, "https://issuer.example.com", cfg.OAuth2.BaseURL)
		assert.Equal(t, "https://issuer.example.com/oauth2/device/verify", cfg.OAuth2.DeviceVerificationURI)
	})
}

func TestConnectDB(t *testing.T) {
	t.Run("returns parse errors", func(t *testing.T) {
		cfg := &Config{}
		cfg.Database.URL = "://bad-url"
		_, err := connectDB(context.Background(), cfg)
		require.Error(t, err)
		assert.ErrorContains(t, err, "parse database url")
	})

	t.Run("returns ping errors", func(t *testing.T) {
		cfg := &Config{}
		cfg.Database.URL = "postgres://demo:demo@127.0.0.1:1/demo?sslmode=disable&connect_timeout=1"
		cfg.Database.MaxConns = 1
		cfg.Database.MinConns = 0

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		db, err := connectDB(ctx, cfg)
		assert.Nil(t, db)
		require.Error(t, err)
	})
}

type adapterMockDB struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (m *adapterMockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (m *adapterMockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return adapterMockRow{err: pgx.ErrNoRows}
}

func (m *adapterMockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return nil, nil
}

type adapterMockRow struct {
	values []any
	err    error
}

func (r adapterMockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan mismatch: got %d destinations for %d values", len(dest), len(r.values))
	}
	for i := range dest {
		if err := assignAdapterScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

func assignAdapterScanValue(dest any, value any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.IsNil() {
		return fmt.Errorf("destination must be a non-nil pointer, got %T", dest)
	}
	target := destValue.Elem()
	if value == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	valueValue := reflect.ValueOf(value)
	if valueValue.Type().AssignableTo(target.Type()) {
		target.Set(valueValue)
		return nil
	}
	if valueValue.Type().ConvertibleTo(target.Type()) {
		target.Set(valueValue.Convert(target.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", value, dest)
}

func deviceCodeRow(deviceCode, userCode string) adapterMockRow {
	expires := time.Now().UTC().Add(5 * time.Minute)
	created := time.Now().UTC()
	return adapterMockRow{
		values: []any{
			deviceCode, userCode, "client-1", []string{"openid"}, []string(nil),
			"https://issuer.example.com/device", (*string)(nil), 5, (*string)(nil), (*string)(nil),
			"pending", (*time.Time)(nil), expires, (*time.Time)(nil), created,
		},
	}
}

func TestDeviceStoreAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("GetDeviceCode returns mapped code", func(t *testing.T) {
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return deviceCodeRow("dc-1", "ABCD-EFGH")
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		dc, err := adapter.GetDeviceCode(ctx, "dc-1")
		require.NoError(t, err)
		require.NotNil(t, dc)
		assert.Equal(t, "dc-1", dc.DeviceCode)
		assert.Equal(t, "ABCD-EFGH", dc.UserCode)
	})

	t.Run("GetDeviceCode returns errors from store", func(t *testing.T) {
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adapterMockRow{err: pgx.ErrNoRows}
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		_, err := adapter.GetDeviceCode(ctx, "missing")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("MarkDeviceCodeApproved approves by user code", func(t *testing.T) {
		var approvedUserCode any
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return deviceCodeRow("dc-2", "IJKL-MNOP")
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_device_codes SET") {
					approvedUserCode = args[0]
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		err := adapter.MarkDeviceCodeApproved(ctx, "dc-2", "identity-1", []string{"openid"})
		require.NoError(t, err)
		assert.Equal(t, "IJKL-MNOP", approvedUserCode)
	})

	t.Run("MarkDeviceCodeApproved returns lookup errors", func(t *testing.T) {
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adapterMockRow{err: pgx.ErrNoRows}
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		err := adapter.MarkDeviceCodeApproved(ctx, "missing", "identity-1", []string{"openid"})
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("MarkDeviceCodeDenied denies by user code", func(t *testing.T) {
		var deniedUserCode any
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return deviceCodeRow("dc-3", "QRST-UVWX")
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_device_codes SET status = 'denied'") {
					deniedUserCode = args[0]
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		err := adapter.MarkDeviceCodeDenied(ctx, "dc-3")
		require.NoError(t, err)
		assert.Equal(t, "QRST-UVWX", deniedUserCode)
	})

	t.Run("MarkDeviceCodeDenied returns lookup errors", func(t *testing.T) {
		oauthStore := store.NewWithDB(&adapterMockDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adapterMockRow{err: pgx.ErrNoRows}
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		err := adapter.MarkDeviceCodeDenied(ctx, "missing")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("MarkDeviceCodeUsed delegates to delete", func(t *testing.T) {
		var deletedCode any
		oauthStore := store.NewWithDB(&adapterMockDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "DELETE FROM oa2_device_codes WHERE device_code = $1") {
					deletedCode = args[0]
				}
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		})
		adapter := &deviceStoreAdapter{Store: oauthStore}

		err := adapter.MarkDeviceCodeUsed(ctx, "dc-4")
		require.NoError(t, err)
		assert.Equal(t, "dc-4", deletedCode)
	})
}

func TestBuildHandler(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	h := buildHandler(cfg, store.NewWithDB(&adapterMockDB{}))
	require.NotNil(t, h)
}

func TestMainWithInjectedHooks(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://demo:demo@127.0.0.1:1/demo?sslmode=disable&connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	origLoadConfigHook := loadConfigHook
	origConnectDBHook := connectDBHook
	origCryptoSelfCheckHook := cryptoSelfCheckHook
	origRunModuleServerHook := runModuleServerHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		loadConfigHook = origLoadConfigHook
		connectDBHook = origConnectDBHook
		cryptoSelfCheckHook = origCryptoSelfCheckHook
		runModuleServerHook = origRunModuleServerHook
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	secretFile := filepath.Join(t.TempDir(), "identity-signing-secret")
	require.NoError(t, os.WriteFile(secretFile, []byte("01234567890123456789012345678901"), 0o600))
	t.Setenv("AEGION_ENV", "development")
	t.Setenv("AEGION_MODULE_IDENTITY_SIGNING_SECRET_FILE", secretFile)
	loadConfigHook = func(path string) (*Config, error) {
		cfg := &Config{}
		applyDefaults(cfg)
		cfg.Database.URL = "postgres://demo:demo@127.0.0.1:1/demo?sslmode=disable"
		cfg.Server.Address = "127.0.0.1"
		cfg.Server.Port = 0
		return cfg, nil
	}
	connectDBHook = func(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
		return pool, nil
	}
	cryptoSelfCheckHook = func() error { return nil }
	runInvoked := false
	runModuleServerHook = func(cfg moduleserver.Config) error {
		runInvoked = true
		assert.Equal(t, "oauth2", cfg.Module)
		assert.Contains(t, cfg.Routes, "/oauth2/device/verify")
		return nil
	}

	os.Args = []string{"oauth2-server"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	assert.True(t, runInvoked, "expected moduleserver runtime to be invoked")
}

func TestTrustedIdentityResolver(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/login", nil)
	req.Header.Set("X-User-ID", "identity-1")
	req.Header.Set("X-User-Session-ID", "session-1")
	req.Header.Set("X-User-AAL", "aal2")
	sig, err := platformcrypto.SignIdentityHeaders(secret, req.Header, []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"}, time.Now().UTC())
	require.NoError(t, err)
	req.Header.Set("X-Aegion-Signature", sig)

	identityID, sessionID, err := newTrustedIdentityResolver(secret, time.Minute)(req)
	require.NoError(t, err)
	assert.Equal(t, "identity-1", identityID)
	assert.Equal(t, "session-1", sessionID)

	req.Header.Set("X-User-ID", "attacker")
	_, _, err = newTrustedIdentityResolver(secret, time.Minute)(req)
	assert.Error(t, err)
}

func TestModuleServerMetadataMatchesMountedOAuthRoutes(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	runtime := newModuleServerConfig(cfg, nil, nil, &handler.OAuth2Handler{}, []byte("01234567890123456789012345678901"))
	assert.Equal(t, []string{"aegion.oauth2.v1.TokenStore"}, runtime.GRPCServices)
	assert.Contains(t, runtime.Routes, "/oauth2/login")
	assert.Contains(t, runtime.Routes, "/oauth2/consent")
	assert.Contains(t, runtime.Routes, "/oauth2/device/verify")
	assert.NotContains(t, runtime.Routes, "/oauth2/logout")
}
