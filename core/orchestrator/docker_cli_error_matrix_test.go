package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeDockerOnPath(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	name := "docker.cmd"
	content := script
	if os.PathListSeparator == ':' {
		name = "docker"
		content = cmdFakeToShell(script)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake docker binary: %v", err)
	}
	return dir
}

func TestDockerClientAdditionalErrorMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("new client uses default docker binary from PATH", func(t *testing.T) {
		pathDir := writeDockerOnPath(t, "@echo off\r\nexit /b 0\r\n")
		prevPath := os.Getenv("PATH")
		t.Setenv("PATH", pathDir+string(os.PathListSeparator)+prevPath)
		t.Setenv("AEGION_DOCKER_BIN", "")
		t.Setenv("DOCKER_HOST", "")

		client, err := NewDockerClient()
		if err != nil {
			t.Fatalf("NewDockerClient() error: %v", err)
		}
		if client.bin != "docker" {
			t.Fatalf("expected default docker binary name, got %q", client.bin)
		}
	})

	t.Run("create container fails when removing existing container fails", func(t *testing.T) {
		fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"ps\" (\r\n"+
			"  echo {\"ID\":\"existing-1\",\"Names\":\"aegion_password\",\"State\":\"running\",\"Status\":\"Up\"}\r\n"+
			"  exit /b 0\r\n"+
			")\r\n"+
			"if \"%1\"==\"rm\" (\r\n"+
			"  echo rm-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
		t.Setenv("DOCKER_HOST", "")

		docker, err := NewDockerClient()
		if err != nil {
			t.Fatalf("NewDockerClient() error: %v", err)
		}
		_, err = docker.CreateContainer(ctx, &ModuleConfig{
			ID:    "password",
			Image: "repo/password",
		}, "token")
		if err == nil || !strings.Contains(err.Error(), "removing existing container") {
			t.Fatalf("expected remove-existing failure, got %v", err)
		}
	})

	t.Run("create container fails on image pull and create command", func(t *testing.T) {
		t.Run("pull failure", func(t *testing.T) {
			fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
				"if \"%1\"==\"ps\" exit /b 0\r\n"+
				"if \"%1\"==\"image\" exit /b 1\r\n"+
				"if \"%1\"==\"pull\" (\r\n"+
				"  echo pull-failed 1>&2\r\n"+
				"  exit /b 1\r\n"+
				")\r\n"+
				"exit /b 0\r\n")
			t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
			t.Setenv("DOCKER_HOST", "")

			docker, err := NewDockerClient()
			if err != nil {
				t.Fatalf("NewDockerClient() error: %v", err)
			}
			_, err = docker.CreateContainer(ctx, &ModuleConfig{
				ID:    "password",
				Image: "repo/password",
			}, "token")
			if err == nil || !strings.Contains(err.Error(), "pulling image") {
				t.Fatalf("expected pull failure, got %v", err)
			}
		})

		t.Run("create command failure with memory reservation branch", func(t *testing.T) {
			fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
				"if \"%1\"==\"ps\" exit /b 0\r\n"+
				"if \"%1\"==\"image\" exit /b 0\r\n"+
				"if \"%1\"==\"create\" (\r\n"+
				"  echo create-failed 1>&2\r\n"+
				"  exit /b 1\r\n"+
				")\r\n"+
				"exit /b 0\r\n")
			t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
			t.Setenv("DOCKER_HOST", "")

			docker, err := NewDockerClient()
			if err != nil {
				t.Fatalf("NewDockerClient() error: %v", err)
			}
			_, err = docker.CreateContainer(ctx, &ModuleConfig{
				ID:    "password",
				Image: "repo/password",
				Resources: ResourceConfig{
					MemoryReservation: "128m",
				},
			}, "token")
			if err == nil || !strings.Contains(err.Error(), "creating container") {
				t.Fatalf("expected create failure, got %v", err)
			}
		})
	})

	t.Run("command wrappers return errors", func(t *testing.T) {
		fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"start\" (\r\n"+
			"  echo start-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"if \"%1\"==\"stop\" (\r\n"+
			"  echo stop-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"if \"%1\"==\"rm\" (\r\n"+
			"  echo rm-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"if \"%1\"==\"logs\" (\r\n"+
			"  echo logs-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
		t.Setenv("DOCKER_HOST", "")

		docker, err := NewDockerClient()
		if err != nil {
			t.Fatalf("NewDockerClient() error: %v", err)
		}

		if err := docker.StartContainer(ctx, "container-1"); err == nil || !strings.Contains(err.Error(), "starting container") {
			t.Fatalf("expected start error, got %v", err)
		}
		if err := docker.StopContainer(ctx, "container-1", 2*time.Second); err == nil || !strings.Contains(err.Error(), "stopping container") {
			t.Fatalf("expected stop error, got %v", err)
		}
		if err := docker.RemoveContainer(ctx, "container-1", true); err == nil || !strings.Contains(err.Error(), "removing container") {
			t.Fatalf("expected remove error, got %v", err)
		}
		if _, err := docker.ContainerLogs(ctx, "container-1", 5, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "getting container logs") {
			t.Fatalf("expected logs error, got %v", err)
		}
	})

	t.Run("inspect-related wrappers return errors", func(t *testing.T) {
		fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"inspect\" (\r\n"+
			"  echo inspect-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
		t.Setenv("DOCKER_HOST", "")

		docker, err := NewDockerClient()
		if err != nil {
			t.Fatalf("NewDockerClient() error: %v", err)
		}

		if _, err := docker.GetContainerInfo(ctx, "container-1"); err == nil || !strings.Contains(err.Error(), "inspecting container") {
			t.Fatalf("expected container info inspect error, got %v", err)
		}
		if _, err := docker.HealthCheck(ctx, "container-1"); err == nil || !strings.Contains(err.Error(), "inspecting container") {
			t.Fatalf("expected health check inspect error, got %v", err)
		}
		if _, err := docker.inspectContainer(ctx, "container-1"); err == nil {
			t.Fatal("expected inspectContainer to return run error")
		}
	})

	t.Run("ps command failures surface through helpers", func(t *testing.T) {
		fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"ps\" (\r\n"+
			"  echo ps-failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
		t.Setenv("DOCKER_HOST", "")

		docker, err := NewDockerClient()
		if err != nil {
			t.Fatalf("NewDockerClient() error: %v", err)
		}

		if _, err := docker.findContainer(ctx, "aegion_password"); err == nil {
			t.Fatal("expected findContainer to fail when ps command fails")
		}
		if _, err := docker.ListContainers(ctx); err == nil || !strings.Contains(err.Error(), "listing containers") {
			t.Fatalf("expected ListContainers to wrap ps failure, got %v", err)
		}
		if _, err := docker.runJSONLines(ctx, "ps"); err == nil {
			t.Fatal("expected runJSONLines to fail")
		}
	})
}

func TestParseMemoryAdditionalBranches(t *testing.T) {
	if _, err := parseMemory("mb"); err == nil {
		t.Fatal("parseMemory(mb) should fail parse-int branch")
	}
	got, err := parseMemory("1024")
	if err != nil || got != 1024 {
		t.Fatalf("parseMemory(1024) = %d, %v", got, err)
	}
}
