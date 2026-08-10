package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/passkeys/handler"
	"github.com/aegion/aegion/modules/passkeys/service"
	"github.com/aegion/aegion/modules/passkeys/store"
)

const (
	listenAddrEnv                    = "AEGION_PASSKEYS_HTTP_LISTEN_ADDR"
	databaseURLEnv                   = "AEGION_PASSKEYS_DATABASE_URL"
	rpIDEnv                          = "AEGION_PASSKEYS_RP_ID"
	rpOriginEnv                      = "AEGION_PASSKEYS_RP_ORIGIN"
	identitySigningSecretFileEnv     = "AEGION_MODULE_IDENTITY_SIGNING_SECRET_FILE"
	defaultListen                    = "0.0.0.0:9004"
	moduleVersion                    = "0.1.0"
	identitySigningSecretMinBytes    = 32
	identityHeaderMaxAge             = time.Minute
	maxPasskeyRequestBodyBytes int64 = (1 << 20) - 128
)

var (
	passkeyRoutes = []string{
		"/api/v1/passkeys/registration/start",
		"/api/v1/passkeys/registration/finish",
		"/api/v1/passkeys/authentication/start",
		"/api/v1/passkeys/authentication/finish",
	}
	signedIdentityHeaders = []string{
		"X-User-ID",
		"X-User-Session-ID",
		"X-User-AAL",
	}

	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	parsePoolConfigHook   = pgxpool.ParseConfig
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) { pool.Close() }
	newPostgresStoreHook  = store.NewPostgres
	schemaCheckHook       = checkPasskeySchema
	readFileHook          = os.ReadFile
)

type runtime struct {
	passkeyService        *service.Service
	pool                  *pgxpool.Pool
	identitySigningSecret []byte
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func passkeyConfig() (service.Config, error) {
	rpID := strings.ToLower(strings.TrimSpace(os.Getenv(rpIDEnv)))
	if !validRPID(rpID) {
		return service.Config{}, fmt.Errorf("%s must be a valid DNS name", rpIDEnv)
	}

	rawOrigin := strings.TrimSpace(os.Getenv(rpOriginEnv))
	origin, err := url.Parse(rawOrigin)
	if err != nil {
		return service.Config{}, fmt.Errorf("parse %s: %w", rpOriginEnv, err)
	}
	if origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.RawQuery != "" || origin.Fragment != "" || origin.Opaque != "" ||
		(origin.Path != "" && origin.Path != "/") || origin.RawPath != "" {
		return service.Config{}, fmt.Errorf("%s must be an exact HTTPS origin", rpOriginEnv)
	}

	originHost := strings.ToLower(origin.Hostname())
	if originHost == "" || net.ParseIP(originHost) != nil ||
		(originHost != rpID && !strings.HasSuffix(originHost, "."+rpID)) {
		return service.Config{}, fmt.Errorf("%s must be an HTTPS origin for %s", rpOriginEnv, rpIDEnv)
	}
	origin.Path = ""

	return service.Config{
		RPID:               rpID,
		RPOrigin:           origin.String(),
		ChallengeTTL:       5 * time.Minute,
		AllowedCredentials: 20,
	}, nil
}

func validRPID(rpID string) bool {
	if rpID == "localhost" {
		return true
	}
	if rpID == "" || len(rpID) > 253 || net.ParseIP(rpID) != nil {
		return false
	}
	labels := strings.Split(rpID, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func loadIdentitySigningSecret() ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(identitySigningSecretFileEnv))
	if path == "" {
		return nil, fmt.Errorf("%s is required", identitySigningSecretFileEnv)
	}
	material, err := readFileHook(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", identitySigningSecretFileEnv, err)
	}
	secret := []byte(strings.TrimSpace(string(material)))
	if len(secret) < identitySigningSecretMinBytes {
		return nil, fmt.Errorf("%s must contain at least %d bytes", identitySigningSecretFileEnv, identitySigningSecretMinBytes)
	}
	return secret, nil
}

func buildRuntime(ctx context.Context) (*runtime, error) {
	cfg, err := passkeyConfig()
	if err != nil {
		return nil, err
	}
	identitySigningSecret, err := loadIdentitySigningSecret()
	if err != nil {
		return nil, err
	}

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		return nil, fmt.Errorf("%s is required", databaseURLEnv)
	}
	poolConfig, err := parsePoolConfigHook(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse passkeys database URL: %w", err)
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("connect passkeys database: %w", err)
	}
	rt := &runtime{
		pool:                  pool,
		identitySigningSecret: identitySigningSecret,
	}
	if err := rt.Readiness(ctx); err != nil {
		rt.Close()
		return nil, err
	}

	durableStore, err := newPostgresStoreHook(pool)
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("create passkeys store: %w", err)
	}
	rt.passkeyService = service.New(durableStore, cfg)
	return rt, nil
}

