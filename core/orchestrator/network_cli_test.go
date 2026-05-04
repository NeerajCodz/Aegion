package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNetworkCLI_EnsureNetworkAndInspect(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses Windows .cmd fake docker executable")
	}
	logFile := filepath.Join(t.TempDir(), "network-args.txt")
	fakeDocker := writeFakeDockerCLI(t, "@echo off\r\n"+
		"setlocal EnableDelayedExpansion\r\n"+
		"echo %*>> \""+logFile+"\"\r\n"+
		"if \"%1\"==\"network\" (\r\n"+
		"  if \"%2\"==\"ls\" exit /b 0\r\n"+
		"  if \"%2\"==\"create\" (\r\n"+
		"    echo network-123\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		"  if \"%2\"==\"inspect\" (\r\n"+
		"    echo [{\"Id\":\"network-123\",\"Name\":\"aegion_modules\",\"Driver\":\"bridge\",\"Scope\":\"local\",\"Containers\":{\"container-123\":{\"Name\":\"aegion_password\",\"IPv4Address\":\"10.10.0.5/16\",\"IPv6Address\":\"\",\"MacAddress\":\"02:42:ac:11:00:02\"}},\"IPAM\":{\"Config\":[{\"Subnet\":\"10.10.0.0/16\",\"Gateway\":\"10.10.0.1\"}]}}]\r\n"+
		"    exit /b 0\r\n"+
		"  )\r\n"+
		")\r\n"+
		"exit /b 0\r\n")

	t.Setenv("AEGION_DOCKER_BIN", fakeDocker)
	t.Setenv("DOCKER_HOST", "")

	docker, err := NewDockerClient()
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	manager := NewNetworkManager(docker, "aegion_modules", "10.10.0.0/16")

	networkID, err := manager.EnsureNetwork(context.Background())
	if err != nil {
		t.Fatalf("ensure network: %v", err)
	}
	if networkID != "network-123" {
		t.Fatalf("unexpected network id %q", networkID)
	}

	info, err := manager.GetNetworkInfo(context.Background())
	if err != nil {
		t.Fatalf("get network info: %v", err)
	}
	if info.Subnet != "10.10.0.0/16" || len(info.Containers) != 1 {
		t.Fatalf("unexpected network info: %+v", info)
	}

	args, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	got := string(args)
	for _, want := range []string{
		"network ls --filter name=aegion_modules --format \"{{json .}}\"",
		"network create --driver bridge --internal --label aegion.network=true --subnet 10.10.0.0/16 aegion_modules",
		"network inspect aegion_modules",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected network args to contain %q, got %q", want, got)
		}
	}
}
