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

magic_link:
  enabled: true
  code_length: 6
  link_lifespan: 15m
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
	assert.True(t, cfg.MagicLink.Enabled)
	assert.Equal(t, 6, cfg.MagicLink.CodeLength)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.LinkLifespan)
}

func TestLoad_EnvironmentVariableExpansion(t *testing.T) {
	// Set environment variables for testing
	os.Setenv("TEST_DB_URL", "postgres://env-user:env-pass@env-host/env-db")
	os.Setenv("TEST_SECRET", "environment-secret-32-characters-long")
	defer func() {
		os.Unsetenv("TEST_DB_URL")
		os.Unsetenv("TEST_SECRET")
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
	assert.Equal(t, "environment-secret-32-characters-long", cfg.Secrets.Cookie[0])
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

	// Magic Link defaults
	assert.Equal(t, 6, cfg.MagicLink.CodeLength)
	assert.Equal(t, "0123456789", cfg.MagicLink.CodeCharset)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.LinkLifespan)
	assert.Equal(t, Duration(15*time.Minute), cfg.MagicLink.CodeLifespan)

	// Admin defaults
	assert.Equal(t, "/aegion", cfg.Admin.Path)
	assert.Equal(t, Duration(4*time.Hour), cfg.Admin.SessionLifespan)
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
		"AEGION_DATABASE_URL":     "postgres://env:pass@host/db",
		"AEGION_CACHE_URL":        "redis://env:6379",
		"AEGION_LOG_LEVEL":        "debug",
		"AEGION_SERVER_PORT":      "9090",
		"AEGION_SECRETS_COOKIE":   "env-cookie-1,env-cookie-2",
		"AEGION_SECRETS_CIPHER":   "env-cipher-1,env-cipher-2",
		"AEGION_SECRETS_INTERNAL": "env-internal-1,env-internal-2",
	}

	for key, value := range envVars {
		os.Setenv(key, value)
	}
	defer func() {
		for key := range envVars {
			os.Unsetenv(key)
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

	applyEnvOverrides(cfg)

	assert.Equal(t, "postgres://env:pass@host/db", cfg.Database.URL)
	assert.Equal(t, "redis://env:6379", cfg.Cache.URL)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, []string{"env-cookie-1", "env-cookie-2"}, cfg.Secrets.Cookie)
	assert.Equal(t, []string{"env-cipher-1", "env-cipher-2"}, cfg.Secrets.Cipher)
	assert.Equal(t, []string{"env-internal-1", "env-internal-2"}, cfg.Secrets.Internal)
}

func TestConfig_Validate(t *testing.T) {
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

func TestLoad_Integration(t *testing.T) {
	// Integration test with defaults, env overrides, and validation
	os.Setenv("AEGION_DATABASE_URL", "postgres://integration:test@localhost/integration_db")
	os.Setenv("AEGION_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("AEGION_DATABASE_URL")
		os.Unsetenv("AEGION_LOG_LEVEL")
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
	assert.Equal(t, "0.0.0.0", cfg.Server.Host) // Default
	assert.Equal(t, Duration(60*time.Second), cfg.Server.RequestTimeout) // Default

	// Verify config values were loaded
	assert.Equal(t, 3000, cfg.Server.Port) // From config

	// Verify env overrides were applied
	assert.Equal(t, "postgres://integration:test@localhost/integration_db", cfg.Database.URL) // From env
	assert.Equal(t, "debug", cfg.Log.Level) // From env

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