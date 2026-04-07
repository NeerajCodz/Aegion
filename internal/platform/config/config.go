// Package config provides configuration loading and validation for Aegion.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete Aegion configuration.
type Config struct {
	ModuleVersions map[string]string `yaml:"module_versions"`
	ModuleRegistry ModuleRegistry    `yaml:"module_registry"`
	Server         ServerConfig      `yaml:"server"`
	Database       DatabaseConfig    `yaml:"database"`
	Cache          CacheConfig       `yaml:"cache"`
	Secrets        SecretsConfig     `yaml:"secrets"`
	Log            LogConfig         `yaml:"log"`
	Operator       OperatorConfig    `yaml:"operator"`
	Courier        CourierConfig     `yaml:"courier"`
	Sessions       SessionsConfig    `yaml:"sessions"`
	Identity       IdentityConfig    `yaml:"identity"`
	Security       SecurityConfig    `yaml:"security"`
	Password       PasswordConfig    `yaml:"password"`
	MagicLink      MagicLinkConfig   `yaml:"magic_link"`
	Admin          AdminConfig       `yaml:"admin"`
	Policy         PolicyConfig      `yaml:"policy"`
	Proxy          ProxyConfig       `yaml:"proxy"`
}

// ModuleRegistry configures where to pull module images from.
type ModuleRegistry struct {
	BaseURL    string `yaml:"base_url"`
	PullPolicy string `yaml:"pull_policy"`
}

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	Port           int               `yaml:"port"`
	Host           string            `yaml:"host"`
	TLS            TLSConfig         `yaml:"tls"`
	CORS           CORSConfig        `yaml:"cors"`
	RequestTimeout Duration          `yaml:"request_timeout"`
	ReadTimeout    Duration          `yaml:"read_timeout"`
	WriteTimeout   Duration          `yaml:"write_timeout"`
	IdleTimeout    Duration          `yaml:"idle_timeout"`
	InternalNet    InternalNetConfig `yaml:"internal_network"`
}

// TLSConfig configures TLS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// CORSConfig configures CORS settings.
type CORSConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"`
}

// InternalNetConfig configures the internal Docker network.
type InternalNetConfig struct {
	Name               string   `yaml:"name"`
	Subnet             string   `yaml:"subnet"`
	HealthCheckInt     Duration `yaml:"health_check_interval"`
	HealthCheckTimeout Duration `yaml:"health_check_timeout"`
	HealthCheckFails   int      `yaml:"health_check_failures"`
	RestartOnFailure   bool     `yaml:"restart_on_failure"`
	StartupTimeout     Duration `yaml:"startup_timeout"`
}

// DatabaseConfig configures the database connection.
type DatabaseConfig struct {
	URL             string   `yaml:"url"`
	MaxOpenConns    int      `yaml:"max_open_connections"`
	MaxIdleConns    int      `yaml:"max_idle_connections"`
	ConnMaxLifetime Duration `yaml:"connection_max_lifetime"`
	ConnMaxIdleTime Duration `yaml:"connection_max_idle_time"`
	MigrateOnly     bool     `yaml:"migrate_only"`
}

// CacheConfig configures the Redis cache.
type CacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	URL       string `yaml:"url"`
	KeyPrefix string `yaml:"key_prefix"`
}

// SecretsConfig holds encryption and signing secrets.
type SecretsConfig struct {
	Cookie   []string `yaml:"cookie"`
	Cipher   []string `yaml:"cipher"`
	Internal []string `yaml:"internal"`
}

// LogConfig configures logging.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// OperatorConfig configures the bootstrap operator.
type OperatorConfig struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

// CourierConfig configures email/SMS delivery.
type CourierConfig struct {
	SMTP SMTPConfig `yaml:"smtp"`
}

// SMTPConfig configures SMTP delivery.
type SMTPConfig struct {
	Host        string   `yaml:"host"`
	Port        int      `yaml:"port"`
	FromAddress string   `yaml:"from_address"`
	FromName    string   `yaml:"from_name"`
	Auth        SMTPAuth `yaml:"auth"`
}

// SMTPAuth configures SMTP authentication.
type SMTPAuth struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// SessionsConfig configures session management.
type SessionsConfig struct {
	Cookie             CookieConfig `yaml:"cookie"`
	Lifespan           Duration     `yaml:"lifespan"`
	IdleTimeout        Duration     `yaml:"idle_timeout"`
	RememberMeLifespan Duration     `yaml:"remember_me_lifespan"`
}

