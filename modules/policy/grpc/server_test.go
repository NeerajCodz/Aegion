package grpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policystore "github.com/aegion/aegion/modules/policy/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRBACStore struct {
	roleIDs     []string
	permissions []policystore.Permission
	abacRules   []policystore.ABACRule
	rebacTuples []policystore.ReBACTuple
	rolesErr    error
	permsErr    error
	abacErr     error
	rebacErr    error

	lastIdentity string
	lastRoleIDs  []string
}

func (m *mockRBACStore) ListRoleIDsByIdentity(ctx context.Context, identityID string) ([]string, error) {
	m.lastIdentity = identityID
	if m.rolesErr != nil {
		return nil, m.rolesErr
	}
	return m.roleIDs, nil
}

func (m *mockRBACStore) ListPermissionsByRoleIDs(ctx context.Context, roleIDs []string) ([]policystore.Permission, error) {
	m.lastRoleIDs = append([]string(nil), roleIDs...)
	if m.permsErr != nil {
		return nil, m.permsErr
	}
	return m.permissions, nil
}

func (m *mockRBACStore) ListABACRules(ctx context.Context) ([]policystore.ABACRule, error) {
	if m.abacErr != nil {
		return nil, m.abacErr
	}
	return m.abacRules, nil
}

func (m *mockRBACStore) ListReBACTuples(ctx context.Context, namespace, objectID, relation string) ([]policystore.ReBACTuple, error) {
	if m.rebacErr != nil {
		return nil, m.rebacErr
	}
	out := make([]policystore.ReBACTuple, 0, len(m.rebacTuples))
	for _, tpl := range m.rebacTuples {
		if tpl.Namespace == namespace && tpl.ObjectID == objectID && tpl.Relation == relation {
			out = append(out, tpl)
		}
	}
	return out, nil
}

func TestServer_Check_Validation(t *testing.T) {
	s := NewServer(&mockRBACStore{})
	ctx := context.Background()

	_, err := s.Check(ctx, nil)
	assert.ErrorContains(t, err, "check request is required")

	_, err = s.Check(ctx, &policypb.CheckRequest{})
	assert.ErrorContains(t, err, "subject is required")

	_, err = s.Check(ctx, &policypb.CheckRequest{Subject: "user:alice"})
	assert.ErrorContains(t, err, "resource_type is required")

	_, err = s.Check(ctx, &policypb.CheckRequest{Subject: "user:alice", ResourceType: "doc"})
	assert.ErrorContains(t, err, "action is required")
}

func TestServer_Check_RBACDecisions(t *testing.T) {
	tests := []struct {
		name        string
		permissions []policystore.Permission
		resource    string
		action      string
		wantAllowed bool
		wantModel   string
	}{
		{
			name: "exact match",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "read"},
			},
			resource:    "documents",
			action:      "read",
			wantAllowed: true,
			wantModel:   "rbac",
		},
		{
			name: "resource wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "*", Action: "read"},
			},
			resource:    "documents",
			action:      "read",
			wantAllowed: true,
			wantModel:   "rbac",
		},
		{
			name: "action wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "*"},
			},
			resource:    "documents",
			action:      "delete",
			wantAllowed: true,
			wantModel:   "rbac",
		},
		{
			name: "global wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "*", Action: "*"},
			},
			resource:    "documents",
			action:      "delete",
			wantAllowed: true,
			wantModel:   "rbac",
		},
		{
			name: "no match",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "read"},
			},
			resource:    "documents",
			action:      "write",
			wantAllowed: false,
			wantModel:   "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &mockRBACStore{
				roleIDs:     []string{"role-1"},
				permissions: tc.permissions,
			}
			s := NewServer(st)

			resp, err := s.Check(context.Background(), &policypb.CheckRequest{
				Subject:      "user:alice",
				ResourceType: tc.resource,
				Action:       tc.action,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantAllowed, resp.GetAllowed())
			assert.Equal(t, tc.wantModel, resp.GetModelUsed())
			assert.Equal(t, "alice", st.lastIdentity)
		})
	}
}

func TestServer_Check_StoreErrors(t *testing.T) {
	s := NewServer(&mockRBACStore{rolesErr: errors.New("roles down")})
	_, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	})
	assert.ErrorContains(t, err, "roles down")

	s = NewServer(&mockRBACStore{
		roleIDs:  []string{"role-1"},
		permsErr: errors.New("permissions down"),
	})
	_, err = s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	})
	assert.ErrorContains(t, err, "permissions down")

	s = NewServer(&mockRBACStore{abacErr: errors.New("abac down")})
	_, err = s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	})
	assert.ErrorContains(t, err, "abac down")
}

