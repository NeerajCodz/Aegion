package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/types"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policystore "github.com/aegion/aegion/modules/policy/store"
)

// PolicyStore defines storage operations required for policy evaluation.
type PolicyStore interface {
	ListRoleIDsByIdentity(ctx context.Context, identityID string) ([]string, error)
	ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]policystore.Permission, error)
	ListABACRules(ctx context.Context) ([]policystore.ABACRule, error)
	ListReBACTuples(ctx context.Context, namespace, objectID, relation string) ([]policystore.ReBACTuple, error)
}

// Server provides policy evaluation operations for generated gRPC transport handlers.
type Server struct {
	store PolicyStore
	env   *cel.Env
	cache sync.Map
}

var errReBACMaxDepthExceeded = errors.New("rebac max depth exceeded")
var errReBACTraversalLimitExceeded = errors.New("rebac traversal limit exceeded")

// NewServer creates a new policy server adapter.
func NewServer(store PolicyStore) *Server {
	env, err := cel.NewEnv(
		cel.VariableDecls(
			decls.NewVariable("subject", types.NewMapType(types.StringType, types.DynType)),
			decls.NewVariable("resource", types.NewMapType(types.StringType, types.DynType)),
			decls.NewVariable("action", types.StringType),
			decls.NewVariable("request", types.NewMapType(types.StringType, types.DynType)),
		),
	)
	if err != nil {
		panic("failed to initialize CEL env: " + err.Error())
	}

	return &Server{store: store, env: env}
}

// Check evaluates a single authorization decision.
func (s *Server) Check(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("policy store is required")
	}
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
	if model == "" {
		model = "default"
	}
	if model != "default" && model != "rbac" && model != "abac" && model != "rebac" {
		return nil, fmt.Errorf("unsupported model %q", req.GetModel())
	}

	if model == "rebac" {
		return s.evaluateReBAC(ctx, req)
	}

	rules, err := s.store.ListABACRules(ctx)
	if err != nil {
		return nil, err
	}

	if model == "abac" {
		return s.evaluateABACOnly(ctx, req, rules)
	}


	if denied, ruleName, err := s.firstMatchedABACRule(ctx, req, rules, "deny"); err != nil {
		return nil, err
	} else if denied {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "abac",
			DenyReason: "abac_deny_rule_matched",
			EvalPath:   []string{"abac:deny:" + ruleName},
		}, nil
	}

	if model == "rbac" {
		return s.evaluateRBAC(ctx, req)
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

	if allowed, ruleName, err := s.firstMatchedABACRule(ctx, req, rules, "allow"); err != nil {
		return nil, err
	} else if allowed {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "abac",
			EvalPath:  []string{"abac:deny_miss", "rbac:miss", "abac:allow:" + ruleName},
		}, nil
	}

	rebacRes, err := s.evaluateReBAC(ctx, req)
	if err != nil {
		return nil, err
	}
	if rebacRes.GetDenyReason() == "max_depth_exceeded" {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "rebac",
			DenyReason: "max_depth_exceeded",
			EvalPath:   []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:max_depth_exceeded"},
		}, nil
	}
	if rebacRes.GetAllowed() {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "rebac",
			EvalPath:  []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:allow"},
		}, nil
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "default",
		DenyReason: "default_deny",
		EvalPath:   []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:miss", "default:deny"},
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

func (s *Server) evaluateRBAC(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	identityID := normalizeSubject(req.GetSubject())
	if !isRBACIdentity(identityID) {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "rbac",
			DenyReason: "rbac_no_matching_permission",
			EvalPath:   []string{"rbac:miss"},
		}, nil
	}

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

func (s *Server) evaluateABACOnly(ctx context.Context, req *policypb.CheckRequest, rules []policystore.ABACRule) (*policypb.CheckResponse, error) {
	if denied, ruleName, err := s.firstMatchedABACRule(ctx, req, rules, "deny"); err != nil {
		return nil, err
	} else if denied {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "abac",
			DenyReason: "abac_deny_rule_matched",
			EvalPath:   []string{"abac:deny:" + ruleName},
		}, nil
	}

	if allowed, ruleName, err := s.firstMatchedABACRule(ctx, req, rules, "allow"); err != nil {
		return nil, err
	} else if allowed {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "abac",
			EvalPath:  []string{"abac:deny_miss", "abac:allow:" + ruleName},
		}, nil
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "abac",
		DenyReason: "abac_no_matching_rule",
		EvalPath:   []string{"abac:deny_miss", "abac:allow_miss"},
	}, nil
}

