package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
)

func writeOrchestratorFakeDockerCLI(t *testing.T) string {
	t.Helper()
	return writeFakeDockerCLI(t, "@echo off\r\n"+
		"if \"%1\"==\"network\" (\r\n"+
		"  if \"%2\"==\"ls\" (\r\n"+
		"    echo {\"ID\":\"net-1\",\"Name\":\"aegion_modules\"}\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		")\r\n"+
		"if \"%1\"==\"ps\" exit /b 0\r\n"+
		"if \"%1\"==\"image\" exit /b 1\r\n"+
		"if \"%1\"==\"pull\" exit /b 0\r\n"+
		"if \"%1\"==\"create\" (\r\n"+
		"  echo container-1234567890\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"start\" exit /b 0\r\n"+
		"if \"%1\"==\"stop\" exit /b 0\r\n"+
		"if \"%1\"==\"rm\" exit /b 0\r\n"+
		"if \"%1\"==\"inspect\" (\r\n"+
		"  echo [{\"Id\":\"container-1234567890\",\"Name\":\"/aegion_password\",\"Created\":\"2024-01-01T00:00:00Z\",\"RestartCount\":1,\"State\":{\"Status\":\"running\",\"Running\":true,\"StartedAt\":\"2024-01-01T00:00:01Z\",\"FinishedAt\":\"0001-01-01T00:00:00Z\",\"ExitCode\":0,\"Error\":\"\",\"Health\":{\"Status\":\"healthy\"}},\"NetworkSettings\":{\"Networks\":{\"aegion_modules\":{\"IPAddress\":\"10.10.0.2\"}},\"Ports\":{}}}]\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"logs\" (\r\n"+
		"  echo line-1\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"exit /b 0\r\n")
}

func TestOrchestratorAdditionalNewBranches(t *testing.T) {
	t.Run("new fails when docker client cannot be created", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("AEGION_DOCKER_BIN", "C:\\non-existent\\docker.exe")
		if _, err := New(Config{ConfigPath: "missing.yaml"}); err == nil || !strings.Contains(err.Error(), "creating docker client") {
			t.Fatalf("New(missing docker) = %v", err)
		}
	})

	t.Run("new fails when config cannot be loaded", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("AEGION_DOCKER_BIN", writeOrchestratorFakeDockerCLI(t))
		if _, err := New(Config{ConfigPath: "missing.yaml"}); err == nil || !strings.Contains(err.Error(), "loading config") {
			t.Fatalf("New(missing config) = %v", err)
		}
	})

	t.Run("new succeeds and initializes dependencies", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("AEGION_DOCKER_BIN", writeOrchestratorFakeDockerCLI(t))
		configPath := writeMinimalMainConfig(t, t.TempDir())
		o, err := New(Config{
			ConfigPath:  configPath,
			TokenSecret: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		})
		if err != nil {
			t.Fatalf("New(success) error = %v", err)
		}
		if o.docker == nil || o.network == nil || o.configLoader == nil || o.tokenGenerator == nil || o.modules == nil {
			t.Fatalf("New(success) expected initialized orchestrator, got %#v", o)
		}
	})
}

func TestOrchestratorAdditionalWrapperFallbacksAndStatusBranches(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("AEGION_DOCKER_BIN", writeOrchestratorFakeDockerCLI(t))
	configPath := writeMinimalMainConfig(t, t.TempDir())

	o, err := New(Config{
		ConfigPath:  configPath,
		TokenSecret: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if _, err := o.ensureNetwork(ctx); err != nil {
		t.Fatalf("ensureNetwork() error = %v", err)
	}
	if err := o.closeDocker(); err != nil {
		t.Fatalf("closeDocker() error = %v", err)
	}
	if _, err := o.loadModuleConfig("password"); err != nil {
		t.Fatalf("loadModuleConfig() error = %v", err)
	}
	if tok, err := o.generateToken("password"); err != nil || strings.TrimSpace(tok) == "" {
		t.Fatalf("generateToken() tok=%q err=%v", tok, err)
	}
	if _, err := o.createContainer(ctx, &ModuleConfig{ID: "password", Name: "Password", Image: "repo/password"}, "auth-token"); err != nil {
		t.Fatalf("createContainer() error = %v", err)
	}
	if err := o.startContainer(ctx, "container-1234567890"); err != nil {
		t.Fatalf("startContainer() error = %v", err)
	}
	if err := o.stopContainer(ctx, "container-1234567890", time.Second); err != nil {
		t.Fatalf("stopContainer() error = %v", err)
	}
	if err := o.removeContainer(ctx, "container-1234567890", true); err != nil {
		t.Fatalf("removeContainer() error = %v", err)
	}
	if _, err := o.getContainerInfo(ctx, "container-1234567890"); err != nil {
		t.Fatalf("getContainerInfo() error = %v", err)
	}
	if logs, err := o.containerLogs(ctx, "container-1234567890", 10, time.Time{}); err != nil || !strings.Contains(logs, "line-1") {
		t.Fatalf("containerLogs() logs=%q err=%v", logs, err)
	}

	status, err := o.GetModuleStatus(ctx, "missing")
	if err != nil || status.State != StateStopped {
		t.Fatalf("GetModuleStatus(missing) status=%#v err=%v", status, err)
	}
}

func TestOrchestratorAdditionalRegistryAndRestartBranches(t *testing.T) {
	ctx := context.Background()
	o := newSeamedOrchestrator()
	o.registry = registry.New(registry.DefaultConfig())
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "container-1", state: StateRunning}
	if err := o.StopModule(ctx, "password"); err != nil {
		t.Fatalf("StopModule(registry deregister warning path) error = %v", err)
	}

	o = newSeamedOrchestrator()
	o.startContainerFn = func(context.Context, string) error { return errors.New("start failed") }
	if err := o.RestartModule(ctx, "password"); err == nil || !strings.Contains(err.Error(), "starting module") {
		t.Fatalf("RestartModule(start failure) = %v", err)
	}
}

