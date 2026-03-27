package workers

import (
	"testing"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
)

func TestWorkerInterface(t *testing.T) {
	// Test that our expected interface matches what we need
	// We can't directly check if BaseWorker implements Worker without the Start method
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Test interface methods that BaseWorker does implement
	if worker.Name() == "" {
		t.Error("Name() returned empty string")
	}
	
	// Stop method exists
	worker.Stop()
}

func TestNewManager(t *testing.T) {
	tests := []struct {
		name   string
		config ManagerConfig
	}{
		{
			name: "with nil logger gets default",
			config: ManagerConfig{
				Log: nil,
			},
		},
		{
			name: "with custom logger",
			config: ManagerConfig{
				Log: logger.New(logger.Config{Level: "debug", Format: "json"}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(tt.config)
			
			if manager == nil {
				t.Fatal("NewManager() returned nil")
			}
			
			if manager.log == nil {
				t.Error("Manager logger is nil")
			}
			
			if manager.workers == nil {
				t.Error("Workers slice is nil")
			}
			
			if len(manager.workers) != 0 {
				t.Errorf("Workers slice length = %d, want 0", len(manager.workers))
			}
		})
	}
}

func TestNewBaseWorker(t *testing.T) {
	name := "test-worker"
	interval := 5 * time.Second
	
	tests := []struct {
		name   string
		logger *logger.Logger
	}{
		{
			name:   "with nil logger gets default",
			logger: nil,
		},
		{
			name:   "with custom logger",
			logger: logger.New(logger.Config{Level: "debug", Format: "json"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := NewBaseWorker(name, nil, tt.logger, interval)
			
			if worker == nil {
				t.Fatal("NewBaseWorker() returned nil")
			}
			
			if worker.name != name {
				t.Errorf("Name = %s, want %s", worker.name, name)
			}
			
			if worker.interval != interval {
				t.Errorf("Interval = %v, want %v", worker.interval, interval)
			}
			
			if worker.log == nil {
				t.Error("Logger is nil")
			}
			
			if worker.done == nil {
				t.Error("Done channel is nil")
			}
			
			if worker.running {
				t.Error("Worker should not be running initially")
			}
		})
	}
}

func TestBaseWorkerName(t *testing.T) {
	name := "test-worker"
	worker := NewBaseWorker(name, nil, nil, time.Second)
	
	if worker.Name() != name {
		t.Errorf("Name() = %s, want %s", worker.Name(), name)
	}
}

func TestBaseWorkerDB(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// DB should return the same reference passed to constructor
	if worker.DB() != nil {
		t.Error("DB() should return nil when nil was passed")
	}
}

func TestBaseWorkerLog(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	if worker.Log() == nil {
		t.Error("Log() returned nil")
	}
}

func TestBaseWorkerInterval(t *testing.T) {
	interval := 10 * time.Second
	worker := NewBaseWorker("test", nil, nil, interval)
	
	if worker.Interval() != interval {
		t.Errorf("Interval() = %v, want %v", worker.Interval(), interval)
	}
}

func TestBaseWorkerDone(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	done := worker.Done()
	if done == nil {
		t.Error("Done() returned nil channel")
	}
	
	// Channel should not be closed initially
	select {
	case <-done:
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}
}

func TestBaseWorkerIsRunning(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Should start as not running
	if worker.IsRunning() {
		t.Error("IsRunning() should return false initially")
	}
}

func TestBaseWorkerSetRunning(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Set to running
	worker.SetRunning(true)
	if !worker.IsRunning() {
		t.Error("IsRunning() should return true after SetRunning(true)")
	}
	
	// Set to not running
	worker.SetRunning(false)
	if worker.IsRunning() {
		t.Error("IsRunning() should return false after SetRunning(false)")
	}
}

func TestBaseWorkerStop(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Set worker as running first
	worker.SetRunning(true)
	
	done := worker.Done()
	
	// Stop the worker
	worker.Stop()
	
	// Done channel should be closed
	select {
	case <-done:
		// Expected - channel should be closed
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Stop()")
	}
	
	// Worker should no longer be running
	if worker.IsRunning() {
		t.Error("IsRunning() should return false after Stop()")
	}
}

func TestBaseWorkerStopTwice(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Set worker as running first
	worker.SetRunning(true)
	
	// Stop should not panic when called multiple times
	worker.Stop()
	worker.Stop() // This should not panic
	
	if worker.IsRunning() {
		t.Error("IsRunning() should return false after Stop()")
	}
}

func TestBaseWorkerStopNotRunning(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Stop should not panic when worker is not running
	worker.Stop() // This should not panic
}

func TestConcurrentAccess(t *testing.T) {
	worker := NewBaseWorker("test", nil, nil, time.Second)
	
	// Test concurrent access to IsRunning and SetRunning
	done := make(chan bool, 2)
	
	// Goroutine 1: Toggle running state
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 100; i++ {
			worker.SetRunning(i%2 == 0)
		}
	}()
	
	// Goroutine 2: Read running state
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 100; i++ {
			_ = worker.IsRunning()
		}
	}()
	
	// Wait for both goroutines to complete
	<-done
	<-done
	
	// Should not panic or race
}

func TestManagerConfig(t *testing.T) {
	log := logger.New(logger.Config{Level: "info", Format: "json"})
	
	config := ManagerConfig{
		Log: log,
	}
	
	if config.Log != log {
		t.Error("ManagerConfig.Log not set correctly")
	}
}

func TestBaseWorkerFields(t *testing.T) {
	name := "test-worker"
	interval := 5 * time.Second
	customLog := logger.New(logger.Config{Level: "debug", Format: "text"})
	
	worker := NewBaseWorker(name, nil, customLog, interval)
	
	// Test all accessor methods
	if worker.Name() != name {
		t.Errorf("Name() = %s, want %s", worker.Name(), name)
	}
	
	if worker.Interval() != interval {
		t.Errorf("Interval() = %v, want %v", worker.Interval(), interval)
	}
	
	if worker.DB() != nil {
		t.Error("DB() should return nil when nil was passed")
	}
	
	if worker.Log() == nil {
		t.Error("Log() should not be nil")
	}
	
	if worker.Done() == nil {
		t.Error("Done() should not return nil channel")
	}
}