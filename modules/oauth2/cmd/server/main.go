package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	platformjwt "github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/modules/oauth2/handler"
	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
	"github.com/aegion/aegion/modules/oauth2/service/introspection"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
)

const (
	version                   = "1.0.0"
	defaultSigningAlgorithm   = "ES256"
	defaultSigningKeyID       = "aegion-oauth2-signing-key"
	signingKeyIDEnv           = "AEGION_OAUTH2_SIGNING_KEY_ID"
	signingPrivateKeyB64Env   = "AEGION_OAUTH2_SIGNING_PRIVATE_KEY_B64"
	signingPublicKeyB64Env    = "AEGION_OAUTH2_SIGNING_PUBLIC_KEY_B64"
	selfTestIssuer            = "aegion-oauth2-selftest"
	accessTokenVerifierLeeway = 30 * time.Second
)

var (
	loadConfigHook        = loadConfig
	connectDBHook         = connectDB
	buildHandlerHook      = buildHandler
	newHTTPServerHook     = newHTTPServer
	cryptoSelfCheckHook   = platformcrypto.RuntimeSelfCheck
	notifySignalsHook     = signal.Notify
	stopSignalsHook       = signal.Stop
	fatalHook             = func(err error, message string) { log.Fatal().Err(err).Msg(message) }
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, db *pgxpool.Pool) error { return db.Ping(ctx) }
	poolCloseHook         = func(db *pgxpool.Pool) {
		if db != nil {
			db.Close()
		}
	}
	generateSigningKeyPairHook  = platformjwt.GenerateECKeyPair
	validateSigningKeyPairHook  = validateSigningKeyPair
	toJWKHook                   = platformjwt.ToJWK
	newOAuth2SigningKeyPairHook = newOAuth2SigningKeyPair
	newStaticJWKSProviderHook   = newStaticJWKSProvider
	listenAndServeHook          = (*http.Server).ListenAndServe
	shutdownServerHook          = func(srv *http.Server, ctx context.Context) error { return srv.Shutdown(ctx) }
)

type Config struct {
	Database struct {
		URL      string `yaml:"url"`
		MaxConns int32  `yaml:"max_conns"`
		MinConns int32  `yaml:"min_conns"`
	} `yaml:"database"`
	Server struct {
		Address      string        `yaml:"address"`
		Port         int           `yaml:"port"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
		IdleTimeout  time.Duration `yaml:"idle_timeout"`
	} `yaml:"server"`
	OAuth2 struct {
		Issuer                string        `yaml:"issuer"`
		BaseURL               string        `yaml:"base_url"`
		DeviceCodeTTL         time.Duration `yaml:"device_code_ttl"`
		DevicePollInterval    int           `yaml:"device_poll_interval"`
		DeviceVerificationURI string        `yaml:"device_verification_uri"`
	} `yaml:"oauth2"`
}

func main() {
	configPath := flag.String("config", getEnv("AEGION_OAUTH2_CONFIG", "oauth2.yaml"), "Path to OAuth2 config file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		_, _ = fmt.Printf("Aegion OAuth2 Module v%s\n", version)
		return
	}
	if err := cryptoSelfCheckHook(); err != nil {
		fatalHook(err, "Go crypto runtime check failed")
		return
	}

	cfg, err := loadConfigHook(*configPath)
	if err != nil {
		fatalHook(err, "Failed to load config")
		return
	}

	ctx := context.Background()
	db, err := connectDBHook(ctx, cfg)
	if err != nil {
		fatalHook(err, "Failed to connect database")
		return
	}
	defer db.Close()

	h := buildHandlerHook(cfg, store.New(db))
	srv := newHTTPServerHook(cfg, h)

	stop := make(chan os.Signal, 1)
	notifySignalsHook(stop, os.Interrupt, syscall.SIGTERM)
	defer stopSignalsHook(stop)
	serve := listenAndServeHook

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("OAuth2 module listening")
		if err := serve(srv); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatalHook(err, "OAuth2 server failed")
		}
	}()

	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := shutdownServerHook(srv, shutdownCtx); err != nil {
		log.Error().Err(err).Msg("OAuth2 server shutdown failed")
	}
}

func loadConfig(path string) (*Config, error) {
	// #nosec G304 -- path is operator-controlled via CLI flag / env configuration.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Address == "" {
		cfg.Server.Address = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8083
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 15 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 15 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 60 * time.Second
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 20
	}
	if cfg.Database.MinConns == 0 {
		cfg.Database.MinConns = 2
	}
	if cfg.OAuth2.Issuer == "" {
		cfg.OAuth2.Issuer = "http://localhost:8083"
	}
	if cfg.OAuth2.BaseURL == "" {
		cfg.OAuth2.BaseURL = cfg.OAuth2.Issuer
	}
	cfg.OAuth2.BaseURL = strings.TrimRight(cfg.OAuth2.BaseURL, "/")
	if cfg.OAuth2.DeviceCodeTTL == 0 {
		cfg.OAuth2.DeviceCodeTTL = 10 * time.Minute
	}
	if cfg.OAuth2.DevicePollInterval == 0 {
		cfg.OAuth2.DevicePollInterval = 5
	}
	if cfg.OAuth2.DeviceVerificationURI == "" {
		cfg.OAuth2.DeviceVerificationURI = cfg.OAuth2.BaseURL + "/oauth2/device/verify"
	}
}

func connectDB(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = cfg.Database.MaxConns
	poolCfg.MinConns = cfg.Database.MinConns

	db, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(pingCtx, db); err != nil {
		poolCloseHook(db)
		return nil, err
	}
	return db, nil
}

func newHTTPServer(cfg *Config, oauthHandler *handler.OAuth2Handler) *http.Server {
	mux := http.NewServeMux()
	registerRoutes(mux, oauthHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"oauth2"}`))
	})

	return &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
}

