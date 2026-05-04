package device

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDeviceStore struct {
	client             *store.Client
	deviceCode         *store.DeviceCode
	deviceCodeByUser   *store.DeviceCode
	getClientErr       error
	createErr          error
	getCodeErr         error
	getByUserErr       error
	approveErr         error
	denyErr            error
	lastCreated        *store.DeviceCode
	lastApprovedCode   string
	lastApprovedUser   string
	lastApprovedScopes []string
	lastDeniedCode     string
	lastUsedCode       string
	usedErr            error
}

func (m *mockDeviceStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.getClientErr != nil {
		return nil, m.getClientErr
	}
	if m.client != nil {
		return m.client, nil
	}
	return &store.Client{ID: id}, nil
}

func (m *mockDeviceStore) CreateDeviceCode(ctx context.Context, dc *store.DeviceCode) error {
	m.lastCreated = dc
	return m.createErr
}

func (m *mockDeviceStore) GetDeviceCode(ctx context.Context, deviceCode string) (*store.DeviceCode, error) {
	if m.getCodeErr != nil {
		return nil, m.getCodeErr
	}
	if m.deviceCode == nil {
		return nil, store.ErrNotFound
	}
	return m.deviceCode, nil
}

func (m *mockDeviceStore) GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*store.DeviceCode, error) {
	if m.getByUserErr != nil {
		return nil, m.getByUserErr
	}
	if m.deviceCodeByUser == nil {
		return nil, store.ErrNotFound
	}
	return m.deviceCodeByUser, nil
}

func (m *mockDeviceStore) MarkDeviceCodeApproved(ctx context.Context, deviceCode, identityID string, scopes []string) error {
	m.lastApprovedCode = deviceCode
	m.lastApprovedUser = identityID
	m.lastApprovedScopes = scopes
	return m.approveErr
}

func (m *mockDeviceStore) MarkDeviceCodeDenied(ctx context.Context, deviceCode string) error {
	m.lastDeniedCode = deviceCode
	return m.denyErr
}

func (m *mockDeviceStore) MarkDeviceCodeUsed(ctx context.Context, deviceCode string) error {
	m.lastUsedCode = deviceCode
	return m.usedErr
}

func TestDeviceService_RequestDeviceAuthorization(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		st := &mockDeviceStore{client: &store.Client{ID: "client-1", Scopes: []string{"openid", "profile"}}}
		svc := NewDeviceService(st, 10*time.Minute, 7, "https://issuer.example.com/device")

		resp, err := svc.RequestDeviceAuthorization(context.Background(), &DeviceAuthorizationRequest{
			ClientID: "client-1",
			Scope:    "openid profile",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.DeviceCode)
		assert.NotEmpty(t, resp.UserCode)
		assert.Equal(t, "https://issuer.example.com/device", resp.VerificationURI)
		assert.Contains(t, resp.VerificationURIComplete, "user_code=")
		assert.Equal(t, 600, resp.ExpiresIn)
		assert.Equal(t, 7, resp.Interval)
		require.NotNil(t, st.lastCreated)
		assert.Equal(t, []string{"openid", "profile"}, st.lastCreated.Scopes)
		assert.Equal(t, 7, st.lastCreated.Interval)
	})

	t.Run("invalid client and create error", func(t *testing.T) {
		st := &mockDeviceStore{getClientErr: errors.New("missing")}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		_, err := svc.RequestDeviceAuthorization(context.Background(), &DeviceAuthorizationRequest{ClientID: "x"})
		assert.ErrorIs(t, err, ErrInvalidClient)

		st = &mockDeviceStore{
			client: &store.Client{
				ID:     "x",
				Scopes: []string{"openid"},
			},
		}
		svc = NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		_, err = svc.RequestDeviceAuthorization(context.Background(), &DeviceAuthorizationRequest{
			ClientID: "x",
			Scope:    "openid admin",
		})
		assert.ErrorIs(t, err, ErrInvalidScope)
		assert.Nil(t, st.lastCreated)

		st = &mockDeviceStore{
			client:    &store.Client{ID: "x", Scopes: []string{"openid"}},
			createErr: errors.New("db"),
		}
		svc = NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		_, err = svc.RequestDeviceAuthorization(context.Background(), &DeviceAuthorizationRequest{ClientID: "x", Scope: "openid"})
		assert.ErrorIs(t, err, st.createErr)
	})
}

