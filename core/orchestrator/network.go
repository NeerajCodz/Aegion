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

	networkCreateFn     func(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	networkInspectFn    func(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	networkConnectFn    func(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
	networkDisconnectFn func(ctx context.Context, networkID, containerID string, force bool) error
	networkRemoveFn     func(ctx context.Context, networkID string) error
	networkListFn       func(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
}

// NewNetworkManager creates a new network manager.
func NewNetworkManager(cli *client.Client, networkName, subnet string) *NetworkManager {
	if networkName == "" {
		networkName = DefaultNetworkName
	}
	n := &NetworkManager{
		cli:         cli,
		networkName: networkName,
		subnet:      subnet,
	}
	if cli != nil {
		n.networkCreateFn = cli.NetworkCreate
		n.networkInspectFn = cli.NetworkInspect
		n.networkConnectFn = cli.NetworkConnect
		n.networkDisconnectFn = cli.NetworkDisconnect
		n.networkRemoveFn = cli.NetworkRemove
		n.networkListFn = cli.NetworkList
	}
	return n
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
			Str("id", shortID(networkID)).
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

	var resp network.CreateResponse
	if n.networkCreateFn != nil {
		resp, err = n.networkCreateFn(ctx, n.networkName, options)
	} else if n.cli != nil {
		resp, err = n.cli.NetworkCreate(ctx, n.networkName, options)
	} else {
		return "", fmt.Errorf("docker network client is nil")
	}
	if err != nil {
		return "", fmt.Errorf("creating network: %w", err)
	}

	log.Info().
		Str("network", n.networkName).
		Str("id", shortID(resp.ID)).
		Msg("network created")

	return resp.ID, nil
}

// ConnectToNetwork adds a container to the network.
func (n *NetworkManager) ConnectToNetwork(ctx context.Context, containerID string, aliases []string) error {
	// Check if already connected
	var (
		inspect network.Inspect
		err     error
	)
	if n.networkInspectFn != nil {
		inspect, err = n.networkInspectFn(ctx, n.networkName, network.InspectOptions{})
	} else if n.cli != nil {
		inspect, err = n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
	} else {
		return fmt.Errorf("docker network client is nil")
	}
	if err != nil {
		return fmt.Errorf("inspecting network: %w", err)
	}

	for id := range inspect.Containers {
		if id == containerID {
			log.Debug().
				Str("container_id", shortID(containerID)).
				Str("network", n.networkName).
				Msg("container already connected to network")
			return nil
		}
	}

	// Connect container
	config := &network.EndpointSettings{
		Aliases: aliases,
	}

	if n.networkConnectFn != nil {
		err = n.networkConnectFn(ctx, n.networkName, containerID, config)
	} else if n.cli != nil {
		err = n.cli.NetworkConnect(ctx, n.networkName, containerID, config)
	} else {
		return fmt.Errorf("docker network client is nil")
	}
	if err != nil {
		return fmt.Errorf("connecting to network: %w", err)
	}

	log.Info().
		Str("container_id", shortID(containerID)).
		Str("network", n.networkName).
		Msg("container connected to network")

	return nil
}

// DisconnectFromNetwork removes a container from the network.
func (n *NetworkManager) DisconnectFromNetwork(ctx context.Context, containerID string) error {
	var err error
	if n.networkDisconnectFn != nil {
		err = n.networkDisconnectFn(ctx, n.networkName, containerID, false)
	} else if n.cli != nil {
		err = n.cli.NetworkDisconnect(ctx, n.networkName, containerID, false)
	} else {
		return fmt.Errorf("docker network client is nil")
	}
	if err != nil {
		return fmt.Errorf("disconnecting from network: %w", err)
	}

	log.Info().
		Str("container_id", shortID(containerID)).
		Str("network", n.networkName).
		Msg("container disconnected from network")

	return nil
}

// RemoveNetwork removes the network (only if no containers are connected).
func (n *NetworkManager) RemoveNetwork(ctx context.Context) error {
	var err error
	if n.networkRemoveFn != nil {
		err = n.networkRemoveFn(ctx, n.networkName)
	} else if n.cli != nil {
		err = n.cli.NetworkRemove(ctx, n.networkName)
	} else {
		return fmt.Errorf("docker network client is nil")
	}
	if err != nil {
		return fmt.Errorf("removing network: %w", err)
	}

	log.Info().Str("network", n.networkName).Msg("network removed")
	return nil
}

// GetNetworkInfo returns information about the network.
func (n *NetworkManager) GetNetworkInfo(ctx context.Context) (*NetworkInfo, error) {
	var (
		inspect network.Inspect
		err     error
	)
	if n.networkInspectFn != nil {
		inspect, err = n.networkInspectFn(ctx, n.networkName, network.InspectOptions{})
	} else if n.cli != nil {
		inspect, err = n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
	} else {
		return nil, fmt.Errorf("docker network client is nil")
	}
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

	var (
		networks []network.Summary
		err      error
	)
	if n.networkListFn != nil {
		networks, err = n.networkListFn(ctx, network.ListOptions{
			Filters: filterArgs,
		})
	} else if n.cli != nil {
		networks, err = n.cli.NetworkList(ctx, network.ListOptions{
			Filters: filterArgs,
		})
	} else {
		return "", fmt.Errorf("docker network client is nil")
	}
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
	var (
		inspect network.Inspect
		err     error
	)
	if n.networkInspectFn != nil {
		inspect, err = n.networkInspectFn(ctx, n.networkName, network.InspectOptions{})
	} else if n.cli != nil {
		inspect, err = n.cli.NetworkInspect(ctx, n.networkName, network.InspectOptions{})
	} else {
		return "", fmt.Errorf("docker network client is nil")
	}
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
