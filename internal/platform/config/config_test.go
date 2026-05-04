package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expected    time.Duration
		wantErr     bool
	}{
		{
			name:        "valid duration seconds",
			yamlContent: "duration: 30s",
			expected:    30 * time.Second,
			wantErr:     false,
		},
		{
			name:        "valid duration minutes",
			yamlContent: "duration: 5m",
			expected:    5 * time.Minute,
			wantErr:     false,
		},
		{
			name:        "valid duration hours",
			yamlContent: "duration: 2h",
			expected:    2 * time.Hour,
			wantErr:     false,
		},
		{
			name:        "valid duration mixed",
			yamlContent: "duration: 1h30m45s",
			expected:    1*time.Hour + 30*time.Minute + 45*time.Second,
			wantErr:     false,
		},
		{
			name:        "invalid duration format",
			yamlContent: "duration: invalid",
			expected:    0,
			wantErr:     true,
		},
		{
			name:        "empty duration",
			yamlContent: "duration: ''",
			expected:    0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg struct {
				Duration Duration `yaml:"duration"`
			}

			err := yaml.Unmarshal([]byte(tt.yamlContent), &cfg)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Duration.Duration())
		})
	}
}

func TestDuration_Duration(t *testing.T) {
	d := Duration(5 * time.Minute)
	assert.Equal(t, 5*time.Minute, d.Duration())
}

func TestLoad(t *testing.T) {
	// Create a temporary config file for testing
	configContent := `
module_versions:
  password: v1.0.0
  magic_link: v1.0.0

server:
  port: 8080
  host: localhost
  request_timeout: 60s

database:
  url: postgres://user:pass@localhost/db
  max_open_connections: 25

cache:
  enabled: true
  url: redis://localhost:6379

secrets:
  cookie:
    - "test-cookie-secret-32-characters-long"
  cipher:
    - "test-cipher-secret-32-characters-long"
  internal:
    - "test-internal-secret-32-characters-long"

log:
  level: info
  format: json

sessions:
  lifespan: 24h
  idle_timeout: 2h
  cookie:
    name: aegion_session
    path: /
    same_site: lax

password:
  enabled: true
  min_length: 8
  require_uppercase: true
  hibp_enabled: true
  hibp_host: api.pwnedpasswords.com
  hibp_timeout: 5s
  hibp_ignore_network_errors: true
  hibp_min_breach_count: 1

magic_link:
  enabled: true
  code_length: 6
  link_lifespan: 15m
  rate_limit: 5
  rate_window: 1h
  recovery_rate_limit: 3
`

	// Create temporary file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "v1.0.0", cfg.ModuleVersions["password"])
	assert.Equal(t, "v1.0.0", cfg.ModuleVersions["magic_link"])
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, Duration(60*time.Second), cfg.Server.RequestTimeout)
	assert.Equal(t, "postgres://user:pass@localhost/db", cfg.Database.URL)
	assert.Equal(t, 25, cfg.Database.MaxOpenConns)
	assert.True(t, cfg.Cache.Enabled)
	assert.Equal(t, "redis://localhost:6379", cfg.Cache.URL)
	assert.Len(t, cfg.Secrets.Cookie, 1)
	assert.Equal(t, "test-cookie-secret-32-characters-long", cfg.Secrets.Cookie[0])
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.Equal(t, Duration(24*time.Hour), cfg.Sessions.Lifespan)
	assert.Equal(t, Duration(2*time.Hour), cfg.Sessions.IdleTimeout)
	assert.Equal(t, "aegion_session", cfg.Sessions.Cookie.Name)
	assert.True(t, cfg.Password.Enabled)
	assert.Equal(t, 8, cfg.Password.MinLength)
	assert.True(t, cfg.Password.RequireUppercase)
	assert.True(t, cfg.Password.HIBPEnabled)
	assert.Equal(t, "api.pwnedpasswords.com", cfg.Password.HIBPHost)
	assert.Equal(t, Duration(5*time.Second), cfg.Password.HIBPTimeout)
	assert.True(t, cfg.Password.HIBPIgnoreNetworkErrors)
	assert.Equal(t, 1, cfg.Password.HIBPMinBreachCount)
	assert.True(t, cfg.MagicLink.Enabled)
	assert.Equal(t, 6, cfg.MagicLink.CodeLength)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.LinkLifespan)
	assert.Equal(t, 5, cfg.MagicLink.RateLimit)
	assert.Equal(t, Duration(time.Hour), cfg.MagicLink.RateWindow)
	assert.Equal(t, 3, cfg.MagicLink.RecoveryRateLimit)
}

