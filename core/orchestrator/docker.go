package orchestrator

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultNetworkName is the default Docker network for modules
	DefaultNetworkName = "aegion_modules"
	// ContainerPrefix is prepended to module IDs for container names
	ContainerPrefix = "aegion_"
	// DefaultStopTimeout is the default graceful shutdown timeout
	DefaultStopTimeout = 30 * time.Second
)

var newDockerEngineClient = func() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
}

// DockerClient wraps the Docker client for container operations.
type DockerClient struct {
	cli         *client.Client
	networkName string

	closeFn            func() error
	containerCreateFn  func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	containerStartFn   func(ctx context.Context, containerID string, options container.StartOptions) error
	containerStopFn    func(ctx context.Context, containerID string, options container.StopOptions) error
	containerRemoveFn  func(ctx context.Context, containerID string, options container.RemoveOptions) error
	containerLogsFn    func(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	containerInspectFn func(ctx context.Context, containerID string) (container.InspectResponse, error)
	containerListFn    func(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	imageInspectFn     func(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (image.InspectResponse, error)
	imagePullFn        func(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
}

// ContainerInfo holds container state information.
type ContainerInfo struct {
	ID           string
	Name         string
	State        string
	Status       string
	Health       string
	IPAddress    string
	Ports        []string
	Created      time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
	ExitCode     int
	Error        string
	RestartCount int
}

// NewDockerClient creates a new Docker client wrapper.
func NewDockerClient() (*DockerClient, error) {
	cli, err := newDockerEngineClient()
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	if cli == nil {
		return nil, fmt.Errorf("creating docker client: nil docker client")
	}

	d := &DockerClient{
		cli:         cli,
		networkName: DefaultNetworkName,
	}
	d.closeFn = cli.Close
	d.containerCreateFn = cli.ContainerCreate
	d.containerStartFn = cli.ContainerStart
	d.containerStopFn = cli.ContainerStop
	d.containerRemoveFn = cli.ContainerRemove
	d.containerLogsFn = cli.ContainerLogs
	d.containerInspectFn = cli.ContainerInspect
	d.containerListFn = cli.ContainerList
	d.imageInspectFn = cli.ImageInspect
	d.imagePullFn = cli.ImagePull
	return d, nil
}

// Close closes the Docker client.
func (d *DockerClient) Close() error {
	if d.closeFn != nil {
		return d.closeFn()
	}
	if d.cli == nil {
		return fmt.Errorf("docker client is nil")
	}
	return d.cli.Close()
}

// SetNetworkName sets the network name for container operations.
func (d *DockerClient) SetNetworkName(name string) {
	d.networkName = name
}

// CreateContainer creates a new container from the module configuration.
func (d *DockerClient) CreateContainer(ctx context.Context, cfg *ModuleConfig, authToken string) (string, error) {
	containerName := ContainerPrefix + cfg.ID

	// Check if container already exists
	existing, err := d.findContainer(ctx, containerName)
	if err == nil && existing != nil {
		log.Info().
			Str("module_id", cfg.ID).
			Str("container_id", shortID(existing.ID)).
			Msg("container already exists, removing")
		if err := d.RemoveContainer(ctx, existing.ID, true); err != nil {
			return "", fmt.Errorf("removing existing container: %w", err)
		}
	}

	// Prepare environment variables
	env := make([]string, 0, len(cfg.Env)+2)
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	env = append(env, fmt.Sprintf("AEGION_AUTH_TOKEN=%s", authToken))
	env = append(env, fmt.Sprintf("AEGION_MODULE_ID=%s", cfg.ID))

	// Prepare port bindings
	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}
	for _, p := range cfg.Ports {
		protocol := p.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		containerPort := nat.Port(fmt.Sprintf("%s/%s", p.ContainerPort, protocol))
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: p.HostPort},
		}
	}

	// Prepare mounts
	mounts := make([]mount.Mount, 0, len(cfg.Volumes))
	for _, v := range cfg.Volumes {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   v.HostPath,
			Target:   v.ContainerPath,
			ReadOnly: v.ReadOnly,
		})
	}

	// Prepare health check
	healthCheck := d.buildHealthCheck(cfg)

	// Prepare resource constraints
	resources := d.buildResources(cfg)

	// Determine restart policy
	restartPolicy := container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
	switch cfg.RestartPolicy {
	case "always":
		restartPolicy.Name = container.RestartPolicyAlways
	case "on-failure":
		restartPolicy.Name = container.RestartPolicyOnFailure
		restartPolicy.MaximumRetryCount = 5
	case "no":
		restartPolicy.Name = container.RestartPolicyDisabled
	}

	// Build image reference
	imageRef := cfg.Image
	if cfg.Version != "" && cfg.Version != "latest" {
		if !strings.Contains(cfg.Image, ":") {
			imageRef = fmt.Sprintf("%s:%s", cfg.Image, cfg.Version)
		}
	}

	// Pull image if needed
	if err := d.pullImageIfNeeded(ctx, imageRef); err != nil {
		return "", fmt.Errorf("pulling image: %w", err)
	}

	// Create container
	containerCfg := &container.Config{
		Image:        imageRef,
		Env:          env,
		ExposedPorts: exposedPorts,
		Labels:       cfg.Labels,
		Healthcheck:  healthCheck,
	}

	hostCfg := &container.HostConfig{
		PortBindings:  portBindings,
		Mounts:        mounts,
		RestartPolicy: restartPolicy,
		Resources:     resources,
		NetworkMode:   container.NetworkMode(d.networkName),
	}

	networkCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			d.networkName: {
				Aliases: []string{cfg.ID},
			},
		},
	}

	var resp container.CreateResponse
	if d.containerCreateFn != nil {
		resp, err = d.containerCreateFn(ctx, containerCfg, hostCfg, networkCfg, nil, containerName)
	} else if d.cli != nil {
		resp, err = d.cli.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, containerName)
	} else {
		return "", fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}

	for _, warning := range resp.Warnings {
		log.Warn().Str("module_id", cfg.ID).Str("warning", warning).Msg("container creation warning")
	}

	log.Info().
		Str("module_id", cfg.ID).
		Str("container_id", shortID(resp.ID)).
		Str("container_name", containerName).
		Msg("container created")

	return resp.ID, nil
}

