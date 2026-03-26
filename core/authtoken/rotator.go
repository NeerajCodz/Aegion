package authtoken

import (
	"context"
	"sync"
	"time"

	"github.com/aegion/aegion/core/eventbus"
)

// Event types for secret rotation.
const (
	EventSecretRotationStarted   = "authtoken.rotation.started"
	EventSecretRotationCompleted = "authtoken.rotation.completed"
)

// RotatorConfig holds secret rotator configuration.
type RotatorConfig struct {
	// Generator is the token generator to update
	Generator *Generator
	// EventBus is optional; if provided, rotation events are published
	EventBus *eventbus.Bus
	// GracePeriod is how long to accept old secrets (default: 10 minutes)
	GracePeriod time.Duration
	// SourceModule identifies this rotator in events
	SourceModule string
}

// Rotator handles zero-downtime secret rotation.
type Rotator struct {
	generator    *Generator
	eventBus     *eventbus.Bus
	gracePeriod  time.Duration
	sourceModule string

	mu             sync.Mutex
	rotationTimer  *time.Timer
	previousSecret []byte
}

// NewRotator creates a new secret rotator.
func NewRotator(cfg RotatorConfig) *Rotator {
	gracePeriod := cfg.GracePeriod
	if gracePeriod == 0 {
		gracePeriod = 10 * time.Minute
	}

	sourceModule := cfg.SourceModule
	if sourceModule == "" {
		sourceModule = "authtoken"
	}

	return &Rotator{
		generator:    cfg.Generator,
		eventBus:     cfg.EventBus,
		gracePeriod:  gracePeriod,
		sourceModule: sourceModule,
	}
}

// Rotate initiates a secret rotation with zero downtime.
// The new secret becomes primary immediately, while the old secret
// remains valid for the grace period.
func (r *Rotator) Rotate(ctx context.Context, newSecret []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Cancel any pending rotation timer
	if r.rotationTimer != nil {
		r.rotationTimer.Stop()
		r.rotationTimer = nil
	}

	// Get current primary secret before rotation
	oldSecret := r.previousSecret

	// Update generator: new secret is primary, old becomes secondary
	if err := r.generator.SetSecrets(newSecret, oldSecret); err != nil {
		return err
	}

	// Store old secret for next rotation
	r.previousSecret = newSecret

	// Emit rotation started event
	if r.eventBus != nil {
		r.eventBus.Publish(ctx, eventbus.Event{
			Type:         EventSecretRotationStarted,
			SourceModule: r.sourceModule,
			EntityType:   "secret",
			Payload: map[string]interface{}{
				"grace_period_seconds": r.gracePeriod.Seconds(),
			},
		})
	}

	// Schedule removal of old secret after grace period
	r.rotationTimer = time.AfterFunc(r.gracePeriod, func() {
		r.completeRotation(ctx, newSecret)
	})

	return nil
}

// completeRotation removes the old secret after grace period.
func (r *Rotator) completeRotation(ctx context.Context, currentSecret []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove old secret, keep only current
	r.generator.SetSecrets(currentSecret)

	r.rotationTimer = nil

	// Emit rotation completed event
	if r.eventBus != nil {
		r.eventBus.Publish(ctx, eventbus.Event{
			Type:         EventSecretRotationCompleted,
			SourceModule: r.sourceModule,
			EntityType:   "secret",
			Payload:      map[string]interface{}{},
		})
	}
}

// RotateWithCallback rotates secrets and calls a callback when grace period ends.
func (r *Rotator) RotateWithCallback(ctx context.Context, newSecret []byte, onComplete func()) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Cancel any pending rotation timer
	if r.rotationTimer != nil {
		r.rotationTimer.Stop()
		r.rotationTimer = nil
	}

	// Get current primary secret before rotation
	oldSecret := r.previousSecret

	// Update generator
	if err := r.generator.SetSecrets(newSecret, oldSecret); err != nil {
		return err
	}

	r.previousSecret = newSecret

	// Emit rotation started event
	if r.eventBus != nil {
		r.eventBus.Publish(ctx, eventbus.Event{
			Type:         EventSecretRotationStarted,
			SourceModule: r.sourceModule,
			EntityType:   "secret",
			Payload: map[string]interface{}{
				"grace_period_seconds": r.gracePeriod.Seconds(),
			},
		})
	}

	// Schedule completion with callback
	r.rotationTimer = time.AfterFunc(r.gracePeriod, func() {
		r.completeRotation(ctx, newSecret)
		if onComplete != nil {
			onComplete()
		}
	})

	return nil
}

// ForceComplete immediately completes any pending rotation.
func (r *Rotator) ForceComplete(ctx context.Context) {
	r.mu.Lock()

	if r.rotationTimer != nil {
		r.rotationTimer.Stop()
		r.rotationTimer = nil
	}

	currentSecret := r.previousSecret
	r.mu.Unlock()

	if currentSecret != nil {
		r.completeRotation(ctx, currentSecret)
	}
}

// IsRotating returns true if a rotation is in progress.
func (r *Rotator) IsRotating() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rotationTimer != nil
}

// GetGracePeriod returns the configured grace period.
func (r *Rotator) GetGracePeriod() time.Duration {
	return r.gracePeriod
}

// Stop cancels any pending rotation and cleans up.
func (r *Rotator) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rotationTimer != nil {
		r.rotationTimer.Stop()
		r.rotationTimer = nil
	}
}
