package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	analytics "github.com/aegion/aegion/modules/analytics"
	analyticsgraphql "github.com/aegion/aegion/modules/analytics/graphql"
	"github.com/aegion/aegion/modules/analytics/rest"
	"github.com/aegion/aegion/modules/analytics/store"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

const (
	analyticsRESTPrefix    = "/api/v1/analytics"
	analyticsGraphQLPrefix = "/graphql/analytics"
)

var version = "dev"

// duckDBRESTAdapter adapts the durable analytics store to the established REST
// query contract. The REST package predates the analytics_ table prefix.
type duckDBRESTAdapter struct{ db *store.DuckDB }

func (a duckDBRESTAdapter) Query(ctx context.Context, query string) ([]map[string]interface{}, error) {
	return a.db.ExecuteSQL(ctx, strings.ReplaceAll(query, "FROM events", "FROM analytics_events"))
}

func (a duckDBRESTAdapter) Count(ctx context.Context, query string) (int, error) {
	rows, err := a.Query(ctx, query)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	for _, value := range rows[0] {
		switch n := value.(type) {
		case int:
			return n, nil
		case int32:
			return int(n), nil
		case int64:
			return int(n), nil
		case float64:
			return int(n), nil
		}
	}
	return 0, errors.New("analytics count query returned a non-numeric result")
}

