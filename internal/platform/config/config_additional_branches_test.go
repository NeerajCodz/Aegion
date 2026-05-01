package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigAdditionalBranchCoverage(t *testing.T) {
	t.Run("duration decode returns type error for non-string yaml value", func(t *testing.T) {
		var cfg struct {
			Value Duration `yaml:"value"`
		}
		err := yaml.Unmarshal([]byte("value: 15"), &cfg)
		if err == nil {
			t.Fatal("expected yaml type error for numeric duration value")
		}
	})

	t.Run("load wraps apply env override errors", func(t *testing.T) {
		tmp := t.TempDir()
		configPath := filepath.Join(tmp, "config.yaml")
		content := `
database:
  url: postgres://user:pass@localhost/db
secrets:
  cookie: ["cookie-secret-32-characters-long!!"]
  cipher: ["cipher-secret-32-characters-long!!"]
  internal: ["internal-secret-32-characters-long"]
`
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
		t.Setenv("AEGION_CACHE_URL_FILE", filepath.Join(tmp, "missing-cache-url.txt"))

		_, err := Load(configPath)
		if err == nil || !strings.Contains(err.Error(), "failed to apply environment overrides") {
			t.Fatalf("expected load env override wrapper error, got %v", err)
		}
	})

	t.Run("apply env overrides returns errors for cache and secret files", func(t *testing.T) {
		cfg := &Config{}

		t.Setenv("AEGION_CACHE_URL_FILE", filepath.Join(t.TempDir(), "missing-cache-url.txt"))
		if err := applyEnvOverrides(cfg); err == nil {
			t.Fatal("expected cache file read error")
		}
		t.Setenv("AEGION_CACHE_URL_FILE", "")

		t.Setenv("AEGION_SECRETS_COOKIE_FILE", filepath.Join(t.TempDir(), "missing-cookie.txt"))
		if err := applyEnvOverrides(cfg); err == nil {
			t.Fatal("expected cookie secret file read error")
		}
		t.Setenv("AEGION_SECRETS_COOKIE_FILE", "")

		t.Setenv("AEGION_SECRETS_CIPHER_FILE", filepath.Join(t.TempDir(), "missing-cipher.txt"))
		if err := applyEnvOverrides(cfg); err == nil {
			t.Fatal("expected cipher secret file read error")
		}
		t.Setenv("AEGION_SECRETS_CIPHER_FILE", "")

		t.Setenv("AEGION_SECRETS_INTERNAL_FILE", filepath.Join(t.TempDir(), "missing-internal.txt"))
		if err := applyEnvOverrides(cfg); err == nil {
			t.Fatal("expected internal secret file read error")
		}
	})

	t.Run("readEnvOrFile rejects empty files", func(t *testing.T) {
		tmp := t.TempDir()
		emptyPath := filepath.Join(tmp, "empty-secret.txt")
		if err := os.WriteFile(emptyPath, []byte("   \n"), 0o600); err != nil {
			t.Fatalf("failed to write empty file: %v", err)
		}
		t.Setenv("AEGION_CACHE_URL_FILE", emptyPath)

		_, _, err := readEnvOrFile("AEGION_CACHE_URL")
		if err == nil || !strings.Contains(err.Error(), "empty file") {
			t.Fatalf("expected empty file error, got %v", err)
		}
	})

	t.Run("split secret list drops empty entries", func(t *testing.T) {
		got := splitSecretList("one,,\n two \r\n , ,three")
		if len(got) != 3 || got[0] != "one" || got[1] != "two" || got[2] != "three" {
			t.Fatalf("unexpected splitSecretList result: %#v", got)
		}
	})

	t.Run("validate rejects negative mfa allowed windows", func(t *testing.T) {
		cfg := &Config{
			Database: DatabaseConfig{URL: "postgres://localhost/db"},
			Secrets: SecretsConfig{
				Cookie:   []string{"cookie-secret-32-characters-long!!"},
				Cipher:   []string{"cipher-secret-32-characters-long!!"},
				Internal: []string{"internal-secret-32-characters-long"},
			},
			MFA: MFAConfig{
				Enabled:            true,
				CodeDigits:         6,
				AllowedTimeWindows: -1,
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "allowed_time_windows") {
			t.Fatalf("expected negative allowed_time_windows validation error, got %v", err)
		}
	})

	t.Run("contains placeholder value returns false for empty input", func(t *testing.T) {
		if containsPlaceholderValue("   ") {
			t.Fatal("expected empty placeholder input to be false")
		}
	})

	t.Run("is placeholder value detects known defaults", func(t *testing.T) {
		if !IsPlaceholderValue("admin123!") {
			t.Fatal("expected admin123! to be treated as placeholder")
		}
		if IsPlaceholderValue("Str0ng-P@ssword!") {
			t.Fatal("expected strong password to not be treated as placeholder")
		}
	})
}
