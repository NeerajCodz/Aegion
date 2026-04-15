package orchestrator

import (
	"errors"
	"testing"
)

func TestModuleStateStrings(t *testing.T) {
	tests := []struct {
		state ModuleState
		want  string
	}{
		{StateUnknown, "unknown"},
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateFailed, "failed"},
	}
	for _, tt := range tests {
		if string(tt.state) != tt.want {
			t.Fatalf("state %q != %q", string(tt.state), tt.want)
		}
	}
}

func TestPublicOrchestratorErrorsRemainStable(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrModuleNotFound, "module not found"},
		{ErrModuleAlreadyRunning, "module is already running"},
		{ErrModuleNotRunning, "module is not running"},
		{ErrOrchestratorClosed, "orchestrator is closed"},
		{ErrStartFailed, "failed to start module"},
		{ErrStopFailed, "failed to stop module"},
	}
	for _, tt := range tests {
		if !errors.Is(tt.err, tt.err) || tt.err.Error() != tt.want {
			t.Fatalf("unexpected error text for %v: %q", tt.err, tt.err.Error())
		}
	}
}
