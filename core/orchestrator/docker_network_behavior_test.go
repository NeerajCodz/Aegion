package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

func TestNewDockerClient_WithSeam(t *testing.T) {
	orig := newDockerEngineClient
	t.Cleanup(func() { newDockerEngineClient = orig })

	newDockerEngineClient = func() (*client.Client, error) { return nil, errors.New("dial failed") }
	if _, err := NewDockerClient(); err == nil || !strings.Contains(err.Error(), "creating docker client") {
		t.Fatalf("expected wrapped docker client creation error, got %v", err)
	}
}

func TestDockerClient_GuardAndUtilityPaths(t *testing.T) {
	t.Run("close returns nil-client error", func(t *testing.T) {
		d := &DockerClient{}
		if err := d.Close(); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected docker client nil error, got %v", err)
		}
	})

	t.Run("close uses seam", func(t *testing.T) {
		d := &DockerClient{closeFn: func() error { return errors.New("close failed") }}
		if err := d.Close(); err == nil || err.Error() != "close failed" {
			t.Fatalf("expected close failed, got %v", err)
		}
	})

	if got := shortID("1234567890123456"); got != "123456789012" {
		t.Fatalf("shortID truncation mismatch: %s", got)
	}
	if got := shortID("short"); got != "short" {
		t.Fatalf("shortID should preserve short IDs")
	}
}