func TestLoad_EnvironmentVariableExpansion(t *testing.T) {
	// Set environment variables for testing
	if err := os.Setenv("TEST_DB_URL", "postgres://env-user:env-pass@env-host/env-db"); err != nil {
		t.Fatalf("failed to set TEST_DB_URL: %v", err)
	}
	if err := os.Setenv("TEST_SECRET", "example-app-secret-for-tests"); err != nil {
		t.Fatalf("failed to set TEST_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("TEST_DB_URL")
		_ = os.Unsetenv("TEST_SECRET")
	}()

	configContent := `
database:
  url: ${TEST_DB_URL}

secrets:
  cookie:
    - ${TEST_SECRET}
  cipher:
    - "static-cipher-secret-32-characters-long"
  internal:
    - "static-internal-secret-32-characters-long"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "postgres://env-user:env-pass@env-host/env-db", cfg.Database.URL)
	assert.Equal(t, "example-app-secret-for-tests", cfg.Secrets.Cookie[0])
}


func TestLoad_ProxyUpstreamTimeoutEnvironmentVariableExpansion(t *testing.T) {
	if err := os.Setenv("AEGION_PROXY_UPSTREAM_TIMEOUT", "45s"); err != nil {
		t.Fatalf("failed to set AEGION_PROXY_UPSTREAM_TIMEOUT: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("AEGION_PROXY_UPSTREAM_TIMEOUT")
	}()

	configContent := `
secrets:
  cookie:
    - "test-cookie-secret-32-characters-long"
  cipher:
    - "test-cipher-secret-32-characters-long"
  internal:
    - "test-internal-secret-32-characters-long"

proxy:
  upstream_timeout: ${AEGION_PROXY_UPSTREAM_TIMEOUT}
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, Duration(45*time.Second), cfg.Proxy.UpstreamTimeout)
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoad_InvalidYAML(t *testing.T) {
	invalidYAML := `
invalid: yaml: content:
  - malformed
    - structure
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	_, err = Load(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	content := `
database:
  url: postgres://user:pass@localhost/db
secrets:
  cookie: ["cookie-secret-32-characters-long!!"]
  cipher: ["cipher-secret-32-characters-long!!"]
  internal: ["internal-secret-32-characters-long"]
server:
  unexpected_flag: true
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "unknown.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	_, err := Load(configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field unexpected_flag not found")
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	// Server defaults
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, Duration(60*time.Second), cfg.Server.RequestTimeout)
	assert.Equal(t, Duration(30*time.Second), cfg.Server.ReadTimeout)
	assert.Equal(t, Duration(60*time.Second), cfg.Server.WriteTimeout)
	assert.Equal(t, Duration(120*time.Second), cfg.Server.IdleTimeout)

	// Database defaults
	assert.Equal(t, 25, cfg.Database.MaxOpenConns)
	assert.Equal(t, 10, cfg.Database.MaxIdleConns)

	// Log defaults
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)

	// Session defaults
	assert.Equal(t, Duration(24*time.Hour), cfg.Sessions.Lifespan)
	assert.Equal(t, Duration(2*time.Hour), cfg.Sessions.IdleTimeout)
	assert.Equal(t, "aegion_session", cfg.Sessions.Cookie.Name)
	assert.Equal(t, "/", cfg.Sessions.Cookie.Path)
	assert.Equal(t, "lax", cfg.Sessions.Cookie.SameSite)

	// Password defaults
	assert.Equal(t, 8, cfg.Password.MinLength)
	assert.Equal(t, "api.pwnedpasswords.com", cfg.Password.HIBPHost)
	assert.Equal(t, Duration(5*time.Second), cfg.Password.HIBPTimeout)
	assert.Equal(t, 1, cfg.Password.HIBPMinBreachCount)

	// Magic Link defaults
	assert.Equal(t, 6, cfg.MagicLink.CodeLength)
	assert.Equal(t, "0123456789", cfg.MagicLink.CodeCharset)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.LinkLifespan)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.CodeLifespan)
	assert.Equal(t, 5, cfg.MagicLink.RateLimit)
	assert.Equal(t, Duration(time.Hour), cfg.MagicLink.RateWindow)
	assert.Equal(t, 3, cfg.MagicLink.RecoveryRateLimit)

	// Admin defaults
	assert.Equal(t, "/aegion", cfg.Admin.Path)
	assert.Equal(t, Duration(4*time.Hour), cfg.Admin.SessionLifespan)
	assert.Equal(t, 20, cfg.Admin.DefaultPageSize)
	assert.Equal(t, 100, cfg.Admin.MaxPageSize)
	assert.Equal(t, "aegion_", cfg.Admin.APIKeyPrefix)
	assert.Equal(t, 12, cfg.Admin.APIKeyLookupPrefixLen)
	assert.Equal(t, 32, cfg.Admin.APIKeyEntropyBytes)
	assert.Equal(t, "/scim/v2", cfg.Admin.SCIM.BasePath)
	assert.Equal(t, "aegion_scim_", cfg.Admin.SCIM.TokenPrefix)
	assert.Equal(t, 12, cfg.Admin.SCIM.TokenLookupPrefixLen)
	assert.Equal(t, 32, cfg.Admin.SCIM.TokenEntropyBytes)
	assert.Equal(t, 20, cfg.Admin.SCIM.DefaultPageSize)
	assert.Equal(t, 1000, cfg.Admin.SCIM.MaxPageSize)
	assert.Equal(t, Duration(2*time.Second), cfg.Admin.SCIM.TokenLastUsedUpdateTimeout)

	// Policy defaults
	assert.Equal(t, "rbac", cfg.Policy.DefaultModel)
	assert.False(t, cfg.Policy.Enabled)
	assert.False(t, cfg.Policy.RBAC.Enabled)
	assert.False(t, cfg.Policy.ABAC.Enabled)
	assert.False(t, cfg.Policy.ReBAC.Enabled)

	// Proxy defaults
	assert.False(t, cfg.Proxy.Enabled)
	assert.Equal(t, Duration(30*time.Second), cfg.Proxy.UpstreamTimeout)
	assert.False(t, cfg.Proxy.TrustForwardedHeaders)
	assert.Equal(t, "X-Aegion-Signature", cfg.Proxy.IdentitySignatureHeader)
	assert.Equal(t, []string{"X-User-ID", "X-User-Session-ID", "X-User-AAL"}, cfg.Proxy.SignedIdentityHeaders)
	assert.Equal(t, "POST", cfg.Courier.SMS.Method)
	assert.Equal(t, Duration(10*time.Second), cfg.Courier.SMS.Timeout)

	// MFA / passkey defaults
	assert.Equal(t, "Aegion", cfg.MFA.Issuer)
	assert.Equal(t, 6, cfg.MFA.CodeDigits)
	assert.Equal(t, Duration(30*time.Second), cfg.MFA.CodePeriod)
	assert.Equal(t, 12, cfg.MFA.BackupCodeCount)
	assert.Equal(t, "aegion_mfa_trusted_device", cfg.MFA.TrustedDeviceCookieName)
	assert.Equal(t, Duration(5*time.Minute), cfg.Passkeys.ChallengeTTL)
	assert.Equal(t, 20, cfg.Passkeys.AllowedCredentials)
}

