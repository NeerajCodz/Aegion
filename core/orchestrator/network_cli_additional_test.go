package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkManagerAdditionalEnsureAndRunBranches(t *testing.T) {
	t.Run("defaults network name and nil docker run guard", func(t *testing.T) {
		manager := NewNetworkManager(nil, "", "")
		if manager.networkName != DefaultNetworkName {
			t.Fatalf("expected default network name %q, got %q", DefaultNetworkName, manager.networkName)
		}
		if _, err := manager.run(context.Background(), "network", "ls"); err == nil {
			t.Fatal("run() with nil docker client should fail")
		}
	})

	t.Run("ensure network returns existing id", func(t *testing.T) {
		fake := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" (\r\n"+
			"  if \"%2\"==\"ls\" (\r\n"+
			"    echo {\"ID\":\"existing-1\",\"Name\":\"existing_net\"}\r\n"+
			"    exit /b 0\r\n"+
			"  )\r\n"+
			"  if \"%2\"==\"create\" (\r\n"+
			"    echo should-not-create\r\n"+
			"    exit /b 0\r\n"+
			"  )\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "existing_net", "10.0.0.0/24")
		id, err := manager.EnsureNetwork(context.Background())
		if err != nil {
			t.Fatalf("EnsureNetwork(existing) error = %v", err)
		}
		if id != "existing-1" {
			t.Fatalf("EnsureNetwork(existing) id = %q", id)
		}
	})

	t.Run("ensure network wraps ls and create errors", func(t *testing.T) {
		lsFail := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"ls\" (\r\n"+
			"  echo ls failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: lsFail, networkName: DefaultNetworkName}, "aegion_modules", "")
		if _, err := manager.EnsureNetwork(context.Background()); err == nil || !strings.Contains(err.Error(), "listing networks") {
			t.Fatalf("EnsureNetwork(ls error) = %v", err)
		}

		createFail := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" (\r\n"+
			"  if \"%2\"==\"ls\" exit /b 0\r\n"+
			"  if \"%2\"==\"create\" (\r\n"+
			"    echo create failed 1>&2\r\n"+
			"    exit /b 1\r\n"+
			"  )\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: createFail, networkName: DefaultNetworkName}, "aegion_modules", "10.0.0.0/24")
		if _, err := manager.EnsureNetwork(context.Background()); err == nil || !strings.Contains(err.Error(), "creating network") {
			t.Fatalf("EnsureNetwork(create error) = %v", err)
		}
	})

	t.Run("runJSONLines trims empty lines", func(t *testing.T) {
		fake := writeFakeDockerCLI(t, "@echo off\r\n"+
			"echo line-1\r\n"+
			"echo.\r\n"+
			"echo line-2\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "aegion_modules", "")
		lines, err := manager.runJSONLines(context.Background(), "network", "ls")
		if err != nil {
			t.Fatalf("runJSONLines() error = %v", err)
		}
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "line-1") || !strings.HasPrefix(lines[1], "line-2") {
			t.Fatalf("runJSONLines() = %#v", lines)
		}
	})
}