// CookieConfig configures session cookies.
type CookieConfig struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	SameSite string `yaml:"same_site"`
	Secure   bool   `yaml:"secure"`
	HTTPOnly bool   `yaml:"http_only"`
}

// IdentityConfig configures identity schemas.
type IdentityConfig struct {
	DefaultSchema string         `yaml:"default_schema"`
	Schemas       []SchemaConfig `yaml:"schemas"`
}

// SchemaConfig defines an identity schema.
type SchemaConfig struct {
	ID  string `yaml:"id"`
	URL string `yaml:"url"`
}

// SecurityConfig configures security settings.
type SecurityConfig struct {
	AccountEnumMitigation bool             `yaml:"account_enumeration_mitigation"`
	RateLimits            RateLimitsConfig `yaml:"rate_limits"`
	BruteForce            BruteForceConfig `yaml:"brute_force"`
}

// RateLimitsConfig configures rate limits.
type RateLimitsConfig struct {
	Login         RateLimitRule `yaml:"login"`
	Registration  RateLimitRule `yaml:"registration"`
	EmailDelivery RateLimitRule `yaml:"email_delivery"`
}

// RateLimitRule defines a rate limit.
type RateLimitRule struct {
	Requests int      `yaml:"requests"`
	Period   Duration `yaml:"period"`
}

// BruteForceConfig configures brute force protection.
type BruteForceConfig struct {
	MaxAttempts     int      `yaml:"max_attempts"`
	LockoutDuration Duration `yaml:"lockout_duration"`
}

// PasswordConfig configures the password module.
type PasswordConfig struct {
	Enabled                 bool     `yaml:"enabled"`
	MinLength               int      `yaml:"min_length"`
	RequireUppercase        bool     `yaml:"require_uppercase"`
	RequireLowercase        bool     `yaml:"require_lowercase"`
	RequireNumber           bool     `yaml:"require_number"`
	RequireSpecial          bool     `yaml:"require_special"`
	HIBPEnabled             bool     `yaml:"hibp_enabled"`
	HIBPHost                string   `yaml:"hibp_host"`
	HIBPTimeout             Duration `yaml:"hibp_timeout"`
	HIBPIgnoreNetworkErrors bool     `yaml:"hibp_ignore_network_errors"`
	HIBPMinBreachCount      int      `yaml:"hibp_min_breach_count"`
	HistoryCount            int      `yaml:"history_count"`
}

// MagicLinkConfig configures the magic link module.
type MagicLinkConfig struct {
	Enabled           bool     `yaml:"enabled"`
	CodeLength        int      `yaml:"code_length"`
	CodeCharset       string   `yaml:"code_charset"`
	LinkLifespan      Duration `yaml:"link_lifespan"`
	CodeLifespan      Duration `yaml:"code_lifespan"`
	RateLimit         int      `yaml:"rate_limit"`
	RateWindow        Duration `yaml:"rate_window"`
	RecoveryRateLimit int      `yaml:"recovery_rate_limit"`
}

// AdminConfig configures the admin module.
type AdminConfig struct {
	Enabled               bool            `yaml:"enabled"`
	Path                  string          `yaml:"path"`
	SessionLifespan       Duration        `yaml:"session_lifespan"`
	DefaultPageSize       int             `yaml:"default_page_size"`
	MaxPageSize           int             `yaml:"max_page_size"`
	APIKeyPrefix          string          `yaml:"api_key_prefix"`
	APIKeyLookupPrefixLen int             `yaml:"api_key_lookup_prefix_len"`
	APIKeyEntropyBytes    int             `yaml:"api_key_entropy_bytes"`
	SCIM                  AdminSCIMConfig `yaml:"scim"`
}

// AdminSCIMConfig configures SCIM behavior under the admin module.
type AdminSCIMConfig struct {
	Enabled                    bool     `yaml:"enabled"`
	BasePath                   string   `yaml:"base_path"`
	TokenPrefix                string   `yaml:"token_prefix"`
	TokenLookupPrefixLen       int      `yaml:"token_lookup_prefix_len"`
	TokenEntropyBytes          int      `yaml:"token_entropy_bytes"`
	DefaultPageSize            int      `yaml:"default_page_size"`
	MaxPageSize                int      `yaml:"max_page_size"`
	TokenLastUsedUpdateTimeout Duration `yaml:"token_last_used_update_timeout"`
}

