package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
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
	listenAddrEnv                = "AEGION_INTROSPECTION_HTTP_LISTEN_ADDR"
	defaultListen                = "0.0.0.0:9008"
	moduleVersion                = "0.2.0"
	dbURLEnv                     = "AEGION_INTROSPECTION_DATABASE_URL"
	issuerEnv                    = "AEGION_INTROSPECTION_OAUTH2_ISSUER"
	environmentEnv               = "AEGION_ENV"
	introspectionRoute           = "/api/v1/introspection/token"
	maxIntrospectionRequestBytes = 64 << 10
	dependencyTimeout             = 5 * time.Second
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
	schemaCheckHook      = ensureOAuth2Schema
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
	readiness          func(context.Context) error
	cleanup            func()
}

// clientAuthorizationBoundary converts an authenticated caller's attempt to
// inspect a different client's token into the same response as failed client
// authentication. This keeps token ownership and existence confidential.
type clientAuthorizationBoundary struct {
	next handler.IntrospectionService
}

func (b clientAuthorizationBoundary) IntrospectToken(ctx context.Context, req *tokenservice.IntrospectionRequest) (*tokenservice.IntrospectionResponse, error) {
	if b.next == nil {
		return nil, tokenservice.ErrInvalidClient
	}
	resp, err := b.next.IntrospectToken(ctx, req)
	if errors.Is(err, tokenservice.ErrUnauthorizedClient) {
		return nil, tokenservice.ErrInvalidClient
	}
	return resp, err
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux), readiness func(context.Context) error) moduleserver.Config {
	return moduleserver.Config{
		Module:             "introspection",
		Version:            moduleVersion,
		ListenAddr:         listenAddr,
		Capabilities:       []string{"token_introspection"},
		Routes:             []string{introspectionRoute},
		RegisterHTTPRoutes: registerHTTPRoutes,
		Readiness:          readiness,
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

	err = runModuleServer(moduleConfig(*listenAddr, runtime.registerHTTPRoutes, runtime.readiness))
	if err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		return nil, fmt.Errorf("%s must be configured for introspection", dbURLEnv)
	}
	if err := validateDatabaseURL(dbURL); err != nil {
		return nil, fmt.Errorf("validate introspection database url: %w", err)
	}
	issuer, err := oauth2Issuer()
	if err != nil {
		return nil, err
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse introspection database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open introspection database: %w", err)
	}

	readiness := func(readyCtx context.Context) error {
		return checkDependencies(readyCtx, pool)
	}
	if err := readiness(ctx); err != nil {
		poolCloseHook(pool)
		return nil, err
	}

	repo := introspectionstore.New(pool)
	tokenSvc := tokenservice.NewTokenService(repo.OAuth2(), nil, issuer)
	svc := introspectionservice.New(tokenSvc)
	h := handler.New(clientAuthorizationBoundary{next: svc})
	sourceMux := http.NewServeMux()
	h.RegisterRoutes(sourceMux)

	return &moduleRuntime{
		registerHTTPRoutes: func(mux *http.ServeMux) {
			if mux == nil {
				return
			}
			mux.Handle(introspectionRoute, boundJSONIntrospectionHandler(sourceMux))
		},
		readiness: readiness,
		cleanup:   func() { poolCloseHook(pool) },
	}, nil
}

func oauth2Issuer() (string, error) {
	issuer := strings.TrimSpace(os.Getenv(issuerEnv))
	if issuer == "" {
		return "", fmt.Errorf("%s must be configured for introspection", issuerEnv)
	}

	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s must be an absolute issuer URL without credentials, query, or fragment", issuerEnv)
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	if strings.EqualFold(parsed.Scheme, "http") && allowsInsecureLoopback() && isLoopbackHost(parsed.Hostname()) {
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	return "", fmt.Errorf("%s must use HTTPS (HTTP is permitted only for an explicit development loopback issuer)", issuerEnv)
}
func validateDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.User == nil || strings.TrimSpace(parsed.User.Username()) == "" {
		return errors.New("must be an absolute postgres URL with an explicit database user")
	}

	sslMode := strings.ToLower(strings.TrimSpace(parsed.Query().Get("sslmode")))
	if sslMode == "verify-full" {
		return nil
	}
	if allowsInsecureLoopback() && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("must use sslmode=verify-full (non-verifying TLS is permitted only for an explicit development loopback database)")
}

func allowsInsecureLoopback() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(environmentEnv))) {
	case "development", "dev", "test":
		return true
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

func checkDependencies(ctx context.Context, pool *pgxpool.Pool) error {
	readyCtx, cancel := context.WithTimeout(ctx, dependencyTimeout)
	defer cancel()
	if err := poolPingHook(readyCtx, pool); err != nil {
		return fmt.Errorf("ping introspection database: %w", err)
	}
	if err := schemaCheckHook(readyCtx, pool); err != nil {
		return fmt.Errorf("verify OAuth2 introspection schema: %w", err)
	}
	return nil
}

func ensureOAuth2Schema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("database pool is nil")
	}
	var present bool
	err := pool.QueryRow(ctx, `
		SELECT to_regclass('public.oa2_clients') IS NOT NULL
			AND to_regclass('public.oa2_access_tokens') IS NOT NULL
	`).Scan(&present)
	if err != nil {
		return err
	}
	if !present {
		return errors.New("required OAuth2 client and access-token tables are missing")
	}
	return nil
}

func boundJSONIntrospectionHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Method == http.MethodPost {
			if r.ContentLength > maxIntrospectionRequestBytes {
				writeBoundedRequestError(w, http.StatusRequestEntityTooLarge, "request_too_large")
				return
			}
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || !strings.EqualFold(mediaType, "application/json") {
				writeBoundedRequestError(w, http.StatusUnsupportedMediaType, "invalid_content_type")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxIntrospectionRequestBytes)
			requestCtx, cancel := context.WithTimeout(r.Context(), dependencyTimeout)
			defer cancel()
			r = r.WithContext(requestCtx)
		}
		next.ServeHTTP(w, r)
	})
}

func writeBoundedRequestError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": "invalid introspection request",
		},
	})
}
