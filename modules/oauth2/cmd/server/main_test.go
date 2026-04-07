package main

import (
	"context"
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
}

func (m *mockLookupStore) GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.token != nil {
		return m.token, nil
	}
	return nil, store.ErrNotFound
}

func TestAccessTokenValidator(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		v := &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:        "jti-1",
					ClientID:   "client-1",
					IdentityID: "identity-1",
					Scopes:     []string{"openid"},
					ExpiresAt:  time.Now().UTC().Add(time.Minute),
				},
			},
		}
		got, err := v.ValidateAccessToken(context.Background(), "jti-1")
		require.NoError(t, err)
		assert.Equal(t, "identity-1", got.IdentityID)
	})

	t.Run("store and inactive errors", func(t *testing.T) {
		v := &accessTokenValidator{store: &mockLookupStore{err: errors.New("db down")}}
		_, err := v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "db down")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					Revoked:   true,
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
		}
		_, err = v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "inactive")

		v = &accessTokenValidator{
			store: &mockLookupStore{
				token: &store.AccessToken{
					JTI:       "jti-1",
					ExpiresAt: time.Now().UTC().Add(-time.Second),
				},
			},
		}
		_, err = v.ValidateAccessToken(context.Background(), "jti-1")
		assert.ErrorContains(t, err, "inactive")
	})
}

func TestUserInfoProviderAndHelpers(t *testing.T) {
	p := &userInfoProvider{}
	info, err := p.GetUserInfo(context.Background(), "identity-1", []string{"openid", "profile", "email"})
	require.NoError(t, err)
	assert.Equal(t, "identity-1", info.Sub)
	require.NotNil(t, info.Name)
	require.NotNil(t, info.Email)

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
	origBuildHandlerHook := buildHandlerHook
	origNewHTTPServerHook := newHTTPServerHook
	origNotifySignalsHook := notifySignalsHook
	origStopSignalsHook := stopSignalsHook
	origListenAndServeHook := listenAndServeHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		loadConfigHook = origLoadConfigHook
		connectDBHook = origConnectDBHook
		buildHandlerHook = origBuildHandlerHook
		newHTTPServerHook = origNewHTTPServerHook
		notifySignalsHook = origNotifySignalsHook
		stopSignalsHook = origStopSignalsHook
		listenAndServeHook = origListenAndServeHook
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	loadConfigHook = func(path string) (*Config, error) {
		cfg := &Config{}
		applyDefaults(cfg)
		cfg.Server.Address = "127.0.0.1"
		cfg.Server.Port = 0
		return cfg, nil
	}
	connectDBHook = func(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
		return pool, nil
	}
	buildInvoked := false
	buildHandlerHook = func(cfg *Config, oauthStore *store.Store) *handler.OAuth2Handler {
		buildInvoked = true
		require.NotNil(t, oauthStore)
		return &handler.OAuth2Handler{}
	}
	newHTTPServerHook = func(cfg *Config, oauthHandler *handler.OAuth2Handler) *http.Server {
		require.NotNil(t, oauthHandler)
		return &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	}
	notifySignalsHook = func(c chan<- os.Signal, sig ...os.Signal) {
		c <- os.Interrupt
	}
	stopSignalsHook = func(c chan<- os.Signal) {}
	listenAndServeHook = func(srv *http.Server) error {
		return http.ErrServerClosed
	}

	os.Args = []string{"oauth2-server"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	main()

	assert.True(t, buildInvoked, "expected buildHandlerHook to be invoked")
}
