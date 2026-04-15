package service

import (
	"context"

	tokenservice "github.com/aegion/aegion/modules/oauth2/service/token"
)

// Introspector captures the token introspection capability exposed by token service.
type Introspector interface {
	IntrospectToken(ctx context.Context, req *tokenservice.IntrospectionRequest) (*tokenservice.IntrospectionResponse, error)
}

// Service contains introspection business logic.
type Service struct {
	introspector Introspector
}

// New creates a new introspection service.
func New(introspector Introspector) *Service {
	return &Service{introspector: introspector}
}

// IntrospectToken delegates RFC 7662 token introspection to the configured introspector.
func (s *Service) IntrospectToken(ctx context.Context, req *tokenservice.IntrospectionRequest) (*tokenservice.IntrospectionResponse, error) {
	return s.introspector.IntrospectToken(ctx, req)
}
