package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersionWithNilRustHook(t *testing.T) {
	var out bytes.Buffer
	deps := defaultMainDeps()
	deps.stdout = &out
	deps.cryptoSelfCheck = nil

	if err := run([]string{"-version"}, deps); err != nil {
		t.Fatalf("run(-version) error = %v", err)
	}
	if !strings.Contains(out.String(), "Aegion Admin Module") {
		t.Fatalf("expected version output, got %q", out.String())
	}
}
func TestBuildRuntimeAdditionalBranches(t *testing.T) {
	t.Run("missing database fails before handlers are exposed", func(t *testing.T) {
		cfg := &Config{}
		cfg.Secrets.Cipher = []string{"cipher-seed"}
		if _, err := buildRuntime(cfg, nil); err == nil || !strings.Contains(err.Error(), "database pool is required") {
			t.Fatalf("buildRuntime(missing database) = %v", err)
		}
	})

}


func TestLoadConfigObservabilityEndpointOverrides(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "admin-observability-extra.yaml")
	configYAML := strings.TrimSpace(`
database:
  url: postgres://localhost:5432/aegion
server:
  address: 127.0.0.1
  port: 8082
`)
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("AEGION_LOZA_COLLECTOR_URL", "http://loza-custom:9308/health")
	t.Setenv("AEGION_ADMIN_OBS_GRAFANA_URL", "http://grafana-custom:3000/api/health")
	t.Setenv("AEGION_ADMIN_OBS_TEMPO_URL", "http://tempo-custom:3200/ready")

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.Observability.Endpoints.LozaCollector != "http://loza-custom:9308/health" ||
		cfg.Observability.Endpoints.Grafana != "http://grafana-custom:3000/api/health" ||
		cfg.Observability.Endpoints.Tempo != "http://tempo-custom:3200/ready" {
		t.Fatalf("unexpected observability endpoints: %#v", cfg.Observability.Endpoints)
	}
}