func (r *runtime) Readiness(ctx context.Context) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("passkeys database is unavailable")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(checkCtx, r.pool); err != nil {
		return fmt.Errorf("ping passkeys database: %w", err)
	}
	if err := schemaCheckHook(checkCtx, r.pool); err != nil {
		return err
	}
	return nil
}

func (r *runtime) Close() {
	if r != nil && r.pool != nil {
		poolCloseHook(r.pool)
	}
}

func checkPasskeySchema(ctx context.Context, pool *pgxpool.Pool) error {
	var ready bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('passkey_credentials') IS NOT NULL
		   AND to_regclass('passkey_challenges') IS NOT NULL
	`).Scan(&ready); err != nil {
		return fmt.Errorf("check passkeys schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("passkeys database schema is not ready")
	}
	return nil
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux), readiness func(context.Context) error) moduleserver.Config {
	return moduleserver.Config{
		Module:             "passkeys",
		Version:            moduleVersion,
		ListenAddr:         listenAddr,
		Capabilities:       []string{"passkey_step_up"},
		Routes:             append([]string(nil), passkeyRoutes...),
		RegisterHTTPRoutes: registerHTTPRoutes,
		Readiness:          readiness,
	}
}

func registerPasskeyRoutes(h *handler.Handler, identitySigningSecret []byte) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		if mux == nil || h == nil {
			return
		}
		moduleMux := http.NewServeMux()
		h.RegisterRoutes(moduleMux)
		protectedRoutes := requireSignedIdentity(identitySigningSecret, moduleMux)
		for _, route := range passkeyRoutes {
			mux.Handle(route, protectedRoutes)
		}
	}
}

func requireSignedIdentity(identitySigningSecret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityID, err := verifiedIdentityID(r, identitySigningSecret)
		if err != nil {
			writeRuntimeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if err := bindTrustedIdentity(w, r, identityID); err != nil {
			writeRuntimeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func verifiedIdentityID(r *http.Request, identitySigningSecret []byte) (string, error) {
	if r == nil || len(identitySigningSecret) < identitySigningSecretMinBytes {
		return "", fmt.Errorf("identity context is unavailable")
	}
	for _, header := range signedIdentityHeaders {
		if strings.TrimSpace(r.Header.Get(header)) == "" {
			return "", fmt.Errorf("identity context is incomplete")
		}
	}
	if !platformcrypto.VerifyIdentityHeaders(
		identitySigningSecret,
		r.Header,
		signedIdentityHeaders,
		r.Header.Get("X-Aegion-Signature"),
		identityHeaderMaxAge,
		time.Now().UTC(),
	) {
		return "", fmt.Errorf("identity context signature is invalid")
	}

	identityID, err := uuid.Parse(strings.TrimSpace(r.Header.Get("X-User-ID")))
	if err != nil {
		return "", fmt.Errorf("identity context has an invalid identity")
	}
	if _, err := uuid.Parse(strings.TrimSpace(r.Header.Get("X-User-Session-ID"))); err != nil {
		return "", fmt.Errorf("identity context has an invalid session")
	}
	switch strings.TrimSpace(r.Header.Get("X-User-AAL")) {
	case "aal1", "aal2":
	default:
		return "", fmt.Errorf("identity context has an invalid assurance level")
	}
	return identityID.String(), nil
}

func bindTrustedIdentity(w http.ResponseWriter, r *http.Request, identityID string) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return fmt.Errorf("request content type is not JSON")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPasskeyRequestBodyBytes)
	defer r.Body.Close()

	var payload map[string]json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return fmt.Errorf("request body must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("request body has trailing content")
	}
	if _, suppliedIdentity := payload["identity_id"]; suppliedIdentity {
		return fmt.Errorf("identity_id is derived from the signed identity context")
	}

	payload["identity_id"] = json.RawMessage(fmt.Sprintf("%q", identityID))
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode trusted request: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	return nil
}

func writeRuntimeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code},
	})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		xlog.Default().Fatal("passkeys server failed", "error", err)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("passkeys-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listenAddr := flags.String("listen", defaultListenAddr(), "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}

	rt, err := buildRuntimeHook(context.Background())
	if err != nil {
		return err
	}
	defer rt.Close()
	if rt.passkeyService == nil {
		return fmt.Errorf("passkeys service is unavailable")
	}

	h := handler.New(rt.passkeyService)
	return runModuleServer(moduleConfig(
		*listenAddr,
		registerPasskeyRoutes(h, rt.identitySigningSecret),
		rt.Readiness,
	))
}