// buildHealthCheck creates Docker health check configuration.
func (d *DockerClient) buildHealthCheck(cfg *ModuleConfig) *container.HealthConfig {
	if cfg.HealthCheck.Endpoint == "" {
		return nil
	}

	return &container.HealthConfig{
		Test:        []string{"CMD-SHELL", fmt.Sprintf("wget -q --spider http://localhost%s || exit 1", cfg.HealthCheck.Endpoint)},
		Interval:    cfg.HealthCheck.Interval,
		Timeout:     cfg.HealthCheck.Timeout,
		Retries:     cfg.HealthCheck.Retries,
		StartPeriod: cfg.HealthCheck.StartPeriod,
	}
}

// buildResources creates container resource constraints.
func (d *DockerClient) buildResources(cfg *ModuleConfig) container.Resources {
	resources := container.Resources{}

	if cfg.Resources.MemoryLimit != "" {
		if mem, err := parseMemory(cfg.Resources.MemoryLimit); err == nil {
			resources.Memory = mem
		}
	}
	if cfg.Resources.MemoryReservation != "" {
		if mem, err := parseMemory(cfg.Resources.MemoryReservation); err == nil {
			resources.MemoryReservation = mem
		}
	}
	if cfg.Resources.CPULimit != "" {
		if cpu, err := parseCPU(cfg.Resources.CPULimit); err == nil {
			resources.NanoCPUs = cpu
		}
	}

	return resources
}

