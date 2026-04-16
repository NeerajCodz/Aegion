package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeComprehensiveFakeDockerCLI(t *testing.T) string {
	t.Helper()
	return writeFakeDockerCLI(t, "@echo off\r\n"+
		"if \"%1\"==\"fail\" (\r\n"+
		"  echo boom 1>&2\r\n"+
		"  exit /b 1\r\n"+
		")\r\n"+
		"if \"%1\"==\"ps\" (\r\n"+
		"  echo {\"ID\":\"cid-1\",\"Names\":\"aegion_password\",\"State\":\"running\",\"Status\":\"Up 10s\"}\r\n"+
		"  echo not-json\r\n"+
		"  echo {\"ID\":\"cid-2\",\"Names\":\"aegion_other\",\"State\":\"exited\",\"Status\":\"Exited (0)\"}\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"inspect\" (\r\n"+
		"  if \"%2\"==\"badjson\" (\r\n"+
		"    echo bad-json\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		"  if \"%2\"==\"empty\" (\r\n"+
		"    echo []\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		"  if \"%2\"==\"nohealth\" (\r\n"+
		"    echo [{\"Id\":\"container-2\",\"Name\":\"/aegion_nohealth\",\"Created\":\"2024-01-01T00:00:00Z\",\"RestartCount\":0,\"State\":{\"Status\":\"exited\",\"Running\":false,\"StartedAt\":\"2024-01-01T00:00:01Z\",\"FinishedAt\":\"2024-01-01T00:00:02Z\",\"ExitCode\":0,\"Error\":\"\"},\"NetworkSettings\":{\"Networks\":{},\"Ports\":{}}}]\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		"  echo [{\"Id\":\"container-1\",\"Name\":\"/aegion_password\",\"Created\":\"2024-01-01T00:00:00Z\",\"RestartCount\":2,\"State\":{\"Status\":\"running\",\"Running\":true,\"StartedAt\":\"2024-01-01T00:00:01Z\",\"FinishedAt\":\"0001-01-01T00:00:00Z\",\"ExitCode\":0,\"Error\":\"\",\"Health\":{\"Status\":\"healthy\"}},\"NetworkSettings\":{\"Networks\":{\"aegion_modules\":{\"IPAddress\":\"172.20.0.2\"}},\"Ports\":{\"8080/tcp\":[{\"HostIp\":\"0.0.0.0\",\"HostPort\":\"18080\"}]}}}]\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"image\" (\r\n"+
		"  if \"%3\"==\"present:image\" exit /b 0\r\n"+
		"  exit /b 1\r\n"+
		")\r\n"+
		"if \"%1\"==\"pull\" (\r\n"+
		"  if \"%2\"==\"missing:image\" (\r\n"+
		"    echo pulled\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		"  exit /b 1\r\n"+
		")\r\n"+
		"if \"%1\"==\"logs\" (\r\n"+
		"  echo line1\r\n"+
		"  echo line2\r\n"+
		"  exit /b 0\r\n"+
		")\r\n"+
		"if \"%1\"==\"start\" exit /b 0\r\n"+
		"if \"%1\"==\"stop\" exit /b 0\r\n"+
		"if \"%1\"==\"rm\" exit /b 0\r\n"+
		"exit /b 0\r\n")
}

func TestDockerCLIAdditionalHelpers(t *testing.T) {
	t.Setenv("FEATURE_FLAG", "yes")
	if !parseBoolEnv("FEATURE_FLAG") {
		t.Fatal("parseBoolEnv should accept yes")
	}
	t.Setenv("FEATURE_FLAG", "0")
	if parseBoolEnv("FEATURE_FLAG") {
		t.Fatal("parseBoolEnv should reject 0")
	}

	if got, err := parseMemory("512m"); err != nil || got != 512*1024*1024 {
		t.Fatalf("parseMemory(512m) = %d, %v", got, err)
	}
	if got, err := parseMemory("2g"); err != nil || got != 2*1024*1024*1024 {
		t.Fatalf("parseMemory(2g) = %d, %v", got, err)
	}
	if _, err := parseMemory("x"); err == nil {
		t.Fatal("parseMemory(x) expected error")
	}

	if got, err := parseCPU("1.5"); err != nil || got != 1500000000 {
		t.Fatalf("parseCPU(1.5) = %d, %v", got, err)
	}
	if _, err := parseCPU("invalid"); err == nil {
		t.Fatal("parseCPU(invalid) expected error")
	}

	if shortID("12345678901234567890") != "123456789012" {
		t.Fatal("shortID should trim to 12 characters")
	}
	if shortID("short") != "short" {
		t.Fatal("shortID should preserve short values")
	}

	d := &DockerClient{networkName: "initial"}
	d.SetNetworkName("   ")
	if d.networkName != "initial" {
		t.Fatalf("expected network to remain initial, got %q", d.networkName)
	}
	d.SetNetworkName(" custom ")
	if d.networkName != "custom" {
		t.Fatalf("expected trimmed network custom, got %q", d.networkName)
	}
}

func TestValidateDockerHostSafetyAdditionalBranches(t *testing.T) {
	t.Setenv("DOCKER_HOST", "http://127.0.0.1:2375")
	if err := validateDockerHostSafety(); err == nil || !strings.Contains(err.Error(), "unsupported DOCKER_HOST scheme") {
		t.Fatalf("expected unsupported scheme error, got %v", err)
	}

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2375")
	t.Setenv("AEGION_ALLOW_REMOTE_DOCKER_HOST", "true")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	if err := validateDockerHostSafety(); err == nil || !strings.Contains(err.Error(), "DOCKER_TLS_VERIFY") {
		t.Fatalf("expected DOCKER_TLS_VERIFY error, got %v", err)
	}

	t.Setenv("DOCKER_TLS_VERIFY", "1")
	if err := validateDockerHostSafety(); err != nil {
		t.Fatalf("expected tcp host to pass with explicit allow+tls, got %v", err)
	}

	t.Setenv("DOCKER_HOST", "ssh://docker.example")
	if err := validateDockerHostSafety(); err != nil {
		t.Fatalf("ssh DOCKER_HOST should be allowed, got %v", err)
	}
}

func TestNewDockerClientMissingBinary(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("AEGION_DOCKER_BIN", filepath.Join(t.TempDir(), "missing-docker.exe"))
	if _, err := NewDockerClient(); err == nil {
		t.Fatal("NewDockerClient should fail when docker binary is missing")
	}
}

func TestDockerClientCommandPathsAndInspectHelpers(t *testing.T) {
	d := &DockerClient{
		bin:         writeComprehensiveFakeDockerCLI(t),
		networkName: DefaultNetworkName,
	}

	ctx := context.Background()

	if _, err := d.run(ctx, "fail"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("run(fail) expected stderr error, got %v", err)
	}

	lines, err := d.runJSONLines(ctx, "ps")
	if err != nil {
		t.Fatalf("runJSONLines(ps) error = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("runJSONLines(ps) expected 3 lines, got %d", len(lines))
	}

	found, err := d.findContainer(ctx, "aegion_password")
	if err != nil || found == nil || found.ID != "cid-1" {
		t.Fatalf("findContainer(aegion_password) = %#v, err=%v", found, err)
	}
	missing, err := d.findContainer(ctx, "does-not-exist")
	if err != nil || missing != nil {
		t.Fatalf("findContainer(missing) = %#v, err=%v", missing, err)
	}

	list, err := d.ListContainers(ctx)
	if err != nil {
		t.Fatalf("ListContainers error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListContainers expected 2 parsed containers, got %d", len(list))
	}

	info, err := d.GetContainerInfo(ctx, "container-1")
	if err != nil {
		t.Fatalf("GetContainerInfo error = %v", err)
	}
	if info.Name != "aegion_password" || info.Health != "healthy" || info.IPAddress != "172.20.0.2" {
		t.Fatalf("unexpected container info: %#v", info)
	}
	if len(info.Ports) != 1 || !strings.Contains(info.Ports[0], "18080->8080/tcp") {
		t.Fatalf("unexpected ports mapping: %#v", info.Ports)
	}

	health, err := d.HealthCheck(ctx, "container-1")
	if err != nil || health != "healthy" {
		t.Fatalf("HealthCheck(container-1) = %q, %v", health, err)
	}
	health, err = d.HealthCheck(ctx, "nohealth")
	if err != nil || health != "exited" {
		t.Fatalf("HealthCheck(nohealth) = %q, %v", health, err)
	}

	if _, err := d.inspectContainer(ctx, "badjson"); err == nil {
		t.Fatal("inspectContainer(badjson) expected unmarshal error")
	}
	if _, err := d.inspectContainer(ctx, "empty"); err == nil || !strings.Contains(err.Error(), "container not found") {
		t.Fatalf("inspectContainer(empty) expected not found, got %v", err)
	}

	if err := d.pullImageIfNeeded(ctx, "present:image"); err != nil {
		t.Fatalf("pullImageIfNeeded(present:image) error = %v", err)
	}
	if err := d.pullImageIfNeeded(ctx, "missing:image"); err != nil {
		t.Fatalf("pullImageIfNeeded(missing:image) error = %v", err)
	}
	if err := d.pullImageIfNeeded(ctx, "missing2:image"); err == nil {
		t.Fatal("pullImageIfNeeded(missing2:image) expected pull failure")
	}

	if err := d.StartContainer(ctx, "container-1"); err != nil {
		t.Fatalf("StartContainer error = %v", err)
	}
	if err := d.StopContainer(ctx, "container-1", 0); err != nil {
		t.Fatalf("StopContainer(default timeout) error = %v", err)
	}
	if err := d.RemoveContainer(ctx, "container-1", true); err != nil {
		t.Fatalf("RemoveContainer(force) error = %v", err)
	}
	if err := d.RemoveContainer(ctx, "container-1", false); err != nil {
		t.Fatalf("RemoveContainer(non-force) error = %v", err)
	}

	logs, err := d.ContainerLogs(ctx, "container-1", 10, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("ContainerLogs error = %v", err)
	}
	if !strings.Contains(logs, "line1") {
		t.Fatalf("ContainerLogs expected line output, got %q", logs)
	}
}
