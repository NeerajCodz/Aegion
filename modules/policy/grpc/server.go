package grpc

import (
	"context"
	"fmt"
	"strings"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// RBACStore defines storage operations required for RBAC evaluation.
type RBACStore interface {
	ListRoleIDsByIdentity(ctx context.Context, identityID string) ([]string, error)
	ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]policystore.Permission, error)
}

// Server provides policy evaluation operations for generated gRPC transport handlers.
type Server struct {
	store RBACStore
}

// NewServer creates a new policy server adapter.
func NewServer(store RBACStore) *Server {
	return &Server{store: store}
}

// Check evaluates a single authorization decision.
func (s *Server) Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("check request is required")
	}
	if strings.TrimSpace(req.GetSubject()) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(req.GetResourceType()) == "" {
		return nil, fmt.Errorf("resource_type is required")
	}
	if strings.TrimSpace(req.GetAction()) == "" {
		return nil, fmt.Errorf("action is required")
	}

	identityID := normalizeSubject(req.GetSubject())
	roleIDs, err := s.store.ListRoleIDsByIdentity(ctx, identityID)
	if err != nil {
		return nil, err
	}
	if len(roleIDs) == 0 {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "rbac",
			DenyReason: "rbac_no_matching_permission",
			EvalPath:   []string{"rbac:miss"},
		}, nil
	}

	permissions, err := s.store.ListPermissionsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}

	if hasPermission(permissions, req.GetResourceType(), req.GetAction()) {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "rbac",
			EvalPath:  []string{"rbac:allow"},
		}, nil
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "rbac",
		DenyReason: "rbac_no_matching_permission",
		EvalPath:   []string{"rbac:miss"},
	}, nil
}

// BatchCheck evaluates multiple authorization decisions.
func (s *Server) BatchCheck(ctx context.Context, req *policypb.BatchCheckRequest) (*policypb.BatchCheckResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("batch check request is required")
	}

	results := make([]*policypb.CheckResponse, 0, len(req.GetChecks()))
	for _, check := range req.GetChecks() {
		res, err := s.Check(ctx, check)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}

	return &policypb.BatchCheckResponse{Results: results}, nil
}

func normalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(subject, "user:") {
		return strings.TrimPrefix(subject, "user:")
	}
	return subject
}

func hasPermission(perms []policystore.Permission, resourceType, action string) bool {
	resourceType = strings.TrimSpace(resourceType)
	action = strings.TrimSpace(action)

	for _, p := range perms {
		if p.ResourceType == resourceType && p.Action == action {
			return true
		}
	}
	for _, p := range perms {
		if p.ResourceType == resourceType && p.Action == "*" {
			return true
		}
	}
	for _, p := range perms {
		if p.ResourceType == "*" && p.Action == action {
			return true
		}
	}
	for _, p := range perms {
		if p.ResourceType == "*" && p.Action == "*" {
			return true
		}
	}

	return false
}
