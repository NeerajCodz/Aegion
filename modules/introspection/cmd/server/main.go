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
	"github.com/aegion/aegion/modules/introspection/handler"
	introspectionservice "github.com/aegion/aegion/modules/introspection/service"
	introspectionstore "github.com/aegion/aegion/modules/introspection/store"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
)

const (
	listenAddrEnv   = "AEGION_INTROSPECTION_HTTP_LISTEN_ADDR"
	defaultListen   = "0.0.0.0:9008"
	moduleVersion   = "0.2.0"
	dbURLEnv        = "AEGION_INTROSPECTION_DATABASE_URL"
	legacyDBURLEnv  = "AEGION_DATABASE_URL"
	issuerEnv       = "AEGION_INTROSPECTION_OAUTH2_ISSUER"
	legacyIssuerEnv = "AEGION_OAUTH2_ISSUER"
	defaultIssuer   = "http://localhost:8083"
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
			xlog.Default().Fatal("introspection server failed")
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
		Module:       "introspection",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"token_introspection", "session_lookup"},
		Routes:       []string{"/oauth2/introspect", "/api/v1/introspection/*"},
		EventSubscriptions: []string{
			"session.created",
			"session.revoked",
			"identity.updated",
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

	err = runModuleServer(moduleConfig(*listenAddr, runtime.registerHTTPRoutes))
	if err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		dbURL = strings.TrimSpace(os.Getenv(legacyDBURLEnv))
	}
	if dbURL == "" {
		return nil, fmt.Errorf("%s or %s must be configured for introspection", dbURLEnv, legacyDBURLEnv)
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse introspection database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(pingCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, err
	}

	repo := introspectionstore.New(pool)
	tokenSvc := tokenservice.NewTokenService(repo.OAuth2(), nil, oauth2Issuer())
	svc := introspectionservice.New(tokenSvc)
	h := handler.New(svc)

	return &moduleRuntime{
		registerHTTPRoutes: h.RegisterRoutes,
		cleanup:            func() { poolCloseHook(pool) },
	}, nil
}

func oauth2Issuer() string {
	issuer := strings.TrimSpace(os.Getenv(issuerEnv))
	if issuer == "" {
		issuer = strings.TrimSpace(os.Getenv(legacyIssuerEnv))
	}
	if issuer == "" {
		return defaultIssuer
	}
	return strings.TrimRight(issuer, "/")
}
