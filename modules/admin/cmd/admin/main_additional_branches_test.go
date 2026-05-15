package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestStartServerRuntimeAdditionalBranches(t *testing.T) {
	t.Run("social provider manager init error path", func(t *testing.T) {
		cfg := &Config{}
		cfg.Server.Address = "127.0.0.1"
		cfg.Server.Port = 0
		cfg.Secrets.Cipher = []string{"cipher-seed"}
		if _, err := startServerRuntime(cfg, nil); err == nil || !strings.Contains(err.Error(), "initialize social provider manager") {
			t.Fatalf("startServerRuntime(social init error) = %v", err)
		}
	})

	t.Run("tls config build error path", func(t *testing.T) {
		cfg := &Config{}
		cfg.Server.Address = "127.0.0.1"
		cfg.Server.Port = 0
		cfg.Server.TLS.Enabled = true
		cfg.Server.TLS.MinVersion = "invalid"
		if _, err := startServerRuntime(cfg, nil); err == nil || !strings.Contains(err.Error(), "unsupported tls min_version") {
			t.Fatalf("startServerRuntime(tls build error) = %v", err)
		}
	})

	t.Run("scim enabled branch initializes runtime", func(t *testing.T) {
		cfg := &Config{}
		cfg.Server.Address = "127.0.0.1"
		cfg.Server.Port = 0
		cfg.Server.ReadTimeout = time.Second
		cfg.Server.WriteTimeout = time.Second
		cfg.Server.IdleTimeout = time.Second
		cfg.Admin.Path = "/admin"
		cfg.Admin.SCIM.Enabled = true
		cfg.Admin.SCIM.BasePath = "/scim/v2"
		cfg.Admin.SCIM.TokenPrefix = "aegion_scim_"
		cfg.Admin.SCIM.TokenLookupPrefixLen = 12
		cfg.Admin.SCIM.TokenEntropyBytes = 32
		cfg.Admin.SCIM.DefaultPageSize = 20
		cfg.Admin.SCIM.MaxPageSize = 100
		cfg.Admin.SCIM.TokenLastUsedUpdateTimeout = time.Minute

		rt, err := startServerRuntime(cfg, nil)
		if err != nil {
			t.Fatalf("startServerRuntime(scim enabled) error = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := rt.shutdown(ctx); err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Fatalf("shutdown(scim enabled) error = %v", err)
		}
	})
}

func TestBuildTLSConfigBranches(t *testing.T) {
	cfg := &Config{}
	cfg.Server.TLS.MinVersion = "1.3"
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig(min 1.3) error = %v", err)
	}
	if tlsCfg.MinVersion == 0 {
		t.Fatal("expected configured minimum TLS version")
	}

	cfg.Server.TLS.MinVersion = "invalid"
	if _, err := buildTLSConfig(cfg); err == nil {
		t.Fatal("buildTLSConfig(invalid min version) expected error")
	}

	cfg.Server.TLS.MinVersion = "1.2"
	cfg.Server.TLS.ClientCAFile = filepath.Join(t.TempDir(), "missing.pem")
	if _, err := buildTLSConfig(cfg); err == nil || !strings.Contains(err.Error(), "failed to read tls client CA file") {
		t.Fatalf("buildTLSConfig(missing client ca) = %v", err)
	}

	dir := t.TempDir()
	pemPath := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(pemPath, []byte("not-a-pem"), 0o644); err != nil {
		t.Fatalf("write bad pem: %v", err)
	}
	cfg.Server.TLS.ClientCAFile = pemPath
	if _, err := buildTLSConfig(cfg); err == nil || !strings.Contains(err.Error(), "failed to parse tls client CA file") {
		t.Fatalf("buildTLSConfig(invalid client ca) = %v", err)
	}
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

	t.Setenv("AEGION_ADMIN_OBS_OTEL_COLLECTOR_URL", "http://otel-custom:13133")
	t.Setenv("AEGION_ADMIN_OBS_GRAFANA_URL", "http://grafana-custom:3000/api/health")
	t.Setenv("AEGION_ADMIN_OBS_TEMPO_URL", "http://tempo-custom:3200/ready")
	t.Setenv("AEGION_ADMIN_OBS_LOKI_URL", "http://loki-custom:3100/ready")

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.Observability.Endpoints.OTelCollector != "http://otel-custom:13133" ||
		cfg.Observability.Endpoints.Grafana != "http://grafana-custom:3000/api/health" ||
		cfg.Observability.Endpoints.Tempo != "http://tempo-custom:3200/ready" ||
		cfg.Observability.Endpoints.Loki != "http://loki-custom:3100/ready" {
		t.Fatalf("unexpected observability endpoints: %#v", cfg.Observability.Endpoints)
	}
}