func registerRoutes(mux *http.ServeMux, h *handler.OAuth2Handler) {
	mux.HandleFunc("/oauth2/authorize", h.HandleAuthorize)
	mux.HandleFunc("/oauth2/token", h.HandleToken)
	mux.HandleFunc("/oauth2/introspect", h.HandleIntrospect)
	mux.HandleFunc("/oauth2/revoke", h.HandleRevoke)
	mux.HandleFunc("/oauth2/device/authorize", h.HandleDeviceAuthorization)
	mux.HandleFunc("/.well-known/openid-configuration", h.HandleDiscovery)
	mux.HandleFunc("/.well-known/jwks.json", h.HandleJWKS)
	mux.HandleFunc("/oauth2/userinfo", h.HandleUserInfo)
	mux.HandleFunc("/oidc/userinfo", h.HandleUserInfo)
}

type deviceStoreAdapter struct {
	*store.Store
}

func (a *deviceStoreAdapter) GetDeviceCode(ctx context.Context, deviceCode string) (*store.DeviceCode, error) {
	return a.GetDeviceCodeByDeviceCode(ctx, deviceCode)
}

func (a *deviceStoreAdapter) MarkDeviceCodeApproved(ctx context.Context, deviceCode, identityID string, scopes []string) error {
	dc, err := a.GetDeviceCodeByDeviceCode(ctx, deviceCode)
	if err != nil {
		return err
	}
	return a.ApproveDeviceCode(ctx, dc.UserCode, identityID, "")
}

func (a *deviceStoreAdapter) MarkDeviceCodeDenied(ctx context.Context, deviceCode string) error {
	dc, err := a.GetDeviceCodeByDeviceCode(ctx, deviceCode)
	if err != nil {
		return err
	}
	return a.DenyDeviceCode(ctx, dc.UserCode)
}

func (a *deviceStoreAdapter) MarkDeviceCodeUsed(ctx context.Context, deviceCode string) error {
	return a.DeleteDeviceCode(ctx, deviceCode)
}

type accessTokenValidator struct {
	store interface {
		GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error)
	}
	publicKey []byte
	algorithm string
	issuer    string
}