func TestServer_Check_ABACDenyPrecedence(t *testing.T) {
	st := &mockRBACStore{
		roleIDs: []string{"role-1"},
		permissions: []policystore.Permission{
			{ResourceType: "documents", Action: "read"},
		},
		abacRules: []policystore.ABACRule{
			{Name: "block_reads", Expression: `action == "read"`, Priority: 1, Effect: "deny", Enabled: true},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "abac", resp.GetModelUsed())
	assert.Equal(t, "abac_deny_rule_matched", resp.GetDenyReason())
	assert.Equal(t, []string{"abac:deny:block_reads"}, resp.GetEvalPath())
}

func TestServer_Check_ABACAllowAfterRBACMiss(t *testing.T) {
	st := &mockRBACStore{
		roleIDs: []string{"role-1"},
		permissions: []policystore.Permission{
			{ResourceType: "documents", Action: "read"},
		},
		abacRules: []policystore.ABACRule{
			{Name: "allow_write", Expression: `action == "write"`, Priority: 5, Effect: "allow", Enabled: true},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "write",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, "abac", resp.GetModelUsed())
	assert.Equal(t, []string{"abac:deny_miss", "rbac:miss", "abac:allow:allow_write"}, resp.GetEvalPath())
}

func TestServer_Check_ModelOverrideABAC(t *testing.T) {
	st := &mockRBACStore{
		abacRules: []policystore.ABACRule{
			{Name: "allow_docs", Expression: `resource.type == "documents"`, Priority: 1, Effect: "allow", Enabled: true},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
		Model:        "abac",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, "abac", resp.GetModelUsed())
	assert.Equal(t, []string{"abac:deny_miss", "abac:allow:allow_docs"}, resp.GetEvalPath())
	assert.Empty(t, st.lastIdentity)
}

func TestServer_Check_ModelOverrideValidation(t *testing.T) {
	s := NewServer(&mockRBACStore{})
	_, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
		Model:        "unknown",
	})
	assert.ErrorContains(t, err, "unsupported model")
}

func TestServer_Check_CELContextEvaluation(t *testing.T) {
	st := &mockRBACStore{
		abacRules: []policystore.ABACRule{
			{Name: "allow_from_ten", Expression: `request.context.ip.startsWith("10.")`, Priority: 1, Effect: "allow", Enabled: true},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:spec-1",
		ResourceType: "documents",
		Action:       "read",
		Model:        "abac",
		Context: &policypb.Context{
			Ip: "10.1.2.3",
		},
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, "abac", resp.GetModelUsed())
}

func TestServer_Check_CELInvalidExpressionReturnsError(t *testing.T) {
	st := &mockRBACStore{
		abacRules: []policystore.ABACRule{
			{Name: "bad_rule", Expression: `request.context.ip.startsWith(`, Priority: 1, Effect: "allow", Enabled: true},
		},
	}
	s := NewServer(st)

	_, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
		Model:        "abac",
	})
	assert.Error(t, err)
	assert.ErrorContains(t, err, "evaluate ABAC rule")
}

func TestServer_Check_ModelOverrideReBAC(t *testing.T) {
	st := &mockRBACStore{
		rebacTuples: []policystore.ReBACTuple{
			{Namespace: "documents", ObjectID: "spec-1", Relation: "viewer", SubjectID: "user:alice"},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:spec-1",
		ResourceType: "documents",
		Action:       "read",
		Model:        "rebac",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, "rebac", resp.GetModelUsed())
	assert.Equal(t, []string{"rebac:allow"}, resp.GetEvalPath())
}

func TestServer_BatchCheck(t *testing.T) {
	st := &mockRBACStore{
		roleIDs: []string{"role-1"},
		permissions: []policystore.Permission{
			{ResourceType: "documents", Action: "read"},
		},
	}
	s := NewServer(st)

	resp, err := s.BatchCheck(context.Background(), &policypb.BatchCheckRequest{
		Checks: []*policypb.CheckRequest{
			{Subject: "user:alice", ResourceType: "documents", Action: "read"},
			{Subject: "user:alice", ResourceType: "documents", Action: "write"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetResults(), 2)
	assert.True(t, resp.GetResults()[0].GetAllowed())
	assert.False(t, resp.GetResults()[1].GetAllowed())
}

func TestServer_Check_NilStoreFailsSafe(t *testing.T) {
	s := NewServer(nil)

	_, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	})
	assert.ErrorContains(t, err, "policy store is required")
}

func TestServer_Check_RBACAnonymousSubjectDeniedWithoutStoreLookup(t *testing.T) {
	st := &mockRBACStore{rolesErr: errors.New("unexpected lookup")}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "anonymous",
		ResourceType: "documents",
		Action:       "read",
		Model:        "rbac",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "rbac", resp.GetModelUsed())
	assert.Equal(t, []string{"rbac:miss"}, resp.GetEvalPath())
	assert.Empty(t, st.lastIdentity)
}

func TestServer_Check_ReBACSubjectSetWithPrefixedObject(t *testing.T) {
	st := &mockRBACStore{
		rebacTuples: []policystore.ReBACTuple{
			{Namespace: "documents", ObjectID: "spec-1", Relation: "viewer", SubjectID: "group:eng#member"},
			{Namespace: "documents", ObjectID: "group:eng", Relation: "member", SubjectID: "user:alice"},
		},
	}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:spec-1",
		ResourceType: "documents",
		Action:       "read",
		Model:        "rebac",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, "rebac", resp.GetModelUsed())
}