func TestDockerClient_ContainerOps_WithSeams(t *testing.T) {
	ctx := context.Background()
	d := &DockerClient{networkName: DefaultNetworkName}

	t.Run("start stop remove error and success", func(t *testing.T) {
		d.containerStartFn = func(context.Context, string, container.StartOptions) error { return errors.New("start failed") }
		if err := d.StartContainer(ctx, "cid"); err == nil || !strings.Contains(err.Error(), "starting container: start failed") {
			t.Fatalf("expected wrapped start error, got %v", err)
		}

		d.containerStopFn = func(context.Context, string, container.StopOptions) error { return errors.New("stop failed") }
		if err := d.StopContainer(ctx, "cid", 0); err == nil || !strings.Contains(err.Error(), "stopping container: stop failed") {
			t.Fatalf("expected wrapped stop error, got %v", err)
		}

		d.containerRemoveFn = func(context.Context, string, container.RemoveOptions) error { return errors.New("remove failed") }
		if err := d.RemoveContainer(ctx, "cid", true); err == nil || !strings.Contains(err.Error(), "removing container: remove failed") {
			t.Fatalf("expected wrapped remove error, got %v", err)
		}

		d.containerStartFn = func(context.Context, string, container.StartOptions) error { return nil }
		d.containerStopFn = func(_ context.Context, _ string, opts container.StopOptions) error {
			if opts.Timeout == nil || *opts.Timeout != int(DefaultStopTimeout.Seconds()) {
				t.Fatalf("expected default stop timeout seconds")
			}
			return nil
		}
		d.containerRemoveFn = func(_ context.Context, _ string, opts container.RemoveOptions) error {
			if !opts.RemoveVolumes || !opts.Force {
				t.Fatalf("expected remove options to preserve flags")
			}
			return nil
		}
		if err := d.StartContainer(ctx, "cid"); err != nil {
			t.Fatalf("StartContainer should succeed: %v", err)
		}
		if err := d.StopContainer(ctx, "cid", 0); err != nil {
			t.Fatalf("StopContainer should succeed: %v", err)
		}
		if err := d.RemoveContainer(ctx, "cid", true); err != nil {
			t.Fatalf("RemoveContainer should succeed: %v", err)
		}
	})

	t.Run("container logs failure and success", func(t *testing.T) {
		d.containerLogsFn = func(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
			return nil, errors.New("logs failed")
		}
		if _, err := d.ContainerLogs(ctx, "cid", 5, time.Now()); err == nil || !strings.Contains(err.Error(), "getting container logs: logs failed") {
			t.Fatalf("expected wrapped logs error, got %v", err)
		}

		d.containerLogsFn = func(_ context.Context, _ string, opts container.LogsOptions) (io.ReadCloser, error) {
			if opts.Tail != "5" {
				t.Fatalf("expected tail=5")
			}
			if opts.Since == "" {
				t.Fatalf("expected since to be set")
			}
			return io.NopCloser(bytes.NewBufferString("line1\nline2")), nil
		}
		logs, err := d.ContainerLogs(ctx, "cid", 5, time.Now())
		if err != nil {
			t.Fatalf("ContainerLogs returned error: %v", err)
		}
		if !strings.Contains(logs, "line1") {
			t.Fatalf("expected logs output to contain content")
		}
	})

	t.Run("container info and health mapping", func(t *testing.T) {
		inspectResp := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				ID:           "cid",
				Name:         "/aegion_password",
				Created:      "2026-01-01T00:00:00.000000000Z",
				RestartCount: 2,
				State: &container.State{
					Status:     "running",
					ExitCode:   0,
					Error:      "",
					StartedAt:  "2026-01-01T00:00:01.000000000Z",
					FinishedAt: "0001-01-01T00:00:00Z",
					Health: &container.Health{
						Status: "healthy",
					},
					Running: true,
				},
			},
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{
					Ports: nat.PortMap{
						nat.Port("8080/tcp"): {{HostIP: "0.0.0.0", HostPort: "18080"}},
					},
				},
				Networks: map[string]*network.EndpointSettings{
					DefaultNetworkName: {IPAddress: "10.0.0.10"},
				},
			},
		}

		d.containerInspectFn = func(context.Context, string) (container.InspectResponse, error) {
			return inspectResp, nil
		}
		info, err := d.GetContainerInfo(ctx, "cid")
		if err != nil {
			t.Fatalf("GetContainerInfo returned error: %v", err)
		}
		if info.Name != "aegion_password" || info.Health != "healthy" || info.IPAddress != "10.0.0.10" {
			t.Fatalf("unexpected container info mapping: %+v", info)
		}
		if len(info.Ports) != 1 || !strings.Contains(info.Ports[0], "18080") {
			t.Fatalf("expected mapped port binding")
		}

		health, err := d.HealthCheck(ctx, "cid")
		if err != nil || health != "healthy" {
			t.Fatalf("expected healthy status, got %q err=%v", health, err)
		}

		inspectResp.ContainerJSONBase.State.Health = nil
		inspectResp.ContainerJSONBase.State.Running = false
		inspectResp.ContainerJSONBase.State.Status = "exited"
		d.containerInspectFn = func(context.Context, string) (container.InspectResponse, error) {
			return inspectResp, nil
		}
		health, err = d.HealthCheck(ctx, "cid")
		if err != nil || health != "exited" {
			t.Fatalf("expected status fallback to exited, got %q err=%v", health, err)
		}

		d.containerInspectFn = func(context.Context, string) (container.InspectResponse, error) {
			return container.InspectResponse{}, errors.New("inspect failed")
		}
		if _, err := d.GetContainerInfo(ctx, "cid"); err == nil || !strings.Contains(err.Error(), "inspecting container: inspect failed") {
			t.Fatalf("expected wrapped inspect failure for info, got %v", err)
		}
		if _, err := d.HealthCheck(ctx, "cid"); err == nil || !strings.Contains(err.Error(), "inspecting container: inspect failed") {
			t.Fatalf("expected wrapped inspect failure for health, got %v", err)
		}
	})

	t.Run("find and list containers", func(t *testing.T) {
		d.containerListFn = func(context.Context, container.ListOptions) ([]container.Summary, error) {
			return nil, errors.New("list failed")
		}
		if _, err := d.findContainer(ctx, "aegion_password"); err == nil {
			t.Fatalf("expected findContainer list error")
		}
		if _, err := d.ListContainers(ctx); err == nil || !strings.Contains(err.Error(), "listing containers: list failed") {
			t.Fatalf("expected wrapped list containers failure, got %v", err)
		}

		d.containerListFn = func(context.Context, container.ListOptions) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "cid-1", Names: []string{"/aegion_password"}, State: "running", Status: "Up", Created: time.Now().Unix()},
				{ID: "cid-2", Names: []string{"/other"}, State: "exited", Status: "Exited", Created: time.Now().Unix()},
			}, nil
		}
		found, err := d.findContainer(ctx, "aegion_password")
		if err != nil {
			t.Fatalf("findContainer returned error: %v", err)
		}
		if found == nil || found.ID != "cid-1" {
			t.Fatalf("expected matching container")
		}
		none, err := d.findContainer(ctx, "missing")
		if err != nil {
			t.Fatalf("findContainer missing should not error: %v", err)
		}
		if none != nil {
			t.Fatalf("expected nil for missing container")
		}
		list, err := d.ListContainers(ctx)
		if err != nil {
			t.Fatalf("ListContainers returned error: %v", err)
		}
		if len(list) != 2 || list[0].Name == "" {
			t.Fatalf("expected normalized list results")
		}
	})

	t.Run("pull image decision paths", func(t *testing.T) {
		d.imageInspectFn = func(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
			return image.InspectResponse{}, nil
		}
		if err := d.pullImageIfNeeded(ctx, "repo/image:tag"); err != nil {
			t.Fatalf("expected no pull when image inspect succeeds: %v", err)
		}

		d.imageInspectFn = func(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
			return image.InspectResponse{}, errors.New("missing")
		}
		d.imagePullFn = func(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewBufferString("pulled")), nil
		}
		if err := d.pullImageIfNeeded(ctx, "repo/image:tag"); err != nil {
			t.Fatalf("expected pull success path: %v", err)
		}

		d.imagePullFn = func(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
			return nil, errors.New("pull failed")
		}
		if err := d.pullImageIfNeeded(ctx, "repo/image:tag"); err == nil || err.Error() != "pull failed" {
			t.Fatalf("expected pull failure, got %v", err)
		}
	})
}

