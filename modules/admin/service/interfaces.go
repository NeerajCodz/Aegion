package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/admin/store"
)

// Store defines the persistence behavior required by admin service.
type Store interface {
	CreateOperator(ctx context.Context, op *store.Operator) error
	GetOperator(ctx context.Context, id uuid.UUID) (*store.Operator, error)
	GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error)
	UpdateOperator(ctx context.Context, op *store.Operator) error
	DeleteOperator(ctx context.Context, id uuid.UUID) error
	ListOperators(ctx context.Context, opts store.ListOptions) ([]*store.Operator, int64, error)

	ListRoles(ctx context.Context, opts store.ListOptions) ([]*store.Role, int64, error)
	GetRoleByName(ctx context.Context, name string) (*store.Role, error)

	ListAuditLogs(ctx context.Context, filter store.AuditFilter, opts store.ListOptions) ([]*store.AuditLogEntry, int64, error)
	LogAction(ctx context.Context, entry *store.AuditLogEntry) error

	GetAPIKeyByPrefix(ctx context.Context, prefix string) (*store.APIKey, error)
	UpdateAPIKeyLastUsed(ctx context.Context, id uuid.UUID) error
}