func TestServer_Check_ReBACMaxDepthExceeded(t *testing.T) {
	tuples := make([]policystore.ReBACTuple, 0, 22)
	for i := 0; i < 21; i++ {
		tuples = append(tuples, policystore.ReBACTuple{
			Namespace: "documents",
			ObjectID:  fmt.Sprintf("group:node-%d", i),
			Relation:  "viewer",
			SubjectID: fmt.Sprintf("group:node-%d#viewer", i+1),
		})
	}
	tuples = append(tuples, policystore.ReBACTuple{
		Namespace: "documents",
		ObjectID:  "group:node-21",
		Relation:  "viewer",
		SubjectID: "user:alice",
	})

	st := &mockRBACStore{rebacTuples: tuples}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:group:node-0",
		ResourceType: "documents",
		Action:       "read",
		Model:        "rebac",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "rebac", resp.GetModelUsed())
	assert.Equal(t, "max_depth_exceeded", resp.GetDenyReason())
	assert.Equal(t, []string{"rebac:max_depth_exceeded"}, resp.GetEvalPath())
}

func TestServer_Check_ReBACTraversalLimitExceeded(t *testing.T) {
	tuples := make([]policystore.ReBACTuple, 0, 1105)
	for i := 0; i < 1105; i++ {
		tuples = append(tuples, policystore.ReBACTuple{
			Namespace: "documents",
			ObjectID:  "spec-1",
			Relation:  "viewer",
			SubjectID: fmt.Sprintf("group:fanout-%d#viewer", i),
		})
	}

	st := &mockRBACStore{rebacTuples: tuples}
	s := NewServer(st)

	resp, err := s.Check(context.Background(), &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:spec-1",
		ResourceType: "documents",
		Action:       "read",
		Model:        "rebac",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "rebac", resp.GetModelUsed())
	assert.Equal(t, "traversal_limit_exceeded", resp.GetDenyReason())
	assert.Equal(t, []string{"rebac:traversal_limit_exceeded"}, resp.GetEvalPath())
}

func TestServer_HelperUtilities(t *testing.T) {
	t.Run("splitCSV trims and skips empty segments", func(t *testing.T) {
		assert.Equal(t, []string{}, splitCSV("   "))
		assert.Equal(t, []string{"admin", "editor", "viewer"}, splitCSV(" admin, editor, ,viewer ,,"))
	})

	t.Run("request context IP round-trip", func(t *testing.T) {
		base := context.Background()
		assert.Equal(t, "", extractClientIP(base))

		withEmpty := WithRequestContextIP(base, "   ")
		assert.Equal(t, "", extractClientIP(withEmpty))

		ctx := WithRequestContextIP(base, " [2001:db8::1]:443 ")
		assert.Equal(t, "2001:db8::1", extractClientIP(ctx))
	})

	t.Run("namespace and relation helpers", func(t *testing.T) {
		ns, obj := parseNamespaceAndObject("documents:spec-1", "")
		assert.Equal(t, "documents", ns)
		assert.Equal(t, "spec-1", obj)

		ns, obj = parseNamespaceAndObject("spec-1", "documents")
		assert.Equal(t, "documents", ns)
		assert.Equal(t, "spec-1", obj)

		ns, obj = parseNamespaceAndObject("", "documents")
		assert.Equal(t, "", ns)
		assert.Equal(t, "", obj)

		assert.Equal(t, "viewer", actionToRelation(" READ "))
		assert.Equal(t, "editor", actionToRelation("update"))
		assert.Equal(t, "owner", actionToRelation("delete"))
		assert.Equal(t, "owner", actionToRelation("owner"))
		assert.Equal(t, "custom", actionToRelation("custom"))

		setObj, setRel, ok := parseSubjectSet("group:eng#member")
		assert.True(t, ok)
		assert.Equal(t, "group:eng", setObj)
		assert.Equal(t, "member", setRel)

		_, _, ok = parseSubjectSet("group:eng# ")
		assert.False(t, ok)
		_, _, ok = parseSubjectSet(" #member")
		assert.False(t, ok)

		_, _, ok = parseSubjectSet("invalid-subject-set")
		assert.False(t, ok)
	})
}

