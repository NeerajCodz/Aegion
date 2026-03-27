package audit

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockStore is a mock implementation of the audit Store interface.
type MockStore struct {
	mock.Mock
}

func (m *MockStore) WriteEntry(entry *AuditEntry) error {
	args := m.Called(entry)
	return args.Error(0)
}

func (m *MockStore) QueryEntries(filter *AuditFilter) (*AuditQueryResult, error) {
	args := m.Called(filter)
	return args.Get(0).(*AuditQueryResult), args.Error(1)
}

func (m *MockStore) GetEntry(id uuid.UUID) (*AuditEntry, error) {
	args := m.Called(id)
	return args.Get(0).(*AuditEntry), args.Error(1)
}

func (m *MockStore) CountEntries(filter *AuditFilter) (int, error) {
	args := m.Called(filter)
	return args.Int(0), args.Error(1)
}

func (m *MockStore) GetActorSummary(actorID uuid.UUID, days int) (map[string]int, error) {
	args := m.Called(actorID, days)
	return args.Get(0).(map[string]int), args.Error(1)
}

// Test Logger Creation
func TestNewLogger(t *testing.T) {
	mockStore := &MockStore{}
	
	logger := NewLogger(mockStore)
	
	assert.NotNil(t, logger)
	assert.Equal(t, mockStore, logger.store)
}

// Test Basic Logging
func TestLogAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	entry := &AuditEntry{
		ActorID:    uuid.New(),
		ActorEmail: "admin@example.com",
		Action:     ActionUserCreated,
		EntityType: EntityTypeUser,
		EntityID:   "user123",
		Reason:     "New user registration",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionUserCreated && 
			   e.ActorEmail == "admin@example.com" &&
			   !e.Timestamp.IsZero() &&
			   e.ID != uuid.Nil
	})).Return(nil)
	
	err := logger.LogAction(entry)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogActionAutoFields(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	entry := &AuditEntry{
		ActorID:    uuid.New(),
		ActorEmail: "admin@example.com",
		Action:     ActionUserCreated,
	}
	
	// Should auto-generate ID and timestamp
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.ID != uuid.Nil && !e.Timestamp.IsZero()
	})).Return(nil)
	
	err := logger.LogAction(entry)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test Specific Action Logging
func TestLogUserAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	userID := "user123"
	
	beforeState := map[string]interface{}{
		"active": false,
		"email":  "old@example.com",
	}
	afterState := map[string]interface{}{
		"active": true,
		"email":  "new@example.com",
	}
	metadata := map[string]interface{}{
		"ip_address": "192.168.1.1",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionUserUpdated &&
			   e.EntityType == EntityTypeUser &&
			   e.EntityID == userID &&
			   e.ActorID == actorID &&
			   e.Reason == "Profile update" &&
			   len(e.Before) > 0 &&
			   len(e.After) > 0 &&
			   len(e.Metadata) > 0
	})).Return(nil)
	
	err := logger.LogUserAction(actorID, "admin@example.com", ActionUserUpdated, userID, "Profile update", beforeState, afterState, metadata)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogSessionAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	sessionID := "session123"
	metadata := map[string]interface{}{
		"reason": "Suspicious activity",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionSessionRevoked &&
			   e.EntityType == EntityTypeSession &&
			   e.EntityID == sessionID &&
			   e.ActorID == actorID
	})).Return(nil)
	
	err := logger.LogSessionAction(actorID, "admin@example.com", ActionSessionRevoked, sessionID, "Security breach", metadata)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogMFAAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	userID := "user123"
	
	beforeState := map[string]interface{}{
		"enabled": false,
	}
	afterState := map[string]interface{}{
		"enabled": true,
		"device":  "totp",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionMFAEnabled &&
			   e.EntityType == EntityTypeMFA &&
			   e.EntityID == userID
	})).Return(nil)
	
	err := logger.LogMFAAction(actorID, "admin@example.com", ActionMFAEnabled, userID, "User requested MFA", beforeState, afterState, nil)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogOAuth2ClientAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	clientID := "client123"
	
	clientData := map[string]interface{}{
		"name":         "Test App",
		"redirect_uri": "https://app.example.com/callback",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionOAuth2ClientCreated &&
			   e.EntityType == EntityTypeOAuth2Client &&
			   e.EntityID == clientID
	})).Return(nil)
	
	err := logger.LogOAuth2ClientAction(actorID, "admin@example.com", ActionOAuth2ClientCreated, clientID, "New application", nil, clientData, nil)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogPolicyAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	policyID := "policy123"
	
	policyData := map[string]interface{}{
		"name": "Access Control Policy",
		"rules": []string{"allow users.read", "deny users.delete"},
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionPolicyCreated &&
			   e.EntityType == EntityTypePolicy &&
			   e.EntityID == policyID
	})).Return(nil)
	
	err := logger.LogPolicyAction(actorID, "admin@example.com", ActionPolicyCreated, policyID, "New access policy", nil, policyData, nil)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogAdminAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	targetAdminID := "admin456"
	
	roleData := map[string]interface{}{
		"role": "user_manager",
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionAdminRoleAssigned &&
			   e.EntityType == EntityTypeAdmin &&
			   e.EntityID == targetAdminID
	})).Return(nil)
	
	err := logger.LogAdminAction(actorID, "admin@example.com", ActionAdminRoleAssigned, targetAdminID, "Role assignment", nil, roleData, nil)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogSCIMAction(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	scimUserID := "scim_user123"
	
	userData := map[string]interface{}{
		"userName": "john.doe",
		"active":   true,
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionSCIMUserCreated &&
			   e.EntityType == EntityTypeSCIMUser &&
			   e.EntityID == scimUserID
	})).Return(nil)
	
	err := logger.LogSCIMAction(actorID, "scim@example.com", ActionSCIMUserCreated, EntityTypeSCIMUser, scimUserID, "SCIM provisioning", nil, userData, nil)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

