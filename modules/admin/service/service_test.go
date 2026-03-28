package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/aegion/aegion/modules/admin/store"
)

func TestCanBootstrap(t *testing.T) {
	mem := newMemoryStore()
	svc := New(mem, Config{BootstrapEnabled: true})

	canBootstrap, err := svc.CanBootstrap(context.Background())
	assert.NoError(t, err)
	assert.True(t, canBootstrap)

	mem.addOperator("viewer")
	canBootstrap, err = svc.CanBootstrap(context.Background())
	assert.NoError(t, err)
	assert.False(t, canBootstrap)
}

func TestBootstrapCreatesOperator(t *testing.T) {
	mem := newMemoryStore()
	svc := New(mem, Config{BootstrapEnabled: true})

	identityID := uuid.New()
	op, err := svc.Bootstrap(context.Background(), identityID, "127.0.0.1")
	assert.NoError(t, err)
	assert.Equal(t, "super_admin", op.Role)
	assert.Equal(t, identityID, op.IdentityID)
	assert.Len(t, mem.auditLogs, 1)
}

func TestCreateOperatorPermissionDenied(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	svc := New(mem, Config{})

	_, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "admin", nil, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrPermissionDenied, err)
}

func TestCreateOperatorSuperAdminDenied(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("operator")
	actor.Permissions = map[string]interface{}{PermOperatorsCreate: true}
	mem.updateOperator(actor)
	svc := New(mem, Config{})

	_, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "super_admin", nil, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrPermissionDenied, err)
}

func TestUpdateOperatorSelfDemotion(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	_, err := svc.UpdateOperator(context.Background(), actor.ID, actor.ID, "admin", nil, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrSelfDemotion, err)
}

func TestDeleteOperatorSelfDeletion(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, actor.ID, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrSelfDeletion, err)
}

func TestEvaluateCapabilityOverrides(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	actor.Permissions = map[string]interface{}{
		PermAuditRead:      false,
		PermOperatorsCreate: true,
	}
	mem.updateOperator(actor)
	svc := New(mem, Config{})

	err := svc.EvaluateCapability(context.Background(), actor.ID, PermAuditRead)
	assert.Error(t, err)
	assert.Equal(t, ErrPermissionDenied, err)

	err = svc.EvaluateCapability(context.Background(), actor.ID, PermOperatorsCreate)
	assert.NoError(t, err)
}

func TestGetEffectivePermissionsWithOverrides(t *testing.T) {
	mem := newMemoryStore()
	mem.roles["custom"] = &store.Role{
		ID:          uuid.New(),
		Name:        "custom",
		Permissions: []string{PermIdentitiesRead},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	op := mem.addOperator("custom")
	op.Permissions = map[string]interface{}{
		PermIdentitiesRead: false,
		PermSessionsRead:   true,
	}
	mem.updateOperator(op)
	svc := New(mem, Config{})

	perms, err := svc.GetEffectivePermissions(context.Background(), op.ID)
	assert.NoError(t, err)

	permSet := make(map[string]bool)
	for _, p := range perms {
		permSet[p] = true
	}

	assert.False(t, permSet[PermIdentitiesRead])
	assert.True(t, permSet[PermSessionsRead])
}

func TestListOperatorsLimitBounds(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	_, _, err := svc.ListOperators(context.Background(), actor.ID, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, 20, mem.lastOperatorListOptions.Limit)

	_, _, err = svc.ListOperators(context.Background(), actor.ID, 500, 0)
	assert.NoError(t, err)
	assert.Equal(t, 100, mem.lastOperatorListOptions.Limit)
}

func TestListAuditLogsLimitBounds(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	svc := New(mem, Config{})

	_, _, err := svc.ListAuditLogs(context.Background(), actor.ID, store.AuditFilter{}, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, 50, mem.lastAuditListOptions.Limit)

	_, _, err = svc.ListAuditLogs(context.Background(), actor.ID, store.AuditFilter{}, 1000, 0)
	assert.NoError(t, err)
	assert.Equal(t, 500, mem.lastAuditListOptions.Limit)
}

type memoryStore struct {
	operators              map[uuid.UUID]*store.Operator
	operatorsByIdentity    map[uuid.UUID]*store.Operator
	roles                  map[string]*store.Role
	auditLogs              []*store.AuditLogEntry
	apiKeysByPrefix        map[string]*store.APIKey
	lastOperatorListOptions store.ListOptions
	lastAuditListOptions    store.ListOptions
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		operators:           make(map[uuid.UUID]*store.Operator),
		operatorsByIdentity: make(map[uuid.UUID]*store.Operator),
		roles:               make(map[string]*store.Role),
		auditLogs:           []*store.AuditLogEntry{},
		apiKeysByPrefix:     make(map[string]*store.APIKey),
	}
}

