package authtoken

import (
	"context"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/core/eventbus"
)

func TestNewRotator(t *testing.T) {
	secret := []byte("test-secret-for-rotator")
	gen, err := NewGenerator(GeneratorConfig{Secret: secret})
	require.NoError(t, err)

	bus := eventbus.New(eventbus.Config{})

	tests := []struct {
		name   string
		config RotatorConfig
		checks func(*testing.T, *Rotator)
	}{
		{
			name: "minimal config with defaults",
			config: RotatorConfig{
				Generator: gen,
			},
			checks: func(t *testing.T, r *Rotator) {
				assert.Equal(t, gen, r.generator)
				assert.Nil(t, r.eventBus)
				assert.Equal(t, 10*time.Minute, r.gracePeriod)
				assert.Equal(t, "authtoken", r.sourceModule)
				assert.False(t, r.IsRotating())
			},
		},
		{
			name: "full config",
			config: RotatorConfig{
				Generator:    gen,
				EventBus:     bus,
				GracePeriod:  5 * time.Minute,
				SourceModule: "custom-module",
			},
			checks: func(t *testing.T, r *Rotator) {
				assert.Equal(t, gen, r.generator)
				assert.Equal(t, bus, r.eventBus)
				assert.Equal(t, 5*time.Minute, r.gracePeriod)
				assert.Equal(t, "custom-module", r.sourceModule)
				assert.False(t, r.IsRotating())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rotator := NewRotator(tt.config)
			assert.NotNil(t, rotator)
			tt.checks(t, rotator)
		})
	}
}

func TestRotator_Rotate(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		EventBus:    nil, // Disable eventbus for testing
		GracePeriod: 100 * time.Millisecond, // Short for testing
	})

	ctx := context.Background()

	// Generate token with first secret
	token1, err := gen.Generate("test-module")
	require.NoError(t, err)

	// Verify token1 is valid
	_, err = gen.Validate(token1)
	require.NoError(t, err)

	// Rotate to new secret
	err = rotator.Rotate(ctx, secret2)
	require.NoError(t, err)

	// Rotator should indicate rotation is in progress
	assert.True(t, rotator.IsRotating())

	// Generate token with new secret
	token2, err := gen.Generate("test-module")
	require.NoError(t, err)

	// New token should be valid
	_, err = gen.Validate(token2)
	assert.NoError(t, err, "new token should be valid")

	// Wait for grace period to complete
	time.Sleep(150 * time.Millisecond)

	// Rotator should no longer be rotating
	assert.False(t, rotator.IsRotating())

	// New token should still be valid
	_, err = gen.Validate(token2)
	assert.NoError(t, err, "new token should still be valid")
}

func TestRotator_RotateWithCallback(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: 50 * time.Millisecond,
	})

	ctx := context.Background()
	callbackCalled := make(chan bool, 1)

	// Rotate with callback
	err = rotator.RotateWithCallback(ctx, secret2, func() {
		callbackCalled <- true
	})
	require.NoError(t, err)

	assert.True(t, rotator.IsRotating())

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(time.Second):
		t.Fatal("callback was not called")
	}

	assert.False(t, rotator.IsRotating())
}

func TestRotator_ForceComplete(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		EventBus:    nil, // Disable eventbus for testing
		GracePeriod: 10 * time.Minute, // Long grace period
	})

	ctx := context.Background()

	// Start rotation
	err = rotator.Rotate(ctx, secret2)
	require.NoError(t, err)
	assert.True(t, rotator.IsRotating())

	// Force complete immediately
	rotator.ForceComplete(ctx)

	// Should no longer be rotating
	assert.False(t, rotator.IsRotating())

	// New tokens should still be valid
	token, err := gen.Generate("test")
	require.NoError(t, err)
	_, err = gen.Validate(token)
	assert.NoError(t, err)
}