func TestApplyDefaults_DoesNotOverrideExisting(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port: 9000,
			Host: "custom.host",
		},
		Log: LogConfig{
			Level:  "debug",
			Format: "text",
		},
	}

	applyDefaults(cfg)

	// Should not override existing values
	assert.Equal(t, 9000, cfg.Server.Port)
	assert.Equal(t, "custom.host", cfg.Server.Host)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, "text", cfg.Log.Format)

	// Should set defaults for unset values
	assert.Equal(t, Duration(60*time.Second), cfg.Server.RequestTimeout)
}

func TestApplyEnvOverrides(t *testing.T) {
	// Set environment variables
	envVars := map[string]string{
		"AEGION_DATABASE_URL":                  "postgres://env:pass@host/db",
		"AEGION_CACHE_URL":                     "redis://env:6379",
		"AEGION_LOG_LEVEL":                     "debug",
		"AEGION_SERVER_PORT":                   "9090",
		"AEGION_PROXY_TRUST_FORWARDED_HEADERS": "true",
		"AEGION_SECRETS_COOKIE":                "env-cookie-1,env-cookie-2",
		"AEGION_SECRETS_CIPHER":                "env-cipher-1,env-cipher-2",
		"AEGION_SECRETS_INTERNAL":              "env-internal-1,env-internal-2",
	}

	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("failed to set %s: %v", key, err)
		}
	}
	defer func() {
		for key := range envVars {
			_ = os.Unsetenv(key)
		}
	}()

	cfg := &Config{
		Database: DatabaseConfig{URL: "original-db-url"},
		Cache:    CacheConfig{URL: "original-cache-url"},
		Log:      LogConfig{Level: "original-level"},
		Server:   ServerConfig{Port: 8080},
		Secrets: SecretsConfig{
			Cookie:   []string{"original-cookie"},
			Cipher:   []string{"original-cipher"},
			Internal: []string{"original-internal"},
		},
	}

	require.NoError(t, applyEnvOverrides(cfg))

	assert.Equal(t, "postgres://env:pass@host/db", cfg.Database.URL)
	assert.Equal(t, "redis://env:6379", cfg.Cache.URL)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.True(t, cfg.Proxy.TrustForwardedHeaders)
	assert.Equal(t, []string{"env-cookie-1", "env-cookie-2"}, cfg.Secrets.Cookie)
	assert.Equal(t, []string{"env-cipher-1", "env-cipher-2"}, cfg.Secrets.Cipher)
	assert.Equal(t, []string{"env-internal-1", "env-internal-2"}, cfg.Secrets.Internal)
}