func (v *accessTokenValidator) ValidateAccessToken(ctx context.Context, token string) (*oidc.AccessToken, error) {
	verifyResult, err := platformjwt.Verify(
		token,
		v.publicKey,
		v.algorithm,
		platformjwt.VerifyOptions{
			Issuer: v.issuer,
			Leeway: accessTokenVerifierLeeway,
		},
	)
	if err != nil {
		return nil, err
	}
	jti := strings.TrimSpace(verifyResult.Claims.JWTID)
	if jti == "" {
		return nil, errors.New("token is missing jti")
	}

	at, err := v.store.GetAccessToken(ctx, jti)
	if err != nil {
		return nil, err
	}
	if at.Revoked || time.Now().UTC().After(at.ExpiresAt) {
		return nil, errors.New("token is inactive")
	}
	if verifyResult.Claims.Subject != "" && at.Subject != "" && verifyResult.Claims.Subject != at.Subject {
		return nil, errors.New("token subject mismatch")
	}
	if verifyResult.Claims.Issuer != "" && at.Issuer != "" && verifyResult.Claims.Issuer != at.Issuer {
		return nil, errors.New("token issuer mismatch")
	}
	return &oidc.AccessToken{
		JTI:        at.JTI,
		IdentityID: at.IdentityID,
		ClientID:   at.ClientID,
		Scopes:     at.Scopes,
		ExpiresAt:  at.ExpiresAt,
	}, nil
}

type userInfoProvider struct{}

func (p *userInfoProvider) GetUserInfo(ctx context.Context, identityID string, scopes []string) (*oidc.UserInfoClaims, error) {
	claims := &oidc.UserInfoClaims{Sub: identityID}
	if containsScope(scopes, "profile") {
		name := identityID
		claims.Name = &name
	}
	if containsScope(scopes, "email") {
		email := identityID + "@example.local"
		emailVerified := false
		claims.Email = &email
		claims.EmailVerified = &emailVerified
	}
	return claims, nil
}

type oauth2JWTSigner struct {
	keyPair *platformjwt.KeyPair
}

func (s *oauth2JWTSigner) SignAccessToken(claims map[string]interface{}) (string, error) {
	return s.sign(claims)
}

func (s *oauth2JWTSigner) SignIDToken(claims map[string]interface{}) (string, error) {
	return s.sign(claims)
}

func (s *oauth2JWTSigner) sign(rawClaims map[string]interface{}) (string, error) {
	if s == nil || s.keyPair == nil {
		return "", errors.New("oauth2 jwt signer is not configured")
	}

	claims, err := mapClaimsToJWTClaims(rawClaims)
	if err != nil {
		return "", err
	}

	return platformjwt.Sign(claims, s.keyPair.PrivateKey, s.keyPair.Algorithm, s.keyPair.KeyID)
}

type staticJWKSProvider struct {
	keys []oidc.JWK
}

func (p *staticJWKSProvider) GetPublicKeys(ctx context.Context) ([]oidc.JWK, error) {
	out := make([]oidc.JWK, len(p.keys))
	copy(out, p.keys)
	return out, nil
}

type disabledJWTAssertionValidator struct{}

func (v *disabledJWTAssertionValidator) ValidateJWTAssertion(ctx context.Context, assertion string, clientID string) (*grants.JWTAssertionClaims, error) {
	return nil, errors.New("jwt bearer assertion validation is not configured")
}

