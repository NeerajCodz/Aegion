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

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/proxy/handler"
	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/aegion/aegion/modules/proxy/store"
)

const (
	listenAddrEnv      = "AEGION_PROXY_HTTP_LISTEN_ADDR"
	defaultListen      = "0.0.0.0:9009"
	moduleVersion      = "0.2.0"
	dbURLEnv           = "AEGION_PROXY_DATABASE_URL"
	legacyDBURLEnv     = "AEGION_DATABASE_URL"
	managementTokenEnv = "AEGION_PROXY_MANAGEMENT_TOKEN"
)

var (
	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) { pool.Close() }
	newPostgresRepoHook   = store.NewPostgres
	logFatal              = func(v ...any) {
		if len(v) == 0 {
			xlog.Default().Fatal("proxy server failed")
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
	cleanup            func()
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:       "proxy",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"authz_proxy", "policy_enforcement", "proxy_rule_registry"},
		Routes:       []string{"/proxy/*", "/api/v1/proxy/*"},
		EventSubscriptions: []string{
			"policy.updated",
			"identity.updated",
			"session.created",
			"session.revoked",
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

	proxySvc := service.New(repo)
	h := handler.New(proxySvc, handler.Config{
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
		return nil, nil, fmt.Errorf("parse proxy database url: %w", err)
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