func TestApplyEnvOverrides_FromFile(t *testing.T) {
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, "database-url.txt")
	cacheFile := filepath.Join(tempDir, "cache-url.txt")
	cookieFile := filepath.Join(tempDir, "cookie-secrets.txt")
	cipherFile := filepath.Join(tempDir, "cipher-secrets.txt")
	internalFile := filepath.Join(tempDir, "internal-secrets.txt")

	require.NoError(t, os.WriteFile(dbFile, []byte("postgres://file:pass@host/filedb?sslmode=require\n"), 0o600))
	require.NoError(t, os.WriteFile(cacheFile, []byte("redis://cache:6379/0\n"), 0o600))
	require.NoError(t, os.WriteFile(cookieFile, []byte("cookie-file-1,cookie-file-2\ncookie-file-3"), 0o600))
	require.NoError(t, os.WriteFile(cipherFile, []byte("cipher-file-1\ncipher-file-2"), 0o600))
	require.NoError(t, os.WriteFile(internalFile, []byte("internal-file-1,internal-file-2"), 0o600))

	t.Setenv("AEGION_DATABASE_URL_FILE", dbFile)
	t.Setenv("AEGION_CACHE_URL_FILE", cacheFile)
	t.Setenv("AEGION_SECRETS_COOKIE_FILE", cookieFile)
	t.Setenv("AEGION_SECRETS_CIPHER_FILE", cipherFile)
	t.Setenv("AEGION_SECRETS_INTERNAL_FILE", internalFile)

	cfg := &Config{}
	require.NoError(t, applyEnvOverrides(cfg))

	assert.Equal(t, "postgres://file:pass@host/filedb?sslmode=require", cfg.Database.URL)
	assert.Equal(t, "redis://cache:6379/0", cfg.Cache.URL)
	assert.Equal(t, []string{"cookie-file-1", "cookie-file-2", "cookie-file-3"}, cfg.Secrets.Cookie)
	assert.Equal(t, []string{"cipher-file-1", "cipher-file-2"}, cfg.Secrets.Cipher)
	assert.Equal(t, []string{"internal-file-1", "internal-file-2"}, cfg.Secrets.Internal)
}

func TestApplyEnvOverrides_EnvPrecedenceOverFile(t *testing.T) {
	tempDir := t.TempDir()
	dbFile := filepath.Join(tempDir, "database-url.txt")
	cookieFile := filepath.Join(tempDir, "cookie-secrets.txt")

	require.NoError(t, os.WriteFile(dbFile, []byte("postgres://file:pass@host/filedb?sslmode=require"), 0o600))
	require.NoError(t, os.WriteFile(cookieFile, []byte("cookie-file-1,cookie-file-2"), 0o600))

	t.Setenv("AEGION_DATABASE_URL", "postgres://env:pass@host/envdb?sslmode=require")
	t.Setenv("AEGION_DATABASE_URL_FILE", dbFile)
	t.Setenv("AEGION_SECRETS_COOKIE", "cookie-env-1,cookie-env-2")
	t.Setenv("AEGION_SECRETS_COOKIE_FILE", cookieFile)

	cfg := &Config{}
	require.NoError(t, applyEnvOverrides(cfg))

	assert.Equal(t, "postgres://env:pass@host/envdb?sslmode=require", cfg.Database.URL)
	assert.Equal(t, []string{"cookie-env-1", "cookie-env-2"}, cfg.Secrets.Cookie)
}

func TestApplyEnvOverrides_InvalidFileReturnsError(t *testing.T) {
	t.Setenv("AEGION_DATABASE_URL_FILE", filepath.Join(t.TempDir(), "missing-db-url.txt"))

	cfg := &Config{}
	err := applyEnvOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AEGION_DATABASE_URL_FILE")
}