func TestRotator_Stop(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: 10 * time.Minute, // Long grace period
	})

	ctx := context.Background()

	// Start rotation
	err = rotator.Rotate(ctx, secret2)
	require.NoError(t, err)
	assert.True(t, rotator.IsRotating())

	// Stop rotation
	rotator.Stop()

	// Should no longer be rotating
	assert.False(t, rotator.IsRotating())

	// Wait a bit to ensure timer doesn't fire
	time.Sleep(50 * time.Millisecond)

	// Should still not be rotating
	assert.False(t, rotator.IsRotating())
}

func TestRotator_MultipleRotations(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	secret3 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)
	_, err = rand.Read(secret3)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: 100 * time.Millisecond,
	})

	ctx := context.Background()

	// First rotation
	err = rotator.Rotate(ctx, secret2)
	require.NoError(t, err)
	assert.True(t, rotator.IsRotating())

	// Second rotation should cancel first
	err = rotator.Rotate(ctx, secret3)
	require.NoError(t, err)
	assert.True(t, rotator.IsRotating())

	// Wait for completion
	time.Sleep(150 * time.Millisecond)
	assert.False(t, rotator.IsRotating())

	// Only the latest secret should be valid for new tokens
	token, err := gen.Generate("test")
	require.NoError(t, err)

	// Should validate with latest secret
	_, err = gen.Validate(token)
	assert.NoError(t, err)
}

func TestRotator_NoEventBus(t *testing.T) {
	secret1 := make([]byte, 32)
	secret2 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)
	_, err = rand.Read(secret2)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	// Rotator without event bus
	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: 50 * time.Millisecond,
	})

	ctx := context.Background()

	// Rotation should work without event bus
	err = rotator.Rotate(ctx, secret2)
	require.NoError(t, err)
	assert.True(t, rotator.IsRotating())

	// Wait for completion
	time.Sleep(100 * time.Millisecond)
	assert.False(t, rotator.IsRotating())
}

func TestRotator_ConcurrentOperations(t *testing.T) {
	secret1 := make([]byte, 32)
	_, err := rand.Read(secret1)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{Secret: secret1})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: 10 * time.Millisecond,
	})

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Start multiple rotations concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			
			secret := make([]byte, 32)
			_, err := rand.Read(secret)
			if err != nil {
				errors <- err
				return
			}

			err = rotator.Rotate(ctx, secret)
			if err != nil {
				errors <- err
				return
			}

			// Check if rotating
			_ = rotator.IsRotating()

			// Force complete some of them
			if i%2 == 0 {
				rotator.ForceComplete(ctx)
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	close(errors)
	for err := range errors {
		assert.NoError(t, err, "concurrent operations should not cause errors")
	}

	// Wait for any pending rotations
	time.Sleep(50 * time.Millisecond)
	assert.False(t, rotator.IsRotating())
}

func TestRotator_GetGracePeriod(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{Secret: []byte("secret")})
	require.NoError(t, err)

	tests := []struct {
		name        string
		gracePeriod time.Duration
		expected    time.Duration
	}{
		{
			name:        "default grace period",
			gracePeriod: 0,
			expected:    10 * time.Minute,
		},
		{
			name:        "custom grace period",
			gracePeriod: 5 * time.Minute,
			expected:    5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rotator := NewRotator(RotatorConfig{
				Generator:   gen,
				GracePeriod: tt.gracePeriod,
			})

			assert.Equal(t, tt.expected, rotator.GetGracePeriod())
		})
	}
}

func TestRotator_InvalidSecret(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{Secret: []byte("secret")})
	require.NoError(t, err)

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		GracePeriod: time.Minute,
	})

	ctx := context.Background()

	// Rotate with invalid (empty) secret should fail
	err = rotator.Rotate(ctx, []byte{})
	assert.ErrorIs(t, err, ErrInvalidSecret)
	assert.False(t, rotator.IsRotating())

	// Rotate with nil secret should fail
	err = rotator.Rotate(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidSecret)
	assert.False(t, rotator.IsRotating())
}