func TestNetworkManagerAdditionalConnectInspectAndInfoBranches(t *testing.T) {
	t.Run("connect uses aliases and skips already-connected container", func(t *testing.T) {
		logFile := filepath.Join(t.TempDir(), "network-connect-args.txt")
		fake := writeFakeDockerCLI(t, "@echo off\r\n"+
			"setlocal EnableDelayedExpansion\r\n"+
			"echo %*>> \""+logFile+"\"\r\n"+
			"if \"%1\"==\"network\" (\r\n"+
			"  if \"%2\"==\"inspect\" (\r\n"+
			"    if \"%3\"==\"already_net\" (\r\n"+
			"      echo [{\"Id\":\"already-1\",\"Name\":\"already_net\",\"Driver\":\"bridge\",\"Scope\":\"local\",\"Containers\":{\"container-1\":{\"Name\":\"existing\",\"IPv4Address\":\"10.0.0.2/24\",\"IPv6Address\":\"\",\"MacAddress\":\"\"}},\"IPAM\":{\"Config\":[]}}]\r\n"+
			"      exit /b 0\r\n"+
			"    )\r\n"+
			"    echo [{\"Id\":\"new-1\",\"Name\":\"new_net\",\"Driver\":\"bridge\",\"Scope\":\"local\",\"Containers\":{},\"IPAM\":{\"Config\":[]}}]\r\n"+
			"    exit /b 0\r\n"+
			"  )\r\n"+
			"  if \"%2\"==\"connect\" exit /b 0\r\n"+
			")\r\n"+
			"exit /b 0\r\n")

		already := NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "already_net", "")
		if err := already.ConnectToNetwork(context.Background(), "container-1", []string{"a", "b"}); err != nil {
			t.Fatalf("ConnectToNetwork(already connected) error = %v", err)
		}

		connect := NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "new_net", "")
		if err := connect.ConnectToNetwork(context.Background(), "container-2", []string{"alias-a", " ", "alias-b"}); err != nil {
			t.Fatalf("ConnectToNetwork(connect) error = %v", err)
		}

		raw, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("read connect log: %v", err)
		}
		logged := string(raw)
		if !strings.Contains(logged, "network connect --alias alias-a --alias alias-b new_net container-2") {
			t.Fatalf("expected alias args in connect command, got %q", logged)
		}
	})

	t.Run("connect and info wrap inspect/command errors", func(t *testing.T) {
		inspectFail := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"inspect\" (\r\n"+
			"  echo inspect failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: inspectFail, networkName: DefaultNetworkName}, "aegion_modules", "")
		if err := manager.ConnectToNetwork(context.Background(), "container-1", nil); err == nil || !strings.Contains(err.Error(), "inspecting network") {
			t.Fatalf("ConnectToNetwork(inspect error) = %v", err)
		}
		if _, err := manager.GetNetworkInfo(context.Background()); err == nil || !strings.Contains(err.Error(), "inspecting network") {
			t.Fatalf("GetNetworkInfo(inspect error) = %v", err)
		}
		if _, err := manager.GetContainerIP(context.Background(), "container-1"); err == nil || !strings.Contains(err.Error(), "inspecting network") {
			t.Fatalf("GetContainerIP(inspect error) = %v", err)
		}

		connectFail := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" (\r\n"+
			"  if \"%2\"==\"inspect\" (\r\n"+
			"    echo [{\"Id\":\"new-1\",\"Name\":\"aegion_modules\",\"Driver\":\"bridge\",\"Scope\":\"local\",\"Containers\":{},\"IPAM\":{\"Config\":[]}}]\r\n"+
			"    exit /b 0\r\n"+
			"  )\r\n"+
			"  if \"%2\"==\"connect\" (\r\n"+
			"    echo connect failed 1>&2\r\n"+
			"    exit /b 1\r\n"+
			"  )\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: connectFail, networkName: DefaultNetworkName}, "aegion_modules", "")
		if err := manager.ConnectToNetwork(context.Background(), "container-2", nil); err == nil || !strings.Contains(err.Error(), "connecting to network") {
			t.Fatalf("ConnectToNetwork(connect error) = %v", err)
		}
	})

	t.Run("inspect parse/not-found branches and container lookup", func(t *testing.T) {
		badJSON := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"inspect\" (\r\n"+
			"  echo not-json\r\n"+
			"  exit /b 0\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: badJSON, networkName: DefaultNetworkName}, "aegion_modules", "")
		if _, err := manager.inspect(context.Background()); err == nil {
			t.Fatal("inspect(invalid json) expected error")
		}

		empty := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"inspect\" (\r\n"+
			"  echo []\r\n"+
			"  exit /b 0\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: empty, networkName: DefaultNetworkName}, "aegion_modules", "")
		if _, err := manager.inspect(context.Background()); err == nil || !strings.Contains(err.Error(), "network not found") {
			t.Fatalf("inspect(empty) = %v", err)
		}

		okInspect := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"inspect\" (\r\n"+
			"  echo [{\"Id\":\"net-1\",\"Name\":\"aegion_modules\",\"Driver\":\"bridge\",\"Scope\":\"local\",\"Containers\":{\"container-1\":{\"Name\":\"module\",\"IPv4Address\":\"10.0.0.5/24\",\"IPv6Address\":\"\",\"MacAddress\":\"00:11\"}},\"IPAM\":{\"Config\":[{\"Subnet\":\"10.0.0.0/24\",\"Gateway\":\"10.0.0.1\"}]}}]\r\n"+
			"  exit /b 0\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: okInspect, networkName: DefaultNetworkName}, "aegion_modules", "")
		ip, err := manager.GetContainerIP(context.Background(), "container-1")
		if err != nil || ip != "10.0.0.5/24" {
			t.Fatalf("GetContainerIP(found) ip=%q err=%v", ip, err)
		}
		if _, err := manager.GetContainerIP(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "container not found") {
			t.Fatalf("GetContainerIP(missing) = %v", err)
		}
	})
}