func TestConfig_Validate(t *testing.T) {
	t.Setenv("AEGION_ENV", "")
	t.Setenv("AEGION_ENVIRONMENT", "")

	tests := []struct {
		name    string
		config  *Config
		wantErr string
	}{
		{
			name: "valid config",
			config: &Config{
				Database: DatabaseConfig{
					URL: "postgres://user:pass@localhost/db",
				},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "",
		},
		{
			name: "missing database URL",
			config: &Config{
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "database.url is required",
		},
		{
			name: "missing cookie secrets",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "secrets.cookie is required",
		},
		{
			name: "missing cipher secrets",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "secrets.cipher is required",
		},
		{
			name: "missing internal secrets",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie: []string{"cookie-secret-32-characters-long!!"},
					Cipher: []string{"cipher-secret-32-characters-long!!"},
				},
			},
			wantErr: "secrets.internal is required",
		},
		{
			name: "short cookie secret",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"short"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "cookie secret must be at least 32 characters",
		},
		{
			name: "short cipher secret",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"short"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
			},
			wantErr: "cipher secret must be at least 32 characters",
		},
		{
			name: "short internal secret",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"short"},
				},
			},
			wantErr: "internal secret must be at least 32 characters",
		},
		{
			name: "ignores optional empty rotated secrets",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!", ""},
					Cipher:   []string{"cipher-secret-32-characters-long!!", "   "},
					Internal: []string{"internal-secret-32-characters-long", ""},
				},
			},
			wantErr: "",
		},
		{
			name: "requires rp id when passkeys enabled",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
				Passkeys: PasskeysConfig{
					Enabled:  true,
					RPOrigin: "https://example.com",
				},
			},
			wantErr: "passkeys.rp_id is required when passkeys are enabled",
		},
		{
			name: "requires rp origin when passkeys enabled",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
				Passkeys: PasskeysConfig{
					Enabled: true,
					RPID:    "example.com",
				},
			},
			wantErr: "passkeys.rp_origin is required when passkeys are enabled",
		},
		{
			name: "rejects unsupported mfa digits",
			config: &Config{
				Database: DatabaseConfig{URL: "postgres://localhost/db"},
				Secrets: SecretsConfig{
					Cookie:   []string{"cookie-secret-32-characters-long!!"},
					Cipher:   []string{"cipher-secret-32-characters-long!!"},
					Internal: []string{"internal-secret-32-characters-long"},
				},
				MFA: MFAConfig{
					Enabled:    true,
					CodeDigits: 7,
				},
			},
			wantErr: "mfa.code_digits must be 6 or 8 when mfa is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestConfig_Validate_ProductionMode(t *testing.T) {
	t.Setenv("AEGION_ENV", "production")
	t.Setenv("AEGION_ENVIRONMENT", "")

	valid := &Config{
		ModuleVersions: map[string]string{
			"password":   "v1.0.0",
			"magic_link": "v1.0.0",
		},
		Database: DatabaseConfig{
			URL: "postgres://user:pass@localhost/db?sslmode=require",
		},
		Secrets: SecretsConfig{
			Cookie:   []string{"cookie-secret-32-characters-long!!", ""},
			Cipher:   []string{"cipher-secret-32-characters-long!!"},
			Internal: []string{"internal-secret-32-characters-long"},
		},
		Sessions: SessionsConfig{
			Cookie: CookieConfig{
				Secure:   true,
				HTTPOnly: true,
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Operator: OperatorConfig{
			Password: "StrongBootstrapPassword#2026",
		},
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "valid production config",
			cfg:     valid,
			wantErr: "",
		},
		{
			name: "requires secure session cookies",
			cfg: func() *Config {
				c := *valid
				c.Sessions.Cookie.Secure = false
				return &c
			}(),
			wantErr: "sessions.cookie.secure must be true in production",
		},
		{
			name: "requires http_only session cookies",
			cfg: func() *Config {
				c := *valid
				c.Sessions.Cookie.HTTPOnly = false
				return &c
			}(),
			wantErr: "sessions.cookie.http_only must be true in production",
		},
		{
			name: "rejects debug logging",
			cfg: func() *Config {
				c := *valid
				c.Log.Level = "debug"
				return &c
			}(),
			wantErr: "log.level=debug is not allowed in production",
		},
		{
			name: "rejects text logging format",
			cfg: func() *Config {
				c := *valid
				c.Log.Format = "text"
				return &c
			}(),
			wantErr: "log.format must be json in production",
		},
		{
			name: "rejects sqlite database in production",
			cfg: func() *Config {
				c := *valid
				c.Database.URL = "sqlite://./aegion.db"
				return &c
			}(),
			wantErr: "database.url must not use sqlite in production",
		},
		{
			name: "rejects non-ssl postgres url in production",
			cfg: func() *Config {
				c := *valid
				c.Database.URL = "postgres://user:pass@localhost/db?sslmode=disable"
				return &c
			}(),
			wantErr: "database.url must enforce ssl in production",
		},
		{
			name: "rejects placeholder secret values",
			cfg: func() *Config {
				c := *valid
				c.Secrets.Cookie = []string{"change-me-change-me-change-me-1234"}
				return &c
			}(),
			wantErr: "secrets must not use placeholder values in production",
		},
		{
			name: "rejects placeholder operator password",
			cfg: func() *Config {
				c := *valid
				c.Operator.Password = "admin123!"
				return &c
			}(),
			wantErr: "operator.password must be rotated before production",
		},
		{
			name: "rejects latest module version tags",
			cfg: func() *Config {
				c := *valid
				c.ModuleVersions = map[string]string{"password": "latest"}
				return &c
			}(),
			wantErr: "module_versions.password must be pinned in production",
		},
		{
			name: "requires https passkey origin in production",
			cfg: func() *Config {
				c := *valid
				c.Passkeys = PasskeysConfig{
					Enabled:  true,
					RPID:     "example.com",
					RPOrigin: "http://example.com",
				}
				return &c
			}(),
			wantErr: "passkeys.rp_origin must use https in production",
		},
		{
			name: "requires courier sms url when enabled",
			cfg: func() *Config {
				c := *valid
				c.Courier.SMS.Enabled = true
				c.Courier.SMS.URL = ""
				return &c
			}(),
			wantErr: "courier.sms.url is required when sms delivery is enabled",
		},
		{
			name: "requires https courier sms url in production",
			cfg: func() *Config {
				c := *valid
				c.Courier.SMS.Enabled = true
				c.Courier.SMS.URL = "http://sms.example.com/send"
				return &c
			}(),
			wantErr: "courier.sms.url must use https in production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConfig_Validate_ProductionTLSAndProxyTrustRequirements(t *testing.T) {
	t.Setenv("AEGION_ENV", "production")
	t.Setenv("AEGION_ENVIRONMENT", "")

	base := &Config{
		Database: DatabaseConfig{
			URL: "postgres://user:pass@localhost/db?sslmode=require",
		},
		Secrets: SecretsConfig{
			Cookie:   []string{"cookie-secret-32-characters-long!!"},
			Cipher:   []string{"cipher-secret-32-characters-long!!"},
			Internal: []string{"internal-secret-32-characters-long"},
		},
		Sessions: SessionsConfig{
			Cookie: CookieConfig{
				Secure:   true,
				HTTPOnly: true,
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Operator: OperatorConfig{
			Password: "StrongBootstrapPassword#2026",
		},
	}

	missingTLS := *base
	missingTLS.Server.TLS.Enabled = true
	err := missingTLS.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.tls.cert_file and server.tls.key_file are required")

	missingProxyCIDRs := *base
	missingProxyCIDRs.Proxy.TrustForwardedHeaders = true
	err = missingProxyCIDRs.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy.trusted_proxy_cidrs is required")
}

func TestLoad_ProductionConfigConformance(t *testing.T) {
	env := map[string]string{
		"AEGION_ENV":                       "production",
		"AEGION_MODULE_PASSWORD_VERSION":   "v1.0.0",
		"AEGION_MODULE_MAGIC_LINK_VERSION": "v1.0.0",
		"AEGION_MODULE_ADMIN_VERSION":      "v1.0.0",
		"AEGION_MODULE_POLICY_VERSION":     "v1.0.0",
		"AEGION_MODULE_PROXY_VERSION":      "v1.0.0",
		"AEGION_MODULE_REGISTRY":           "ghcr.io/aegion",
		"AEGION_CORS_ORIGIN":               "https://example.com",
		"AEGION_DATABASE_URL":              "postgres://user:pass@localhost/db?sslmode=require",
		"AEGION_REDIS_URL":                 "redis://localhost:6379",
		"AEGION_SECRET_COOKIE_1":           "cookie-secret-32-characters-long!!",
		"AEGION_SECRET_CIPHER_1":           "cipher-secret-32-characters-long!!",
		"AEGION_SECRET_INTERNAL_1":         "internal-secret-32-characters-long",
		"AEGION_OPERATOR_EMAIL":            "admin@example.com",
		"AEGION_OPERATOR_PASSWORD":         "StrongBootstrapPassword#2026",
		"AEGION_SMTP_HOST":                 "smtp.example.com",
		"AEGION_SMTP_FROM_ADDRESS":         "noreply@example.com",
		"AEGION_SMTP_USERNAME":             "smtp-user",
		"AEGION_SMTP_PASSWORD":             "smtp-password",
		"AEGION_TLS_CERT_FILE":             "/etc/ssl/cert.pem",
		"AEGION_TLS_KEY_FILE":              "/etc/ssl/key.pem",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	cfg, err := Load(filepath.Join("..", "..", "..", "configs", "aegion.production.yaml"))
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())

	assert.True(t, cfg.Server.TLS.Enabled)
	assert.Equal(t, "/etc/ssl/cert.pem", cfg.Server.TLS.CertFile)
	assert.Equal(t, "1.2", cfg.Server.TLS.MinVersion)
	assert.True(t, cfg.Cache.TLSEnabled == false)
	assert.True(t, cfg.Log.IncludeRequestID)
	assert.Contains(t, cfg.Log.RedactFields, "token")
	assert.Equal(t, "__Host-aegion_session", cfg.Sessions.Cookie.Name)
	assert.Equal(t, 5, cfg.Sessions.MaxPerUser)
	assert.True(t, cfg.Security.CSRF.Enabled)
	assert.Equal(t, "__Host-aegion_csrf", cfg.Security.CSRF.CookieName)
	assert.True(t, cfg.Admin.RequireReauth)
	assert.Equal(t, Duration(5*time.Minute), cfg.Admin.ReauthTimeout)
	assert.Equal(t, "/metrics", cfg.Observability.Metrics.Path)
}

func TestConfig_StructFields(t *testing.T) {
	cfg := Config{
		ModuleVersions: map[string]string{"test": "v1.0.0"},
		ModuleRegistry: ModuleRegistry{
			BaseURL:    "https://registry.example.com",
			PullPolicy: "always",
		},
		Server: ServerConfig{
			Port: 8080,
			Host: "localhost",
			TLS: TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
				KeyFile:  "/path/to/key.pem",
			},
			CORS: CORSConfig{
				Enabled:          true,
				AllowedOrigins:   []string{"https://example.com"},
				AllowedMethods:   []string{"GET", "POST"},
				AllowedHeaders:   []string{"Content-Type", "Authorization"},
				AllowCredentials: true,
				MaxAge:           3600,
			},
			InternalNet: InternalNetConfig{
				Name:               "aegion-internal",
				Subnet:             "172.20.0.0/16",
				HealthCheckInt:     Duration(30 * time.Second),
				HealthCheckTimeout: Duration(10 * time.Second),
				HealthCheckFails:   3,
				RestartOnFailure:   true,
				StartupTimeout:     Duration(5 * time.Minute),
			},
		},
		Database: DatabaseConfig{
			URL:             "postgres://localhost/db",
			MaxOpenConns:    25,
			MaxIdleConns:    10,
			ConnMaxLifetime: Duration(time.Hour),
			ConnMaxIdleTime: Duration(30 * time.Minute),
			MigrateOnly:     false,
		},
		Cache: CacheConfig{
			Enabled:   true,
			URL:       "redis://localhost:6379",
			KeyPrefix: "aegion:",
		},
		Courier: CourierConfig{
			SMTP: SMTPConfig{
				Host:        "smtp.example.com",
				Port:        587,
				FromAddress: "noreply@example.com",
				FromName:    "Aegion",
				Auth: SMTPAuth{
					Enabled:  true,
					Username: "smtp-user",
					Password: "smtp-pass",
				},
			},
		},
		Identity: IdentityConfig{
			DefaultSchema: "default",
			Schemas: []SchemaConfig{
				{ID: "default", URL: "/schemas/default.json"},
				{ID: "admin", URL: "/schemas/admin.json"},
			},
		},
		Security: SecurityConfig{
			AccountEnumMitigation: true,
			RateLimits: RateLimitsConfig{
				Login:         RateLimitRule{Requests: 5, Period: Duration(time.Minute)},
				Registration:  RateLimitRule{Requests: 3, Period: Duration(time.Hour)},
				EmailDelivery: RateLimitRule{Requests: 10, Period: Duration(time.Hour)},
			},
			BruteForce: BruteForceConfig{
				MaxAttempts:     5,
				LockoutDuration: Duration(15 * time.Minute),
			},
		},
	}

	// Verify struct fields are accessible and have expected types
	assert.NotEmpty(t, cfg.ModuleVersions)
	assert.Equal(t, "v1.0.0", cfg.ModuleVersions["test"])
	assert.Equal(t, "https://registry.example.com", cfg.ModuleRegistry.BaseURL)
	assert.True(t, cfg.Server.TLS.Enabled)
	assert.True(t, cfg.Server.CORS.Enabled)
	assert.True(t, cfg.Cache.Enabled)
	assert.True(t, cfg.Courier.SMTP.Auth.Enabled)
	assert.Len(t, cfg.Identity.Schemas, 2)
	assert.True(t, cfg.Security.AccountEnumMitigation)
	assert.Equal(t, 5, cfg.Security.RateLimits.Login.Requests)
	assert.Equal(t, 5, cfg.Security.BruteForce.MaxAttempts)
}

func TestConfigYAMLTags(t *testing.T) {
	// Test that YAML unmarshaling works with the struct tags
	yamlContent := `
module_versions:
  password: v1.0.0

server:
  port: 9000
  cors:
    enabled: true
    allowed_origins:
      - https://example.com
    max_age: 3600

database:
  url: postgres://test/db
  max_open_connections: 50

secrets:
  cookie:
    - test-cookie-secret-32-characters-long
  cipher:
    - test-cipher-secret-32-characters-long
  internal:
    - test-internal-secret-32-characters-long

security:
  rate_limits:
    login:
      requests: 10
      period: 5m
`

	var cfg Config
	err := yaml.Unmarshal([]byte(yamlContent), &cfg)
	require.NoError(t, err)

	assert.Equal(t, "v1.0.0", cfg.ModuleVersions["password"])
	assert.Equal(t, 9000, cfg.Server.Port)
	assert.True(t, cfg.Server.CORS.Enabled)
	assert.Equal(t, []string{"https://example.com"}, cfg.Server.CORS.AllowedOrigins)
	assert.Equal(t, 3600, cfg.Server.CORS.MaxAge)
	assert.Equal(t, "postgres://test/db", cfg.Database.URL)
	assert.Equal(t, 50, cfg.Database.MaxOpenConns)
	assert.Equal(t, "test-cookie-secret-32-characters-long", cfg.Secrets.Cookie[0])
	assert.Equal(t, 10, cfg.Security.RateLimits.Login.Requests)
	assert.Equal(t, Duration(5*time.Minute), cfg.Security.RateLimits.Login.Period)
}

func TestConfigYAMLTags_Phase3PolicyProxy(t *testing.T) {
	yamlContent := `
courier:
  sms:
    enabled: true
    url: https://sms.example.com/send
    method: POST
    headers:
      Authorization: Bearer token
    body_template: '{"to":"{{.To}}","body":"{{.Body}}"}'
    timeout: 5s

policy:
  enabled: true
  default_model: rebac
  rbac:
    enabled: true
  abac:
    enabled: true
  rebac:
    enabled: true

proxy:
  enabled: true
  upstream_timeout: 45s
  preserve_host: true
  trust_forwarded_headers: true
  strip_inbound_identity_headers: true
  identity_signing_secret: test-signing-secret
  identity_signature_header: X-Test-Signature
  signed_identity_headers:
    - X-User-ID
    - X-User-Session-ID
`

	var cfg Config
	err := yaml.Unmarshal([]byte(yamlContent), &cfg)
	require.NoError(t, err)
	applyDefaults(&cfg)

	assert.True(t, cfg.Policy.Enabled)
	assert.Equal(t, "rebac", cfg.Policy.DefaultModel)
	assert.True(t, cfg.Policy.RBAC.Enabled)
	assert.True(t, cfg.Policy.ABAC.Enabled)
	assert.True(t, cfg.Policy.ReBAC.Enabled)

	assert.True(t, cfg.Proxy.Enabled)
	assert.Equal(t, Duration(45*time.Second), cfg.Proxy.UpstreamTimeout)
	assert.True(t, cfg.Proxy.PreserveHost)
	assert.True(t, cfg.Proxy.TrustForwardedHeaders)
	assert.True(t, cfg.Proxy.StripInboundIdentityHeaders)
	assert.Equal(t, "test-signing-secret", cfg.Proxy.IdentitySigningSecret)
	assert.Equal(t, "X-Test-Signature", cfg.Proxy.IdentitySignatureHeader)
	assert.Equal(t, []string{"X-User-ID", "X-User-Session-ID"}, cfg.Proxy.SignedIdentityHeaders)
	assert.True(t, cfg.Courier.SMS.Enabled)
	assert.Equal(t, "https://sms.example.com/send", cfg.Courier.SMS.URL)
	assert.Equal(t, "POST", cfg.Courier.SMS.Method)
	assert.Equal(t, "Bearer token", cfg.Courier.SMS.Headers["Authorization"])
	assert.Equal(t, `{"to":"{{.To}}","body":"{{.Body}}"}`, cfg.Courier.SMS.BodyTemplate)
	assert.Equal(t, Duration(5*time.Second), cfg.Courier.SMS.Timeout)
}

func TestLoad_Integration(t *testing.T) {
	// Integration test with defaults, env overrides, and validation
	if err := os.Setenv("AEGION_DATABASE_URL", "postgres://integration:test@localhost/integration_db"); err != nil {
		t.Fatalf("failed to set AEGION_DATABASE_URL: %v", err)
	}
	if err := os.Setenv("AEGION_LOG_LEVEL", "debug"); err != nil {
		t.Fatalf("failed to set AEGION_LOG_LEVEL: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("AEGION_DATABASE_URL")
		_ = os.Unsetenv("AEGION_LOG_LEVEL")
	}()

	configContent := `
server:
  port: 3000

secrets:
  cookie:
    - integration-cookie-secret-32-characters
  cipher:
    - integration-cipher-secret-32-characters
  internal:
    - integration-internal-secret-32-chars
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "integration.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Verify defaults were applied
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)                          // Default
	assert.Equal(t, Duration(60*time.Second), cfg.Server.RequestTimeout) // Default

	// Verify config values were loaded
	assert.Equal(t, 3000, cfg.Server.Port) // From config

	// Verify env overrides were applied
	assert.Equal(t, "postgres://integration:test@localhost/integration_db", cfg.Database.URL) // From env
	assert.Equal(t, "debug", cfg.Log.Level)                                                   // From env

	// Verify validation passes
	err = cfg.Validate()
	assert.NoError(t, err)
}

// Benchmark the config loading process
func BenchmarkLoad(b *testing.B) {
	configContent := `
server:
  port: 8080

database:
  url: postgres://bench:test@localhost/bench_db

secrets:
  cookie: ["benchmark-cookie-secret-32-characters-long"]
  cipher: ["benchmark-cipher-secret-32-characters-long"] 
  internal: ["benchmark-internal-secret-32-characters-long"]
`

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "bench.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Load(configPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}
