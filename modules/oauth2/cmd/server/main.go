package main

import (
	"context"
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

	"github.com/aegion/aegion/modules/oauth2/handler"
	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/service/device"
	"github.com/aegion/aegion/modules/oauth2/service/grants"
	"github.com/aegion/aegion/modules/oauth2/service/oidc"
	"github.com/aegion/aegion/modules/oauth2/service/revocation"
	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/aegion/aegion/modules/oauth2/store"
)

const version = "1.0.0"

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

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	ctx := context.Background()
	db, err := connectDB(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect database")
	}
	defer db.Close()

	h := buildHandler(cfg, store.New(db))
	srv := newHTTPServer(cfg, h)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("OAuth2 module listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("OAuth2 server failed")
		}
	}()

	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("OAuth2 server shutdown failed")
	}
}

func loadConfig(path string) (*Config, error) {
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

	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.Ping(pingCtx); err != nil {
		db.Close()
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
	mux.HandleFunc("/oauth2/revoke", h.HandleRevoke)
	mux.HandleFunc("/oauth2/device/authorize", h.HandleDeviceAuthorization)
	mux.HandleFunc("/.well-known/openid-configuration", h.HandleDiscovery)
	mux.HandleFunc("/.well-known/jwks.json", h.HandleJWKS)
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
}

func (v *accessTokenValidator) ValidateAccessToken(ctx context.Context, token string) (*oidc.AccessToken, error) {
	at, err := v.store.GetAccessToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if at.Revoked || time.Now().UTC().After(at.ExpiresAt) {
		return nil, errors.New("token is inactive")
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

func buildHandler(cfg *Config, oauthStore *store.Store) *handler.OAuth2Handler {
	signer := &tokenservice.MockJWTSigner{}
	deviceStore := &deviceStoreAdapter{Store: oauthStore}

	authzSvc := authorization.NewAuthorizationService(oauthStore)
	tokenSvc := tokenservice.NewTokenService(oauthStore, signer, cfg.OAuth2.Issuer)
	revocationSvc := revocation.NewRevocationService(oauthStore)
	deviceSvc := device.NewDeviceService(deviceStore, cfg.OAuth2.DeviceCodeTTL, cfg.OAuth2.DevicePollInterval, cfg.OAuth2.DeviceVerificationURI)
	clientCredsSvc := grants.NewClientCredentialsService(oauthStore, signer, cfg.OAuth2.Issuer)
	jwtBearerSvc := grants.NewJWTBearerService(oauthStore, signer, cfg.OAuth2.Issuer, &grants.MockJWTValidator{})
	discoverySvc := oidc.NewDiscoveryService(cfg.OAuth2.Issuer, cfg.OAuth2.BaseURL)
	jwksSvc := oidc.NewJWKSService(&oidc.MockJWKSProvider{})
	userInfoSvc := oidc.NewUserInfoService(&accessTokenValidator{store: oauthStore}, &userInfoProvider{})

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
	)
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
