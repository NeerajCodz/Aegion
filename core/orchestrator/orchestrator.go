package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/core/authtoken"
	"github.com/aegion/aegion/core/registry"
	"github.com/rs/zerolog/log"
)

var (
	ErrModuleNotFound       = errors.New("module not found")
	ErrModuleAlreadyRunning = errors.New("module is already running")
	ErrModuleNotRunning     = errors.New("module is not running")
	ErrOrchestratorClosed   = errors.New("orchestrator is closed")
	ErrStartFailed          = errors.New("failed to start module")
	ErrStopFailed           = errors.New("failed to stop module")
)

// ModuleState represents the current state of a module.
type ModuleState string

const (
	StateUnknown  ModuleState = "unknown"
	StateStopped  ModuleState = "stopped"
	StateStarting ModuleState = "starting"
	StateRunning  ModuleState = "running"
	StateStopping ModuleState = "stopping"
	StateFailed   ModuleState = "failed"
)

// ModuleStatus contains status information for a module.
type ModuleStatus struct {
	ModuleID     string      `json:"module_id"`
	ContainerID  string      `json:"container_id,omitempty"`
	State        ModuleState `json:"state"`
	Health       string      `json:"health,omitempty"`
	IPAddress    string      `json:"ip_address,omitempty"`
	Ports        []string    `json:"ports,omitempty"`
	StartedAt    time.Time   `json:"started_at,omitempty"`
	RestartCount int         `json:"restart_count"`
	Error        string      `json:"error,omitempty"`
}

// Orchestrator manages module container lifecycle.
type Orchestrator struct {
	docker         *DockerClient
	network        *NetworkManager
	registry       *registry.Registry
	configLoader   *ConfigLoader
	tokenGenerator *authtoken.Generator

	modules map[string]*moduleInstance
	mu      sync.RWMutex
	closed  bool
}

// moduleInstance tracks a running module.
type moduleInstance struct {
	moduleID    string
	containerID string
	config      *ModuleConfig
	state       ModuleState
	startedAt   time.Time
	error       string
}

// Config holds orchestrator configuration.
type Config struct {
	ConfigPath  string
	Registry    *registry.Registry
	TokenSecret []byte
}

// New creates a new orchestrator.
func New(cfg Config) (*Orchestrator, error) {
	docker, err := NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	configLoader := NewConfigLoader(cfg.ConfigPath)
	mainCfg, err := configLoader.Load()
	if err != nil {
		docker.Close()
		return nil, fmt.Errorf("loading config: %w", err)
	}

	// Create network manager
	networkCfg := mainCfg.Server.InternalNetwork
	network := NewNetworkManager(docker.cli, networkCfg.Name, networkCfg.Subnet)

	// Create token generator
	secret := cfg.TokenSecret
	if len(secret) == 0 && len(mainCfg.Secrets.Internal) > 0 {
		secret = []byte(mainCfg.Secrets.Internal[0])
	}

	var tokenGen *authtoken.Generator
	if len(secret) > 0 {
		tokenGen, err = authtoken.NewGenerator(authtoken.GeneratorConfig{
			Secret: secret,
		})
		if err != nil {
			docker.Close()
			return nil, fmt.Errorf("creating token generator: %w", err)
		}
	}

	o := &Orchestrator{
		docker:         docker,
		network:        network,
		registry:       cfg.Registry,
		configLoader:   configLoader,
		tokenGenerator: tokenGen,
		modules:        make(map[string]*moduleInstance),
	}

	return o, nil
}

// Start initializes the orchestrator and ensures network exists.
func (o *Orchestrator) Start(ctx context.Context) error {
	// Ensure network exists
	_, err := o.network.EnsureNetwork(ctx)
	if err != nil {
		return fmt.Errorf("ensuring network: %w", err)
	}

	log.Info().Msg("orchestrator started")
	return nil
}