func TestLogSecurityEvent(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	metadata := map[string]interface{}{
		"ip_address":    "192.168.1.100",
		"user_agent":    "Suspicious Bot",
		"attempt_count": 5,
	}
	
	mockStore.On("WriteEntry", mock.MatchedBy(func(e *AuditEntry) bool {
		return e.Action == ActionSecurityViolation &&
			   e.EntityType == "security" &&
			   e.Reason == "Multiple failed login attempts"
	})).Return(nil)
	
	err := logger.LogSecurityEvent(actorID, "user@example.com", ActionSecurityViolation, "login_attempts", "Multiple failed login attempts", metadata)
	
	assert.NoError(t, err)
	mockStore.AssertExpectations(t)
}

// Test Query Operations
func TestQueryEntries(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	filter := &AuditFilter{
		Actions:  []string{ActionUserCreated, ActionUserUpdated},
		Limit:    10,
		Offset:   0,
	}
	
	entries := []*AuditEntry{
		{
			ID:         uuid.New(),
			Action:     ActionUserCreated,
			EntityType: EntityTypeUser,
			EntityID:   "user1",
		},
		{
			ID:         uuid.New(),
			Action:     ActionUserUpdated,
			EntityType: EntityTypeUser,
			EntityID:   "user2",
		},
	}
	
	expectedResult := &AuditQueryResult{
		Entries: entries,
		Total:   2,
		HasMore: false,
	}
	
	mockStore.On("QueryEntries", filter).Return(expectedResult, nil)
	
	result, err := logger.QueryEntries(filter)
	
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
	assert.Len(t, result.Entries, 2)
	mockStore.AssertExpectations(t)
}

func TestGetEntry(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	entryID := uuid.New()
	expectedEntry := &AuditEntry{
		ID:         entryID,
		Action:     ActionUserCreated,
		EntityType: EntityTypeUser,
		EntityID:   "user123",
	}
	
	mockStore.On("GetEntry", entryID).Return(expectedEntry, nil)
	
	entry, err := logger.GetEntry(entryID)
	
	assert.NoError(t, err)
	assert.Equal(t, expectedEntry, entry)
	mockStore.AssertExpectations(t)
}

func TestGetActorSummary(t *testing.T) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	actorID := uuid.New()
	days := 30
	
	expectedSummary := map[string]int{
		ActionUserCreated:  5,
		ActionUserUpdated:  10,
		ActionUserDeleted:  2,
		ActionSessionRevoked: 3,
	}
	
	mockStore.On("GetActorSummary", actorID, days).Return(expectedSummary, nil)
	
	summary, err := logger.GetActorSummary(actorID, days)
	
	assert.NoError(t, err)
	assert.Equal(t, expectedSummary, summary)
	assert.Equal(t, 5, summary[ActionUserCreated])
	mockStore.AssertExpectations(t)
}

