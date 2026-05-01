package flows

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test store interface implementation
func TestFlowStoreInterface(t *testing.T) {
	// Ensure MockFlowStore implements FlowStore
	var _ FlowStore = (*MockFlowStore)(nil)
}

func TestContinuityStoreInterface(t *testing.T) {
	// Ensure MockContinuityStore implements ContinuityStore
	var _ ContinuityStore = (*MockContinuityStore)(nil)
}

// Test PostgresFlowStore nil db handling
func TestPostgresFlowStore_Creation(t *testing.T) {
	store := NewPostgresFlowStore(nil)
	assert.NotNil(t, store)
}

// Test PostgresContinuityStore nil db handling
func TestPostgresContinuityStore_Creation(t *testing.T) {
	store := NewPostgresContinuityStore(nil)
	assert.NotNil(t, store)
}

// Verify mock store implements all interface methods
func TestMockFlowStore_ImplementsInterface(t *testing.T) {
	store := NewMockFlowStore()

	// Verify all methods are implemented and callable
	assert.NotNil(t, store.Create)
	assert.NotNil(t, store.Get)
	assert.NotNil(t, store.GetByCSRF)
	assert.NotNil(t, store.Update)
	assert.NotNil(t, store.Delete)
	assert.NotNil(t, store.DeleteExpired)
	assert.NotNil(t, store.ListByIdentity)
}

func TestMockContinuityStore_ImplementsInterface(t *testing.T) {
	store := NewMockContinuityStore()

	// Verify all methods are implemented and callable
	assert.NotNil(t, store.Store)
	assert.NotNil(t, store.Retrieve)
	assert.NotNil(t, store.RetrieveByName)
	assert.NotNil(t, store.Delete)
	assert.NotNil(t, store.DeleteExpired)
}

func TestMockFlowStore_CallCounting(t *testing.T) {
	store := NewMockFlowStore()

	assert.Equal(t, 0, store.createCalls)
	assert.Equal(t, 0, store.getCalls)
	assert.Equal(t, 0, store.updateCalls)
	assert.Equal(t, 0, store.deleteCalls)
}
