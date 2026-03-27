package flows

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockContinuityStore is a mock implementation of ContinuityStore
type MockContinuityStore struct {
	containers map[uuid.UUID]*ContinuityContainer
	byName     map[string]*ContinuityContainer
}

func NewMockContinuityStore() *MockContinuityStore {
	return &MockContinuityStore{
		containers: make(map[uuid.UUID]*ContinuityContainer),
		byName:     make(map[string]*ContinuityContainer),
	}
}

func (m *MockContinuityStore) Store(ctx context.Context, container *ContinuityContainer) error {
	m.containers[container.ID] = container
	if container.IdentityID != nil {
		key := container.Name + ":" + container.IdentityID.String()
		m.byName[key] = container
	}
	return nil
}

func (m *MockContinuityStore) Retrieve(ctx context.Context, id uuid.UUID) (*ContinuityContainer, error) {
	if container, ok := m.containers[id]; ok {
		return container, nil
	}
	return nil, ErrContainerNotFound
}

func (m *MockContinuityStore) RetrieveByName(ctx context.Context, name string, identityID uuid.UUID) (*ContinuityContainer, error) {
	key := name + ":" + identityID.String()
	if container, ok := m.byName[key]; ok {
		return container, nil
	}
	return nil, ErrContainerNotFound
}

func (m *MockContinuityStore) Delete(ctx context.Context, id uuid.UUID) error {
	delete(m.containers, id)
	return nil
}

func (m *MockContinuityStore) DeleteExpired(ctx context.Context) (int64, error) {
	count := int64(0)
	for id, container := range m.containers {
		if container.IsExpired() {
			delete(m.containers, id)
			count++
		}
	}
	return count, nil
}

func TestNewContinuityContainer(t *testing.T) {
	container := NewContinuityContainer("test-container", 15*time.Minute)

	assert.NotEqual(t, uuid.Nil, container.ID)
	assert.Equal(t, "test-container", container.Name)
	assert.Nil(t, container.IdentityID)
	assert.Nil(t, container.SessionID)
	assert.NotNil(t, container.Payload)
	assert.Equal(t, 0, len(container.Payload))
	assert.False(t, container.IsExpired())
}

func TestNewContinuityContainer_DefaultTTL(t *testing.T) {
	container := NewContinuityContainer("test", 0)

	assert.False(t, container.IsExpired())
	now := time.Now().UTC()
	expectedExpiry := now.Add(DefaultContinuityTTL)
	assert.WithinDuration(t, expectedExpiry, container.ExpiresAt, 100*time.Millisecond)
}

func TestNewContinuityContainer_NegativeTTL(t *testing.T) {
	container := NewContinuityContainer("test", -1*time.Minute)

	assert.False(t, container.IsExpired())
	now := time.Now().UTC()
	expectedExpiry := now.Add(DefaultContinuityTTL)
	assert.WithinDuration(t, expectedExpiry, container.ExpiresAt, 100*time.Millisecond)
}

func TestContinuityContainer_IsExpired(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		expiresAt time.Time
		expected  bool
	}{
		{
			name:      "not expired - future expiry",
			expiresAt: now.Add(10 * time.Minute),
			expected:  false,
		},
		{
			name:      "expired - past expiry",
			expiresAt: now.Add(-10 * time.Minute),
			expected:  true,
		},
		{
			name:      "just expired - at boundary",
			expiresAt: now.Add(-1 * time.Millisecond),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &ContinuityContainer{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.expected, container.IsExpired())
		})
	}
}

func TestContinuityContainer_SetIdentity(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)
	assert.Nil(t, container.IdentityID)

	identityID := uuid.New()
	container.SetIdentity(identityID)

	assert.NotNil(t, container.IdentityID)
	assert.Equal(t, identityID, *container.IdentityID)
}