func mapClaimsToJWTClaims(rawClaims map[string]interface{}) (platformjwt.Claims, error) {
	claims := platformjwt.Claims{
		Custom: make(map[string]interface{}),
	}
	for key, value := range rawClaims {
		switch key {
		case "iss":
			iss, ok := value.(string)
			if !ok {
				return platformjwt.Claims{}, fmt.Errorf("invalid iss claim type %T", value)
			}
			claims.Issuer = iss
		case "sub":
			sub, ok := value.(string)
			if !ok {
				return platformjwt.Claims{}, fmt.Errorf("invalid sub claim type %T", value)
			}
			claims.Subject = sub
		case "aud":
			aud, err := normalizeAudienceClaim(value)
			if err != nil {
				return platformjwt.Claims{}, err
			}
			claims.Audience = aud
		case "exp":
			exp, err := normalizeUnixClaim("exp", value)
			if err != nil {
				return platformjwt.Claims{}, err
			}
			claims.ExpiresAt = exp
		case "nbf":
			nbf, err := normalizeUnixClaim("nbf", value)
			if err != nil {
				return platformjwt.Claims{}, err
			}
			claims.NotBefore = nbf
		case "iat":
			iat, err := normalizeUnixClaim("iat", value)
			if err != nil {
				return platformjwt.Claims{}, err
			}
			claims.IssuedAt = iat
		case "jti":
			jti, ok := value.(string)
			if !ok {
				return platformjwt.Claims{}, fmt.Errorf("invalid jti claim type %T", value)
			}
			claims.JWTID = jti
		case "sid":
			sid, ok := value.(string)
			if !ok {
				return platformjwt.Claims{}, fmt.Errorf("invalid sid claim type %T", value)
			}
			claims.SessionID = sid
		default:
			claims.Custom[key] = value
		}
	}
	return claims, nil
}

func normalizeAudienceClaim(value interface{}) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []string:
		if len(typed) == 0 {
			return "", nil
		}
		return typed[0], nil
	case []interface{}:
		if len(typed) == 0 {
			return "", nil
		}
		first, ok := typed[0].(string)
		if !ok {
			return "", fmt.Errorf("invalid aud claim item type %T", typed[0])
		}
		return first, nil
	default:
		return "", fmt.Errorf("invalid aud claim type %T", value)
	}
}

func normalizeUnixClaim(claimName string, value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid %s claim value: %w", claimName, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid %s claim type %T", claimName, value)
	}
}

func newOAuth2SigningKeyPair() (*platformjwt.KeyPair, error) {
	keyID := strings.TrimSpace(os.Getenv(signingKeyIDEnv))
	if keyID == "" {
		keyID = defaultSigningKeyID
	}

	privateKeyB64 := strings.TrimSpace(os.Getenv(signingPrivateKeyB64Env))
	publicKeyB64 := strings.TrimSpace(os.Getenv(signingPublicKeyB64Env))

	switch {
	case privateKeyB64 == "" && publicKeyB64 == "":
		if isProductionEnvironment() {
			return nil, errors.New("production requires static OAuth2 signing keys via AEGION_OAUTH2_SIGNING_PRIVATE_KEY_B64 and AEGION_OAUTH2_SIGNING_PUBLIC_KEY_B64")
		}
		keyPair, err := generateSigningKeyPairHook(keyID)
		if err != nil {
			return nil, fmt.Errorf("generate signing key pair: %w", err)
		}
		if keyPair.Algorithm == "" {
			keyPair.Algorithm = defaultSigningAlgorithm
		}
		if err := validateSigningKeyPairHook(keyPair); err != nil {
			return nil, err
		}
		log.Warn().Str("key_id", keyPair.KeyID).Msg("Using ephemeral OAuth2 signing keys; configure static signing keys for production")
		return keyPair, nil
	case privateKeyB64 == "" || publicKeyB64 == "":
		return nil, fmt.Errorf("both %s and %s must be set together", signingPrivateKeyB64Env, signingPublicKeyB64Env)
	default:
		privateKey, err := decodeBase64Key(privateKeyB64)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", signingPrivateKeyB64Env, err)
		}
		publicKey, err := decodeBase64Key(publicKeyB64)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", signingPublicKeyB64Env, err)
		}

		keyPair := &platformjwt.KeyPair{
			Algorithm:  defaultSigningAlgorithm,
			KeyID:      keyID,
			PrivateKey: privateKey,
			PublicKey:  publicKey,
		}
		if err := validateSigningKeyPairHook(keyPair); err != nil {
			return nil, err
		}

		return keyPair, nil
	}
}

