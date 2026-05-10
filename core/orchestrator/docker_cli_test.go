package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFakeDockerCLI(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS != "windows" {
		path := filepath.Join(dir, "fake-docker")
		if err := os.WriteFile(path, []byte(cmdFakeToShell(script)), 0o755); err != nil {
			t.Fatalf("write fake docker cli: %v", err)
		}
		return path
	}
	path := filepath.Join(dir, "fake-docker.cmd")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker cli: %v", err)
	}
	return path
}

func cmdFakeToShell(script string) string {
	var out []string
	out = append(out, "#!/bin/sh")
	ifBlock := regexp.MustCompile(`^if "%([0-9]+)"=="([^"]*)" \($`)
	ifInline := regexp.MustCompile(`^if "%([0-9]+)"=="([^"]*)" exit /b ([0-9]+)$`)
	ifNestedBlock := regexp.MustCompile(`^if "%([0-9]+)"=="([^"]*)" if "%([0-9]+)"=="([^"]*)" \($`)
	for _, raw := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", strings.EqualFold(line, "@echo off"), strings.HasPrefix(strings.ToLower(line), "setlocal "):
			continue
		case line == ")":
			out = append(out, "fi")
		case strings.HasPrefix(strings.ToLower(line), "exit /b "):
			out = append(out, "exit "+strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "exit /b ")))
		case strings.HasPrefix(line, "echo %*>>"):
			target := strings.TrimSpace(strings.TrimPrefix(line, "echo %*>>"))
			out = append(out, `printf '%s\n' "$*" >> `+target)
		case strings.HasPrefix(line, "echo %* >"):
			target := strings.TrimSpace(strings.TrimPrefix(line, "echo %* >"))
			out = append(out, `printf '%s\n' "$*" > `+target)
		case strings.EqualFold(line, "echo."):
			out = append(out, "echo")
		case strings.HasPrefix(strings.ToLower(line), "echo "):
			msg := strings.TrimSpace(line[5:])
			stderr := false
			if strings.HasSuffix(msg, "1>&2") {
				stderr = true
				msg = strings.TrimSpace(strings.TrimSuffix(msg, "1>&2"))
			}
			cmd := "printf '%s\\n' " + shellQuote(msg)
			if stderr {
				cmd += " >&2"
			}
			out = append(out, cmd)
		case ifNestedBlock.MatchString(line):
			parts := ifNestedBlock.FindStringSubmatch(line)
			out = append(out, `if [ "$`+parts[1]+`" = `+shellQuote(parts[2])+` ] && [ "$`+parts[3]+`" = `+shellQuote(parts[4])+` ]; then`)
		case ifBlock.MatchString(line):
			parts := ifBlock.FindStringSubmatch(line)
			out = append(out, `if [ "$`+parts[1]+`" = `+shellQuote(parts[2])+` ]; then`)
		case ifInline.MatchString(line):
			parts := ifInline.FindStringSubmatch(line)
			out = append(out, `if [ "$`+parts[1]+`" = `+shellQuote(parts[2])+` ]; then exit `+parts[3]+`; fi`)
		default:
			out = append(out, ":")
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestDockerCLI_CreateContainerPreservesExpectedFlags(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses Windows .cmd fake docker executable")
	}
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
		"setlocal EnableDelayedExpansion\r\n"+
		"echo %* > \""+argsFile+"\"\r\n"+
		"if \"%1\"==\"image\" exit /b 1\r\n"+
		"if \"%1\"==\"pull\" (\r\n"+
		"  echo pulled\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"create\" (\r\n"+
		"  echo container-123\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"exit /b 0\r\n")

	t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
	t.Setenv("DOCKER_HOST", "")

	docker, err := NewDockerClient()
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}

	cfg := &ModuleConfig{
		ID:      "password",
		Name:    "Password",
		Image:   "repo/password",
		Version: "v1.2.3",
		Env: map[string]string{
			"FEATURE": "on",
		},
		Labels: map[string]string{
			"aegion.module": "true",
		},
		Ports: []PortMapping{
			{HostPort: "18080", ContainerPort: "8080"},
		},
		Volumes: []VolumeMapping{
			{HostPath: "C:\\data", ContainerPath: "/data", ReadOnly: true},
		},
		Resources: ResourceConfig{
			MemoryLimit: "512m",
			CPULimit:    "1.5",
		},
		RestartPolicy: "on-failure",
		HealthCheck: HealthCheckConfig{
			Endpoint:    "/health",
			Interval:    5 * time.Second,
			Timeout:     2 * time.Second,
			Retries:     3,
			StartPeriod: 30 * time.Second,
		},
	}

	containerID, err := docker.CreateContainer(context.Background(), cfg, "auth-token")
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	if containerID != "container-123" {
		t.Fatalf("unexpected container id %q", containerID)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	got := string(args)
	for _, want := range []string{
		"create",
		"--name aegion_password",
		"--network aegion_modules",
		"--network-alias password",
		"--label aegion.module=true",
		"--env-file",
		"-p 127.0.0.1:18080:8080/tcp",
		"-v C:\\data:/data:ro",
		"--memory 512m",
		"--cpus 1.5",
		"--restart on-failure:5",
		"--health-cmd \"wget -q --spider http://localhost/health || exit 1\"",
		"repo/password:v1.2.3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected args to contain %q, got %q", want, got)
		}
	}

	if strings.Contains(got, "auth-token") {
		t.Fatalf("expected auth token to be omitted from cli args, got %q", got)
	}
	if strings.Contains(got, "-e FEATURE=on") || strings.Contains(got, "-e AEGION_AUTH_TOKEN") {
		t.Fatalf("expected environment values to be passed via env-file, got %q", got)
	}

}

func TestDockerCLI_ValidateRemoteHostSafety(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2375")
	t.Setenv("AEGION_ALLOW_REMOTE_DOCKER_HOST", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")

	if _, err := NewDockerClient(); err == nil || !strings.Contains(err.Error(), "AEGION_ALLOW_REMOTE_DOCKER_HOST") {
		t.Fatalf("expected remote host safety error, got %v", err)
	}
}
