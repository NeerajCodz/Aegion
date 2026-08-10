package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/platform/securefile"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/sso/handler"
	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

const (
	listenAddrEnv                = "AEGION_SSO_HTTP_LISTEN_ADDR"
	defaultListen                = "0.0.0.0:9007"
	moduleVersion                = "0.2.0"
	dbURLEnv                     = "AEGION_SSO_DATABASE_URL"
	stateSecretEnv               = "AEGION_SSO_STATE_SECRET"
	coreGRPCAddrEnv              = "AEGION_CORE_GRPC_ADDR"
	moduleCredentialFileEnv      = "AEGION_MODULE_CREDENTIAL_FILE"
	moduleTLSCertFileEnv         = "AEGION_MODULE_TLS_CERT_FILE"
	moduleTLSKeyFileEnv          = "AEGION_MODULE_TLS_KEY_FILE"
	moduleCACertFileEnv          = "AEGION_MODULE_CA_CERT_FILE"
	identitySigningSecretFileEnv = "AEGION_MODULE_IDENTITY_SIGNING_SECRET_FILE"
	minSecretBytes               = 32
	maxMountedSecretBytes        = 4096
	databaseReadinessTimeout     = 5 * time.Second
)

var (
	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) { pool.Close() }
	newPostgresRepoHook   = store.NewPostgres
	checkSSOSchemaHook    = checkSSOSchema
	logFatal              = func(v ...any) {
		if len(v) == 0 {
			xlog.Default().Fatal("sso server failed")
			return
		}
		if err, ok := v[0].(error); ok {
			xlog.Default().Fatal(err.Error(), "error", err)
			return
		}
		xlog.Default().Fatal(v[0].(string), v[1:]...)
	}
)

type moduleRuntime struct {
	registerHTTPRoutes func(mux *http.ServeMux)
	readiness          func(context.Context) error
	cleanup            func()
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux), readiness ...func(context.Context) error) moduleserver.Config {
	cfg := moduleserver.Config{
		Module:       "sso",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"saml", "connection_registry", "domain_routing"},
		Routes: []string{
			"/api/v1/sso/connections",
			"/api/v1/sso/resolve-domain",
			"/api/v1/sso/admin/connections",
			"/api/v1/sso/admin/connections/{slug}",
			"/self-service/sso/{connection}/start",
			"/self-service/sso/{connection}/callback",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
	if len(readiness) > 0 {
		cfg.Readiness = readiness[0]
	}
	return cfg
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	runtime, err := buildRuntimeHook(context.Background())
	if err != nil {
		logFatal(err)
	}
	if runtime.cleanup != nil {
		defer runtime.cleanup()
	}

	if err := runModuleServer(moduleConfig(*listenAddr, runtime.registerHTTPRoutes, runtime.readiness)); err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	stateSecret, err := deriveStateSecret()
	if err != nil {
		return nil, err
	}
	identitySigningSecret, err := readMountedSecret(identitySigningSecretFileEnv)
	if err != nil {
		return nil, err
	}
	if err := validateLifecycleConfiguration(); err != nil {
		return nil, err
	}

	repo, cleanup, readiness, err := buildDurableRepository(ctx)
	if err != nil {
		return nil, err
	}
	ssoSvc := service.New(repo, stateSecret)
	h := handler.New(ssoSvc, handler.Config{
		IdentityContextSecret: identitySigningSecret,
		TrustForwardedHeaders: parseTrustForwardedHeaders(),
	})
	return &moduleRuntime{
		registerHTTPRoutes: h.RegisterRoutes,
		readiness:          readiness,
		cleanup:            cleanup,
	}, nil
}

func parseTrustForwardedHeaders() bool {
	raw := strings.TrimSpace(os.Getenv("AEGION_PROXY_TRUST_FORWARDED_HEADERS"))
	if raw == "" {
		return false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return parsed
}

func buildRepository(ctx context.Context) (store.Repository, func(), error) {
	repo, cleanup, _, err := buildDurableRepository(ctx)
	return repo, cleanup, err
}

func buildDurableRepository(ctx context.Context) (store.Repository, func(), func(context.Context) error, error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		return nil, nil, nil, fmt.Errorf("%s is required", dbURLEnv)
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse sso database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, nil, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, databaseReadinessTimeout)
	defer cancel()
	if err := poolPingHook(pingCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, nil, nil, err
	}

	repo, err := newPostgresRepoHook(pool)
	if err != nil {
		poolCloseHook(pool)
		return nil, nil, nil, err
	}
	return repo, func() { poolCloseHook(pool) }, databaseReadiness(pool), nil
}

func databaseReadiness(pool *pgxpool.Pool) func(context.Context) error {
	return func(ctx context.Context) error {
		checkCtx, cancel := context.WithTimeout(ctx, databaseReadinessTimeout)
		defer cancel()
		if err := poolPingHook(checkCtx, pool); err != nil {
			return fmt.Errorf("ping sso database: %w", err)
		}
		if err := checkSSOSchemaHook(checkCtx, pool); err != nil {
			return fmt.Errorf("check sso schema: %w", err)
		}
		return nil
	}
}

func checkSSOSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var connectionsRelation, authRequestsRelation *string
	if err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('public.sso_connections')::text,
			to_regclass('public.sso_auth_requests')::text
	`).Scan(&connectionsRelation, &authRequestsRelation); err != nil {
		return err
	}
	if connectionsRelation == nil || strings.TrimSpace(*connectionsRelation) == "" {
		return errors.New("required relation sso_connections is missing")
	}
	if authRequestsRelation == nil || strings.TrimSpace(*authRequestsRelation) == "" {
		return errors.New("required relation sso_auth_requests is missing")
	}
	return nil
}

func deriveStateSecret() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(stateSecretEnv))
	if len(raw) < minSecretBytes {
		return nil, fmt.Errorf("SSO state secret must be at least %d bytes", minSecretBytes)
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func readMountedSecret(envName string) ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(envName))
	if path == "" {
		return nil, fmt.Errorf("%s is required", envName)
	}
	value, err := securefile.ReadRegularFile(path, maxMountedSecretBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", envName, err)
	}
	secret := []byte(strings.TrimSpace(string(value)))
	if len(secret) < minSecretBytes {
		return nil, fmt.Errorf("%s must contain at least %d bytes", envName, minSecretBytes)
	}
	return secret, nil
}

func validateLifecycleConfiguration() error {
	for _, envName := range []string{
		coreGRPCAddrEnv,
		moduleCredentialFileEnv,
		moduleTLSCertFileEnv,
		moduleTLSKeyFileEnv,
		moduleCACertFileEnv,
	} {
		if strings.TrimSpace(os.Getenv(envName)) == "" {
			return fmt.Errorf("%s is required for SSO core lifecycle registration", envName)
		}
	}
	return nil
}
