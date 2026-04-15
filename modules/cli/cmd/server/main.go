package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/cli/handler"
	"github.com/aegion/aegion/modules/cli/service"
	"github.com/aegion/aegion/modules/cli/store"
)

const (
	listenAddrEnv      = "AEGION_CLI_HTTP_LISTEN_ADDR"
	defaultListen      = "0.0.0.0:9010"
	moduleVersion      = "0.2.0"
	dbURLEnv           = "AEGION_CLI_DATABASE_URL"
	legacyDBURLEnv     = "AEGION_DATABASE_URL"
	managementTokenEnv = "AEGION_CLI_MANAGEMENT_TOKEN"
)

var (
	runModuleServer  = moduleserver.Run
	buildRuntimeHook = buildRuntime
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
		Module:       "cli",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"ops_interface", "read_only_commands", "diagnostics"},
		Routes:       []string{"/api/v1/cli/*"},
		GRPCServices: []string{"cli.CommandGateway"},
		EventSubscriptions: []string{
			"system.health",
			"policy.updated",
			"proxy.updated",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	runtime, err := buildRuntimeHook(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if runtime.cleanup != nil {
		defer runtime.cleanup()
	}

	if err := runModuleServer(moduleConfig(*listenAddr, runtime.registerHTTPRoutes)); err != nil {
		log.Fatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	repo, cleanup, err := buildRepository(ctx)
	if err != nil {
		return nil, err
	}

	cliSvc := service.New(repo)
	h := handler.New(cliSvc, handler.Config{
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
		return nil, nil, fmt.Errorf("parse cli database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, err
	}

	repo, err := store.NewPostgres(pool)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return repo, pool.Close, nil
}
