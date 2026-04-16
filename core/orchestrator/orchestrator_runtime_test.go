package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func newSeamedOrchestrator() *Orchestrator {
	return &Orchestrator{
		modules: make(map[string]*moduleInstance),
		ensureNetworkFn: func(context.Context) (string, error) {
			return "network-1", nil
		},
		dockerCloseFn: func() error { return nil },
		loadModuleCfgFn: func(moduleID string) (*ModuleConfig, error) {
			return &ModuleConfig{ID: moduleID, Name: "Module " + moduleID, Image: "repo/" + moduleID}, nil
		},
		generateTokenFn: func(string) (string, error) { return "auth-token", nil },
		createContainerFn: func(context.Context, *ModuleConfig, string) (string, error) {
			return "container-1234567890", nil
		},
		startContainerFn: func(context.Context, string) error { return nil },
		stopContainerFn:  func(context.Context, string, time.Duration) error { return nil },
		removeContainerFn: func(context.Context, string, bool) error {
			return nil
		},
		getContainerInfoFn: func(context.Context, string) (*ContainerInfo, error) {
			return &ContainerInfo{
				ID:           "container-1234567890",
				State:        "running",
				Health:       "healthy",
				IPAddress:    "10.10.0.2",
				Ports:        []string{"8080/tcp"},
				RestartCount: 2,
			}, nil
		},
		containerLogsFn: func(context.Context, string, int, time.Time) (string, error) {
			return "module logs", nil
		},
	}
}

func TestOrchestratorStartAndStopPaths(t *testing.T) {
	ctx := context.Background()
	o := newSeamedOrchestrator()

	if err := o.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	o.ensureNetworkFn = func(context.Context) (string, error) {
		return "", errors.New("network failed")
	}
	if err := o.Start(ctx); err == nil || err.Error() == "network failed" {
		t.Fatalf("Start(network error) = %v, want wrapped error", err)
	}

	o = newSeamedOrchestrator()
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	o.stopContainerFn = func(context.Context, string, time.Duration) error { return errors.New("stop failed") }
	if err := o.Stop(ctx); err == nil {
		t.Fatalf("Stop(stopContainer error) expected error")
	}

	o = newSeamedOrchestrator()
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	o.dockerCloseFn = func() error { return errors.New("close failed") }
	if err := o.Stop(ctx); err == nil || err.Error() != "close failed" {
		t.Fatalf("Stop(close error) = %v, want close failed", err)
	}
	if !o.closed {
		t.Fatalf("Stop() should mark orchestrator closed")
	}
}

func TestOrchestratorStartModuleBranches(t *testing.T) {
	ctx := context.Background()
	o := newSeamedOrchestrator()

	o.closed = true
	if err := o.StartModule(ctx, "password"); !errors.Is(err, ErrOrchestratorClosed) {
		t.Fatalf("StartModule(closed) = %v, want %v", err, ErrOrchestratorClosed)
	}
	o.closed = false

	o.modules["password"] = &moduleInstance{moduleID: "password", state: StateRunning}
	if err := o.StartModule(ctx, "password"); !errors.Is(err, ErrModuleAlreadyRunning) {
		t.Fatalf("StartModule(already running) = %v, want %v", err, ErrModuleAlreadyRunning)
	}
	o.modules = map[string]*moduleInstance{}

	o.loadModuleCfgFn = func(string) (*ModuleConfig, error) { return nil, errors.New("cfg failed") }
	if err := o.StartModule(ctx, "password"); err == nil || !strings.Contains(err.Error(), "cfg failed") {
		t.Fatalf("StartModule(load config error) = %v", err)
	}

	o = newSeamedOrchestrator()
	o.generateTokenFn = func(string) (string, error) { return "", errors.New("token failed") }
	if err := o.StartModule(ctx, "password"); err == nil {
		t.Fatalf("StartModule(token error) expected error")
	}

	o = newSeamedOrchestrator()
	o.createContainerFn = func(context.Context, *ModuleConfig, string) (string, error) { return "", errors.New("create failed") }
	if err := o.StartModule(ctx, "password"); err == nil {
		t.Fatalf("StartModule(create container error) expected error")
	}

	o = newSeamedOrchestrator()
	removed := false
	o.startContainerFn = func(context.Context, string) error { return errors.New("start failed") }
	o.removeContainerFn = func(context.Context, string, bool) error {
		removed = true
		return nil
	}
	if err := o.StartModule(ctx, "password"); err == nil {
		t.Fatalf("StartModule(start container error) expected error")
	}
	if !removed {
		t.Fatalf("StartModule(start failure) should remove failed container")
	}

	o = newSeamedOrchestrator()
	if err := o.StartModule(ctx, "password"); err != nil {
		t.Fatalf("StartModule(success) error = %v", err)
	}
	inst := o.modules["password"]
	if inst == nil || inst.state != StateRunning || inst.containerID == "" || inst.startedAt.IsZero() {
		t.Fatalf("StartModule(success) unexpected instance: %#v", inst)
	}
}