func TestContinuityContainer_SetSession(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)
	assert.Nil(t, container.SessionID)

	sessionID := uuid.New()
	container.SetSession(sessionID)

	assert.NotNil(t, container.SessionID)
	assert.Equal(t, sessionID, *container.SessionID)
}

func TestContinuityContainer_Set_Get(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	container.Set("key1", "value1")
	container.Set("key2", 42)
	container.Set("key3", map[string]interface{}{"nested": "data"})

	val1, exists1 := container.Get("key1")
	assert.True(t, exists1)
	assert.Equal(t, "value1", val1)

	val2, exists2 := container.Get("key2")
	assert.True(t, exists2)
	assert.Equal(t, 42, val2)

	val3, exists3 := container.Get("key3")
	assert.True(t, exists3)
	nestedMap := val3.(map[string]interface{})
	assert.Equal(t, "data", nestedMap["nested"])

	// Non-existent key
	val4, exists4 := container.Get("nonexistent")
	assert.False(t, exists4)
	assert.Nil(t, val4)
}

func TestContinuityContainer_GetString(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	container.Set("email", "user@example.com")
	container.Set("count", 42)

	email := container.GetString("email")
	assert.Equal(t, "user@example.com", email)

	// Non-string value should return empty string
	count := container.GetString("count")
	assert.Equal(t, "", count)

	// Non-existent key should return empty string
	missing := container.GetString("missing")
	assert.Equal(t, "", missing)
}

func TestContinuityContainer_GetUUID(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	testID := uuid.New()
	container.Set("user_id", testID)

	// Get as UUID
	id, exists := container.GetUUID("user_id")
	assert.True(t, exists)
	assert.Equal(t, testID, id)

	// Get UUID stored as string
	container.Set("session_id", testID.String())
	sessionID, exists := container.GetUUID("session_id")
	assert.True(t, exists)
	assert.Equal(t, testID, sessionID)

	// Invalid UUID string
	container.Set("invalid_id", "not-a-uuid")
	invalid, exists := container.GetUUID("invalid_id")
	assert.False(t, exists)
	assert.Equal(t, uuid.Nil, invalid)

	// Non-existent key
	missing, exists := container.GetUUID("missing")
	assert.False(t, exists)
	assert.Equal(t, uuid.Nil, missing)
}

func TestNewPostgresContinuityStore(t *testing.T) {
	// This just tests that the constructor doesn't panic
	store := NewPostgresContinuityStore(nil)
	assert.NotNil(t, store)
}

func TestNewContinuityManager(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)

	assert.NotNil(t, manager)
}

func TestNewContinuityManager_DefaultTTL(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 0)

	assert.NotNil(t, manager)
	// TTL should be set to default internally
}

func TestContinuityManager_Create(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	payload := Payload{
		"email": "user@example.com",
		"code":  "123456",
	}

	container, err := manager.Create(ctx, "email-verification", nil, payload)

	require.NoError(t, err)
	assert.NotNil(t, container)
	assert.Equal(t, "email-verification", container.Name)
	assert.Nil(t, container.IdentityID)
	assert.Equal(t, "user@example.com", container.Payload["email"])
	assert.Equal(t, "123456", container.Payload["code"])
}

func TestContinuityManager_Create_WithIdentity(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	identityID := uuid.New()
	payload := Payload{
		"action": "password_reset",
	}

	container, err := manager.Create(ctx, "password-reset", &identityID, payload)

	require.NoError(t, err)
	assert.NotNil(t, container)
	assert.NotNil(t, container.IdentityID)
	assert.Equal(t, identityID, *container.IdentityID)
	assert.Equal(t, "password_reset", container.Payload["action"])
}

func TestContinuityManager_Get(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	payload := Payload{"data": "test"}
	created, _ := manager.Create(ctx, "test", nil, payload)

	retrieved, err := manager.Get(ctx, created.ID)

	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, "test", retrieved.Name)
}

func TestContinuityManager_Get_NotFound(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	container, err := manager.Get(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, container)
}

