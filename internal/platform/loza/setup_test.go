package loza

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aegion/aegion/internal/platform/config"
)

func TestReadAPIKeyFileTakesPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loza-key")
	if err := os.WriteFile(path, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGION_LOZA_API_KEY", "env-key")
	t.Setenv("AEGION_LOZA_API_KEY_FILE", path)

	got, err := readAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if got != "file-key" {
		t.Fatalf("readAPIKey() = %q, want file-key", got)
	}
}

func TestInitializeRequiresCollectorEndpoint(t *testing.T) {
	processState.Lock()
	processState.configured = false
	processState.Unlock()
	t.Setenv("AEGION_LOZA_COLLECTOR_URL", "")

	_, _, err := Initialize(structLozaConfig(), "aegion.core", "test")
	if err == nil || err.Error() != "loza collector endpoint is required" {
		t.Fatalf("Initialize() error = %v", err)
	}
}

func TestProductionEndpointRequiresTLS(t *testing.T) {
	if err := validateSecureEndpoint("http://collector.internal:9308", false); err == nil {
		t.Fatal("expected plain HTTP production endpoint to be rejected")
	}
	if err := validateSecureEndpoint("https://collector.internal:9308", false); err != nil {
		t.Fatalf("HTTPS endpoint rejected: %v", err)
	}
}

func TestAuthorizationHeaderDoesNotExposeEmptyCredential(t *testing.T) {
	if got := authorizationHeader(""); got != nil {
		t.Fatalf("empty API key produced headers: %#v", got)
	}
	if got := authorizationHeader("secret")["Authorization"]; got != "Bearer secret" {
		t.Fatalf("authorization header = %q", got)
	}
}

func structLozaConfig() config.LozaConfig { return config.LozaConfig{} }
