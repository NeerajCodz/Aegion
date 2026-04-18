package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/sso/handler"
	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

const (
	listenAddrEnv      = "AEGION_SSO_HTTP_LISTEN_ADDR"
	defaultListen      = "0.0.0.0:9007"
	moduleVersion      = "0.2.0"
	dbURLEnv           = "AEGION_SSO_DATABASE_URL"
	legacyDBURLEnv     = "AEGION_DATABASE_URL"
	managementTokenEnv = "AEGION_SSO_MANAGEMENT_TOKEN"
	stateSecretEnv     = "AEGION_SSO_STATE_SECRET"
	legacyStateEnv     = "AEGION_SECRETS_INTERNAL"
)

var (
	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	logFatal              = log.Fatal
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) { pool.Close() }
	newPostgresRepoHook   = store.NewPostgres
)

type moduleRuntime struct {
	registerHTTPRoutes func(mux *http.ServeMux)
	cleanup            func()
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:       "sso",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"saml", "connection_registry", "domain_routing"},
		Routes:       []string{"/self-service/sso/*", "/api/v1/sso/*"},
		GRPCServices: []string{"sso.SSOEngine"},
		EventSubscriptions: []string{
			"identity.updated",
			"identity.deleted",
			"domain.updated",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
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

	if err := runModuleServer(moduleConfig(*listenAddr, runtime.registerHTTPRoutes)); err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	repo, cleanup, err := buildRepository(ctx)
	if err != nil {
		return nil, err
	}
	stateSecret := deriveStateSecret()
	ssoSvc := service.New(repo, stateSecret)
	h := handler.New(ssoSvc, handler.Config{
		ManagementToken: strings.TrimSpace(os.Getenv(managementTokenEnv)),
	})
	return &moduleRuntime{
		registerHTTPRoutes: h.RegisterRoutes,
		cleanup:            cleanup,
	}, nil
}

func buildRepository(ctx context.Context) (store.Repository, func(), error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv(legacyDBURLEnv))
	}
	if dbURL == "" {
		return store.New(), func() {}, nil
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse sso database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(pingCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, nil, err
	}

	repo, err := newPostgresRepoHook(pool)
	if err != nil {
		poolCloseHook(pool)
		return nil, nil, err
	}
	return repo, func() { poolCloseHook(pool) }, nil
}

func deriveStateSecret() []byte {
	raw := strings.TrimSpace(os.Getenv(stateSecretEnv))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(legacyStateEnv))
	}
	if raw == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