// PolicyConfig configures the phase-3 policy engine defaults.
type PolicyConfig struct {
	Enabled      bool              `yaml:"enabled"`
	DefaultModel string            `yaml:"default_model"`
	RBAC         PolicyRBACConfig  `yaml:"rbac"`
	ABAC         PolicyABACConfig  `yaml:"abac"`
	ReBAC        PolicyReBACConfig `yaml:"rebac"`
}

// PolicyRBACConfig configures RBAC defaults.
type PolicyRBACConfig struct {
	Enabled bool `yaml:"enabled"`
}

// PolicyABACConfig configures ABAC defaults.
type PolicyABACConfig struct {
	Enabled bool `yaml:"enabled"`
}

// PolicyReBACConfig configures ReBAC defaults.
type PolicyReBACConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ProxyConfig configures phase-3 identity-aware proxy defaults.
type ProxyConfig struct {
	Enabled                     bool     `yaml:"enabled"`
	UpstreamTimeout             Duration `yaml:"upstream_timeout"`
	PreserveHost                bool     `yaml:"preserve_host"`
	StripInboundIdentityHeaders bool     `yaml:"strip_inbound_identity_headers"`
	IdentitySigningSecret       string   `yaml:"identity_signing_secret"`
	IdentitySignatureHeader     string   `yaml:"identity_signature_header"`
	SignedIdentityHeaders       []string `yaml:"signed_identity_headers"`
}

// Duration wraps time.Duration for YAML unmarshaling.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	// Override with environment variables
	applyEnvOverrides(&cfg)

	return &cfg, nil
}

