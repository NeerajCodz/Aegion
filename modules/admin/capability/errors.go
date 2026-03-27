// Package capability provides error definitions and context utilities.
package capability

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Errors
var (
	ErrInvalidRole             = errors.New("invalid role")
	ErrInvalidCapability       = errors.New("invalid capability")
	ErrCannotModifySystemRole  = errors.New("cannot modify system role")
	ErrCannotDeleteSystemRole  = errors.New("cannot delete system role")
	ErrAdminIdentityNotFound   = errors.New("admin identity not found")
	ErrRoleNotFound           = errors.New("role not found")
	ErrPermissionDenied       = errors.New("permission denied")
)

// Context keys
type contextKey string

const (
	contextKeyIdentityID contextKey = "aegion.capability.identity_id"
)

// SetIdentityIDInContext stores an identity ID in the context.
func SetIdentityIDInContext(ctx context.Context, identityID uuid.UUID) context.Context {
	return context.WithValue(ctx, contextKeyIdentityID, identityID)
}

// GetIdentityIDFromContext retrieves the identity ID from the context.
func GetIdentityIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if id, ok := ctx.Value(contextKeyIdentityID).(uuid.UUID); ok {
		return id, true
	}
	return uuid.UUID{}, false
}