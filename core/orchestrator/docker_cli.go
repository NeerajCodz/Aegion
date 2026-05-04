package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"log/slog"
)

const (
	DefaultNetworkName       = "aegion_modules"
	ContainerPrefix          = "aegion_"
	DefaultStopTimeout       = 30 * time.Second
	allowRemoteDockerHostEnv = "AEGION_ALLOW_REMOTE_DOCKER_HOST"
)

type DockerClient struct {
	bin         string
	networkName string
}

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

func NewDockerClient() (*DockerClient, error) {
	if err := validateDockerHostSafety(); err != nil {
		return nil, err
	}
	bin := strings.TrimSpace(os.Getenv("AEGION_DOCKER_BIN"))
	if bin == "" {
		bin = "docker"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("locating docker cli: %w", err)
	}
	return &DockerClient{bin: bin, networkName: DefaultNetworkName}, nil
}

func validateDockerHostSafety() error {
	host := strings.ToLower(strings.TrimSpace(os.Getenv("DOCKER_HOST")))
	if host == "" || strings.HasPrefix(host, "unix://") || strings.HasPrefix(host, "npipe://") || strings.HasPrefix(host, "ssh://") {
		return nil
	}
	if !strings.HasPrefix(host, "tcp://") {
		return fmt.Errorf("unsupported DOCKER_HOST scheme: %s", strings.TrimSpace(os.Getenv("DOCKER_HOST")))
	}
	if !parseBoolEnv(allowRemoteDockerHostEnv) {
		return fmt.Errorf("remote DOCKER_HOST over tcp requires %s=true", allowRemoteDockerHostEnv)
	}
	if !parseBoolEnv("DOCKER_TLS_VERIFY") {
		return errors.New("DOCKER_TLS_VERIFY must be enabled for remote DOCKER_HOST")
	}
	return nil
}

func parseBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (d *DockerClient) Close() error { return nil }

func (d *DockerClient) SetNetworkName(name string) {
	if strings.TrimSpace(name) != "" {
		d.networkName = strings.TrimSpace(name)
	}
}

func (d *DockerClient) CreateContainer(ctx context.Context, cfg *ModuleConfig, authToken string) (string, error) {
	containerName := ContainerPrefix + cfg.ID
	existing, err := d.findContainer(ctx, containerName)
	if err == nil && existing != nil {
		if err := d.RemoveContainer(ctx, existing.ID, true); err != nil {
			return "", fmt.Errorf("removing existing container: %w", err)
		}
	}

	imageRef := cfg.Image
	if cfg.Version != "" && cfg.Version != "latest" && !strings.Contains(cfg.Image, ":") {
		imageRef = fmt.Sprintf("%s:%s", cfg.Image, cfg.Version)
	}
	if err := d.pullImageIfNeeded(ctx, imageRef); err != nil {
		return "", fmt.Errorf("pulling image: %w", err)
	}

	args := []string{"create", "--name", containerName, "--network", d.networkName}
	for key, value := range cfg.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	for _, alias := range []string{cfg.ID} {
		if strings.TrimSpace(alias) != "" {
			args = append(args, "--network-alias", alias)
		}
	}
	for key, value := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, "-e", "AEGION_AUTH_TOKEN")
	args = append(args, "-e", fmt.Sprintf("AEGION_MODULE_ID=%s", cfg.ID))

	for _, p := range cfg.Ports {
		protocol := strings.TrimSpace(p.Protocol)
		if protocol == "" {
			protocol = "tcp"
		}
		args = append(args, "-p", fmt.Sprintf("127.0.0.1:%s:%s/%s", p.HostPort, p.ContainerPort, protocol))
	}
	for _, v := range cfg.Volumes {
		mountSpec := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
		if v.ReadOnly {
			mountSpec += ":ro"
		}
		args = append(args, "-v", mountSpec)
	}
	if cfg.Resources.MemoryLimit != "" {
		args = append(args, "--memory", cfg.Resources.MemoryLimit)
	}
	if cfg.Resources.MemoryReservation != "" {
		args = append(args, "--memory-reservation", cfg.Resources.MemoryReservation)
	}
	if cfg.Resources.CPULimit != "" {
		args = append(args, "--cpus", cfg.Resources.CPULimit)
	}
	if cfg.RestartPolicy != "" {
		policy := cfg.RestartPolicy
		if policy == "on-failure" {
			policy = "on-failure:5"
		}
		args = append(args, "--restart", policy)
	}
	if cfg.HealthCheck.Endpoint != "" {
		args = append(args, "--health-cmd", fmt.Sprintf("wget -q --spider http://localhost%s || exit 1", cfg.HealthCheck.Endpoint))
		if cfg.HealthCheck.Interval > 0 {
			args = append(args, "--health-interval", cfg.HealthCheck.Interval.String())
		}
		if cfg.HealthCheck.Timeout > 0 {
			args = append(args, "--health-timeout", cfg.HealthCheck.Timeout.String())
		}
		if cfg.HealthCheck.Retries > 0 {
			args = append(args, "--health-retries", strconv.Itoa(cfg.HealthCheck.Retries))
		}
		if cfg.HealthCheck.StartPeriod > 0 {
			args = append(args, "--health-start-period", cfg.HealthCheck.StartPeriod.String())
		}
	}
	args = append(args, imageRef)

	stdout, err := d.runWithEnv(ctx, []string{fmt.Sprintf("AEGION_AUTH_TOKEN=%s", authToken)}, args...)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}
	id := strings.TrimSpace(stdout)
	slog.InfoContext(ctx, "container created", "module_id", cfg.ID, "container_id", shortID(id), "container_name", containerName)
	return id, nil
}

