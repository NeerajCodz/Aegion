package orchestrator

import (
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
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type failingReadCloser struct{}

func (f failingReadCloser) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (f failingReadCloser) Close() error {
	return nil
}

func TestDockerClient_AdditionalCoverageBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("default docker engine constructor executes", func(t *testing.T) {
		_, _ = newDockerEngineClient()
	})

	t.Run("nil client guard branches", func(t *testing.T) {
		d := &DockerClient{}
		if err := d.StartContainer(ctx, "cid"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client start error, got %v", err)
		}
		if err := d.StopContainer(ctx, "cid", time.Second); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client stop error, got %v", err)
		}
		if err := d.RemoveContainer(ctx, "cid", true); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client remove error, got %v", err)
		}
		if _, err := d.ContainerLogs(ctx, "cid", 10, time.Now()); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client logs error, got %v", err)
		}
		if _, err := d.GetContainerInfo(ctx, "cid"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client inspect error, got %v", err)
		}
		if _, err := d.HealthCheck(ctx, "cid"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client health error, got %v", err)
		}
		if _, err := d.findContainer(ctx, "aegion_password"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client find error, got %v", err)
		}
		if err := d.pullImageIfNeeded(ctx, "repo/image:tag"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client image inspect error, got %v", err)
		}
		if _, err := d.ListContainers(ctx); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client list error, got %v", err)
		}
	})

	t.Run("pull branch with missing pull delegate", func(t *testing.T) {
		d := &DockerClient{
			imageInspectFn: func(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
				return image.InspectResponse{}, errors.New("missing")
			},
		}
		if err := d.pullImageIfNeeded(ctx, "repo/image:tag"); err == nil || err.Error() != "docker client is nil" {
			t.Fatalf("expected nil-client image pull error, got %v", err)
		}
	})

	t.Run("container logs read failure", func(t *testing.T) {
		d := &DockerClient{
			containerLogsFn: func(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
				return failingReadCloser{}, nil
			},
		}
		if _, err := d.ContainerLogs(ctx, "cid", 1, time.Now()); err == nil || !strings.Contains(err.Error(), "reading container logs") {
			t.Fatalf("expected wrapped read error, got %v", err)
		}
	})

	t.Run("health check fallback for running containers", func(t *testing.T) {
		d := &DockerClient{
			containerInspectFn: func(context.Context, string) (container.InspectResponse, error) {
				return container.InspectResponse{
					ContainerJSONBase: &container.ContainerJSONBase{
						State: &container.State{
							Running: true,
							Status:  "running",
						},
					},
				}, nil
			},
		}
		health, err := d.HealthCheck(ctx, "cid")
		if err != nil {
			t.Fatalf("HealthCheck returned error: %v", err)
		}
		if health != "running" {
			t.Fatalf("expected running fallback status, got %q", health)
		}
	})

	t.Run("create container warning branch", func(t *testing.T) {
		d := &DockerClient{
			networkName: DefaultNetworkName,
			containerListFn: func(context.Context, container.ListOptions) ([]container.Summary, error) {
				return nil, nil
			},
			imageInspectFn: func(context.Context, string, ...client.ImageInspectOption) (image.InspectResponse, error) {
				return image.InspectResponse{}, nil
			},
			containerCreateFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
				return container.CreateResponse{
					ID:       "cid-created",
					Warnings: []string{"warning-message"},
				}, nil
			},
		}

		id, err := d.CreateContainer(ctx, &ModuleConfig{
			ID:     "password",
			Name:   "Password",
			Image:  "repo/password",
			Env:    map[string]string{},
			Labels: map[string]string{"aegion.module": "true"},
		}, "token")
		if err != nil {
			t.Fatalf("CreateContainer returned error: %v", err)
		}
		if id != "cid-created" {
			t.Fatalf("unexpected container id: %q", id)
		}
	})

	t.Run("parseMemory short format error", func(t *testing.T) {
		if _, err := parseMemory("x"); err == nil {
			t.Fatalf("expected short memory format error")
		}
	})
}

func TestOrchestrator_AdditionalCoverageBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("stop returns module stop error from shutdown loop", func(t *testing.T) {
		o := &Orchestrator{
			modules: map[string]*moduleInstance{
				"mod": {moduleID: "mod", state: StateFailed},
			},
			dockerCloseFn: func() error { return nil },
		}

		err := o.Stop(ctx)
		if !errors.Is(err, ErrModuleNotRunning) {
			t.Fatalf("expected ErrModuleNotRunning, got %v", err)
		}
	})

	t.Run("stop returns close docker error", func(t *testing.T) {
		o := &Orchestrator{
			modules:       map[string]*moduleInstance{},
			dockerCloseFn: func() error { return errors.New("close failed") },
		}

		err := o.Stop(ctx)
		if err == nil || err.Error() != "close failed" {
			t.Fatalf("expected close failed error, got %v", err)
		}
	})

	t.Run("restart returns wrapped stop error", func(t *testing.T) {
		o := &Orchestrator{
			modules: map[string]*moduleInstance{
				"mod": {moduleID: "mod", state: StateRunning, containerID: "cid"},
			},
			stopContainerFn: func(context.Context, string, time.Duration) error {
				return errors.New("stop failed")
			},
		}

		err := o.RestartModule(ctx, "mod")
		if err == nil || !strings.Contains(err.Error(), "stopping module") {
			t.Fatalf("expected wrapped stopping module error, got %v", err)
		}
	})

	t.Run("restart succeeds when module is not running", func(t *testing.T) {
		o := &Orchestrator{
			modules: map[string]*moduleInstance{
				"mod": {moduleID: "mod", state: StateFailed},
			},
			loadModuleCfgFn: func(moduleID string) (*ModuleConfig, error) {
				return &ModuleConfig{
					ID:     moduleID,
					Name:   "Module",
					Image:  "repo/module",
					Env:    map[string]string{},
					Labels: map[string]string{"aegion.module": "true"},
				}, nil
			},
			createContainerFn: func(context.Context, *ModuleConfig, string) (string, error) {
				return "cid-123456789012", nil
			},
			startContainerFn: func(context.Context, string) error { return nil },
		}

		if err := o.RestartModule(ctx, "mod"); err != nil {
			t.Fatalf("RestartModule returned error: %v", err)
		}
		if !o.IsRunning("mod") {
			t.Fatalf("expected module to be running after restart")
		}
	})
}