func (s *Server) firstMatchedABACRule(ctx context.Context, req *policypb.CheckRequest, rules []policystore.ABACRule, effect string) (bool, string, error) {
	effect = normalizeModel(effect)
	for _, rule := range rules {
		if normalizeModel(rule.Effect) != effect {
			continue
		}
		matched, err := s.evaluateABACExpression(ctx, req, rule.Expression)
		if err != nil {
			return false, "", fmt.Errorf("evaluate ABAC rule %q: %w", rule.Name, err)
		}
		if matched {
			return true, rule.Name, nil
		}
	}
	return false, "", nil
}

func (s *Server) evaluateABACExpression(ctx context.Context, req *policypb.CheckRequest, expression string) (bool, error) {
	ast, err := s.compileABACExpression(expression)
	if err != nil {
		return false, err
	}
	prg, err := s.env.Program(ast)
	if err != nil {
		return false, err
	}

	activation, err := buildABACActivation(ctx, req)
	if err != nil {
		return false, err
	}

	out, _, err := prg.Eval(activation)
	if err != nil {
		return false, err
	}

	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("ABAC expression must evaluate to bool")
	}
	return b, nil
}

func (s *Server) compileABACExpression(expression string) (*cel.Ast, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("empty ABAC expression")
	}

	if cached, ok := s.cache.Load(expression); ok {
		if ast, ok := cached.(*cel.Ast); ok {
			return ast, nil
		}
	}

	ast, issues := s.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() == nil || ast.OutputType().String() != "bool" {
		return nil, fmt.Errorf("ABAC expression must return bool")
	}

	s.cache.Store(expression, ast)
	return ast, nil
}

func buildABACActivation(ctx context.Context, req *policypb.CheckRequest) (map[string]any, error) {
	requestContext := req.GetContext()
	contextExtra := map[string]any{}
	if requestContext != nil && requestContext.GetExtra() != nil {
		for k, v := range requestContext.GetExtra() {
			contextExtra[k] = v
		}
	}

	clientIP := ""
	tenantID := ""
	if requestContext != nil {
		clientIP = strings.TrimSpace(requestContext.GetIp())
		tenantID = strings.TrimSpace(requestContext.GetTenantId())
	}
	if clientIP == "" {
		clientIP = extractClientIP(ctx)
	}

	activation := map[string]any{
		"subject": map[string]any{
			"id":    normalizeSubject(req.GetSubject()),
			"roles": []string{},
		},
		"resource": map[string]any{
			"id":   strings.TrimSpace(req.GetResource()),
			"type": strings.TrimSpace(req.GetResourceType()),
		},
		"action": strings.TrimSpace(req.GetAction()),
		"request": map[string]any{
			"context": map[string]any{
				"ip":        clientIP,
				"tenant_id": tenantID,
				"extra":     contextExtra,
			},
		},
	}

	return activation, nil
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

type requestContextIPKey struct{}

// WithRequestContextIP attaches a best-effort client IP for policy context evaluation.
func WithRequestContextIP(ctx context.Context, ip string) context.Context {
	if strings.TrimSpace(ip) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestContextIPKey{}, strings.TrimSpace(ip))
}

func extractClientIP(ctx context.Context) string {
	v := ctx.Value(requestContextIPKey{})
	ip, _ := v.(string)
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	host := ip
	if parsedHost, _, err := net.SplitHostPort(ip); err == nil {
		host = parsedHost
	}
	return strings.Trim(host, "[]")
}

func (s *Server) evaluateReBAC(ctx context.Context, req *policypb.CheckRequest) (*policypb.CheckResponse, error) {
	namespace, objectID := parseNamespaceAndObject(req.GetResource(), req.GetResourceType())
	relation := actionToRelation(req.GetAction())
	if namespace == "" || objectID == "" || relation == "" {
		return &policypb.CheckResponse{
			Allowed:    false,
			ModelUsed:  "rebac",
			DenyReason: "rebac_invalid_resource",
			EvalPath:   []string{"rebac:invalid_resource"},
		}, nil
	}

	if allowed, err := s.expandReBAC(ctx, namespace, objectID, relation, normalizeSubject(req.GetSubject()), 20); err != nil {
		if errors.Is(err, errReBACMaxDepthExceeded) {
			return &policypb.CheckResponse{
				Allowed:    false,
				ModelUsed:  "rebac",
				DenyReason: "max_depth_exceeded",
				EvalPath:   []string{"rebac:max_depth_exceeded"},
			}, nil
		}
		if errors.Is(err, errReBACTraversalLimitExceeded) {
			return &policypb.CheckResponse{
				Allowed:    false,
				ModelUsed:  "rebac",
				DenyReason: "traversal_limit_exceeded",
				EvalPath:   []string{"rebac:traversal_limit_exceeded"},
			}, nil
		}
		return nil, err
	} else if allowed {
		return &policypb.CheckResponse{
			Allowed:   true,
			ModelUsed: "rebac",
			EvalPath:  []string{"rebac:allow"},
		}, nil
	}

	return &policypb.CheckResponse{
		Allowed:    false,
		ModelUsed:  "rebac",
		DenyReason: "rebac_no_matching_tuple",
		EvalPath:   []string{"rebac:miss"},
	}, nil
}

