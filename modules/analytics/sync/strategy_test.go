package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/analytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type managerTestStrategy struct {
	name         string
	enabled      bool
	startErr     error
	stopErr      error
	publishErr   error
	started      bool
	stopped      bool
	publishCount int
	position     *analytics.SyncPosition
	health       *analytics.StrategyHealthStatus
}

func newManagerTestStrategy(name string) *managerTestStrategy {
	return &managerTestStrategy{
		name:    name,
		enabled: true,
		position: &analytics.SyncPosition{
			ID:          name + "-position",
			Strategy:    name,
			SourceTable: "audit_events",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		health: &analytics.StrategyHealthStatus{
			Enabled:   true,
			Healthy:   true,
			SyncLagMs: 10,
		},
	}
}

func (m *managerTestStrategy) Name() string { return m.name }
func (m *managerTestStrategy) Start(ctx context.Context) error {
	m.started = true
	return m.startErr
}
func (m *managerTestStrategy) Stop(ctx context.Context) error {
	m.stopped = true
	return m.stopErr
}
func (m *managerTestStrategy) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	m.publishCount++
	return m.publishErr
}
func (m *managerTestStrategy) Health(ctx context.Context) (*analytics.StrategyHealthStatus, error) {
	return m.health, nil
}
func (m *managerTestStrategy) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	if m.position == nil || m.position.SourceTable != table {
		return nil, errors.New("position not found")
	}
	return m.position, nil
}
func (m *managerTestStrategy) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	m.position = position
	return nil
}
func (m *managerTestStrategy) IsEnabled() bool { return m.enabled }

func TestManagerRegistersUniqueStrategies(t *testing.T) {
	manager := NewManager(&MockLogger{})
	require.NoError(t, manager.RegisterStrategy(newManagerTestStrategy("real_time")))

	err := manager.RegisterStrategy(newManagerTestStrategy("real_time"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestManagerPublishesAndDeduplicatesEvents(t *testing.T) {
	manager := NewManager(&MockLogger{})
	realTime := newManagerTestStrategy("real_time")
	batch := newManagerTestStrategy("batch")

	require.NoError(t, manager.RegisterStrategy(realTime))
	require.NoError(t, manager.RegisterStrategy(batch))

	event := &analytics.SyncEvent{
		ID:          "event-1",
		SourceTable: "audit_events",
		EventType:   "insert",
		Timestamp:   time.Now(),
	}

	require.NoError(t, manager.PublishEvent(context.Background(), event))
	require.NoError(t, manager.PublishEvent(context.Background(), event))

	assert.Equal(t, 1, realTime.publishCount)
	assert.Equal(t, 1, batch.publishCount)
}

func TestManagerAggregatesHealthAndPositions(t *testing.T) {
	manager := NewManager(&MockLogger{})
	realTime := newManagerTestStrategy("real_time")
	batch := newManagerTestStrategy("batch")
	batch.health = &analytics.StrategyHealthStatus{
		Enabled:    true,
		Healthy:    false,
		ErrorCount: 2,
	}

	require.NoError(t, manager.RegisterStrategy(realTime))
	require.NoError(t, manager.RegisterStrategy(batch))

	health, err := manager.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "degraded", health.Overall)
	assert.NotEmpty(t, health.ErrorMetrics)
	assert.NotEmpty(t, health.SyncPositions)
}

func TestManagerStartsAndStopsEnabledStrategies(t *testing.T) {
	manager := NewManager(&MockLogger{})
	strategy := newManagerTestStrategy("real_time")

	require.NoError(t, manager.RegisterStrategy(strategy))
	require.NoError(t, manager.Start(context.Background()))
	assert.True(t, strategy.started)

	require.NoError(t, manager.Stop(context.Background()))
	assert.True(t, strategy.stopped)
}
