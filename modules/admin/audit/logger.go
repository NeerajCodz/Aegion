// Package audit provides append-only audit logging for admin operations.
package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	Timestamp   time.Time       `json:"timestamp" db:"timestamp"`
	ActorID     uuid.UUID       `json:"actorId" db:"actor_id"`
	ActorEmail  string          `json:"actorEmail" db:"actor_email"`
	Action      string          `json:"action" db:"action"`
	EntityType  string          `json:"entityType" db:"entity_type"`
	EntityID    string          `json:"entityId" db:"entity_id"`
	Before      json.RawMessage `json:"before,omitempty" db:"before"`
	After       json.RawMessage `json:"after,omitempty" db:"after"`
	Reason      string          `json:"reason,omitempty" db:"reason"`
	IPAddress   string          `json:"ipAddress,omitempty" db:"ip_address"`
	UserAgent   string          `json:"userAgent,omitempty" db:"user_agent"`
	RequestID   string          `json:"requestId,omitempty" db:"request_id"`
	SessionID   string          `json:"sessionId,omitempty" db:"session_id"`
	Metadata    json.RawMessage `json:"metadata,omitempty" db:"metadata"`
}

// Common audit actions
const (
	// User management actions
	ActionUserCreated   = "user.created"
	ActionUserUpdated   = "user.updated"
	ActionUserDeleted   = "user.deleted"
	ActionUserSuspended = "user.suspended"
	ActionUserActivated = "user.activated"

	// Session management actions
	ActionSessionRevoked    = "session.revoked"
	ActionSessionRevokedAll = "session.revoked_all"

	// MFA management actions
	ActionMFAEnabled        = "mfa.enabled"
	ActionMFADisabled       = "mfa.disabled"
	ActionMFADeviceAdded    = "mfa.device.added"
	ActionMFADeviceRemoved  = "mfa.device.removed"
	ActionMFADeviceReset    = "mfa.device.reset"

	// OAuth2 management actions
	ActionOAuth2ClientCreated = "oauth2.client.created"
	ActionOAuth2ClientUpdated = "oauth2.client.updated"
	ActionOAuth2ClientDeleted = "oauth2.client.deleted"
	ActionOAuth2TokenRevoked  = "oauth2.token.revoked"

	// Policy management actions
	ActionPolicyCreated = "policy.created"
	ActionPolicyUpdated = "policy.updated"
	ActionPolicyDeleted = "policy.deleted"

	// System configuration actions
	ActionSystemConfigUpdated = "system.config.updated"

	// Admin team management actions
	ActionAdminRoleAssigned   = "admin.role.assigned"
	ActionAdminRoleRemoved    = "admin.role.removed"
	ActionAdminGrantAdded     = "admin.grant.added"
	ActionAdminGrantRevoked   = "admin.grant.revoked"
	ActionAdminDenyAdded      = "admin.deny.added"
	ActionAdminDenyRemoved    = "admin.deny.removed"
	ActionAdminCreated        = "admin.created"
	ActionAdminDeleted        = "admin.deleted"

	// Role management actions
	ActionRoleCreated = "role.created"
	ActionRoleUpdated = "role.updated"
	ActionRoleDeleted = "role.deleted"

	// SCIM actions
	ActionSCIMUserCreated    = "scim.user.created"
	ActionSCIMUserUpdated    = "scim.user.updated"
	ActionSCIMUserDeleted    = "scim.user.deleted"
	ActionSCIMGroupCreated   = "scim.group.created"
	ActionSCIMGroupUpdated   = "scim.group.updated"
	ActionSCIMGroupDeleted   = "scim.group.deleted"
	ActionSCIMTokenCreated   = "scim.token.created"
	ActionSCIMTokenDeleted   = "scim.token.deleted"
	ActionSCIMTokenUsed      = "scim.token.used"

	// Authentication actions
	ActionLoginSuccess       = "auth.login.success"
	ActionLoginFailed        = "auth.login.failed"
	ActionPasswordChanged    = "auth.password.changed"
	ActionPasswordReset      = "auth.password.reset"

	// Security events
	ActionSecurityViolation   = "security.violation"
	ActionRateLimitExceeded   = "security.rate_limit_exceeded"
	ActionSuspiciousActivity  = "security.suspicious_activity"
)

// Entity types
const (
	EntityTypeUser         = "user"
	EntityTypeSession      = "session"
	EntityTypeMFA          = "mfa"
	EntityTypeOAuth2Client = "oauth2_client"
	EntityTypeOAuth2Token  = "oauth2_token"
	EntityTypePolicy       = "policy"
	EntityTypeSystemConfig = "system_config"
	EntityTypeAdmin        = "admin"
	EntityTypeRole         = "role"
	EntityTypeSCIMUser     = "scim_user"
	EntityTypeSCIMGroup    = "scim_group"
	EntityTypeSCIMToken    = "scim_token"
)