func TestServer_ABACCompilerAndEvaluationHelpers(t *testing.T) {
	s := NewServer(&mockRBACStore{})
	req := &policypb.CheckRequest{
		Subject:      "user:alice",
		Resource:     "documents:spec-1",
		ResourceType: "documents",
		Action:       "read",
		Context: &policypb.Context{
			Extra: map[string]string{
				"subject_roles": "admin, editor",
			},
		},
	}

	activation, err := buildABACActivation(WithRequestContextIP(context.Background(), "10.0.0.5:443"), req)
	require.NoError(t, err)
	requestMap, ok := activation["request"].(map[string]any)
	require.True(t, ok)
	contextMap, ok := requestMap["context"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "10.0.0.5", contextMap["ip"])

	subjectMap, ok := activation["subject"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, subjectMap, "roles")
	assert.Equal(t, []string{}, subjectMap["roles"])

	_, err = s.compileABACExpression(" ")
	assert.ErrorContains(t, err, "empty ABAC expression")

	_, err = s.compileABACExpression(`"nope"`)
	assert.ErrorContains(t, err, "must return bool")

	ast1, err := s.compileABACExpression(`action == "read"`)
	require.NoError(t, err)
	ast2, err := s.compileABACExpression(`action == "read"`)
	require.NoError(t, err)
	assert.Same(t, ast1, ast2)

	matched, err := s.evaluateABACExpression(WithRequestContextIP(context.Background(), "10.0.0.5"), req, `request.context.ip.startsWith("10.")`)
	require.NoError(t, err)
	assert.True(t, matched)

	_, err = s.evaluateABACExpression(context.Background(), req, `request.context.extra["missing"].startsWith("10.")`)
	assert.Error(t, err)
}