func TestDeviceService_PollDeviceToken(t *testing.T) {
	now := time.Now().UTC()
	identity := "identity-1"

	tests := []struct {
		name      string
		dc        *store.DeviceCode
		getErr    error
		wantError error
	}{
		{name: "not found", getErr: store.ErrNotFound, wantError: ErrInvalidGrant},
		{
			name: "expired",
			dc: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "pending",
				ExpiresAt: now.Add(-time.Second),
			},
			wantError: ErrExpiredToken,
		},
		{
			name: "denied",
			dc: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "denied",
				ExpiresAt: now.Add(time.Minute),
			},
			wantError: ErrAccessDenied,
		},
		{
			name: "slow down",
			dc: &store.DeviceCode{
				ClientID:   "client-1",
				Status:     "pending",
				Interval:   10,
				LastPollAt: ptrTime(now.Add(-3 * time.Second)),
				ExpiresAt:  now.Add(time.Minute),
			},
			wantError: ErrSlowDown,
		},
		{
			name: "authorization pending",
			dc: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "pending",
				ExpiresAt: now.Add(time.Minute),
			},
			wantError: ErrAuthorizationPending,
		},
		{
			name: "approved missing identity",
			dc: &store.DeviceCode{
				ClientID:  "client-1",
				Status:    "approved",
				ExpiresAt: now.Add(time.Minute),
			},
			wantError: ErrInvalidGrant,
		},
		{
			name: "approved success",
			dc: &store.DeviceCode{
				ClientID:   "client-1",
				Status:     "approved",
				IdentityID: &identity,
				ExpiresAt:  now.Add(time.Minute),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &mockDeviceStore{deviceCode: tt.dc, getCodeErr: tt.getErr}
			svc := NewDeviceService(st, 10*time.Minute, 5, "https://issuer/device")
			resp, err := svc.PollDeviceToken(context.Background(), &DeviceTokenRequest{
				DeviceCode: "dc",
				ClientID:   "client-1",
			})
			if tt.wantError != nil {
				assert.ErrorIs(t, err, tt.wantError)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, "approved", resp.Status)
			}
		})
	}

	t.Run("client mismatch", func(t *testing.T) {
		st := &mockDeviceStore{
			deviceCode: &store.DeviceCode{
				ClientID:  "other",
				Status:    "pending",
				ExpiresAt: now.Add(time.Minute),
			},
		}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		_, err := svc.PollDeviceToken(context.Background(), &DeviceTokenRequest{
			DeviceCode: "dc",
			ClientID:   "client-1",
		})
		assert.ErrorIs(t, err, ErrInvalidClient)
	})
}

func TestDeviceService_ApproveAndDeny(t *testing.T) {
	now := time.Now().UTC()

	t.Run("approve success", func(t *testing.T) {
		st := &mockDeviceStore{
			deviceCodeByUser: &store.DeviceCode{
				DeviceCode: "dc-1",
				Status:     "pending",
				ExpiresAt:  now.Add(time.Minute),
			},
		}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err := svc.ApproveDeviceAuthorization(context.Background(), "UCODE", "identity-1", []string{"openid"})
		require.NoError(t, err)
		assert.Equal(t, "dc-1", st.lastApprovedCode)
		assert.Equal(t, "identity-1", st.lastApprovedUser)
		assert.Equal(t, []string{"openid"}, st.lastApprovedScopes)
	})

	t.Run("approve validations", func(t *testing.T) {
		st := &mockDeviceStore{getByUserErr: errors.New("missing")}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err := svc.ApproveDeviceAuthorization(context.Background(), "UCODE", "identity-1", nil)
		assert.ErrorIs(t, err, st.getByUserErr)

		st = &mockDeviceStore{
			deviceCodeByUser: &store.DeviceCode{
				DeviceCode: "dc-1",
				Status:     "pending",
				ExpiresAt:  now.Add(-time.Second),
			},
		}
		svc = NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err = svc.ApproveDeviceAuthorization(context.Background(), "UCODE", "identity-1", nil)
		assert.ErrorIs(t, err, ErrExpiredToken)

		st = &mockDeviceStore{
			deviceCodeByUser: &store.DeviceCode{
				DeviceCode: "dc-1",
				Status:     "approved",
				ExpiresAt:  now.Add(time.Minute),
			},
		}
		svc = NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err = svc.ApproveDeviceAuthorization(context.Background(), "UCODE", "identity-1", nil)
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("deny", func(t *testing.T) {
		st := &mockDeviceStore{
			deviceCodeByUser: &store.DeviceCode{DeviceCode: "dc-2"},
		}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err := svc.DenyDeviceAuthorization(context.Background(), "UCODE")
		require.NoError(t, err)
		assert.Equal(t, "dc-2", st.lastDeniedCode)
	})

	t.Run("consume device code", func(t *testing.T) {
		st := &mockDeviceStore{}
		svc := NewDeviceService(st, time.Minute, 5, "https://issuer/device")
		err := svc.ConsumeDeviceCode(context.Background(), "dc-3")
		require.NoError(t, err)
		assert.Equal(t, "dc-3", st.lastUsedCode)

		st.usedErr = errors.New("mark used failed")
		err = svc.ConsumeDeviceCode(context.Background(), "dc-4")
		assert.ErrorIs(t, err, st.usedErr)
	})
}

func TestDeviceHelpers(t *testing.T) {
	for i := 0; i < 10; i++ {
		code, err := generateUserCode()
		assert.NoError(t, err)
		assert.Len(t, code, 9)
		assert.Equal(t, '-', rune(code[4]))
		for _, ch := range strings.ReplaceAll(code, "-", "") {
			assert.True(t, strings.ContainsRune("BCDFGHJKLMNPQRSTVWXYZ", ch))
		}
	}

	assert.Equal(t, []string{}, parseScopes(""))
	assert.Equal(t, []string{"openid", "profile"}, parseScopes(" openid   profile "))
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