func TestOrchestratorStopRestartStatusAndLogs(t *testing.T) {
	ctx := context.Background()
	o := newSeamedOrchestrator()

	if err := o.StopModule(ctx, "missing"); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("StopModule(missing) = %v, want %v", err, ErrModuleNotFound)
	}

	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateFailed}
	if err := o.StopModule(ctx, "password"); !errors.Is(err, ErrModuleNotRunning) {
		t.Fatalf("StopModule(not running) = %v, want %v", err, ErrModuleNotRunning)
	}

	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	o.stopContainerFn = func(context.Context, string, time.Duration) error { return errors.New("stop failed") }
	if err := o.StopModule(ctx, "password"); err == nil {
		t.Fatalf("StopModule(stop error) expected error")
	}

	o = newSeamedOrchestrator()
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	o.removeContainerFn = func(context.Context, string, bool) error { return errors.New("remove failed") }
	if err := o.StopModule(ctx, "password"); err != nil {
		t.Fatalf("StopModule(remove warning path) error = %v", err)
	}
	if _, exists := o.modules["password"]; exists {
		t.Fatalf("StopModule(success) should delete module instance")
	}

	o = newSeamedOrchestrator()
	if err := o.RestartModule(ctx, "password"); err != nil {
		t.Fatalf("RestartModule(not existing) error = %v", err)
	}
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	o.stopContainerFn = func(context.Context, string, time.Duration) error { return errors.New("stop failed") }
	if err := o.RestartModule(ctx, "password"); err == nil {
		t.Fatalf("RestartModule(stop failure) expected error")
	}

	o = newSeamedOrchestrator()
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateFailed}
	if err := o.RestartModule(ctx, "password"); err != nil {
		t.Fatalf("RestartModule(not-running stop path) error = %v", err)
	}

	o = newSeamedOrchestrator()
	o.modules["password"] = &moduleInstance{moduleID: "password", containerID: "c1", state: StateRunning}
	status, err := o.GetModuleStatus(ctx, "password")
	if err != nil || status.State != StateRunning || status.Health != "healthy" {
		t.Fatalf("GetModuleStatus(running) status=%#v err=%v", status, err)
	}
	o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
		return &ContainerInfo{State: "exited", ExitCode: 2}, nil
	}
	status, err = o.GetModuleStatus(ctx, "password")
	if err != nil || status.State != StateFailed || !strings.Contains(status.Error, "code 2") {
		t.Fatalf("GetModuleStatus(exited) status=%#v err=%v", status, err)
	}
	o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
		return &ContainerInfo{State: "paused"}, nil
	}
	status, err = o.GetModuleStatus(ctx, "password")
	if err != nil || status.State != StateStarting {
		t.Fatalf("GetModuleStatus(paused) status=%#v err=%v", status, err)
	}
	o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
		return nil, errors.New("inspect failed")
	}
	status, err = o.GetModuleStatus(ctx, "password")
	if err != nil || status.State != StateRunning {
		t.Fatalf("GetModuleStatus(info error) status=%#v err=%v", status, err)
	}

	o.modules["oauth2"] = &moduleInstance{moduleID: "oauth2", containerID: "c2", state: StateRunning}
	list, err := o.ListModules(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListModules() len=%d err=%v", len(list), err)
	}
	if _, err := o.GetModuleLogs(ctx, "missing", 100); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("GetModuleLogs(missing) = %v, want %v", err, ErrModuleNotFound)
	}
	logs, err := o.GetModuleLogs(ctx, "oauth2", 50)
	if err != nil || logs != "module logs" {
		t.Fatalf("GetModuleLogs(success) logs=%q err=%v", logs, err)
	}

	o.setModuleError("oauth2", errors.New("boom"))
	if !o.IsRunning("password") || o.IsRunning("oauth2") {
		t.Fatalf("IsRunning() unexpected values: password=%v oauth2=%v", o.IsRunning("password"), o.IsRunning("oauth2"))
	}
	if o.GetNetwork() != nil || o.GetDocker() != nil {
		t.Fatalf("GetNetwork/GetDocker expected nil for seamed orchestrator")
	}
}

func TestOrchestratorInternalWrapperFallbacks(t *testing.T) {
	ctx := context.Background()
	o := &Orchestrator{modules: make(map[string]*moduleInstance)}

	if _, err := o.ensureNetwork(ctx); err == nil {
		t.Fatalf("ensureNetwork(nil manager) expected error")
	}
	if err := o.closeDocker(); err == nil {
		t.Fatalf("closeDocker(nil client) expected error")
	}
	if _, err := o.loadModuleConfig("password"); err == nil {
		t.Fatalf("loadModuleConfig(nil loader) expected error")
	}
	if token, err := o.generateToken("password"); err != nil || token != "" {
		t.Fatalf("generateToken(nil generator) token=%q err=%v, want empty,nil", token, err)
	}
	if _, err := o.createContainer(ctx, &ModuleConfig{ID: "password"}, ""); err == nil {
		t.Fatalf("createContainer(nil docker) expected error")
	}
	if err := o.startContainer(ctx, "c1"); err == nil {
		t.Fatalf("startContainer(nil docker) expected error")
	}
	if err := o.stopContainer(ctx, "c1", time.Second); err == nil {
		t.Fatalf("stopContainer(nil docker) expected error")
	}
	if err := o.removeContainer(ctx, "c1", false); err == nil {
		t.Fatalf("removeContainer(nil docker) expected error")
	}
	if _, err := o.getContainerInfo(ctx, "c1"); err == nil {
		t.Fatalf("getContainerInfo(nil docker) expected error")
	}
	if _, err := o.containerLogs(ctx, "c1", 10, time.Time{}); err == nil {
		t.Fatalf("containerLogs(nil docker) expected error")
	}
}