// applyDefaults sets default values for unset fields.
func applyDefaults(cfg *Config) {
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.RequestTimeout == 0 {
		cfg.Server.RequestTimeout = Duration(60 * time.Second)
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = Duration(30 * time.Second)
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = Duration(60 * time.Second)
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = Duration(120 * time.Second)
	}
	if cfg.Database.MaxOpenConns == 0 {
		cfg.Database.MaxOpenConns = 25
	}
	if cfg.Database.MaxIdleConns == 0 {
		cfg.Database.MaxIdleConns = 10
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "json"
	}
	if cfg.Sessions.Lifespan == 0 {
		cfg.Sessions.Lifespan = Duration(24 * time.Hour)
	}
	if cfg.Sessions.IdleTimeout == 0 {
		cfg.Sessions.IdleTimeout = Duration(2 * time.Hour)
	}
	if cfg.Sessions.Cookie.Name == "" {
		cfg.Sessions.Cookie.Name = "aegion_session"
	}
	if cfg.Sessions.Cookie.Path == "" {
		cfg.Sessions.Cookie.Path = "/"
	}
	if cfg.Sessions.Cookie.SameSite == "" {
		cfg.Sessions.Cookie.SameSite = "lax"
	}
	if cfg.Password.MinLength == 0 {
		cfg.Password.MinLength = 8
	}
	if cfg.Password.HIBPHost == "" {
		cfg.Password.HIBPHost = "api.pwnedpasswords.com"
	}
	if cfg.Password.HIBPTimeout == 0 {
		cfg.Password.HIBPTimeout = Duration(5 * time.Second)
	}
	if cfg.Password.HIBPMinBreachCount == 0 {
		cfg.Password.HIBPMinBreachCount = 1
	}
	if !cfg.Password.HIBPEnabled {
		cfg.Password.HIBPIgnoreNetworkErrors = true
	}
	if cfg.MagicLink.CodeLength == 0 {
		cfg.MagicLink.CodeLength = 6
	}
	if cfg.MagicLink.CodeCharset == "" {
		cfg.MagicLink.CodeCharset = "0123456789"
	}
	if cfg.MagicLink.LinkLifespan == 0 {
		cfg.MagicLink.LinkLifespan = Duration(15 * time.Minute)
	}
	if cfg.MagicLink.CodeLifespan == 0 {
		cfg.MagicLink.CodeLifespan = Duration(15 * time.Minute)
	}
	if cfg.MagicLink.RateLimit == 0 {
		cfg.MagicLink.RateLimit = 5
	}
	if cfg.MagicLink.RateWindow == 0 {
		cfg.MagicLink.RateWindow = Duration(time.Hour)
	}
	if cfg.MagicLink.RecoveryRateLimit == 0 {
		cfg.MagicLink.RecoveryRateLimit = 3
	}
	if cfg.Admin.Path == "" {
		cfg.Admin.Path = "/aegion"
	}
	if cfg.Admin.SessionLifespan == 0 {
		cfg.Admin.SessionLifespan = Duration(4 * time.Hour)
	}
	if cfg.Admin.DefaultPageSize == 0 {
		cfg.Admin.DefaultPageSize = 20
	}
	if cfg.Admin.MaxPageSize == 0 {
		cfg.Admin.MaxPageSize = 100
	}
	if cfg.Admin.APIKeyPrefix == "" {
		cfg.Admin.APIKeyPrefix = "aegion_"
	}
	if cfg.Admin.APIKeyLookupPrefixLen == 0 {
		cfg.Admin.APIKeyLookupPrefixLen = 12
	}
	if cfg.Admin.APIKeyEntropyBytes == 0 {
		cfg.Admin.APIKeyEntropyBytes = 32
	}
	if cfg.Admin.SCIM.BasePath == "" {
		cfg.Admin.SCIM.BasePath = "/scim/v2"
	}
	if cfg.Admin.SCIM.TokenPrefix == "" {
		cfg.Admin.SCIM.TokenPrefix = "aegion_scim_"
	}
	if cfg.Admin.SCIM.TokenLookupPrefixLen == 0 {
		cfg.Admin.SCIM.TokenLookupPrefixLen = 12
	}
	if cfg.Admin.SCIM.TokenEntropyBytes == 0 {
		cfg.Admin.SCIM.TokenEntropyBytes = 32
	}
	if cfg.Admin.SCIM.DefaultPageSize == 0 {
		cfg.Admin.SCIM.DefaultPageSize = 20
	}
	if cfg.Admin.SCIM.MaxPageSize == 0 {
		cfg.Admin.SCIM.MaxPageSize = 1000
	}
	if cfg.Admin.SCIM.TokenLastUsedUpdateTimeout == 0 {
		cfg.Admin.SCIM.TokenLastUsedUpdateTimeout = Duration(2 * time.Second)
	}
	if cfg.Policy.DefaultModel == "" {
		cfg.Policy.DefaultModel = "rbac"
	}
	if cfg.Proxy.UpstreamTimeout == 0 {
		cfg.Proxy.UpstreamTimeout = Duration(30 * time.Second)
	}
	if cfg.Proxy.IdentitySignatureHeader == "" {
		cfg.Proxy.IdentitySignatureHeader = "X-Aegion-Signature"
	}
	if len(cfg.Proxy.SignedIdentityHeaders) == 0 {
		cfg.Proxy.SignedIdentityHeaders = []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"}
	}
}

// applyEnvOverrides overrides config with environment variables.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("AEGION_DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("AEGION_CACHE_URL"); v != "" {
		cfg.Cache.URL = v
	}
	if v := os.Getenv("AEGION_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("AEGION_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
		}
	}
	// Cookie secrets from env (comma-separated)
	if v := os.Getenv("AEGION_SECRETS_COOKIE"); v != "" {
		cfg.Secrets.Cookie = strings.Split(v, ",")
	}
	if v := os.Getenv("AEGION_SECRETS_CIPHER"); v != "" {
		cfg.Secrets.Cipher = strings.Split(v, ",")
	}
	if v := os.Getenv("AEGION_SECRETS_INTERNAL"); v != "" {
		cfg.Secrets.Internal = strings.Split(v, ",")
	}
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("database.url is required")
	}
	if len(c.Secrets.Cookie) == 0 {
		return fmt.Errorf("secrets.cookie is required")
	}
	if len(c.Secrets.Cipher) == 0 {
		return fmt.Errorf("secrets.cipher is required")
	}
	if len(c.Secrets.Internal) == 0 {
		return fmt.Errorf("secrets.internal is required")
	}
	for _, s := range c.Secrets.Cookie {
		if len(s) < 32 {
			return fmt.Errorf("cookie secret must be at least 32 characters")
		}
	}
	for _, s := range c.Secrets.Cipher {
		if len(s) < 32 {
			return fmt.Errorf("cipher secret must be at least 32 characters")
		}
	}
	return nil
}
