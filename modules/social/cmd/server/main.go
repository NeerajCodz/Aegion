package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/social/handler"
	"github.com/aegion/aegion/modules/social/providers/catalog"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

const (
	listenAddrEnv      = "AEGION_SOCIAL_HTTP_LISTEN_ADDR"
	defaultListen      = "0.0.0.0:9006"
	moduleVersion      = "0.2.0"
	dbURLEnv           = "AEGION_SOCIAL_DATABASE_URL"
	legacyDBURLEnv     = "AEGION_DATABASE_URL"
	managementTokenEnv = "AEGION_SOCIAL_PROVIDER_MANAGEMENT_TOKEN"
	cipherSecretEnv    = "AEGION_SECRETS_CIPHER"
	legacyCipherEnv    = "AEGION_SECRET_CIPHER_1"
)

var runModuleServer = moduleserver.Run
var buildRepositoryHook = buildRepository
var newSocialServiceHook = func(repo store.Repository) runtimeSocialService { return service.New(repo) }
var newPoolWithConfigHook = pgxpool.NewWithConfig
var poolPingHook = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
var poolCloseHook = func(pool *pgxpool.Pool) { pool.Close() }
var deriveCipherKeyHook = deriveCipherKey
var newPostgresRepoHook = func(pool *pgxpool.Pool, cipherKey []byte) (store.Repository, error) {
	return store.NewPostgres(pool, cipherKey)
}
var logFatal = func(v ...any) {
	if len(v) == 0 {
		xlog.Default().Fatal("social server failed")
		return
	}
	if err, ok := v[0].(error); ok {
		xlog.Default().Fatal(err.Error(), "error", err)
		return
	}
	xlog.Default().Fatal("social server failed", v...)
}

type runtimeSocialService interface {
	handler.SocialService
	EnsurePresetProviders(ctx context.Context) error
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:       "social",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"oauth2_social_login", "social_provider_registry"},
		Routes:       []string{"/self-service/social/*", "/api/v1/social/*", "/api/v1/social/admin/*"},
		EventSubscriptions: []string{
			"identity.created",
			"identity.updated",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		logFatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("social-server", flag.ContinueOnError)
	listenAddr := fs.String("listen", defaultListenAddr(), "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	repo, cleanup, err := buildRepositoryHook(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	socialSvc := newSocialServiceHook(repo)
	if err := socialSvc.EnsurePresetProviders(ctx); err != nil {
		return err
	}
	if err := bootstrapEnvProviders(ctx, socialSvc); err != nil {
		return err
	}

	h := handler.New(socialSvc, handler.Config{
		ManagementToken: strings.TrimSpace(os.Getenv(managementTokenEnv)),
	})
	return runModuleServer(moduleConfig(*listenAddr, h.RegisterRoutes))
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
		return nil, nil, fmt.Errorf("parse social database url: %w", err)
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

	cipherKey, err := deriveCipherKeyHook()
	if err != nil {
		poolCloseHook(pool)
		return nil, nil, err
	}
	repo, err := newPostgresRepoHook(pool, cipherKey)
	if err != nil {
		poolCloseHook(pool)
		return nil, nil, err
	}
	return repo, func() { poolCloseHook(pool) }, nil
}

func deriveCipherKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(cipherSecretEnv))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(legacyCipherEnv))
	}
	if raw == "" {
		return nil, fmt.Errorf("db-backed social provider registry requires %s or %s", cipherSecretEnv, legacyCipherEnv)
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func bootstrapEnvProviders(ctx context.Context, svc interface {
	UpsertProvider(ctx context.Context, req service.ProviderUpsertRequest) (*store.Provider, error)
}) error {
	for _, slug := range catalog.Names() {
		req := envProviderRequest(slug)
		if req == nil {
			continue
		}
		if _, err := svc.UpsertProvider(ctx, *req); err != nil {
			return fmt.Errorf("bootstrap %s provider: %w", slug, err)
		}
	}
	return nil
}

func envProviderRequest(slug string) *service.ProviderUpsertRequest {
	prefix := "AEGION_SOCIAL_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_")) + "_"
	clientID := strings.TrimSpace(os.Getenv(prefix + "CLIENT_ID"))
	redirectURI := strings.TrimSpace(os.Getenv(prefix + "REDIRECT_URI"))
	if clientID == "" || redirectURI == "" {
		return nil
	}
	return &service.ProviderUpsertRequest{
		Slug:               slug,
		Preset:             slug,
		ClientID:           clientID,
		ClientSecret:       strings.TrimSpace(os.Getenv(prefix + "CLIENT_SECRET")),
		RedirectURI:        redirectURI,
		Enabled:            true,
		TrustEmailVerified: true,
	}
}