// StartContainer starts an existing container.
func (d *DockerClient) StartContainer(ctx context.Context, containerID string) error {
	var err error
	if d.containerStartFn != nil {
		err = d.containerStartFn(ctx, containerID, container.StartOptions{})
	} else if d.cli != nil {
		err = d.cli.ContainerStart(ctx, containerID, container.StartOptions{})
	} else {
		return fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	log.Info().Str("container_id", shortID(containerID)).Msg("container started")
	return nil
}

// StopContainer gracefully stops a running container.
func (d *DockerClient) StopContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultStopTimeout
	}

	timeoutSec := int(timeout.Seconds())
	options := container.StopOptions{Timeout: &timeoutSec}

	var err error
	if d.containerStopFn != nil {
		err = d.containerStopFn(ctx, containerID, options)
	} else if d.cli != nil {
		err = d.cli.ContainerStop(ctx, containerID, options)
	} else {
		return fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}

	log.Info().Str("container_id", shortID(containerID)).Msg("container stopped")
	return nil
}

// RemoveContainer removes a container.
func (d *DockerClient) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	options := container.RemoveOptions{
		RemoveVolumes: true,
		Force:         force,
	}

	var err error
	if d.containerRemoveFn != nil {
		err = d.containerRemoveFn(ctx, containerID, options)
	} else if d.cli != nil {
		err = d.cli.ContainerRemove(ctx, containerID, options)
	} else {
		return fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return fmt.Errorf("removing container: %w", err)
	}

	log.Info().Str("container_id", shortID(containerID)).Msg("container removed")
	return nil
}

// ContainerLogs retrieves container logs for debugging.
func (d *DockerClient) ContainerLogs(ctx context.Context, containerID string, tail int, since time.Time) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
	}

	if tail > 0 {
		options.Tail = strconv.Itoa(tail)
	}
	if !since.IsZero() {
		options.Since = since.Format(time.RFC3339)
	}

	var (
		reader io.ReadCloser
		err    error
	)
	if d.containerLogsFn != nil {
		reader, err = d.containerLogsFn(ctx, containerID, options)
	} else if d.cli != nil {
		reader, err = d.cli.ContainerLogs(ctx, containerID, options)
	} else {
		return "", fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return "", fmt.Errorf("getting container logs: %w", err)
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("reading container logs: %w", err)
	}

	return string(logs), nil
}

// GetContainerInfo retrieves detailed container information.
func (d *DockerClient) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	var (
		inspect container.InspectResponse
		err     error
	)
	if d.containerInspectFn != nil {
		inspect, err = d.containerInspectFn(ctx, containerID)
	} else if d.cli != nil {
		inspect, err = d.cli.ContainerInspect(ctx, containerID)
	} else {
		return nil, fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return nil, fmt.Errorf("inspecting container: %w", err)
	}

	info := &ContainerInfo{
		ID:           inspect.ID,
		Name:         strings.TrimPrefix(inspect.Name, "/"),
		State:        inspect.State.Status,
		Status:       inspect.State.Status,
		ExitCode:     inspect.State.ExitCode,
		Error:        inspect.State.Error,
		RestartCount: inspect.RestartCount,
	}

	// Parse timestamps
	if created, err := time.Parse(time.RFC3339Nano, inspect.Created); err == nil {
		info.Created = created
	}
	if started, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
		info.StartedAt = started
	}
	if finished, err := time.Parse(time.RFC3339Nano, inspect.State.FinishedAt); err == nil {
		info.FinishedAt = finished
	}

	// Get health status
	if inspect.State.Health != nil {
		info.Health = inspect.State.Health.Status
	}

	// Get IP address from network
	if networkSettings, ok := inspect.NetworkSettings.Networks[d.networkName]; ok {
		info.IPAddress = networkSettings.IPAddress
	}

	// Get port mappings
	for containerPort, bindings := range inspect.NetworkSettings.Ports {
		for _, binding := range bindings {
			info.Ports = append(info.Ports, fmt.Sprintf("%s:%s->%s", binding.HostIP, binding.HostPort, containerPort))
		}
	}

	return info, nil
}

