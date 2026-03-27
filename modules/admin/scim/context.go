// Package scim provides context utilities for SCIM operations.
package scim

import (
	"context"
)

type contextKey string

const (
	contextKeySCIMToken contextKey = "aegion.scim.token"
)

// contextWithSCIMToken adds a SCIM token to the context.
func contextWithSCIMToken(ctx context.Context, token *SCIMToken) context.Context {
	return context.WithValue(ctx, contextKeySCIMToken, token)
}

// scimTokenFromContext retrieves the SCIM token from the context.
func scimTokenFromContext(ctx context.Context) *SCIMToken {
	if token, ok := ctx.Value(contextKeySCIMToken).(*SCIMToken); ok {
		return token
	}
	return nil
}