// AuditFilter represents filters for audit log queries.
type AuditFilter struct {
	ActorID    *uuid.UUID `json:"actorId,omitempty"`
	Actions    []string   `json:"actions,omitempty"`
	EntityType *string    `json:"entityType,omitempty"`
	EntityID   *string    `json:"entityId,omitempty"`
	FromTime   *time.Time `json:"fromTime,omitempty"`
	ToTime     *time.Time `json:"toTime,omitempty"`
	IPAddress  *string    `json:"ipAddress,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	Offset     int        `json:"offset,omitempty"`
}

// AuditQueryResult represents the result of an audit query.
type AuditQueryResult struct {
	Entries    []*AuditEntry `json:"entries"`
	Total      int           `json:"total"`
	HasMore    bool          `json:"hasMore"`
}

// Store defines the interface for audit log persistence.
type Store interface {
	// WriteEntry appends a new audit entry (immutable).
	WriteEntry(entry *AuditEntry) error

	// QueryEntries retrieves audit entries based on filters.
	QueryEntries(filter *AuditFilter) (*AuditQueryResult, error)

	// GetEntry retrieves a specific audit entry by ID.
	GetEntry(id uuid.UUID) (*AuditEntry, error)

	// CountEntries returns the total number of audit entries matching the filter.
	CountEntries(filter *AuditFilter) (int, error)

	// GetActorSummary returns audit activity summary for an actor.
	GetActorSummary(actorID uuid.UUID, days int) (map[string]int, error)
}

// Logger provides audit logging functionality.
type Logger struct {
	store Store
}

// NewLogger creates a new audit logger.
func NewLogger(store Store) *Logger {
	return &Logger{store: store}
}

// LogAction logs an administrative action.
func (l *Logger) LogAction(entry *AuditEntry) error {
	// Ensure required fields
	if entry.ID == (uuid.UUID{}) {
		entry.ID = uuid.New()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	return l.store.WriteEntry(entry)
}

// LogUserAction logs a user-related action.
func (l *Logger) LogUserAction(actorID uuid.UUID, actorEmail, action, userID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypeUser, userID, reason, before, after, metadata)
}

// LogSessionAction logs a session-related action.
func (l *Logger) LogSessionAction(actorID uuid.UUID, actorEmail, action, sessionID, reason string, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypeSession, sessionID, reason, nil, nil, metadata)
}

// LogMFAAction logs an MFA-related action.
func (l *Logger) LogMFAAction(actorID uuid.UUID, actorEmail, action, userID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypeMFA, userID, reason, before, after, metadata)
}

// LogOAuth2ClientAction logs an OAuth2 client-related action.
func (l *Logger) LogOAuth2ClientAction(actorID uuid.UUID, actorEmail, action, clientID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypeOAuth2Client, clientID, reason, before, after, metadata)
}

// LogPolicyAction logs a policy-related action.
func (l *Logger) LogPolicyAction(actorID uuid.UUID, actorEmail, action, policyID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypePolicy, policyID, reason, before, after, metadata)
}

// LogAdminAction logs an admin team-related action.
func (l *Logger) LogAdminAction(actorID uuid.UUID, actorEmail, action, targetAdminID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, EntityTypeAdmin, targetAdminID, reason, before, after, metadata)
}

// LogSCIMAction logs a SCIM-related action.
func (l *Logger) LogSCIMAction(actorID uuid.UUID, actorEmail, action, entityType, entityID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, entityType, entityID, reason, before, after, metadata)
}

// LogSecurityEvent logs a security-related event.
func (l *Logger) LogSecurityEvent(actorID uuid.UUID, actorEmail, action, entityID, reason string, metadata map[string]interface{}) error {
	return l.logEntityAction(actorID, actorEmail, action, "security", entityID, reason, nil, nil, metadata)
}

// logEntityAction is a helper to log entity-specific actions.
func (l *Logger) logEntityAction(actorID uuid.UUID, actorEmail, action, entityType, entityID, reason string, before, after interface{}, metadata map[string]interface{}) error {
	entry := &AuditEntry{
		ID:         uuid.New(),
		Timestamp:  time.Now().UTC(),
		ActorID:    actorID,
		ActorEmail: actorEmail,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		Reason:     reason,
	}

	// Serialize before state
	if before != nil {
		beforeBytes, err := json.Marshal(before)
		if err == nil {
			entry.Before = beforeBytes
		}
	}

	// Serialize after state
	if after != nil {
		afterBytes, err := json.Marshal(after)
		if err == nil {
			entry.After = afterBytes
		}
	}

	// Serialize metadata
	if metadata != nil {
		metaBytes, err := json.Marshal(metadata)
		if err == nil {
			entry.Metadata = metaBytes
		}
	}

	return l.LogAction(entry)
}

// QueryEntries retrieves audit entries based on filters.
func (l *Logger) QueryEntries(filter *AuditFilter) (*AuditQueryResult, error) {
	return l.store.QueryEntries(filter)
}

// GetEntry retrieves a specific audit entry by ID.
func (l *Logger) GetEntry(id uuid.UUID) (*AuditEntry, error) {
	return l.store.GetEntry(id)
}

// GetActorSummary returns audit activity summary for an actor.
func (l *Logger) GetActorSummary(actorID uuid.UUID, days int) (map[string]int, error) {
	return l.store.GetActorSummary(actorID, days)
}