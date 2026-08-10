package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/platform/securefile"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/mfa/handler"
	"github.com/aegion/aegion/modules/mfa/service"
	"github.com/aegion/aegion/modules/mfa/store"
)

const (
	listenAddrEnv                = "AEGION_MFA_HTTP_LISTEN_ADDR"
	databaseURLEnv               = "AEGION_MFA_DATABASE_URL"
	cipherSecretEnv              = "AEGION_SECRETS_CIPHER"
	legacyCipherSecretEnv        = "AEGION_SECRET_CIPHER_1"
	identitySigningSecretFileEnv = "AEGION_MODULE_IDENTITY_SIGNING_SECRET_FILE"
	defaultListen                = "0.0.0.0:9003"
	moduleVersion                = "0.2.0"
	startupTimeout               = 5 * time.Second
)

var (
	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) {
		if pool != nil {
			pool.Close()
		}
	}
	logFatal = func(v ...any) {
		if len(v) == 0 {
			xlog.Default().Fatal("mfa server failed")
			return
		}
		if err, ok := v[0].(error); ok {
			xlog.Default().Fatal(err.Error(), "error", err)
			return
		}
		xlog.Default().Fatal("mfa server failed", v...)
	}
)

type moduleRuntime struct {
	registerHTTPRoutes func(*http.ServeMux)
	readiness          func(context.Context) error
	cleanup            func()
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, runtime *moduleRuntime) moduleserver.Config {
	return moduleserver.Config{
		Module:       "mfa",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"totp", "backup_codes", "trusted_devices"},
		Routes: []string{
			"/api/v1/mfa/totp/start",
			"/api/v1/mfa/totp/finish",
			"/api/v1/mfa/totp/verify",
			"/api/v1/mfa/backup/verify",
			"/api/v1/mfa/backup/regenerate",
			"/api/v1/mfa/trusted-device",
		},
		RegisterHTTPRoutes: runtime.registerHTTPRoutes,
		Readiness:          runtime.readiness,
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	runtime, err := buildRuntimeHook(context.Background())
	if err != nil {
		logFatal(err)
		return
	}
	err = runModuleServer(moduleConfig(*listenAddr, runtime))
	runtime.cleanup()
	if err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	dbURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if dbURL == "" {
		return nil, fmt.Errorf("%s must be configured", databaseURLEnv)
	}
	cipherKey, err := deriveCipherKey()
	if err != nil {
		return nil, err
	}
	identitySigningSecret, err := readIdentitySigningSecret()
	if err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse MFA database URL: %w", err)
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1
	pool, err := newPoolWithConfigHook(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("connect MFA database: %w", err)
	}

	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := checkSchema(startupCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, fmt.Errorf("MFA database is not ready: %w", err)
	}

	repository, err := store.NewPostgres(pool)
	if err != nil {
		poolCloseHook(pool)
		return nil, fmt.Errorf("create MFA repository: %w", err)
	}
	mfaService := service.New(repository, service.Config{CipherKey: cipherKey})
	mfaHandler := handler.New(mfaService, handler.WithIdentitySigningSecret(identitySigningSecret))

	return &moduleRuntime{
		registerHTTPRoutes: mfaHandler.RegisterRoutes,
		readiness: func(ctx context.Context) error {
			return checkSchema(ctx, pool)
		},
		cleanup: func() {
			poolCloseHook(pool)
		},
	}, nil
}

func deriveCipherKey() ([]byte, error) {
	secret := strings.TrimSpace(os.Getenv(cipherSecretEnv))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv(legacyCipherSecretEnv))
	}
	if secret == "" {
		return nil, fmt.Errorf("%s or %s must be configured", cipherSecretEnv, legacyCipherSecretEnv)
	}
	key := platformcrypto.SHA256Digest([]byte(secret))
	return key[:], nil
}

func readIdentitySigningSecret() ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(identitySigningSecretFileEnv))
	if path == "" {
		return nil, fmt.Errorf("%s must be configured", identitySigningSecretFileEnv)
	}
	const maxSecretBytes = 4096
	secret, err := securefile.ReadRegularFile(path, maxSecretBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", identitySigningSecretFileEnv, err)
	}
	secret = []byte(strings.TrimSpace(string(secret)))
	if len(secret) < 32 {
		return nil, fmt.Errorf("%s must contain at least 32 bytes", identitySigningSecretFileEnv)
	}
	return secret, nil
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("MFA database pool is unavailable")
	}
	if err := poolPingHook(ctx, pool); err != nil {
		return fmt.Errorf("ping MFA database: %w", err)
	}

	var missing []string
	for _, relation := range []string{
		"mfa_enrollments",
		"mfa_totp_factors",
		"mfa_backup_codes",
		"mfa_trusted_devices",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", relation).Scan(&exists); err != nil {
			return fmt.Errorf("check MFA relation %s: %w", relation, err)
		}
		if !exists {
			missing = append(missing, relation)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("MFA schema missing required relations: %s", strings.Join(missing, ", "))
	}
	return nil
}
