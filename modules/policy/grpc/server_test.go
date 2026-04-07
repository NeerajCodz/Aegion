package grpc

import (
	"context"
	"errors"
	"testing"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policystore "github.com/aegion/aegion/modules/policy/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRBACStore struct {
	roleIDs     []string
	permissions []policystore.Permission
	rolesErr    error
	permsErr    error

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
	}{
		{
			name: "exact match",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "read"},
			},
			resource:    "documents",
			action:      "read",
			wantAllowed: true,
		},
		{
			name: "resource wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "*", Action: "read"},
			},
			resource:    "documents",
			action:      "read",
			wantAllowed: true,
		},
		{
			name: "action wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "*"},
			},
			resource:    "documents",
			action:      "delete",
			wantAllowed: true,
		},
		{
			name: "global wildcard",
			permissions: []policystore.Permission{
				{ResourceType: "*", Action: "*"},
			},
			resource:    "documents",
			action:      "delete",
			wantAllowed: true,
		},
		{
			name: "no match",
			permissions: []policystore.Permission{
				{ResourceType: "documents", Action: "read"},
			},
			resource:    "documents",
			action:      "write",
			wantAllowed: false,
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
			assert.Equal(t, "rbac", resp.GetModelUsed())
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