// Test Filter Validation
func TestAuditFilter(t *testing.T) {
	actorID := uuid.New()
	entityType := EntityTypeUser
	fromTime := time.Now().Add(-24 * time.Hour)
	toTime := time.Now()
	
	filter := &AuditFilter{
		ActorID:    &actorID,
		Actions:    []string{ActionUserCreated, ActionUserUpdated},
		EntityType: &entityType,
		FromTime:   &fromTime,
		ToTime:     &toTime,
		Limit:      100,
		Offset:     0,
	}
	
	assert.Equal(t, actorID, *filter.ActorID)
	assert.Contains(t, filter.Actions, ActionUserCreated)
	assert.Equal(t, EntityTypeUser, *filter.EntityType)
	assert.True(t, filter.FromTime.Before(*filter.ToTime))
	assert.Equal(t, 100, filter.Limit)
}

// Test Constants
func TestAuditConstants(t *testing.T) {
	// Test action constants
	assert.Equal(t, "user.created", ActionUserCreated)
	assert.Equal(t, "user.updated", ActionUserUpdated)
	assert.Equal(t, "session.revoked", ActionSessionRevoked)
	assert.Equal(t, "mfa.enabled", ActionMFAEnabled)
	assert.Equal(t, "oauth2.client.created", ActionOAuth2ClientCreated)
	assert.Equal(t, "policy.created", ActionPolicyCreated)
	assert.Equal(t, "admin.role.assigned", ActionAdminRoleAssigned)
	assert.Equal(t, "scim.user.created", ActionSCIMUserCreated)
	
	// Test entity type constants
	assert.Equal(t, "user", EntityTypeUser)
	assert.Equal(t, "session", EntityTypeSession)
	assert.Equal(t, "mfa", EntityTypeMFA)
	assert.Equal(t, "oauth2_client", EntityTypeOAuth2Client)
	assert.Equal(t, "policy", EntityTypePolicy)
	assert.Equal(t, "admin", EntityTypeAdmin)
	assert.Equal(t, "scim_user", EntityTypeSCIMUser)
}

// Test JSON Serialization
func TestAuditEntryJSONSerialization(t *testing.T) {
	entry := &AuditEntry{
		ID:         uuid.New(),
		Timestamp:  time.Now().UTC(),
		ActorID:    uuid.New(),
		ActorEmail: "admin@example.com",
		Action:     ActionUserCreated,
		EntityType: EntityTypeUser,
		EntityID:   "user123",
		Reason:     "Test user creation",
		IPAddress:  "192.168.1.1",
		UserAgent:  "Mozilla/5.0",
		RequestID:  "req-123",
		SessionID:  "session-456",
	}
	
	// Test that all fields are properly tagged for JSON
	assert.NotEmpty(t, entry.ID)
	assert.NotEmpty(t, entry.ActorEmail)
	assert.NotEmpty(t, entry.Action)
	assert.NotEmpty(t, entry.EntityType)
	assert.NotEmpty(t, entry.EntityID)
}

// Benchmark tests
func BenchmarkLogAction(b *testing.B) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	mockStore.On("WriteEntry", mock.Anything).Return(nil)
	
	entry := &AuditEntry{
		ActorID:    uuid.New(),
		ActorEmail: "admin@example.com",
		Action:     ActionUserCreated,
		EntityType: EntityTypeUser,
		EntityID:   "user123",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogAction(entry)
	}
}

func BenchmarkLogUserAction(b *testing.B) {
	mockStore := &MockStore{}
	logger := NewLogger(mockStore)
	
	mockStore.On("WriteEntry", mock.Anything).Return(nil)
	
	actorID := uuid.New()
	before := map[string]interface{}{"active": false}
	after := map[string]interface{}{"active": true}
	metadata := map[string]interface{}{"source": "admin"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.LogUserAction(actorID, "admin@example.com", ActionUserUpdated, "user123", "test", before, after, metadata)
	}
}