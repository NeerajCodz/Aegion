package grpc

import (
	"context"
	"fmt"
	"strings"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// PolicyStore defines storage operations required for policy evaluation.
type PolicyStore interface {
	ListRoleIDsByIdentity(ctx context.Context, identityID string) ([]string, error)
	ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]policystore.Permission, error)
	ListABACRules(ctx context.Context) ([]policystore.ABACRule, error)
}

// Server provides policy evaluation operations for generated gRPC transport handlers.
type Server struct {
	store PolicyStore
}

// NewServer creates a new policy server adapter.
func NewServer(store PolicyStore) *Server {
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

	model := normalizeModel(req.GetModel())
	if model != "" && model != "rbac" && model != "abac" && model != "rebac" {
		return nil, fmt.Errorf("unsupported model %q", req.GetModel())
	}

	if model == "rbac" {
		return s.evaluateRBAC(ctx, req)
	}

	if model == "rebac" {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "rebac",
			DenyReason: "rebac_unimplemented",
			EvalPath:   []string{"rebac:unimplemented"},
		}, nil
	}

	rules, err := s.store.ListABACRules(ctx)
	if err != nil {
		return nil, err
	}

	if model == "abac" {
		return evaluateABACOnly(rules, req), nil
	}

	if denied, ruleName := firstMatchedABACRule(rules, req, "deny"); denied {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "abac",
			DenyReason: "abac_deny_rule_matched",
			EvalPath:   []string{"abac:deny:" + ruleName},
		}, nil
	}

	rbacRes, err := s.evaluateRBAC(ctx, req)
	if err != nil {
		return nil, err
	}
	if rbacRes.GetAllowed() {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "rbac",
			EvalPath:  []string{"abac:deny_miss", "rbac:allow"},
		}, nil
	}

	if allowed, ruleName := firstMatchedABACRule(rules, req, "allow"); allowed {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "abac",
			EvalPath:  []string{"abac:deny_miss", "rbac:miss", "abac:allow:" + ruleName},
		}, nil
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "default",
		DenyReason: "default_deny",
		EvalPath:   []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:unimplemented", "default:deny"},
	}, nil
}

func (s *Server) evaluateRBAC(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
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

	return &policypb.CheckResponse{Allowed: false, ModelUsed: "rbac", DenyReason: "rbac_no_matching_permission", EvalPath: []string{"rbac:miss"}}, nil
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

func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func evaluateABACOnly(rules []policystore.ABACRule, req *policypb.CheckRequest) *policypb.CheckResponse {
	if denied, ruleName := firstMatchedABACRule(rules, req, "deny"); denied {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "abac",
			DenyReason: "abac_deny_rule_matched",
			EvalPath:   []string{"abac:deny:" + ruleName},
		}
	}

	if allowed, ruleName := firstMatchedABACRule(rules, req, "allow"); allowed {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "abac",
			EvalPath:  []string{"abac:deny_miss", "abac:allow:" + ruleName},
		}
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "abac",
		DenyReason: "abac_no_matching_rule",
		EvalPath:   []string{"abac:deny_miss", "abac:allow_miss"},
	}
}

func firstMatchedABACRule(rules []policystore.ABACRule, req *policypb.CheckRequest, effect string) (bool, string) {
	effect = strings.ToLower(strings.TrimSpace(effect))
	for _, rule := range rules {
		if strings.ToLower(strings.TrimSpace(rule.Effect)) != effect {
			continue
		}
		if evaluateABACExpression(rule.Expression, req) {
			return true, rule.Name
		}
	}
	return false, ""
}

func evaluateABACExpression(expr string, req *policypb.CheckRequest) bool {
	normalized := strings.ToLower(strings.TrimSpace(expr))
	switch normalized {
	case "true":
		return true
	case "false", "":
		return false
	}

	if strings.HasPrefix(normalized, `action == "`) && strings.HasSuffix(normalized, `"`) {
		want := expr[len(`action == "`) : len(expr)-1]
		return strings.EqualFold(strings.TrimSpace(req.GetAction()), strings.TrimSpace(want))
	}

	if strings.HasPrefix(normalized, `resource.type == "`) && strings.HasSuffix(normalized, `"`) {
		want := expr[len(`resource.type == "`) : len(expr)-1]
		return strings.EqualFold(strings.TrimSpace(req.GetResourceType()), strings.TrimSpace(want))
	}

	if strings.HasPrefix(normalized, `request.context.ip.startswith("`) && strings.HasSuffix(normalized, `")`) {
		prefix := expr[len(`request.context.ip.startsWith("`) : len(expr)-2]
		ctx := req.GetContext()
		if ctx == nil {
			return false
		}
		return strings.HasPrefix(ctx.GetIp(), prefix)
	}

	return false
}
