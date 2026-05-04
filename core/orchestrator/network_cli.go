package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"log/slog"
)

type NetworkManager struct {
	docker      *DockerClient
	networkName string
	subnet      string
}

func NewNetworkManager(docker *DockerClient, networkName, subnet string) *NetworkManager {
	if networkName == "" {
		networkName = DefaultNetworkName
	}
	return &NetworkManager{docker: docker, networkName: networkName, subnet: subnet}
}

func (n *NetworkManager) EnsureNetwork(ctx context.Context) (string, error) {
	networkID, err := n.findNetwork(ctx)
	if err != nil {
		return "", err
	}
	if networkID != "" {
		return networkID, nil
	}
	args := []string{"network", "create", "--driver", "bridge", "--label", "aegion.network=true"}
	if strings.TrimSpace(n.subnet) != "" {
		args = append(args, "--subnet", n.subnet)
	}
	args = append(args, n.networkName)
	stdout, err := n.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("creating network: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

func (n *NetworkManager) ConnectToNetwork(ctx context.Context, containerID string, aliases []string) error {
	inspect, err := n.inspect(ctx)
	if err != nil {
		return fmt.Errorf("inspecting network: %w", err)
	}
	for id := range inspect.Containers {
		if id == containerID {
			return nil
		}
	}
	args := []string{"network", "connect"}
	for _, alias := range aliases {
		if strings.TrimSpace(alias) != "" {
			args = append(args, "--alias", alias)
		}
	}
	args = append(args, n.networkName, containerID)
	if _, err := n.run(ctx, args...); err != nil {
		return fmt.Errorf("connecting to network: %w", err)
	}
	return nil
}

func (n *NetworkManager) DisconnectFromNetwork(ctx context.Context, containerID string) error {
	if _, err := n.run(ctx, "network", "disconnect", n.networkName, containerID); err != nil {
		return fmt.Errorf("disconnecting from network: %w", err)
	}
	return nil
}

func (n *NetworkManager) RemoveNetwork(ctx context.Context) error {
	if _, err := n.run(ctx, "network", "rm", n.networkName); err != nil {
		return fmt.Errorf("removing network: %w", err)
	}
	return nil
}

func (n *NetworkManager) GetNetworkInfo(ctx context.Context) (*NetworkInfo, error) {
	inspect, err := n.inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspecting network: %w", err)
	}
	info := &NetworkInfo{
		ID:         inspect.ID,
		Name:       inspect.Name,
		Driver:     inspect.Driver,
		Scope:      inspect.Scope,
		Containers: make([]NetworkContainer, 0, len(inspect.Containers)),
	}
	if len(inspect.IPAM.Config) > 0 {
		info.Subnet = inspect.IPAM.Config[0].Subnet
		info.Gateway = inspect.IPAM.Config[0].Gateway
	}
	for id, container := range inspect.Containers {
		info.Containers = append(info.Containers, NetworkContainer{
			ID:         id,
			Name:       container.Name,
			IPv4:       container.IPv4Address,
			IPv6:       container.IPv6Address,
			MacAddress: container.MacAddress,
		})
	}
	return info, nil
}

type NetworkInfo struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Subnet     string
	Gateway    string
	Containers []NetworkContainer
}

type NetworkContainer struct {
	ID         string
	Name       string
	IPv4       string
	IPv6       string
	MacAddress string
}

func (n *NetworkManager) findNetwork(ctx context.Context) (string, error) {
	lines, err := n.runJSONLines(ctx, "network", "ls", "--filter", "name="+n.networkName, "--format", "{{json .}}")
	if err != nil {
		return "", fmt.Errorf("listing networks: %w", err)
	}
	for _, line := range lines {
		var item struct {
			ID   string `json:"ID"`
			Name string `json:"Name"`
		}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item.Name == n.networkName {
			return item.ID, nil
		}
	}
	return "", nil
}

func (n *NetworkManager) NetworkExists(ctx context.Context) (bool, error) {
	id, err := n.findNetwork(ctx)
	return id != "", err
}

func (n *NetworkManager) GetContainerIP(ctx context.Context, containerID string) (string, error) {
	inspect, err := n.inspect(ctx)
	if err != nil {
		return "", fmt.Errorf("inspecting network: %w", err)
	}
	for id, container := range inspect.Containers {
		if id == containerID {
			return container.IPv4Address, nil
		}
	}
	return "", errors.New("container not found on network")
}

func (n *NetworkManager) inspect(ctx context.Context) (*dockerNetworkInspect, error) {
	stdout, err := n.run(ctx, "network", "inspect", n.networkName)
	if err != nil {
		return nil, err
	}
	var items []dockerNetworkInspect
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("network not found")
	}
	return &items[0], nil
}

func (n *NetworkManager) runJSONLines(ctx context.Context, args ...string) ([]string, error) {
	stdout, err := n.run(ctx, args...)
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

func (n *NetworkManager) run(ctx context.Context, args ...string) (string, error) {
	if n.docker == nil {
		return "", errors.New("docker network client is nil")
	}
	return n.docker.run(ctx, args...)
}

type dockerNetworkInspect struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Scope      string `json:"Scope"`
	Containers map[string]struct {
		Name        string `json:"Name"`
		IPv4Address string `json:"IPv4Address"`
		IPv6Address string `json:"IPv6Address"`
		MacAddress  string `json:"MacAddress"`
	} `json:"Containers"`
	IPAM struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
}

func init() {
	slog.Debug("using docker CLI orchestrator backend")
}