func TestNetworkManager_WithSeams(t *testing.T) {
	ctx := context.Background()

	t.Run("constructor defaults", func(t *testing.T) {
		nm := NewNetworkManager(nil, "", "10.10.0.0/16")
		if nm.networkName != DefaultNetworkName {
			t.Fatalf("expected default network name")
		}
		if nm.subnet != "10.10.0.0/16" {
			t.Fatalf("expected subnet passthrough")
		}
	})

	t.Run("ensure network existing and create branches", func(t *testing.T) {
		nm := NewNetworkManager(nil, "aegion_modules", "")
		nm.networkListFn = func(context.Context, network.ListOptions) ([]network.Summary, error) {
			return []network.Summary{{ID: "net-1", Name: "aegion_modules"}}, nil
		}
		id, err := nm.EnsureNetwork(ctx)
		if err != nil || id != "net-1" {
			t.Fatalf("expected existing network path, got id=%q err=%v", id, err)
		}

		nm.networkListFn = func(context.Context, network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		}
		nm.subnet = "10.20.0.0/16"
		nm.networkCreateFn = func(_ context.Context, _ string, opts network.CreateOptions) (network.CreateResponse, error) {
			if opts.IPAM == nil || len(opts.IPAM.Config) == 0 || opts.IPAM.Config[0].Subnet != "10.20.0.0/16" {
				t.Fatalf("expected subnet in create options")
			}
			return network.CreateResponse{ID: "net-2"}, nil
		}
		id, err = nm.EnsureNetwork(ctx)
		if err != nil || id != "net-2" {
			t.Fatalf("expected create network path, got id=%q err=%v", id, err)
		}

		nm.networkCreateFn = func(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
			return network.CreateResponse{}, errors.New("create failed")
		}
		if _, err := nm.EnsureNetwork(ctx); err == nil || !strings.Contains(err.Error(), "creating network: create failed") {
			t.Fatalf("expected wrapped create failure, got %v", err)
		}
	})

	t.Run("connect disconnect remove info and exists", func(t *testing.T) {
		nm := NewNetworkManager(nil, "aegion_modules", "")
		nm.networkInspectFn = func(context.Context, string, network.InspectOptions) (network.Inspect, error) {
			return network.Inspect{
				ID:   "net-1",
				Name: "aegion_modules",
				Containers: map[string]network.EndpointResource{
					"already": {Name: "existing"},
				},
			}, nil
		}
		if err := nm.ConnectToNetwork(ctx, "already", []string{"alias"}); err != nil {
			t.Fatalf("expected already connected path: %v", err)
		}

		var gotAliases []string
		nm.networkInspectFn = func(context.Context, string, network.InspectOptions) (network.Inspect, error) {
			return network.Inspect{Containers: map[string]network.EndpointResource{}}, nil
		}
		nm.networkConnectFn = func(_ context.Context, _ string, _ string, cfg *network.EndpointSettings) error {
			gotAliases = cfg.Aliases
			return nil
		}
		if err := nm.ConnectToNetwork(ctx, "cid", []string{"a1", "a2"}); err != nil {
			t.Fatalf("ConnectToNetwork returned error: %v", err)
		}
		if len(gotAliases) != 2 {
			t.Fatalf("expected aliases to be passed to endpoint settings")
		}

		nm.networkConnectFn = func(context.Context, string, string, *network.EndpointSettings) error {
			return errors.New("connect failed")
		}
		if err := nm.ConnectToNetwork(ctx, "cid", nil); err == nil || !strings.Contains(err.Error(), "connecting to network: connect failed") {
			t.Fatalf("expected wrapped connect failure, got %v", err)
		}

		nm.networkDisconnectFn = func(context.Context, string, string, bool) error { return errors.New("disconnect failed") }
		if err := nm.DisconnectFromNetwork(ctx, "cid"); err == nil || !strings.Contains(err.Error(), "disconnecting from network: disconnect failed") {
			t.Fatalf("expected wrapped disconnect failure, got %v", err)
		}
		nm.networkDisconnectFn = func(context.Context, string, string, bool) error { return nil }
		if err := nm.DisconnectFromNetwork(ctx, "cid"); err != nil {
			t.Fatalf("DisconnectFromNetwork should succeed: %v", err)
		}

		nm.networkRemoveFn = func(context.Context, string) error { return errors.New("remove failed") }
		if err := nm.RemoveNetwork(ctx); err == nil || !strings.Contains(err.Error(), "removing network: remove failed") {
			t.Fatalf("expected wrapped remove failure, got %v", err)
		}
		nm.networkRemoveFn = func(context.Context, string) error { return nil }
		if err := nm.RemoveNetwork(ctx); err != nil {
			t.Fatalf("RemoveNetwork should succeed: %v", err)
		}

		nm.networkInspectFn = func(context.Context, string, network.InspectOptions) (network.Inspect, error) {
			return network.Inspect{
				ID:     "net-1",
				Name:   "aegion_modules",
				Driver: "bridge",
				Scope:  "local",
				IPAM: network.IPAM{
					Config: []network.IPAMConfig{{Subnet: "10.10.0.0/16", Gateway: "10.10.0.1"}},
				},
				Containers: map[string]network.EndpointResource{
					"cid": {Name: "svc", IPv4Address: "10.10.0.20/16", MacAddress: "aa:bb"},
				},
			}, nil
		}
		info, err := nm.GetNetworkInfo(ctx)
		if err != nil {
			t.Fatalf("GetNetworkInfo returned error: %v", err)
		}
		if info.Name != "aegion_modules" || len(info.Containers) != 1 {
			t.Fatalf("unexpected network info mapping: %+v", info)
		}

		ip, err := nm.GetContainerIP(ctx, "cid")
		if err != nil || ip != "10.10.0.20/16" {
			t.Fatalf("expected container IP lookup, got ip=%q err=%v", ip, err)
		}
		if _, err := nm.GetContainerIP(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "container not found in network") {
			t.Fatalf("expected missing container error, got %v", err)
		}

		nm.networkListFn = func(context.Context, network.ListOptions) ([]network.Summary, error) {
			return []network.Summary{{ID: "net-1", Name: "aegion_modules"}}, nil
		}
		exists, err := nm.NetworkExists(ctx)
		if err != nil || !exists {
			t.Fatalf("expected network exists true, got %v %v", exists, err)
		}

		nm.networkListFn = func(context.Context, network.ListOptions) ([]network.Summary, error) {
			return nil, errors.New("list failed")
		}
		if _, err := nm.NetworkExists(ctx); err == nil {
			t.Fatalf("expected NetworkExists error")
		}
	})
}
