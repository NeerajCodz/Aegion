package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestServiceStoreAccessor(t *testing.T) {
	mem := newMemoryStore()
	svc := New(mem, Config{BootstrapEnabled: true})
	assert.Same(t, mem, svc.Store())
}

func TestCanBootstrapErrorAndDisabled(t *testing.T) {
	mem := newMemoryStore()
	mem.listOperatorsErr = errors.New("list operators failed")
	svc := New(mem, Config{BootstrapEnabled: true})

	ok, err := svc.CanBootstrap(context.Background())
	assert.Error(t, err)
	assert.False(t, ok)

	mem.listOperatorsErr = nil
	svc = New(mem, Config{BootstrapEnabled: false})
	ok, err = svc.CanBootstrap(context.Background())
	assert.NoError(t, err)
	assert.False(t, ok)
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

func TestBootstrapNotAllowed(t *testing.T) {
	mem := newMemoryStore()
	mem.addOperator("super_admin")
	svc := New(mem, Config{BootstrapEnabled: true})

	_, err := svc.Bootstrap(context.Background(), uuid.New(), "127.0.0.1")
	assert.ErrorIs(t, err, ErrBootstrapNotAllowed)
}

func TestCreateOperatorPermissionDenied(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	svc := New(mem, Config{})

	_, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "admin", nil, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrPermissionDenied, err)
}

func TestCreateOperatorInvalidRole(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	_, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "not-a-role", nil, "10.0.0.1")
	assert.ErrorIs(t, err, ErrInvalidRole)
}

func TestCreateOperatorBySuperAdmin(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	created, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "super_admin", nil, "10.0.0.1")
	assert.NoError(t, err)
	assert.Equal(t, "super_admin", created.Role)
	assert.NotNil(t, created.Permissions)
	assert.Len(t, mem.auditLogs, 1)
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

func TestGetOperatorAndGetOperatorByIdentityID(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("viewer")
	svc := New(mem, Config{})

	got, err := svc.GetOperator(context.Background(), actor.ID, target.ID)
	assert.NoError(t, err)
	assert.Equal(t, target.ID, got.ID)

	gotByIdentity, err := svc.GetOperatorByIdentityID(context.Background(), target.IdentityID)
	assert.NoError(t, err)
	assert.Equal(t, target.ID, gotByIdentity.ID)
}

func TestGetOperatorPermissionDenied(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	target := mem.addOperator("operator")
	svc := New(mem, Config{})

	_, err := svc.GetOperator(context.Background(), actor.ID, target.ID)
	assert.ErrorIs(t, err, ErrPermissionDenied)
}

func TestUpdateOperatorSelfDemotion(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	_, err := svc.UpdateOperator(context.Background(), actor.ID, actor.ID, "admin", nil, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrSelfDemotion, err)
}

func TestUpdateOperatorInvalidRole(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("admin")
	svc := New(mem, Config{})

	_, err := svc.UpdateOperator(context.Background(), actor.ID, target.ID, "bad-role", nil, "10.0.0.1")
	assert.ErrorIs(t, err, ErrInvalidRole)
}

func TestUpdateOperatorSuperAdminRestriction(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("operator")
	actor.Permissions = map[string]interface{}{PermOperatorsUpdate: true}
	mem.updateOperator(actor)
	target := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	_, err := svc.UpdateOperator(context.Background(), actor.ID, target.ID, "admin", nil, "10.0.0.1")
	assert.ErrorIs(t, err, ErrPermissionDenied)
}

func TestUpdateOperatorSuccess(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("admin")
	svc := New(mem, Config{})

	perms := map[string]interface{}{PermSessionsRead: true}
	updated, err := svc.UpdateOperator(context.Background(), actor.ID, target.ID, "viewer", perms, "10.0.0.1")
	assert.NoError(t, err)
	assert.Equal(t, "viewer", updated.Role)
	assert.Equal(t, true, updated.Permissions[PermSessionsRead])
	assert.Len(t, mem.auditLogs, 1)
}

func TestDeleteOperatorSelfDeletion(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, actor.ID, "10.0.0.1")
	assert.Error(t, err)
	assert.Equal(t, ErrSelfDeletion, err)
}

func TestDeleteOperatorPermissionDenied(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	target := mem.addOperator("operator")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, target.ID, "10.0.0.1")
	assert.ErrorIs(t, err, ErrPermissionDenied)
}