func (d *DockerClient) StartContainer(ctx context.Context, containerID string) error {
	if _, err := d.run(ctx, "start", containerID); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}
	return nil
}

func (d *DockerClient) StopContainer(ctx context.Context, containerID string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultStopTimeout
	}
	if _, err := d.run(ctx, "stop", "--time", strconv.Itoa(int(timeout.Seconds())), containerID); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}
	return nil
}

func (d *DockerClient) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	args := []string{"rm", "-v"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, containerID)
	if _, err := d.run(ctx, args...); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}
	return nil
}

func (d *DockerClient) ContainerLogs(ctx context.Context, containerID string, tail int, since time.Time) (string, error) {
	args := []string{"logs", "--timestamps"}
	if tail > 0 {
		args = append(args, "--tail", strconv.Itoa(tail))
	}
	if !since.IsZero() {
		args = append(args, "--since", since.Format(time.RFC3339))
	}
	args = append(args, containerID)
	stdout, err := d.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("getting container logs: %w", err)
	}
	return stdout, nil
}

func (d *DockerClient) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	inspect, err := d.inspectContainer(ctx, containerID)
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
	info.Created, _ = time.Parse(time.RFC3339Nano, inspect.Created)
	info.StartedAt, _ = time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
	info.FinishedAt, _ = time.Parse(time.RFC3339Nano, inspect.State.FinishedAt)
	if inspect.State.Health != nil {
		info.Health = inspect.State.Health.Status
	}
	if networkSettings, ok := inspect.NetworkSettings.Networks[d.networkName]; ok {
		info.IPAddress = networkSettings.IPAddress
	}
	for containerPort, bindings := range inspect.NetworkSettings.Ports {
		for _, binding := range bindings {
			info.Ports = append(info.Ports, fmt.Sprintf("%s:%s->%s", binding.HostIP, binding.HostPort, containerPort))
		}
	}
	return info, nil
}

func (d *DockerClient) HealthCheck(ctx context.Context, containerID string) (string, error) {
	inspect, err := d.inspectContainer(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}
	if inspect.State.Health == nil {
		if inspect.State.Running {
			return "running", nil
		}
		return inspect.State.Status, nil
	}
	return inspect.State.Health.Status, nil
}

func (d *DockerClient) findContainer(ctx context.Context, name string) (*ContainerInfo, error) {
	lines, err := d.runJSONLines(ctx, "ps", "-a", "--filter", "name="+name, "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		var item dockerPSItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if strings.TrimSpace(item.Names) == name {
			return &ContainerInfo{ID: item.ID, Name: item.Names, State: item.State, Status: item.Status}, nil
		}
	}
	return nil, nil
}

func (d *DockerClient) pullImageIfNeeded(ctx context.Context, imageRef string) error {
	if _, err := d.run(ctx, "image", "inspect", imageRef); err == nil {
		return nil
	}
	_, err := d.run(ctx, "pull", imageRef)
	return err
}

func (d *DockerClient) ListContainers(ctx context.Context) ([]*ContainerInfo, error) {
	lines, err := d.runJSONLines(ctx, "ps", "-a", "--filter", "label=aegion.module=true", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	result := make([]*ContainerInfo, 0, len(lines))
	for _, line := range lines {
		var item dockerPSItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		info := &ContainerInfo{ID: item.ID, Name: item.Names, State: item.State, Status: item.Status}
		result = append(result, info)
	}
	return result, nil
}

func (d *DockerClient) runJSONLines(ctx context.Context, args ...string) ([]string, error) {
	stdout, err := d.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

func (d *DockerClient) run(ctx context.Context, args ...string) (string, error) {
	return d.runWithEnv(ctx, nil, args...)
}

func (d *DockerClient) runWithEnv(ctx context.Context, extraEnv []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, d.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func (d *DockerClient) inspectContainer(ctx context.Context, containerID string) (*dockerInspectContainer, error) {
	stdout, err := d.run(ctx, "inspect", containerID)
	if err != nil {
		return nil, err
	}
	var items []dockerInspectContainer
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("container not found")
	}
	return &items[0], nil
}

type dockerPSItem struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	State  string `json:"State"`
	Status string `json:"Status"`
}

type dockerInspectContainer struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Created      string `json:"Created"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

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