func TestServer_EvaluateABACOnlyPaths(t *testing.T) {
	s := NewServer(&mockRBACStore{})
	req := &policypb.CheckRequest{
		Subject:      "user:alice",
		ResourceType: "documents",
		Action:       "read",
	}

	resp, err := s.evaluateABACOnly(context.Background(), req, []policystore.ABACRule{
		{Name: "deny_reads", Expression: `action == "read"`, Effect: "deny", Enabled: true},
		{Name: "allow_reads", Expression: `action == "read"`, Effect: "allow", Enabled: true},
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "abac_deny_rule_matched", resp.GetDenyReason())
	assert.Equal(t, []string{"abac:deny:deny_reads"}, resp.GetEvalPath())

	resp, err = s.evaluateABACOnly(context.Background(), req, []policystore.ABACRule{
		{Name: "allow_reads", Expression: `action == "read"`, Effect: "allow", Enabled: true},
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed())
	assert.Equal(t, []string{"abac:deny_miss", "abac:allow:allow_reads"}, resp.GetEvalPath())

	resp, err = s.evaluateABACOnly(context.Background(), req, []policystore.ABACRule{})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	assert.Equal(t, "abac_no_matching_rule", resp.GetDenyReason())
	assert.Equal(t, []string{"abac:deny_miss", "abac:allow_miss"}, resp.GetEvalPath())

	_, err = s.evaluateABACOnly(context.Background(), req, []policystore.ABACRule{
		{Name: "broken", Expression: `request.context.ip.startsWith(`, Effect: "allow", Enabled: true},
	})
	assert.ErrorContains(t, err, "evaluate ABAC rule")

	_, err = s.evaluateABACOnly(context.Background(), req, []policystore.ABACRule{
		{Name: "broken_deny", Expression: `request.context.ip.startsWith(`, Effect: "deny", Enabled: true},
	})
	assert.ErrorContains(t, err, "evaluate ABAC rule")
}

func TestServer_BatchCheck_Errors(t *testing.T) {
	s := NewServer(&mockRBACStore{})

	_, err := s.BatchCheck(context.Background(), nil)
	assert.ErrorContains(t, err, "batch check request is required")

	_, err = s.BatchCheck(context.Background(), &policypb.BatchCheckRequest{
		Checks: []*policypb.CheckRequest{
			{Subject: "user:alice", ResourceType: "documents", Action: "read"},
			{},
		},
	})
	assert.ErrorContains(t, err, "subject is required")
}

func TestServer_EvaluateReBAC_AdditionalBranches(t *testing.T) {
	t.Run("invalid resource fails closed", func(t *testing.T) {
		s := NewServer(&mockRBACStore{})
		resp, err := s.evaluateReBAC(context.Background(), &policypb.CheckRequest{
			Subject:      "user:alice",
			ResourceType: "documents",
			Action:       "read",
		})
		require.NoError(t, err)
		assert.False(t, resp.GetAllowed())
		assert.Equal(t, "rebac_invalid_resource", resp.GetDenyReason())
	})

	t.Run("store error propagates", func(t *testing.T) {
		s := NewServer(&mockRBACStore{rebacErr: errors.New("rebac store failed")})
		_, err := s.evaluateReBAC(context.Background(), &policypb.CheckRequest{
			Subject:      "user:alice",
			Resource:     "documents:spec-1",
			ResourceType: "documents",
			Action:       "read",
		})
		assert.ErrorContains(t, err, "rebac store failed")
	})

	t.Run("no matching tuple returns deterministic miss", func(t *testing.T) {
		s := NewServer(&mockRBACStore{})
		resp, err := s.evaluateReBAC(context.Background(), &policypb.CheckRequest{
			Subject:      "user:alice",
			Resource:     "documents:spec-404",
			ResourceType: "documents",
			Action:       "read",
		})
		require.NoError(t, err)
		assert.False(t, resp.GetAllowed())
		assert.Equal(t, "rebac_no_matching_tuple", resp.GetDenyReason())
		assert.Equal(t, []string{"rebac:miss"}, resp.GetEvalPath())
	})
}

func TestServer_Check_DefaultModelReBACTransitions(t *testing.T) {
	t.Run("default model allows via ReBAC", func(t *testing.T) {
		st := &mockRBACStore{
			rebacTuples: []policystore.ReBACTuple{
				{Namespace: "documents", ObjectID: "spec-1", Relation: "viewer", SubjectID: "user:alice"},
			},
		}
		s := NewServer(st)

		resp, err := s.Check(context.Background(), &policypb.CheckRequest{
			Subject:      "user:alice",
			Resource:     "documents:spec-1",
			ResourceType: "documents",
			Action:       "read",
		})
		require.NoError(t, err)
		assert.True(t, resp.GetAllowed())
		assert.Equal(t, "rebac", resp.GetModelUsed())
		assert.Equal(t, []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:allow"}, resp.GetEvalPath())
	})

	t.Run("default model maps ReBAC max depth to deterministic deny", func(t *testing.T) {
		tuples := make([]policystore.ReBACTuple, 0, 22)
		for i := 0; i < 21; i++ {
			tuples = append(tuples, policystore.ReBACTuple{
				Namespace: "documents",
				ObjectID:  fmt.Sprintf("group:node-%d", i),
				Relation:  "viewer",
				SubjectID: fmt.Sprintf("group:node-%d#viewer", i+1),
			})
		}
		tuples = append(tuples, policystore.ReBACTuple{
			Namespace: "documents",
			ObjectID:  "group:node-21",
			Relation:  "viewer",
			SubjectID: "user:alice",
		})

		s := NewServer(&mockRBACStore{rebacTuples: tuples})
		resp, err := s.Check(context.Background(), &policypb.CheckRequest{
			Subject:      "user:alice",
			Resource:     "documents:group:node-0",
			ResourceType: "documents",
			Action:       "read",
		})
		require.NoError(t, err)
		assert.False(t, resp.GetAllowed())
		assert.Equal(t, "rebac", resp.GetModelUsed())
		assert.Equal(t, "max_depth_exceeded", resp.GetDenyReason())
		assert.Equal(t, []string{"abac:deny_miss", "rbac:miss", "abac:allow_miss", "rebac:max_depth_exceeded"}, resp.GetEvalPath())
	})
}