func TestDeleteOperatorSuperAdminRestriction(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("admin")
	actor.Permissions = map[string]interface{}{PermOperatorsDelete: true}
	mem.updateOperator(actor)
	target := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, target.ID, "10.0.0.1")
	assert.ErrorIs(t, err, ErrPermissionDenied)
}

func TestDeleteOperatorSuccess(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("operator")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, target.ID, "10.0.0.1")
	assert.NoError(t, err)
	_, lookupErr := mem.GetOperator(context.Background(), target.ID)
	assert.ErrorIs(t, lookupErr, store.ErrOperatorNotFound)
	assert.Len(t, mem.auditLogs, 1)
}

func TestEvaluateCapabilityOverrides(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("viewer")
	actor.Permissions = map[string]interface{}{
		PermAuditRead:       false,
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

func TestEvaluateCapabilityByIdentity(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("admin")
	svc := New(mem, Config{})

	err := svc.EvaluateCapabilityByIdentity(context.Background(), actor.IdentityID, PermOperatorsRead)
	assert.NoError(t, err)

	err = svc.EvaluateCapabilityByIdentity(context.Background(), uuid.New(), PermOperatorsRead)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestHasCapabilityAndRequireCapability(t *testing.T) {
	mem := newMemoryStore()
	operator := mem.addOperator("viewer")
	svc := New(mem, Config{})

	assert.True(t, svc.HasCapability(context.Background(), operator.ID, PermAuditRead))
	assert.False(t, svc.HasCapability(context.Background(), operator.ID, PermOperatorsCreate))

	assert.NotPanics(t, func() {
		svc.RequireCapability(context.Background(), operator.ID, PermAuditRead)
	})
	assert.Panics(t, func() {
		svc.RequireCapability(context.Background(), operator.ID, PermOperatorsCreate)
	})
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

func TestMatchPermission(t *testing.T) {
	assert.True(t, matchPermission(PermAll, PermOperatorsRead))
	assert.True(t, matchPermission(PermOperatorsAll, PermOperatorsCreate))
	assert.True(t, matchPermission(PermOperatorsRead, PermOperatorsRead))
	assert.False(t, matchPermission(PermOperatorsRead, PermOperatorsDelete))
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

func TestListRolesAndGetRole(t *testing.T) {
	mem := newMemoryStore()
	mem.roles["admin"] = &store.Role{
		ID:          uuid.New(),
		Name:        "admin",
		Description: "Administrator",
		Permissions: []string{PermOperatorsRead},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	roles, total, err := svc.ListRoles(context.Background(), actor.ID, 0, 0)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, roles, 1)
	assert.Equal(t, 20, mem.lastRoleListOptions.Limit)

	_, _, err = svc.ListRoles(context.Background(), actor.ID, 200, 0)
	assert.NoError(t, err)
	assert.Equal(t, 100, mem.lastRoleListOptions.Limit)

	role, err := svc.GetRole(context.Background(), actor.ID, "admin")
	assert.NoError(t, err)
	assert.Equal(t, "admin", role.Name)
}

func TestGetRolePermissionDenied(t *testing.T) {
	mem := newMemoryStore()
	mem.roles["admin"] = &store.Role{
		ID:          uuid.New(),
		Name:        "admin",
		Permissions: []string{PermOperatorsRead},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	actor := mem.addOperator("viewer")
	svc := New(mem, Config{})

	_, err := svc.GetRole(context.Background(), actor.ID, "admin")
	assert.ErrorIs(t, err, ErrPermissionDenied)
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
	operators               map[uuid.UUID]*store.Operator
	operatorsByIdentity     map[uuid.UUID]*store.Operator
	roles                   map[string]*store.Role
	auditLogs               []*store.AuditLogEntry
	apiKeysByPrefix         map[string]*store.APIKey
	lastOperatorListOptions store.ListOptions
	lastRoleListOptions     store.ListOptions
	lastAuditListOptions    store.ListOptions

	createOperatorErr error
	getOperatorErr    error
	getByIdentityErr  error
	updateOperatorErr error
	deleteOperatorErr error
	listOperatorsErr  error
	listRolesErr      error
	getRoleByNameErr  error
	listAuditLogsErr  error
	logActionErr      error
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
	if m.createOperatorErr != nil {
		return m.createOperatorErr
	}
	if _, exists := m.operatorsByIdentity[op.IdentityID]; exists {
		return store.ErrDuplicateOperator
	}
	copyOp := *op
	m.operators[op.ID] = &copyOp
	m.operatorsByIdentity[op.IdentityID] = &copyOp
	return nil
}

func (m *memoryStore) GetOperator(ctx context.Context, id uuid.UUID) (*store.Operator, error) {
	if m.getOperatorErr != nil {
		return nil, m.getOperatorErr
	}
	if op, ok := m.operators[id]; ok {
		copyOp := *op
		return &copyOp, nil
	}
	return nil, store.ErrOperatorNotFound
}

func (m *memoryStore) GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error) {
	if m.getByIdentityErr != nil {
		return nil, m.getByIdentityErr
	}
	if op, ok := m.operatorsByIdentity[identityID]; ok {
		copyOp := *op
		return &copyOp, nil
	}
	return nil, store.ErrOperatorNotFound
}

func (m *memoryStore) UpdateOperator(ctx context.Context, op *store.Operator) error {
	if m.updateOperatorErr != nil {
		return m.updateOperatorErr
	}
	if _, ok := m.operators[op.ID]; !ok {
		return store.ErrOperatorNotFound
	}
	copyOp := *op
	m.operators[op.ID] = &copyOp
	m.operatorsByIdentity[op.IdentityID] = &copyOp
	return nil
}

func (m *memoryStore) DeleteOperator(ctx context.Context, id uuid.UUID) error {
	if m.deleteOperatorErr != nil {
		return m.deleteOperatorErr
	}
	op, ok := m.operators[id]
	if !ok {
		return store.ErrOperatorNotFound
	}
	delete(m.operators, id)
	delete(m.operatorsByIdentity, op.IdentityID)
	return nil
}

func (m *memoryStore) ListOperators(ctx context.Context, opts store.ListOptions) ([]*store.Operator, int64, error) {
	if m.listOperatorsErr != nil {
		return nil, 0, m.listOperatorsErr
	}
	m.lastOperatorListOptions = opts
	operators := make([]*store.Operator, 0, len(m.operators))
	for _, op := range m.operators {
		copyOp := *op
		operators = append(operators, &copyOp)
	}
	return operators, int64(len(operators)), nil
}

func (m *memoryStore) ListRoles(ctx context.Context, opts store.ListOptions) ([]*store.Role, int64, error) {
	if m.listRolesErr != nil {
		return nil, 0, m.listRolesErr
	}
	m.lastRoleListOptions = opts
	roles := make([]*store.Role, 0, len(m.roles))
	for _, role := range m.roles {
		copyRole := *role
		roles = append(roles, &copyRole)
	}
	return roles, int64(len(roles)), nil
}

func (m *memoryStore) GetRoleByName(ctx context.Context, name string) (*store.Role, error) {
	if m.getRoleByNameErr != nil {
		return nil, m.getRoleByNameErr
	}
	if role, ok := m.roles[name]; ok {
		copyRole := *role
		return &copyRole, nil
	}
	return nil, store.ErrRoleNotFound
}

func (m *memoryStore) ListAuditLogs(ctx context.Context, filter store.AuditFilter, opts store.ListOptions) ([]*store.AuditLogEntry, int64, error) {
	if m.listAuditLogsErr != nil {
		return nil, 0, m.listAuditLogsErr
	}
	m.lastAuditListOptions = opts
	entries := make([]*store.AuditLogEntry, 0, len(m.auditLogs))
	for _, entry := range m.auditLogs {
		copyEntry := *entry
		entries = append(entries, &copyEntry)
	}
	return entries, int64(len(entries)), nil
}

func (m *memoryStore) LogAction(ctx context.Context, entry *store.AuditLogEntry) error {
	if m.logActionErr != nil {
		return m.logActionErr
	}
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

func (m *memoryStore) DB() *pgxpool.Pool {
	return nil
}

func (m *memoryStore) AuthenticateOperatorByEmail(ctx context.Context, email, password string) (*store.Operator, error) {
	return nil, store.ErrInvalidCredentials
}

func (m *memoryStore) GetIdentityProfile(ctx context.Context, identityID uuid.UUID) (*store.IdentityProfile, error) {
	return nil, store.ErrIdentityNotFound
}

func (m *memoryStore) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	copyKey := *key
	m.apiKeysByPrefix[key.KeyPrefix] = &copyKey
	return nil
}
