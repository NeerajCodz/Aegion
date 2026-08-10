package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/platform/securefile"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/social/handler"
	"github.com/aegion/aegion/modules/social/providers/catalog"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

const (
	listenAddrEnv                = "AEGION_SOCIAL_HTTP_LISTEN_ADDR"
	defaultListen                = "0.0.0.0:9006"
	moduleVersion                = "0.2.0"
	dbURLEnv                     = "AEGION_SOCIAL_DATABASE_URL"
	managementTokenEnv           = "AEGION_SOCIAL_PROVIDER_MANAGEMENT_TOKEN"
	cipherSecretEnv              = "AEGION_SECRETS_CIPHER"
	identitySigningSecretFileEnv = "AEGION_MODULE_IDENTITY_SIGNING_SECRET_FILE"
	maxMountedSecretBytes        = 4096
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

var schemaCheckHook = checkSchema
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
		Module:             "social",
		Version:            moduleVersion,
		ListenAddr:         listenAddr,
		Capabilities:       []string{"oauth2_social_login", "social_provider_registry"},
		Routes:             []string{"/self-service/social/*", "/api/v1/social/*"},
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
	managementToken, err := requiredManagementToken()
	if err != nil {
		return err
	}
	identitySigningSecret, err := readIdentitySigningSecret()
	if err != nil {
		return err
	}
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
		ManagementToken:       managementToken,
		IdentitySigningSecret: identitySigningSecret,
	})
	cfg := moduleConfig(*listenAddr, h.RegisterRoutes)
	cfg.Readiness = socialReadiness(socialSvc)
	return runModuleServer(cfg)
}

func buildRepository(ctx context.Context) (store.Repository, func(), error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		return nil, nil, fmt.Errorf("social server requires %s", dbURLEnv)
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse social database URL: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect social database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(pingCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, nil, fmt.Errorf("ping social database: %w", err)
	}
	if err := schemaCheckHook(pingCtx, pool); err != nil {
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
	if len(raw) < sha256.Size {
		return nil, fmt.Errorf("social server requires at least %d bytes in %s", sha256.Size, cipherSecretEnv)
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func socialReadiness(svc runtimeSocialService) func(context.Context) error {
	return func(ctx context.Context) error {
		if svc == nil {
			return errors.New("social service is unavailable")
		}
		if _, err := svc.ListConfiguredProviders(ctx, true); err != nil {
			return fmt.Errorf("social provider store is unavailable: %w", err)
		}
		return nil
	}
}

func requiredManagementToken() (string, error) {
	token := strings.TrimSpace(os.Getenv(managementTokenEnv))
	if len(token) < sha256.Size {
		return "", fmt.Errorf("social server requires at least %d bytes in %s", sha256.Size, managementTokenEnv)
	}
	return token, nil
}

func readIdentitySigningSecret() ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(identitySigningSecretFileEnv))
	if path == "" {
		return nil, fmt.Errorf("social server requires %s", identitySigningSecretFileEnv)
	}
	raw, err := securefile.ReadRegularFile(path, maxMountedSecretBytes)
	if err != nil {
		return nil, fmt.Errorf("read social identity signing secret file: %w", err)
	}
	secret := bytes.TrimSpace(raw)
	if len(secret) < sha256.Size {
		return nil, fmt.Errorf("social server requires at least %d bytes in %s", sha256.Size, identitySigningSecretFileEnv)
	}
	return append([]byte(nil), secret...), nil
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("social database pool is unavailable")
	}
	const schemaQuery = `
		SELECT COALESCE(string_agg(name, ', ' ORDER BY name), '')
		FROM unnest($1::text[]) AS required(name)
		WHERE to_regclass(name) IS NULL
	`
	requiredRelations := []string{
		"soc_providers",
		"soc_provider_credentials",
		"soc_auth_states",
		"soc_identity_links",
		"core_identities",
		"core_identity_addresses",
		"core_identity_schemas",
	}
	var missing string
	if err := pool.QueryRow(ctx, schemaQuery, requiredRelations).Scan(&missing); err != nil {
		return fmt.Errorf("check social database schema: %w", err)
	}
	if missing != "" {
		return fmt.Errorf("social database schema is missing required relations: %s", missing)
	}
	return nil
}

func bootstrapEnvProviders(ctx context.Context, svc interface {
	UpsertProvider(ctx context.Context, req service.ProviderUpsertRequest) (*store.Provider, error)
}) error {
	for _, slug := range catalog.Names() {
		req, configured, err := envProviderRequest(slug)
		if err != nil {
			return err
		}
		if !configured {
			continue
		}
		if _, err := svc.UpsertProvider(ctx, req); err != nil {
			return fmt.Errorf("bootstrap %s provider: %w", slug, err)
		}
	}
	return nil
}

func envProviderRequest(slug string) (service.ProviderUpsertRequest, bool, error) {
	prefix := "AEGION_SOCIAL_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_")) + "_"
	clientID := strings.TrimSpace(os.Getenv(prefix + "CLIENT_ID"))
	redirectURI := strings.TrimSpace(os.Getenv(prefix + "REDIRECT_URI"))
	if clientID == "" && redirectURI == "" {
		return service.ProviderUpsertRequest{}, false, nil
	}
	if clientID == "" || redirectURI == "" {
		return service.ProviderUpsertRequest{}, false, fmt.Errorf("%s provider requires both %sCLIENT_ID and %sREDIRECT_URI", slug, prefix, prefix)
	}
	if err := validateCallbackRedirectURI(slug, redirectURI); err != nil {
		return service.ProviderUpsertRequest{}, false, fmt.Errorf("%s provider redirect URI: %w", slug, err)
	}
	trustEmailVerified, err := boolEnv(prefix + "TRUST_EMAIL_VERIFIED")
	if err != nil {
		return service.ProviderUpsertRequest{}, false, fmt.Errorf("%s provider: %w", slug, err)
	}
	return service.ProviderUpsertRequest{
		Slug:               slug,
		Preset:             slug,
		ClientID:           clientID,
		ClientSecret:       strings.TrimSpace(os.Getenv(prefix + "CLIENT_SECRET")),
		RedirectURI:        redirectURI,
		Enabled:            true,
		TrustEmailVerified: trustEmailVerified,
	}, true, nil
}

func boolEnv(name string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func validateCallbackRedirectURI(slug, raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed == nil || parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return errors.New("must be an absolute callback URL without userinfo or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return errors.New("must use HTTPS or loopback HTTP")
	}
	if parsed.Path != "/self-service/social/"+slug+"/callback" && parsed.Path != "/api/v1/social/"+slug+"/callback" {
		return errors.New("must target a registered social callback route")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
