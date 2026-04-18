package authorization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingAcceptLoginStore struct {
	*mockAuthzStore
	err error
}

func (s *failingAcceptLoginStore) AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error {
	if s.err != nil {
		return s.err
	}
	return s.mockAuthzStore.AcceptLoginChallenge(ctx, id, identityID, sessionID)
}

func TestStartAuthorizationAdditionalValidationBranches(t *testing.T) {
	ctx := context.Background()

	svc := NewAuthorizationService(&mockAuthzStore{})
	_, err := svc.StartAuthorization(ctx, nil)
	assert.ErrorIs(t, err, ErrInvalidRequest)

	_, err = svc.StartAuthorization(ctx, &AuthorizeRequest{
		ClientID:     "client-1",
		ResponseType: "code",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest)

	_, err = svc.StartAuthorization(ctx, &AuthorizeRequest{
		ClientID:    "client-1",
		RedirectURI: "https://app.example.com/callback",
	})
	assert.ErrorIs(t, err, ErrInvalidRequest)

	responseTypeRestricted := NewAuthorizationService(&mockAuthzStore{
		client: &store.Client{
			ID:            "client-1",
			RedirectURIs:  []string{"https://app.example.com/callback"},
			GrantTypes:    []string{"authorization_code"},
			ResponseTypes: []string{"token"},
			Scopes:        []string{"openid"},
		},
	})
	_, err = responseTypeRestricted.StartAuthorization(ctx, &AuthorizeRequest{
		ClientID:     "client-1",
		RedirectURI:  "https://app.example.com/callback",
		ResponseType: "code",
		Scope:        "openid",
	})
	assert.ErrorIs(t, err, ErrUnauthorizedClient)

	invalidScopeSvc := NewAuthorizationService(&mockAuthzStore{
		client: &store.Client{
			ID:            "client-1",
			RedirectURIs:  []string{"https://app.example.com/callback"},
			GrantTypes:    []string{"authorization_code"},
			ResponseTypes: []string{"code"},
			Scopes:        []string{"openid"},
		},
	})
	_, err = invalidScopeSvc.StartAuthorization(ctx, &AuthorizeRequest{
		ClientID:     "client-1",
		RedirectURI:  "https://app.example.com/callback",
		ResponseType: "code",
		Scope:        "openid email",
	})
	assert.ErrorIs(t, err, ErrInvalidScope)
}

func TestAcceptLoginAndConsentAdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("accept login propagates accept failure", func(t *testing.T) {
		loginErr := errors.New("accept login failed")
		st := &failingAcceptLoginStore{
			mockAuthzStore: &mockAuthzStore{
				loginChallenge: &store.LoginChallenge{
					ID:        "lc-fail",
					ClientID:  "client-1",
					ExpiresAt: time.Now().UTC().Add(time.Minute),
				},
			},
			err: loginErr,
		}
		svc := NewAuthorizationService(st)
		_, err := svc.AcceptLogin(ctx, "lc-fail", "identity-1", "session-1")
		assert.ErrorIs(t, err, loginErr)
	})

	t.Run("accept consent returns server error when session auth context fails", func(t *testing.T) {
		st := &mockAuthzStore{
			getSessionAuthErr: errors.New("session auth lookup failed"),
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-ctx-fail",
				ClientID:  "client-1",
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc-ctx-fail",
				LoginChallengeID: "lc-ctx-fail",
				ClientID:         "client-1",
				IdentityID:       "identity-1",
				SessionID:        "session-1",
				RequestedScopes:  []string{"openid"},
				ExpiresAt:        time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewAuthorizationService(st)
		_, err := svc.AcceptConsent(ctx, "cc-ctx-fail", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, ErrServerError)
	})
}

func TestAuthorizationAuthContextHelperBranches(t *testing.T) {
	fallback := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	challengeAuthTime := fallback.Add(-time.Hour)

	acr, amr, authTime := deriveAuthContext(&store.LoginChallenge{AuthenticatedAt: &challengeAuthTime})
	assert.Equal(t, "aal1", acr)
	assert.Equal(t, []string{"pwd"}, amr)
	assert.Equal(t, challengeAuthTime, authTime)

	acr, amr, authTime = deriveAuthContextFromSession(nil, fallback)
	assert.Equal(t, "aal1", acr)
	assert.Equal(t, []string{"pwd"}, amr)
	assert.Equal(t, fallback, authTime)

	sessionAuthTime := fallback.Add(time.Minute)
	acr, amr, authTime = deriveAuthContextFromSession(&store.SessionAuthContext{
		AAL:             "aal2",
		AuthenticatedAt: sessionAuthTime,
		Methods:         []string{"password", "password", "totp", "webauthn", "social", "unknown"},
	}, fallback)
	assert.Equal(t, "aal2", acr)
	assert.Equal(t, []string{"pwd", "otp", "hwk", "federated"}, amr)
	assert.Equal(t, sessionAuthTime, authTime)

	acr, amr, authTime = deriveAuthContextFromSession(&store.SessionAuthContext{
		AAL:     "unsupported",
		Methods: []string{"unknown"},
	}, fallback)
	assert.Equal(t, "aal1", acr)
	assert.Equal(t, []string{"pwd"}, amr)
	assert.Equal(t, fallback, authTime)

	assert.Equal(t, "aal0", normalizeACR("aal0"))
	assert.Equal(t, "aal1", normalizeACR("aal1"))
	assert.Equal(t, "aal2", normalizeACR("aal2"))
	assert.Equal(t, "", normalizeACR("not-an-aal"))

	assert.Nil(t, dedupeAMR(nil))
	assert.Equal(t, []string{"otp"}, dedupeAMR([]string{"sms", "backup_code"}))
	assert.Equal(t, "federated", mapMethodToAMR("saml"))
	assert.Equal(t, "", mapMethodToAMR("unsupported"))

	assert.True(t, supportsValue([]string{"code"}, "code"))
	assert.False(t, supportsValue([]string{"code"}, "token"))

	require.True(t, isSubset([]string{"openid"}, []string{"openid", "profile"}))
}
