package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/aegion/aegion/modules/admin/store"
)

func TestBootstrapCreateOperatorError(t *testing.T) {
	mem := newMemoryStore()
	mem.createOperatorErr = errors.New("create failed")
	svc := New(mem, Config{BootstrapEnabled: true})

	_, err := svc.Bootstrap(context.Background(), uuid.New(), "127.0.0.1")
	assert.ErrorContains(t, err, "create failed")
}

func TestCreateOperatorStoreErrorPaths(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	svc := New(mem, Config{})

	mem.getOperatorErr = errors.New("actor lookup failed")
	_, err := svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "super_admin", nil, "10.0.0.1")
	assert.ErrorIs(t, err, ErrUnauthorized)

	mem.getOperatorErr = nil
	mem.createOperatorErr = errors.New("insert failed")
	_, err = svc.CreateOperator(context.Background(), actor.ID, uuid.New(), "admin", nil, "10.0.0.1")
	assert.ErrorContains(t, err, "insert failed")
}

func TestUpdateOperatorStoreErrorPaths(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("admin")
	svc := New(mem, Config{})

	mem.getOperatorErr = errors.New("actor lookup failed")
	_, err := svc.UpdateOperator(context.Background(), actor.ID, actor.ID, "admin", nil, "10.0.0.1")
	assert.ErrorIs(t, err, ErrUnauthorized)

	mem.getOperatorErr = nil
	_, err = svc.UpdateOperator(context.Background(), actor.ID, uuid.New(), "viewer", nil, "10.0.0.1")
	assert.ErrorIs(t, err, store.ErrOperatorNotFound)

	mem.updateOperatorErr = errors.New("update failed")
	_, err = svc.UpdateOperator(context.Background(), actor.ID, target.ID, "viewer", nil, "10.0.0.1")
	assert.ErrorContains(t, err, "update failed")
}

func TestDeleteOperatorStoreErrorPaths(t *testing.T) {
	mem := newMemoryStore()
	actor := mem.addOperator("super_admin")
	target := mem.addOperator("operator")
	svc := New(mem, Config{})

	err := svc.DeleteOperator(context.Background(), actor.ID, uuid.New(), "10.0.0.1")
	assert.ErrorIs(t, err, store.ErrOperatorNotFound)

	mem.deleteOperatorErr = errors.New("delete failed")
	err = svc.DeleteOperator(context.Background(), actor.ID, target.ID, "10.0.0.1")
	assert.ErrorContains(t, err, "delete failed")
}

func TestListPermissionAndStoreErrors(t *testing.T) {
	mem := newMemoryStore()
	superAdmin := mem.addOperator("super_admin")
	viewer := mem.addOperator("viewer")
	unknown := mem.addOperator("unknown-role")
	svc := New(mem, Config{})

	_, _, err := svc.ListOperators(context.Background(), viewer.ID, 20, 0)
	assert.ErrorIs(t, err, ErrPermissionDenied)

	mem.listOperatorsErr = errors.New("list operators failed")
	_, _, err = svc.ListOperators(context.Background(), superAdmin.ID, 20, 0)
	assert.ErrorContains(t, err, "list operators failed")
	mem.listOperatorsErr = nil

	_, _, err = svc.ListRoles(context.Background(), viewer.ID, 20, 0)
	assert.ErrorIs(t, err, ErrPermissionDenied)

	mem.listRolesErr = errors.New("list roles failed")
	_, _, err = svc.ListRoles(context.Background(), superAdmin.ID, 20, 0)
	assert.ErrorContains(t, err, "list roles failed")
	mem.listRolesErr = nil

	_, _, err = svc.ListAuditLogs(context.Background(), unknown.ID, store.AuditFilter{}, 50, 0)
	assert.ErrorIs(t, err, ErrPermissionDenied)

	mem.listAuditLogsErr = errors.New("list audit logs failed")
	_, _, err = svc.ListAuditLogs(context.Background(), superAdmin.ID, store.AuditFilter{}, 50, 0)
	assert.ErrorContains(t, err, "list audit logs failed")
}

func TestPermissionResolutionFallbackAndOverrides(t *testing.T) {
	mem := newMemoryStore()
	svc := New(mem, Config{})

	op := mem.addOperator("viewer")
	op.Permissions = map[string]interface{}{
		PermSessionsDelete:  true,
		PermOperatorsCreate: "true",
	}
	mem.updateOperator(op)

	mem.getRoleByNameErr = errors.New("role backend unavailable")
	perms, err := svc.GetEffectivePermissions(context.Background(), op.ID)
	assert.NoError(t, err)
	assert.Contains(t, perms, PermIdentitiesRead)
	assert.Contains(t, perms, PermSessionsDelete)
	assert.NotContains(t, perms, PermOperatorsCreate)

	assert.True(t, svc.hasPermission("unknown-role", map[string]interface{}{PermOperatorsAll: true}, PermOperatorsDelete))
	assert.False(t, svc.hasPermission("unknown-role", map[string]interface{}{PermOperatorsDelete: "yes"}, PermOperatorsDelete))
}