func main() {
	configPath := flag.String("config", moduleserver.EnvOrDefault("AEGION_ANALYTICS_CONFIG", ""), "path to analytics configuration")
	flag.Parse()
	if strings.TrimSpace(*configPath) == "" {
		fatal(errors.New("analytics configuration is required"))
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
		return
	}
	if err := validateRuntimeConfig(cfg); err != nil {
		fatal(err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.NewDuckDB(store.DuckDBConfig{
		Path: cfg.DuckDB.Path, MaxMemory: cfg.DuckDB.MaxMemory, Threads: cfg.DuckDB.Threads,
		ConnectionPoolSize: cfg.DuckDB.ConnectionPoolSize, HealthCheckInterval: cfg.DuckDB.HealthCheckInterval,
		InitializeOnStartup: false, // schema migration is owned by the migrator
	})
	if err != nil {
		fatal(fmt.Errorf("open durable analytics database: %w", err))
		return
	}
	defer func() { _ = db.Close(context.Background()) }()
	backend, err := newStorageBackend(cfg.Storage)
	if err != nil {
		fatal(err)
		return
	}
	defer func() { _ = backend.Close(context.Background()) }()
	if err := backend.Initialize(ctx); err != nil {
		fatal(fmt.Errorf("initialize analytics storage: %w", err))
		return
	}
	if err := checkSchema(ctx, db); err != nil {
		fatal(err)
		return
	}

	log := xlog.Default()
	handler, err := rest.Initialize(rest.InitParams{Logger: log, DB: duckDBRESTAdapter{db: db}, Config: rest.Config{
		BasePath: analyticsRESTPrefix, RateLimit: cfg.REST.RateLimitPerMinute,
		QueryTimeoutSeconds: cfg.REST.QueryTimeoutSeconds, MaxPageSize: cfg.REST.MaxPageSize,
		DefaultPageSize: cfg.REST.DefaultPageSize,
	}})
	if err != nil {
		fatal(fmt.Errorf("initialize analytics REST handlers: %w", err))
		return
	}

	var graphqlServer *analyticsgraphql.Server
	if cfg.GraphQL.Enabled {
		graphqlStore, err := analyticsgraphql.NewDuckDBStore(db, backend, func(checkCtx context.Context) error {
			return checkSchema(checkCtx, db)
		})
		if err != nil {
			fatal(fmt.Errorf("initialize durable GraphQL store: %w", err))
			return
		}
		module, err := analyticsgraphql.Initialize(ctx, analyticsgraphql.InitOptions{
			Logger: log,
			Config: &analyticsgraphql.Config{
				Enabled:             true,
				Endpoint:            analyticsGraphQLPrefix,
				EnableIntrospection: cfg.GraphQL.Introspection,
				EnablePlayground:    cfg.GraphQL.Playground,
				MaxQueryDepth:       cfg.GraphQL.MaxQueryDepth,
				MaxQueryComplexity:  cfg.GraphQL.MaxQueryComplexity,
				QueryTimeoutSeconds: cfg.GraphQL.QueryTimeoutSeconds,
				RateLimitPerMinute:  cfg.GraphQL.RateLimitPerMinute,
			},
			Store: graphqlStore,
		})
		if err != nil {
			fatal(fmt.Errorf("initialize GraphQL handlers: %w", err))
			return
		}
		graphqlServer = module.GetServer()
	}

	readiness := func(checkCtx context.Context) error {
		if err := db.Health(checkCtx); err != nil { return fmt.Errorf("analytics database: %w", err) }
		if err := backend.Health(checkCtx); err != nil { return fmt.Errorf("analytics storage: %w", err) }
		return checkSchema(checkCtx, db)
	}
	routes := []string{analyticsRESTPrefix}
	capabilities := []string{"analytics.events.read", "analytics.dashboards", "analytics.queries"}
	if graphqlServer != nil {
		routes = append(routes, analyticsGraphQLPrefix)
		capabilities = append(capabilities, "analytics.graphql")
	}
	if err := moduleserver.Run(moduleserver.Config{
		Module: "analytics", Version: version,
		ListenAddr: moduleserver.EnvOrDefault("AEGION_ANALYTICS_LISTEN_ADDR", "0.0.0.0:8080"),
		Capabilities: capabilities,
		Routes: routes,
		Readiness: readiness,
		RegisterHTTPRoutes: func(mux *http.ServeMux) {
			mux.Handle(analyticsRESTPrefix+"/", http.StripPrefix(analyticsRESTPrefix, protectedRoutes(handler, log, cfg.REST)))
			if graphqlServer != nil {
				mux.Handle(analyticsGraphQLPrefix, graphqlServer.HTTPHandler())
			}
		},
	}); err != nil { fatal(err) }
}

func protectedRoutes(h *rest.Handler, log *xlog.Logger, cfg analytics.RestAPIConfig) http.Handler {
	r := chi.NewRouter()
	r.Use(rest.AuthMiddleware(log), rest.RateLimitMiddleware(log, cfg.RateLimitPerMinute), rest.QueryTimeoutMiddleware(time.Duration(cfg.QueryTimeoutSeconds)*time.Second))
	r.Route("/events", func(r chi.Router) {
		r.Get("/", h.ListEvents); r.Post("/search", h.SearchEvents); r.Get("/{id}", h.GetEvent)
		r.Get("/{id}/related", h.GetRelatedEvents); r.Post("/export", h.ExportEventsBlob)
	})
	r.Route("/dashboards", func(r chi.Router) {
		r.Get("/", h.ListDashboards); r.Post("/", h.CreateDashboard); r.Get("/{id}", h.GetDashboard)
		r.Put("/{id}", h.UpdateDashboard); r.Delete("/{id}", h.DeleteDashboard)
		r.Post("/{id}/share", h.ShareDashboard); r.Post("/{id}/components/{componentId}/execute", h.ExecuteDashboardQuery)
	})
	r.Route("/queries", func(r chi.Router) {
		r.Get("/", h.ListQueries); r.Post("/", h.SaveQuery); r.Get("/{id}/execute", h.ExecuteQuery); r.Delete("/{id}", h.DeleteQuery)
	})
	return r
}

func loadConfig(path string) (*analytics.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil { return nil, fmt.Errorf("read analytics configuration: %w", err) }
	var cfg analytics.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil { return nil, fmt.Errorf("parse analytics configuration: %w", err) }
	if err := cfg.Validate(); err != nil { return nil, fmt.Errorf("validate analytics configuration: %w", err) }
	return &cfg, nil
}

func validateRuntimeConfig(cfg *analytics.Config) error {
	if !cfg.Enabled || !cfg.REST.Enabled {
		return errors.New("analytics and its REST API must be enabled")
	}
	if cfg.REST.Endpoint != analyticsRESTPrefix {
		return fmt.Errorf("analytics REST endpoint must be %q", analyticsRESTPrefix)
	}
	if cfg.GraphQL.Enabled {
		if cfg.GraphQL.Endpoint != analyticsGraphQLPrefix {
			return fmt.Errorf("analytics GraphQL endpoint must be %q", analyticsGraphQLPrefix)
		}
		if cfg.GraphQL.Introspection || cfg.GraphQL.Playground {
			return errors.New("GraphQL introspection and playground are not exposed by the production endpoint")
		}
		if cfg.GraphQL.MaxQueryDepth <= 0 || cfg.GraphQL.MaxQueryComplexity <= 0 || cfg.GraphQL.QueryTimeoutSeconds <= 0 || cfg.GraphQL.RateLimitPerMinute <= 0 {
			return errors.New("analytics GraphQL limits must all be greater than zero")
		}
	}
	if cfg.GRPC.Enabled {
		return errors.New("gRPC has no scoped-token runtime and must not be advertised")
	}
	if cfg.Sync.Enabled || cfg.Webhooks.Enabled || cfg.Retention.Enabled {
		return errors.New("sync, webhooks, and retention require durable runtime adapters that are not installed")
	}
	if strings.TrimSpace(cfg.DuckDB.Path) == "" || cfg.DuckDB.Path == ":memory:" {
		return errors.New("analytics requires a persistent DuckDB path")
	}
	if strings.TrimSpace(os.Getenv("AEGION_ANALYTICS_REST_JWT_PUBLIC_KEY_BASE64")) == "" {
		return errors.New("AEGION_ANALYTICS_REST_JWT_PUBLIC_KEY_BASE64 is required")
	}
	return nil
}

func newStorageBackend(cfg analytics.StorageConfig) (store.StorageBackend, error) {
	switch cfg.Type {
	case analytics.StorageTypeLocal:
		return store.NewLocalStorage(cfg.LocalPath), nil
	case analytics.StorageTypeS3:
		return store.NewS3Storage(cfg.S3.Bucket, cfg.S3.Region, cfg.S3.Prefix, cfg.S3.EndpointURL, cfg.S3.UsePathStyle, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey)
	case analytics.StorageTypeIceberg:
		return store.NewIcebergStorage(cfg.Iceberg.CatalogType, cfg.Iceberg.WarehousePath, cfg.Iceberg.CatalogName, cfg.Iceberg.NessieURI)
	default:
		return nil, fmt.Errorf("storage backend %q has no durable runtime adapter", cfg.Type)
	}
}

func checkSchema(ctx context.Context, db *store.DuckDB) error {
	for _, table := range []string{"analytics_events", "analytics_metrics", "analytics_dashboards", "analytics_queries", "analytics_webhooks"} {
		if _, err := db.Query(ctx, "SELECT 1 FROM "+table+" LIMIT 1"); err != nil {
			return fmt.Errorf("analytics schema is not ready (%s): %w", table, err)
		}
	}
	return nil
}

func fatal(err error) { xlog.Default().Fatal("analytics startup failed", "error", err) }