// Stop gracefully shuts down the orchestrator and all modules.
func (o *Orchestrator) Stop(ctx context.Context) error {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()

	// Stop all running modules
	var lastErr error
	for moduleID := range o.modules {
		if err := o.StopModule(ctx, moduleID); err != nil {
			log.Error().Err(err).Str("module_id", moduleID).Msg("failed to stop module during shutdown")
			lastErr = err
		}
	}

	if err := o.docker.Close(); err != nil {
		log.Error().Err(err).Msg("failed to close docker client")
		lastErr = err
	}

	log.Info().Msg("orchestrator stopped")
	return lastErr
}

// StartModule starts a module container.
func (o *Orchestrator) StartModule(ctx context.Context, moduleID string) error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return ErrOrchestratorClosed
	}

	// Check if already running
	if inst, exists := o.modules[moduleID]; exists {
		if inst.state == StateRunning || inst.state == StateStarting {
			o.mu.Unlock()
			return ErrModuleAlreadyRunning
		}
	}

	// Set starting state
	o.modules[moduleID] = &moduleInstance{
		moduleID: moduleID,
		state:    StateStarting,
	}
	o.mu.Unlock()

	log.Info().Str("module_id", moduleID).Msg("starting module")

	// Load module config
	cfg, err := o.configLoader.LoadModuleConfig(moduleID)
	if err != nil {
		o.setModuleError(moduleID, fmt.Errorf("loading config: %w", err))
		return fmt.Errorf("loading module config: %w", err)
	}

	// Generate auth token
	var authToken string
	if o.tokenGenerator != nil {
		authToken, err = o.tokenGenerator.Generate(moduleID)
		if err != nil {
			o.setModuleError(moduleID, fmt.Errorf("generating token: %w", err))
			return fmt.Errorf("generating auth token: %w", err)
		}
	}

	// Create container
	containerID, err := o.docker.CreateContainer(ctx, cfg, authToken)
	if err != nil {
		o.setModuleError(moduleID, fmt.Errorf("creating container: %w", err))
		return fmt.Errorf("creating container: %w", err)
	}

	// Start container
	if err := o.docker.StartContainer(ctx, containerID); err != nil {
		// Clean up container on failure
		_ = o.docker.RemoveContainer(ctx, containerID, true)
		o.setModuleError(moduleID, fmt.Errorf("starting container: %w", err))
		return fmt.Errorf("starting container: %w", err)
	}

	// Update instance
	o.mu.Lock()
	o.modules[moduleID] = &moduleInstance{
		moduleID:    moduleID,
		containerID: containerID,
		config:      cfg,
		state:       StateRunning,
		startedAt:   time.Now(),
	}
	o.mu.Unlock()

	log.Info().
		Str("module_id", moduleID).
		Str("container_id", containerID[:12]).
		Msg("module started")

	return nil
}

// StopModule gracefully stops a module container.
func (o *Orchestrator) StopModule(ctx context.Context, moduleID string) error {
	o.mu.Lock()
	inst, exists := o.modules[moduleID]
	if !exists {
		o.mu.Unlock()
		return ErrModuleNotFound
	}
	if inst.state != StateRunning && inst.state != StateStarting {
		o.mu.Unlock()
		return ErrModuleNotRunning
	}

	inst.state = StateStopping
	o.mu.Unlock()

	log.Info().Str("module_id", moduleID).Msg("stopping module")

	// Stop container
	if err := o.docker.StopContainer(ctx, inst.containerID, DefaultStopTimeout); err != nil {
		o.setModuleError(moduleID, fmt.Errorf("stopping container: %w", err))
		return fmt.Errorf("stopping container: %w", err)
	}

	// Remove container
	if err := o.docker.RemoveContainer(ctx, inst.containerID, false); err != nil {
		log.Warn().Err(err).Str("module_id", moduleID).Msg("failed to remove container")
	}

	// Deregister from registry
	if o.registry != nil {
		if _, err := o.registry.Deregister(moduleID); err != nil {
			log.Warn().Err(err).Str("module_id", moduleID).Msg("failed to deregister module")
		}
	}

	// Update state
	o.mu.Lock()
	delete(o.modules, moduleID)
	o.mu.Unlock()

	log.Info().Str("module_id", moduleID).Msg("module stopped")

	return nil
}