func (m *memoryStore) addOperator(role string) *store.Operator {
	op := &store.Operator{
		ID:          uuid.New(),
		IdentityID:  uuid.New(),
		Role:        role,
		Permissions: make(map[string]interface{}),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	_ = m.CreateOperator(context.Background(), op)
	return op
}

func (m *memoryStore) updateOperator(op *store.Operator) {
	_ = m.UpdateOperator(context.Background(), op)
}

func (m *memoryStore) CreateOperator(ctx context.Context, op *store.Operator) error {
	if _, exists := m.operatorsByIdentity[op.IdentityID]; exists {
		return store.ErrDuplicateOperator
	}
	copyOp := *op
	m.operators[op.ID] = &copyOp
	m.operatorsByIdentity[op.IdentityID] = &copyOp
	return nil
}

func (m *memoryStore) GetOperator(ctx context.Context, id uuid.UUID) (*store.Operator, error) {
	if op, ok := m.operators[id]; ok {
		copyOp := *op
		return &copyOp, nil
	}
	return nil, store.ErrOperatorNotFound
}

func (m *memoryStore) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
	if op, ok := m.operatorsByIdentity[identityID]; ok {
		copyOp := *op
		return &copyOp, nil
	}
	return nil, store.ErrOperatorNotFound
}

func (m *memoryStore) UpdateOperator(ctx context.Context, op *store.Operator) error {
	if _, ok := m.operators[op.ID]; !ok {
		return store.ErrOperatorNotFound
	}
	copyOp := *op
	m.operators[op.ID] = &copyOp
	m.operatorsByIdentity[op.IdentityID] = &copyOp
	return nil
}

func (m *memoryStore) DeleteOperator(ctx context.Context, id uuid.UUID) error {
	op, ok := m.operators[id]
	if !ok {
		return store.ErrOperatorNotFound
	}
	delete(m.operators, id)
	delete(m.operatorsByIdentity, op.IdentityID)
	return nil
}

func (m *memoryStore) ListOperators(ctx context.Context, opts store.ListOptions) ([]*store.Operator, int64, error) {
	m.lastOperatorListOptions = opts
	operators := make([]*store.Operator, 0, len(m.operators))
	for _, op := range m.operators {
		copyOp := *op
		operators = append(operators, &copyOp)
	}
	return operators, int64(len(operators)), nil
}

func (m *memoryStore) ListRoles(ctx context.Context, opts store.ListOptions) ([]*store.Role, int64, error) {
	roles := make([]*store.Role, 0, len(m.roles))
	for _, role := range m.roles {
		copyRole := *role
		roles = append(roles, &copyRole)
	}
	return roles, int64(len(roles)), nil
}

func (m *memoryStore) GetRoleByName(ctx context.Context, name string) (*store.Role, error) {
	if role, ok := m.roles[name]; ok {
		copyRole := *role
		return &copyRole, nil
	}
	return nil, store.ErrRoleNotFound
}

func (m *memoryStore) ListAuditLogs(ctx context.Context, filter store.AuditFilter, opts store.ListOptions) ([]*store.AuditLogEntry, int64, error) {
	m.lastAuditListOptions = opts
	entries := make([]*store.AuditLogEntry, 0, len(m.auditLogs))
	for _, entry := range m.auditLogs {
		copyEntry := *entry
		entries = append(entries, &copyEntry)
	}
	return entries, int64(len(entries)), nil
}

func (m *memoryStore) LogAction(ctx context.Context, entry *store.AuditLogEntry) error {
	copyEntry := *entry
	m.auditLogs = append(m.auditLogs, &copyEntry)
	return nil
}

func (m *memoryStore) GetAPIKeyByPrefix(ctx context.Context, prefix string) (*store.APIKey, error) {
	if key, ok := m.apiKeysByPrefix[prefix]; ok {
		copyKey := *key
		return &copyKey, nil
	}
	return nil, store.ErrAPIKeyNotFound
}

func (m *memoryStore) UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error {
	for _, key := range m.apiKeysByPrefix {
		if key.ID == id {
			now := time.Now().UTC()
			key.LastUsedAt = &now
			key.UpdatedAt = now
			return nil
		}
	}
	return store.ErrAPIKeyNotFound
}