func TestNetworkManagerAdditionalExistsDisconnectRemoveBranches(t *testing.T) {
	t.Run("network exists parses invalid json lines and matches by name", func(t *testing.T) {
		fake := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"ls\" (\r\n"+
			"  echo not-json\r\n"+
			"  echo {\"ID\":\"net-1\",\"Name\":\"other\"}\r\n"+
			"  echo {\"ID\":\"net-2\",\"Name\":\"target\"}\r\n"+
			"  exit /b 0\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "target", "")
		ok, err := manager.NetworkExists(context.Background())
		if err != nil || !ok {
			t.Fatalf("NetworkExists(target) ok=%v err=%v", ok, err)
		}

		manager = NewNetworkManager(&DockerClient{bin: fake, networkName: DefaultNetworkName}, "absent", "")
		ok, err = manager.NetworkExists(context.Background())
		if err != nil || ok {
			t.Fatalf("NetworkExists(absent) ok=%v err=%v", ok, err)
		}
	})

	t.Run("disconnect and remove network wrap command failures", func(t *testing.T) {
		failDisconnect := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"disconnect\" (\r\n"+
			"  echo disconnect failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager := NewNetworkManager(&DockerClient{bin: failDisconnect, networkName: DefaultNetworkName}, "aegion_modules", "")
		if err := manager.DisconnectFromNetwork(context.Background(), "container-1"); err == nil || !strings.Contains(err.Error(), "disconnecting from network") {
			t.Fatalf("DisconnectFromNetwork(error) = %v", err)
		}

		failRemove := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" if \"%2\"==\"rm\" (\r\n"+
			"  echo remove failed 1>&2\r\n"+
			"  exit /b 1\r\n"+
			")\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: failRemove, networkName: DefaultNetworkName}, "aegion_modules", "")
		if err := manager.RemoveNetwork(context.Background()); err == nil || !strings.Contains(err.Error(), "removing network") {
			t.Fatalf("RemoveNetwork(error) = %v", err)
		}

		ok := writeFakeDockerCLI(t, "@echo off\r\n"+
			"if \"%1\"==\"network\" exit /b 0\r\n"+
			"exit /b 0\r\n")
		manager = NewNetworkManager(&DockerClient{bin: ok, networkName: DefaultNetworkName}, "aegion_modules", "")
		if err := manager.DisconnectFromNetwork(context.Background(), "container-1"); err != nil {
			t.Fatalf("DisconnectFromNetwork(success) = %v", err)
		}
		if err := manager.RemoveNetwork(context.Background()); err != nil {
			t.Fatalf("RemoveNetwork(success) = %v", err)
		}
	})
}

