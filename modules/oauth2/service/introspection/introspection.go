package introspection

import (
	"context"

	"github.com/aegion/aegion/modules/oauth2/service/token"
)

// Introspector captures the token introspection capability exposed by token service.
type Introspector interface {
	IntrospectToken(ctx context.Context, req *token.IntrospectionRequest) (*token.IntrospectionResponse, error)
}

// Service provides RFC7662 introspection behavior for HTTP handlers.
type Service struct {
	introspector Introspector
}

// NewService creates a new introspection service.
func NewService(introspector Introspector) *Service {
	return &Service{introspector: introspector}
}

// IntrospectToken introspects a token and returns RFC7662 response payload.
func (s *Service) IntrospectToken(ctx context.Context, req *token.IntrospectionRequest) (*token.IntrospectionResponse, error) {
	return s.introspector.IntrospectToken(ctx, req)
}