func parseNamespaceAndObject(resource, resourceType string) (string, string) {
	resource = strings.TrimSpace(resource)
	resourceType = strings.TrimSpace(resourceType)
	if resource != "" {
		if idx := strings.Index(resource, ":"); idx > 0 && idx < len(resource)-1 {
			return strings.TrimSpace(resource[:idx]), strings.TrimSpace(resource[idx+1:])
		}
	}
	if resourceType != "" && resource != "" {
		return resourceType, resource
	}
	return "", ""
}

func actionToRelation(action string) string {
	action = normalizeModel(action)
	switch action {
	case "read", "view", "get", "list":
		return "viewer"
	case "write", "update", "edit", "patch":
		return "editor"
	case "delete", "remove":
		return "owner"
	case "owner":
		return "owner"
	default:
		return action
	}
}

func (s *Server) expandReBAC(ctx context.Context, namespace, objectID, relation, subject string, maxDepth int) (bool, error) {
	const rebacMaxVisitedNodes = 1000

	type node struct {
		objectID string
		relation string
		depth    int
	}

	queue := []node{{objectID: objectID, relation: relation, depth: 0}}
	visited := map[string]struct{}{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth > maxDepth {
			return false, errReBACMaxDepthExceeded
		}

		key := namespace + "|" + current.objectID + "|" + current.relation
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}
		if len(visited) > rebacMaxVisitedNodes {
			return false, errReBACTraversalLimitExceeded
		}

		tuples, err := s.store.ListReBACTuples(ctx, namespace, current.objectID, current.relation)
		if err != nil {
			return false, err
		}

		for _, tpl := range tuples {
			subjectID := strings.TrimSpace(tpl.SubjectID)
			if subjectID == "" {
				continue
			}

			if strings.EqualFold(subjectID, subject) || strings.EqualFold(subjectID, "user:"+subject) {
				return true, nil
			}

			if setObj, setRel, ok := parseSubjectSet(subjectID); ok {
				queue = append(queue, node{objectID: setObj, relation: setRel, depth: current.depth + 1})
			}
		}
	}

	return false, nil
}

func parseSubjectSet(subjectID string) (string, string, bool) {
	parts := strings.Split(subjectID, "#")
	if len(parts) != 2 {
		return "", "", false
	}
	left := strings.TrimSpace(parts[0])
	rel := strings.TrimSpace(parts[1])
	if left == "" || rel == "" {
		return "", "", false
	}
	return left, rel, true
}

func normalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(subject, "user:") {
		return strings.TrimPrefix(subject, "user:")
	}
	return subject
}

func hasPermission(perms []policystore.Permission, resourceType, action string) bool {
	resourceType = normalizePermissionToken(resourceType)
	action = normalizePermissionToken(action)

	for _, p := range perms {
		if normalizePermissionToken(p.ResourceType) == resourceType && normalizePermissionToken(p.Action) == action {
			return true
		}
	}
	for _, p := range perms {
		if normalizePermissionToken(p.ResourceType) == resourceType && normalizePermissionToken(p.Action) == "*" {
			return true
		}
	}
	for _, p := range perms {
		if normalizePermissionToken(p.ResourceType) == "*" && normalizePermissionToken(p.Action) == action {
			return true
		}
	}
	for _, p := range perms {
		if normalizePermissionToken(p.ResourceType) == "*" && normalizePermissionToken(p.Action) == "*" {
			return true
		}
	}

	return false
}

func isRBACIdentity(identityID string) bool {
	identityID = strings.TrimSpace(identityID)
	return identityID != "" && !strings.EqualFold(identityID, "anonymous")
}

func normalizePermissionToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}