func validateSigningKeyPair(keyPair *platformjwt.KeyPair) error {
	now := time.Now().UTC()
	token, err := platformjwt.Sign(
		platformjwt.Claims{
			Issuer:    selfTestIssuer,
			Subject:   "self-test",
			JWTID:     "self-test",
			IssuedAt:  now.Unix(),
			ExpiresAt: now.Add(60 * time.Second).Unix(),
		},
		keyPair.PrivateKey,
		keyPair.Algorithm,
		keyPair.KeyID,
	)
	if err != nil {
		return fmt.Errorf("signing key self-test failed: %w", err)
	}
	if _, err := platformjwt.Verify(
		token,
		keyPair.PublicKey,
		keyPair.Algorithm,
		platformjwt.VerifyOptions{Issuer: selfTestIssuer},
	); err != nil {
		return fmt.Errorf("signing keypair verification failed: %w", err)
	}
	return nil
}

func decodeBase64Key(input string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(input)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64 key encoding")
}

func newStaticJWKSProvider(keyPair *platformjwt.KeyPair) (*staticJWKSProvider, error) {
	jwkJSON, err := toJWKHook(keyPair.Algorithm, keyPair.KeyID, keyPair.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("build jwk: %w", err)
	}
	var key oidc.JWK
	if err := json.Unmarshal([]byte(jwkJSON), &key); err != nil {
		return nil, fmt.Errorf("decode jwk: %w", err)
	}
	if key.KID == "" {
		key.KID = keyPair.KeyID
	}
	if key.ALG == "" {
		key.ALG = keyPair.Algorithm
	}
	if key.USE == "" {
		key.USE = "sig"
	}
	return &staticJWKSProvider{keys: []oidc.JWK{key}}, nil
}

func buildHandler(cfg *Config, oauthStore *store.Store) *handler.OAuth2Handler {
	keyPair, err := newOAuth2SigningKeyPairHook()
	if err != nil {
		panic(fmt.Sprintf("initialize oauth2 signing keys: %v", err))
	}
	signer := &oauth2JWTSigner{keyPair: keyPair}
	jwksProvider, err := newStaticJWKSProviderHook(keyPair)
	if err != nil {
		panic(fmt.Sprintf("initialize oauth2 jwks provider: %v", err))
	}
	deviceStore := &deviceStoreAdapter{Store: oauthStore}

	authzSvc := authorization.NewAuthorizationService(oauthStore)
	tokenSvc := tokenservice.NewTokenService(oauthStore, signer, cfg.OAuth2.Issuer)
	revocationSvc := revocation.NewRevocationService(oauthStore)
	deviceSvc := device.NewDeviceService(deviceStore, cfg.OAuth2.DeviceCodeTTL, cfg.OAuth2.DevicePollInterval, cfg.OAuth2.DeviceVerificationURI)
	clientCredsSvc := grants.NewClientCredentialsService(oauthStore, signer, cfg.OAuth2.Issuer)
	jwtBearerSvc := grants.NewJWTBearerService(oauthStore, signer, cfg.OAuth2.Issuer, &disabledJWTAssertionValidator{})
	discoverySvc := oidc.NewDiscoveryService(cfg.OAuth2.Issuer, cfg.OAuth2.BaseURL)
	jwksSvc := oidc.NewJWKSService(jwksProvider)
	introspectSvc := introspection.NewService(tokenSvc)
	userInfoSvc := oidc.NewUserInfoService(&accessTokenValidator{
		store:     oauthStore,
		publicKey: keyPair.PublicKey,
		algorithm: keyPair.Algorithm,
		issuer:    cfg.OAuth2.Issuer,
	}, &userInfoProvider{})

	return handler.NewOAuth2Handler(
		authzSvc,
		tokenSvc,
		revocationSvc,
		deviceSvc,
		clientCredsSvc,
		jwtBearerSvc,
		discoverySvc,
		jwksSvc,
		userInfoSvc,
	).WithIntrospectionService(introspectSvc)
}

func containsScope(scopes []string, wanted string) bool {
	for _, s := range scopes {
		if s == wanted {
			return true
		}
	}
	return false
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func isProductionEnvironment() bool {
	for _, key := range []string{"AEGION_ENV", "AEGION_ENVIRONMENT", "APP_ENV", "ENV"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		switch value {
		case "prod", "production":
			return true
		}
	}
	return false
}
