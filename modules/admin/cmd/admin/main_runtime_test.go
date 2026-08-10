package main

import (
	"os"
	"testing"
)

func TestDefaultMainDepsProvidesRuntimeHooks(t *testing.T) {
	deps := defaultMainDeps()

	if deps.stdout == nil || deps.loadConfig == nil || deps.setupLogger == nil {
		t.Fatalf("defaultMainDeps returned nil core hooks")
	}
	if deps.parseDBConfig == nil || deps.newDBPool == nil || deps.pingDB == nil || deps.closeDB == nil {
		t.Fatalf("defaultMainDeps returned nil db hooks")
	}
	if deps.runMigrations == nil || deps.buildRuntime == nil || deps.runModuleServer == nil {
		t.Fatalf("defaultMainDeps returned nil runtime hooks")
	}

	deps.closeDB(nil)
}

func TestAdminModuleConfigMatchesPublicRuntime(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = 8082
	cfg.Admin.SCIM.Enabled = true
	moduleCfg := adminModuleConfig(cfg, &moduleRuntime{})

	if moduleCfg.Module != "admin" || moduleCfg.Version != adminModuleVersion {
		t.Fatalf("unexpected module identity: %#v", moduleCfg)
	}
	if len(moduleCfg.Routes) != 1 || moduleCfg.Routes[0] != adminPublicRoutePrefix {
		t.Fatalf("unexpected route metadata: %#v", moduleCfg.Routes)
	}
	if len(moduleCfg.GRPCServices) != 0 || len(moduleCfg.EventSubscriptions) != 0 {
		t.Fatalf("admin must not advertise unimplemented gRPC or event APIs: %#v", moduleCfg)
	}
	if len(moduleCfg.Capabilities) != 8 || moduleCfg.Capabilities[0] != "admin.identities" || moduleCfg.Capabilities[7] != "admin.scim" {
		t.Fatalf("unexpected capability metadata: %#v", moduleCfg.Capabilities)
	}
}

func TestPreparePublicRouteConfigUsesCoreOwnedPrefix(t *testing.T) {
	cfg := &Config{}
	cfg.Admin.Path = "/admin"
	cfg.Admin.SCIM.Enabled = true
	cfg.Admin.SCIM.BasePath = "/scim/v2"

	preparePublicRouteConfig(cfg)

	if cfg.Admin.Path != adminPublicRoutePrefix {
		t.Fatalf("admin path = %q, want %q", cfg.Admin.Path, adminPublicRoutePrefix)
	}
	if cfg.Admin.SCIM.BasePath != "/aegion/scim/v2" {
		t.Fatalf("SCIM path = %q, want /aegion/scim/v2", cfg.Admin.SCIM.BasePath)
	}
}

func TestBuildRuntimeFailsWithoutDurableDependencies(t *testing.T) {
	cfg := &Config{}
	if _, err := buildRuntime(cfg, nil); err == nil {
		t.Fatal("expected missing database dependency to fail")
	}
}

func TestMainVersionPath(t *testing.T) {
	origArgs := os.Args
	defer func() {
		os.Args = origArgs
	}()

	os.Args = []string{"aegion-admin", "-version"}
	main()
}
