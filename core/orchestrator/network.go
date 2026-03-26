package orchestrator

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
)

// NetworkManager manages Docker networks for module communication.
type NetworkManager struct {
	cli         *client.Client
	networkName string
	subnet      string
}

// NewNetworkManager creates a new network manager.
func NewNetworkManager(cli *client.Client, networkName, subnet string) *NetworkManager {
	if networkName == "" {
		networkName = DefaultNetworkName
	}
	return &NetworkManager{
		cli:         cli,
		networkName: networkName,
		subnet:      subnet,
	}
}

// EnsureNetwork creates the aegion_modules network if it doesn't exist.
func (n *NetworkManager) EnsureNetwork(ctx context.Context) (string, error) {
	// Check if network already exists
	networkID, err := n.findNetwork(ctx)
	if err != nil {
		return "", err
	}
	if networkID != "" {
		log.Debug().
			Str("network", n.networkName).
			Str("id", networkID[:12]).
			Msg("network already exists")
		return networkID, nil
	}

	// Create network
	options := network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"aegion.network": "true",
		},
	}

	// Add IPAM config if subnet specified
	if n.subnet != "" {
		options.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{
				{Subnet: n.subnet},
			},
		}
	}

	resp, err := n.cli.NetworkCreate(ctx, n.networkName, options)
	if err != nil {
		return "", fmt.Errorf("creating network: %w", err)
	}

	log.Info().
		Str("network", n.networkName).
		Str("id", resp.ID[:12]).
		Msg("network created")

	return resp.ID, nil
}

// ConnectToNetwork adds a container to the network.
func (n *NetworkManager) ConnectToNetwork(ctx context.Context, containerID string, aliases []string) error {
	// Check if already connected
	inspect, err := n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
	if err != nil {
		return fmt.Errorf("inspecting network: %w", err)
	}

	for id := range inspect.Containers {
		if id == containerID {
			log.Debug().
				Str("container_id", containerID[:12]).
				Str("network", n.networkName).
				Msg("container already connected to network")
			return nil
		}
	}

	// Connect container
	config := &network.EndpointSettings{
		Aliases: aliases,
	}

	if err := n.cli.NetworkConnect(ctx, n.networkName, containerID, config); err != nil {
		return fmt.Errorf("connecting to network: %w", err)
	}

	log.Info().
		Str("container_id", containerID[:12]).
		Str("network", n.networkName).
		Msg("container connected to network")

	return nil
}

// DisconnectFromNetwork removes a container from the network.
func (n *NetworkManager) DisconnectFromNetwork(ctx context.Context, containerID string) error {
	if err := n.cli.NetworkDisconnect(ctx, n.networkName, containerID, false); err != nil {
		return fmt.Errorf("disconnecting from network: %w", err)
	}

	log.Info().
		Str("container_id", containerID[:12]).
		Str("network", n.networkName).
		Msg("container disconnected from network")

	return nil
}

// RemoveNetwork removes the network (only if no containers are connected).
func (n *NetworkManager) RemoveNetwork(ctx context.Context) error {
	if err := n.cli.NetworkRemove(ctx, n.networkName); err != nil {
		return fmt.Errorf("removing network: %w", err)
	}

	log.Info().Str("network", n.networkName).Msg("network removed")
	return nil
}

// GetNetworkInfo returns information about the network.
func (n *NetworkManager) GetNetworkInfo(ctx context.Context) (*NetworkInfo, error) {
	inspect, err := n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
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

	// Get IPAM config
	if len(inspect.IPAM.Config) > 0 {
		info.Subnet = inspect.IPAM.Config[0].Subnet
		info.Gateway = inspect.IPAM.Config[0].Gateway
	}

	// Get connected containers
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

// NetworkInfo holds network details.
type NetworkInfo struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Subnet     string
	Gateway    string
	Containers []NetworkContainer
}

// NetworkContainer represents a container connected to the network.
type NetworkContainer struct {
	ID         string
	Name       string
	IPv4       string
	IPv6       string
	MacAddress string
}

// findNetwork looks up the network by name.
func (n *NetworkManager) findNetwork(ctx context.Context) (string, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", n.networkName)

	networks, err := n.cli.NetworkList(ctx, network.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return "", fmt.Errorf("listing networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == n.networkName {
			return net.ID, nil
		}
	}

	return "", nil
}

// NetworkExists checks if the network exists.
func (n *NetworkManager) NetworkExists(ctx context.Context) (bool, error) {
	networkID, err := n.findNetwork(ctx)
	if err != nil {
		return false, err
	}
	return networkID != "", nil
}

// GetContainerIP returns the IP address of a container on the network.
func (n *NetworkManager) GetContainerIP(ctx context.Context, containerID string) (string, error) {
	inspect, err := n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspecting network: %w", err)
	}

	for id, container := range inspect.Containers {
		if id == containerID {
			return container.IPv4Address, nil
		}
	}

	return "", fmt.Errorf("container not found in network")
}