// HealthCheck checks the health status of a container.
func (d *DockerClient) HealthCheck(ctx context.Context, containerID string) (string, error) {
	var (
		inspect container.InspectResponse
		err     error
	)
	if d.containerInspectFn != nil {
		inspect, err = d.containerInspectFn(ctx, containerID)
	} else if d.cli != nil {
		inspect, err = d.cli.ContainerInspect(ctx, containerID)
	} else {
		return "", fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}

	if inspect.State.Health == nil {
		// No health check configured, check if running
		if inspect.State.Running {
			return "running", nil
		}
		return inspect.State.Status, nil
	}

	return inspect.State.Health.Status, nil
}

// findContainer finds a container by name.
func (d *DockerClient) findContainer(ctx context.Context, name string) (*ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", name)

	var (
		containers []container.Summary
		err        error
	)
	if d.containerListFn != nil {
		containers, err = d.containerListFn(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
	} else if d.cli != nil {
		containers, err = d.cli.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
	} else {
		return nil, fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return nil, err
	}

	for _, c := range containers {
		for _, cName := range c.Names {
			if strings.TrimPrefix(cName, "/") == name {
				return &ContainerInfo{
					ID:    c.ID,
					Name:  name,
					State: c.State,
				}, nil
			}
		}
	}

	return nil, nil
}

// pullImageIfNeeded pulls an image if it doesn't exist locally.
func (d *DockerClient) pullImageIfNeeded(ctx context.Context, imageRef string) error {
	var err error
	if d.imageInspectFn != nil {
		_, err = d.imageInspectFn(ctx, imageRef)
	} else if d.cli != nil {
		_, err = d.cli.ImageInspect(ctx, imageRef)
	} else {
		return fmt.Errorf("docker client is nil")
	}
	if err == nil {
		return nil // Image exists
	}

	log.Info().Str("image", imageRef).Msg("pulling image")

	var reader io.ReadCloser
	if d.imagePullFn != nil {
		reader, err = d.imagePullFn(ctx, imageRef, image.PullOptions{})
	} else if d.cli != nil {
		reader, err = d.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	} else {
		return fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return err
	}
	defer reader.Close()

	// Consume output to ensure pull completes
	_, err = io.Copy(io.Discard, reader)
	return err
}

// ListContainers lists all aegion module containers.
func (d *DockerClient) ListContainers(ctx context.Context) ([]*ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "aegion.module=true")

	var (
		containers []container.Summary
		err        error
	)
	if d.containerListFn != nil {
		containers, err = d.containerListFn(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
	} else if d.cli != nil {
		containers, err = d.cli.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filterArgs,
		})
	} else {
		return nil, fmt.Errorf("docker client is nil")
	}
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	result := make([]*ContainerInfo, 0, len(containers))
	for _, c := range containers {
		info := &ContainerInfo{
			ID:      c.ID,
			State:   c.State,
			Status:  c.Status,
			Created: time.Unix(c.Created, 0),
		}
		if len(c.Names) > 0 {
			info.Name = strings.TrimPrefix(c.Names[0], "/")
		}
		result = append(result, info)
	}

	return result, nil
}

// parseMemory parses a memory string (e.g., "256m", "1g") to bytes.
func parseMemory(s string) (int64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid memory format: %s", s)
	}

	suffix := s[len(s)-1]
	value, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil {
		return 0, err
	}

	switch suffix {
	case 'k':
		return value * 1024, nil
	case 'm':
		return value * 1024 * 1024, nil
	case 'g':
		return value * 1024 * 1024 * 1024, nil
	default:
		return strconv.ParseInt(s, 10, 64)
	}
}

// parseCPU parses a CPU limit string (e.g., "0.5", "2") to nanoseconds.
func parseCPU(s string) (int64, error) {
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(value * 1e9), nil
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