// RestartModule stops and starts a module.
func (o *Orchestrator) RestartModule(ctx context.Context, moduleID string) error {
	log.Info().Str("module_id", moduleID).Msg("restarting module")

	// Stop if running
	o.mu.RLock()
	_, exists := o.modules[moduleID]
	o.mu.RUnlock()

	if exists {
		if err := o.StopModule(ctx, moduleID); err != nil && !errors.Is(err, ErrModuleNotRunning) {
			return fmt.Errorf("stopping module: %w", err)
		}
	}

	// Start module
	if err := o.StartModule(ctx, moduleID); err != nil {
		return fmt.Errorf("starting module: %w", err)
	}

	return nil
}

// GetModuleStatus returns the current status of a module.
func (o *Orchestrator) GetModuleStatus(ctx context.Context, moduleID string) (*ModuleStatus, error) {
	o.mu.RLock()
	inst, exists := o.modules[moduleID]
	o.mu.RUnlock()

	status := &ModuleStatus{
		ModuleID: moduleID,
		State:    StateStopped,
	}

	if !exists {
		return status, nil
	}

	status.ContainerID = inst.containerID
	status.State = inst.state
	status.StartedAt = inst.startedAt
	status.Error = inst.error

	// Get container details if running
	if inst.containerID != "" && (inst.state == StateRunning || inst.state == StateStarting) {
		info, err := o.docker.GetContainerInfo(ctx, inst.containerID)
		if err != nil {
			log.Warn().Err(err).Str("module_id", moduleID).Msg("failed to get container info")
		} else {
			status.Health = info.Health
			status.IPAddress = info.IPAddress
			status.Ports = info.Ports
			status.RestartCount = info.RestartCount

			// Map container state to module state
			switch info.State {
			case "running":
				status.State = StateRunning
			case "exited", "dead":
				status.State = StateFailed
				if info.ExitCode != 0 {
					status.Error = fmt.Sprintf("exited with code %d", info.ExitCode)
				}
			case "paused", "restarting":
				status.State = StateStarting
			}
		}
	}

	return status, nil
}

// ListModules returns status for all managed modules.
func (o *Orchestrator) ListModules(ctx context.Context) ([]*ModuleStatus, error) {
	o.mu.RLock()
	moduleIDs := make([]string, 0, len(o.modules))
	for id := range o.modules {
		moduleIDs = append(moduleIDs, id)
	}
	o.mu.RUnlock()

	statuses := make([]*ModuleStatus, 0, len(moduleIDs))
	for _, id := range moduleIDs {
		status, err := o.GetModuleStatus(ctx, id)
		if err != nil {
			log.Warn().Err(err).Str("module_id", id).Msg("failed to get module status")
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// GetModuleLogs retrieves logs for a module.
func (o *Orchestrator) GetModuleLogs(ctx context.Context, moduleID string, tail int) (string, error) {
	o.mu.RLock()
	inst, exists := o.modules[moduleID]
	o.mu.RUnlock()

	if !exists || inst.containerID == "" {
		return "", ErrModuleNotFound
	}

	return o.docker.ContainerLogs(ctx, inst.containerID, tail, time.Time{})
}

// setModuleError updates the module state to failed with an error.
func (o *Orchestrator) setModuleError(moduleID string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if inst, exists := o.modules[moduleID]; exists {
		inst.state = StateFailed
		inst.error = err.Error()
	}
}

// IsRunning checks if a module is currently running.
func (o *Orchestrator) IsRunning(moduleID string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()

	inst, exists := o.modules[moduleID]
	return exists && inst.state == StateRunning
}

// GetNetwork returns the network manager.
func (o *Orchestrator) GetNetwork() *NetworkManager {
	return o.network
}

// GetDocker returns the Docker client.
func (o *Orchestrator) GetDocker() *DockerClient {
	return o.docker
}