func TestContinuityManager_GetByName(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	identityID := uuid.New()
	payload := Payload{"code": "secret"}
	created, _ := manager.Create(ctx, "verification", &identityID, payload)

	retrieved, err := manager.GetByName(ctx, "verification", identityID)

	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, created.ID, retrieved.ID)
}

func TestContinuityManager_GetByName_NotFound(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	identityID := uuid.New()
	container, err := manager.GetByName(ctx, "nonexistent", identityID)

	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, container)
}

func TestContinuityManager_Consume(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	payload := Payload{"data": "sensitive"}
	created, _ := manager.Create(ctx, "one-time", nil, payload)

	// Consume the container
	retrieved, err := manager.Consume(ctx, created.ID)

	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, created.ID, retrieved.ID)

	// Try to get it again - should not exist
	notFound, err := manager.Get(ctx, created.ID)
	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, notFound)
}

func TestContinuityManager_Consume_NotFound(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()

	container, err := manager.Consume(ctx, uuid.New())

	assert.ErrorIs(t, err, ErrContainerNotFound)
	assert.Nil(t, container)
}

func TestContinuityManager_Cleanup(t *testing.T) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 1*time.Millisecond)
	ctx := context.Background()

	// Create containers
	manager.Create(ctx, "test1", nil, Payload{})
	manager.Create(ctx, "test2", nil, Payload{})

	// Wait for them to expire
	time.Sleep(10 * time.Millisecond)

	deleted, err := manager.Cleanup(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
}

func TestContinuityContainer_MultiplePayloadTypes(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	// Test various types
	container.Set("string", "value")
	container.Set("int", 42)
	container.Set("float", 3.14)
	container.Set("bool", true)
	container.Set("slice", []string{"a", "b", "c"})
	container.Set("map", map[string]interface{}{"nested": "value"})
	container.Set("uuid", uuid.New())

	assert.Len(t, container.Payload, 7)
	assert.Equal(t, "value", container.Payload["string"])
	assert.Equal(t, 42, container.Payload["int"])
	assert.Equal(t, true, container.Payload["bool"])
	assert.Equal(t, 3, len(container.Payload["slice"].([]string)))
}

func TestContinuityContainer_PayloadOverwrite(t *testing.T) {
	container := NewContinuityContainer("test", 15*time.Minute)

	container.Set("key", "value1")
	assert.Equal(t, "value1", container.Payload["key"])

	container.Set("key", "value2")
	assert.Equal(t, "value2", container.Payload["key"])
}

func TestPayload_AsMap(t *testing.T) {
	payload := Payload{
		"email": "user@example.com",
		"code":  "123456",
		"count": 1,
	}

	assert.Equal(t, "user@example.com", payload["email"])
	assert.Equal(t, "123456", payload["code"])
	assert.Equal(t, 1, payload["count"])

	// Iterate over payload
	count := 0
	for k, v := range payload {
		assert.NotEmpty(t, k)
		assert.NotNil(t, v)
		count++
	}
	assert.Equal(t, 3, count)
}

func BenchmarkContinuityContainer_Get(b *testing.B) {
	container := NewContinuityContainer("test", 15*time.Minute)
	container.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.Get("key")
	}
}

func BenchmarkContinuityContainer_GetString(b *testing.B) {
	container := NewContinuityContainer("test", 15*time.Minute)
	container.Set("key", "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.GetString("key")
	}
}

func BenchmarkContinuityContainer_GetUUID(b *testing.B) {
	container := NewContinuityContainer("test", 15*time.Minute)
	testID := uuid.New()
	container.Set("key", testID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.GetUUID("key")
	}
}

func BenchmarkContinuityManager_Create(b *testing.B) {
	store := NewMockContinuityStore()
	manager := NewContinuityManager(store, 15*time.Minute)
	ctx := context.Background()
	payload := Payload{"data": "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.Create(ctx, "test", nil, payload)
	}
}
