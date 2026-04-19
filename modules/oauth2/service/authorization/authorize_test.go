// Package authorization tests
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

// Mock store for testing
type mockAuthzStore struct {
	client            *store.Client
	clientErr         error
	authCode          *store.AuthCode
	sessionAuthCtx    *store.SessionAuthContext
	getSessionAuthErr error
	getLoginErr       error
	getConsentErr     error
	loginChallenge    *store.LoginChallenge
	consentChallenge  *store.ConsentChallenge
	consentSession    *store.ConsentSession
	createLoginErr    error
	createConsentErr  error
	acceptConsentErr  error
	rejectConsentErr  error
	createSessionErr  error
	createAuthCodeErr error
}

func (m *mockAuthzStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.clientErr != nil {
		return nil, m.clientErr
	}
	if m.client != nil {
		return m.client, nil
	}
	return &store.Client{
		ID:                      id,
		Name:                    "Test Client",
		RedirectURIs:            []string{"https://app.example.com/callback"},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		Scopes:                  []string{"openid", "profile"},
		TokenEndpointAuthMethod: "client_secret_post",
		RequirePKCE:             false,
	}, nil
}

func (m *mockAuthzStore) CreateAuthCode(ctx context.Context, code *store.AuthCode) error {
	m.authCode = code
	return m.createAuthCodeErr
}

func (m *mockAuthzStore) GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error) {
	if m.authCode != nil && m.authCode.Code == code {
		return m.authCode, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockAuthzStore) MarkAuthCodeUsed(ctx context.Context, code string) error {
	if m.authCode != nil && m.authCode.Code == code {
		m.authCode.Used = true
		return nil
	}
	return store.ErrNotFound
}

func (m *mockAuthzStore) GetSessionAuthContext(ctx context.Context, sessionID string) (*store.SessionAuthContext, error) {
	if m.getSessionAuthErr != nil {
		return nil, m.getSessionAuthErr
	}
	if m.sessionAuthCtx != nil {
		return m.sessionAuthCtx, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockAuthzStore) CreateLoginChallenge(ctx context.Context, challenge *store.LoginChallenge) error {
	m.loginChallenge = challenge
	return m.createLoginErr
}

func (m *mockAuthzStore) GetLoginChallenge(ctx context.Context, id string) (*store.LoginChallenge, error) {
	if m.getLoginErr != nil {
		return nil, m.getLoginErr
	}
	if m.loginChallenge != nil && m.loginChallenge.ID == id {
		return m.loginChallenge, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockAuthzStore) AcceptLoginChallenge(ctx context.Context, id, identityID, sessionID string) error {
	if m.loginChallenge != nil && m.loginChallenge.ID == id {
		m.loginChallenge.IdentityID = &identityID
		m.loginChallenge.SessionID = &sessionID
		now := time.Now().UTC()
		m.loginChallenge.AuthenticatedAt = &now
		return nil
	}
	return store.ErrNotFound
}

func (m *mockAuthzStore) CreateConsentChallenge(ctx context.Context, challenge *store.ConsentChallenge) error {
	m.consentChallenge = challenge
	return m.createConsentErr
}

func (m *mockAuthzStore) GetConsentChallenge(ctx context.Context, id string) (*store.ConsentChallenge, error) {
	if m.getConsentErr != nil {
		return nil, m.getConsentErr
	}
	if m.consentChallenge != nil && m.consentChallenge.ID == id {
		return m.consentChallenge, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockAuthzStore) AcceptConsentChallenge(ctx context.Context, id string, grantedScopes, grantedAudience []string, remember bool, rememberFor *int) error {
	if m.acceptConsentErr != nil {
		return m.acceptConsentErr
	}
	if m.consentChallenge != nil && m.consentChallenge.ID == id {
		m.consentChallenge.Handled = true
		m.consentChallenge.GrantedScopes = grantedScopes
		return nil
	}
	return store.ErrNotFound
}

func (m *mockAuthzStore) RejectConsentChallenge(ctx context.Context, id, errorCode, errorDesc string) error {
	if m.rejectConsentErr != nil {
		return m.rejectConsentErr
	}
	if m.consentChallenge != nil && m.consentChallenge.ID == id {
		m.consentChallenge.Rejected = true
		return nil
	}
	return store.ErrNotFound
}

func (m *mockAuthzStore) GetConsentSession(ctx context.Context, clientID, identityID string) (*store.ConsentSession, error) {
	if m.consentSession != nil {
		return m.consentSession, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockAuthzStore) CreateConsentSession(ctx context.Context, consent *store.ConsentSession) error {
	m.consentSession = consent
	return m.createSessionErr
}

func TestStartAuthorization(t *testing.T) {
	ctx := context.Background()

	t.Run("ValidRequest", func(t *testing.T) {
		mockStore := &mockAuthzStore{}
		svc := NewAuthorizationService(mockStore)

		req := &AuthorizeRequest{
			ClientID:     "client-123",
			RedirectURI:  "https://app.example.com/callback",
			RequestURL:   "/oauth2/authorize?client_id=client-123&response_type=code",
			ResponseType: "code",
			Scope:        "openid profile",
		}

		resp, err := svc.StartAuthorization(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.LoginChallenge)
		assert.NotEmpty(t, mockStore.loginChallenge)
		assert.Equal(t, req.RequestURL, mockStore.loginChallenge.RequestURL)
	})

	t.Run("InvalidClient", func(t *testing.T) {
		mockStore := &mockAuthzStore{
			clientErr: store.ErrNotFound,
		}
		svc := NewAuthorizationService(mockStore)

		req := &AuthorizeRequest{
			ClientID:     "invalid",
			RedirectURI:  "https://app.example.com/callback",
			ResponseType: "code",
		}

		_, err := svc.StartAuthorization(ctx, req)
		assert.ErrorIs(t, err, ErrUnauthorizedClient)
	})

	t.Run("InvalidRedirectURI", func(t *testing.T) {
		mockStore := &mockAuthzStore{}
		svc := NewAuthorizationService(mockStore)

		req := &AuthorizeRequest{
			ClientID:     "client-123",
			RedirectURI:  "https://evil.com/callback",
			ResponseType: "code",
		}

		_, err := svc.StartAuthorization(ctx, req)
		assert.Error(t, err)
	})

	t.Run("UnsupportedResponseType", func(t *testing.T) {
		mockStore := &mockAuthzStore{}
		svc := NewAuthorizationService(mockStore)

		req := &AuthorizeRequest{
			ClientID:     "client-123",
			RedirectURI:  "https://app.example.com/callback",
			ResponseType: "token",
		}

		_, err := svc.StartAuthorization(ctx, req)
		assert.ErrorIs(t, err, ErrUnsupportedResponseType)
	})

	t.Run("ClientNotAllowedGrantOrResponseType", func(t *testing.T) {
		mockStore := &mockAuthzStore{
			client: &store.Client{
				ID:            "client-123",
				RedirectURIs:  []string{"https://app.example.com/callback"},
				GrantTypes:    []string{"client_credentials"},
				ResponseTypes: []string{"token"},
				Scopes:        []string{"openid"},
			},
		}
		svc := NewAuthorizationService(mockStore)

		_, err := svc.StartAuthorization(ctx, &AuthorizeRequest{
			ClientID:     "client-123",
			RedirectURI:  "https://app.example.com/callback",
			ResponseType: "code",
		})
		assert.ErrorIs(t, err, ErrUnauthorizedClient)
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		svc := NewAuthorizationService(&mockAuthzStore{})
		_, err := svc.StartAuthorization(ctx, &AuthorizeRequest{})
		assert.ErrorIs(t, err, ErrInvalidRequest)
	})

	t.Run("PKCERequired", func(t *testing.T) {
		mockStore := &mockAuthzStore{
			client: &store.Client{
				ID:            "client-123",
				RedirectURIs:  []string{"https://app.example.com/callback"},
				Scopes:        []string{"openid", "profile"},
				ResponseTypes: []string{"code"},
				RequirePKCE:   true,
			},
		}
		svc := NewAuthorizationService(mockStore)

		req := &AuthorizeRequest{
			ClientID:     "client-123",
			RedirectURI:  "https://app.example.com/callback",
			ResponseType: "code",
		}

		_, err := svc.StartAuthorization(ctx, req)
		assert.ErrorIs(t, err, ErrPKCERequired)
	})
}

func TestAcceptLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mockStore := &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc_test123",
				ClientID:  "client-123",
				Scopes:    []string{"openid"},
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			},
		}
		svc := NewAuthorizationService(mockStore)

		resp, err := svc.AcceptLogin(ctx, "lc_test123", "identity-456", "session-789")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ConsentChallenge)
		require.NotNil(t, mockStore.loginChallenge.IdentityID)
		require.NotNil(t, mockStore.loginChallenge.SessionID)
		assert.Equal(t, "identity-456", *mockStore.loginChallenge.IdentityID)
		assert.Equal(t, "session-789", *mockStore.loginChallenge.SessionID)
	})

	t.Run("ChallengeNotFound", func(t *testing.T) {
		mockStore := &mockAuthzStore{}
		svc := NewAuthorizationService(mockStore)

		_, err := svc.AcceptLogin(ctx, "invalid", "identity-456", "session-789")
		assert.Error(t, err)
	})
}

func TestAcceptConsent(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		identityID := "identity-456"
		sessionID := "session-789"
		mockStore := &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc_test123",
				ClientID:  "client-123",
				State:     ptrString("state-123"),
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			},
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc_test123",
				LoginChallengeID: "lc_test123",
				ClientID:         "client-123",
				IdentityID:       identityID,
				SessionID:        sessionID,
				RequestedScopes:  []string{"openid", "profile"},
				Handled:          false,
				ExpiresAt:        time.Now().UTC().Add(10 * time.Minute),
			},
		}
		svc := NewAuthorizationService(mockStore)

		resp, err := svc.AcceptConsent(ctx, "cc_test123", []string{"openid", "profile"}, true, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Code)
		assert.Equal(t, "state-123", resp.State)
	})

	t.Run("DerivesAuthContextFromSession", func(t *testing.T) {
		authTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		mockStore := &mockAuthzStore{
			sessionAuthCtx: &store.SessionAuthContext{
				AAL:             "aal2",
				AuthenticatedAt: authTime,
				Methods:         []string{"password", "totp"},
			},
			loginChallenge: &store.LoginChallenge{
				ID:        "lc_ctx",
				ClientID:  "client-123",
				State:     ptrString("state-ctx"),
				ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			},
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc_ctx",
				LoginChallengeID: "lc_ctx",
				ClientID:         "client-123",
				IdentityID:       "identity-ctx",
				SessionID:        "session-ctx",
				RequestedScopes:  []string{"openid"},
				Handled:          false,
				ExpiresAt:        time.Now().UTC().Add(10 * time.Minute),
			},
		}
		svc := NewAuthorizationService(mockStore)

		resp, err := svc.AcceptConsent(ctx, "cc_ctx", []string{"openid"}, false, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Code)
		require.NotNil(t, mockStore.authCode)
		assert.Equal(t, "aal2", mockStore.authCode.ACR)
		assert.Equal(t, []string{"pwd", "otp"}, mockStore.authCode.AMR)
		assert.Equal(t, authTime, mockStore.authCode.AuthTime)
	})
}

func TestRejectConsent(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mockStore := &mockAuthzStore{
			consentChallenge: &store.ConsentChallenge{
				ID:       "cc_test123",
				ClientID: "client-123",
			},
		}
		svc := NewAuthorizationService(mockStore)

		err := svc.RejectConsent(ctx, "cc_test123", "access_denied", "User denied")
		require.NoError(t, err)
		assert.True(t, mockStore.consentChallenge.Rejected)
	})
}

func TestAuthorizationService_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("StartAuthorization create login failure and bad PKCE method", func(t *testing.T) {
		st := &mockAuthzStore{
			client: &store.Client{
				ID:            "client-1",
				RedirectURIs:  []string{"https://app.example.com/callback"},
				ResponseTypes: []string{"code"},
				Scopes:        []string{"openid"},
				RequirePKCE:   true,
			},
			createLoginErr: errors.New("db down"),
		}
		svc := NewAuthorizationService(st)
		_, err := svc.StartAuthorization(ctx, &AuthorizeRequest{
			ClientID:            "client-1",
			RedirectURI:         "https://app.example.com/callback",
			ResponseType:        "code",
			Scope:               "openid",
			CodeChallenge:       "abc",
			CodeChallengeMethod: "unsupported",
		})
		assert.Error(t, err)

		_, err = svc.StartAuthorization(ctx, &AuthorizeRequest{
			ClientID:            "client-1",
			RedirectURI:         "https://app.example.com/callback",
			ResponseType:        "code",
			Scope:               "openid",
			CodeChallenge:       "abc",
			CodeChallengeMethod: "S256",
		})
		assert.ErrorIs(t, err, ErrServerError)
	})

	t.Run("AcceptLogin expiration and consent creation failure", func(t *testing.T) {
		st := &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-1",
				ClientID:  "client-1",
				ExpiresAt: time.Now().UTC().Add(-time.Minute),
			},
		}
		svc := NewAuthorizationService(st)
		_, err := svc.AcceptLogin(ctx, "lc-1", "identity-1", "session-1")
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st = &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-2",
				ClientID:  "client-1",
				Scopes:    []string{"openid"},
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			createConsentErr: errors.New("cannot persist consent"),
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptLogin(ctx, "lc-2", "identity-1", "session-1")
		assert.ErrorIs(t, err, st.createConsentErr)
	})

	t.Run("AcceptLogin skip consent and auto-accept branches", func(t *testing.T) {
		st := &mockAuthzStore{
			client: &store.Client{
				ID:             "client-1",
				RequireConsent: false,
			},
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-skip",
				ClientID:  "client-1",
				Scopes:    []string{"openid"},
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewAuthorizationService(st)
		resp, err := svc.AcceptLogin(ctx, "lc-skip", "identity-1", "session-1")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ConsentChallenge)
		require.NotNil(t, st.consentChallenge)
		assert.True(t, st.consentChallenge.Skip)

		st = &mockAuthzStore{
			client: &store.Client{
				ID:             "client-1",
				RequireConsent: true,
			},
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-remember",
				ClientID:  "client-1",
				Scopes:    []string{"openid"},
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			consentSession: &store.ConsentSession{
				ClientID: "client-1",
				Remember: true,
				Scopes:   []string{"openid"},
			},
		}
		svc = NewAuthorizationService(st)
		resp, err = svc.AcceptLogin(ctx, "lc-remember", "identity-1", "session-1")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ConsentChallenge)
		require.NotNil(t, st.consentChallenge)
		assert.True(t, st.consentChallenge.Skip)

		st.acceptConsentErr = errors.New("auto-accept failed")
		_, err = svc.AcceptLogin(ctx, "lc-remember", "identity-1", "session-1")
		assert.ErrorIs(t, err, st.acceptConsentErr)

		st = &mockAuthzStore{
			client: &store.Client{
				ID:             "client-1",
				RequireConsent: true,
			},
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-remember-escalation",
				ClientID:  "client-1",
				Scopes:    []string{"openid", "profile"},
				Audience:  []string{"api://new"},
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			consentSession: &store.ConsentSession{
				ClientID: "client-1",
				Remember: true,
				Scopes:   []string{"openid"},
				Audience: []string{"api://old"},
			},
		}
		svc = NewAuthorizationService(st)
		resp, err = svc.AcceptLogin(ctx, "lc-remember-escalation", "identity-1", "session-1")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ConsentChallenge)
		require.NotNil(t, st.consentChallenge)
		assert.False(t, st.consentChallenge.Skip)
	})

	t.Run("AcceptConsent validations and downstream failures", func(t *testing.T) {
		st := &mockAuthzStore{
			getConsentErr: errors.New("missing"),
		}
		svc := NewAuthorizationService(st)
		_, err := svc.AcceptConsent(ctx, "missing", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st = &mockAuthzStore{
			consentChallenge: &store.ConsentChallenge{
				ID:        "cc-expired",
				ExpiresAt: time.Now().UTC().Add(-time.Second),
			},
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptConsent(ctx, "cc-expired", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st = &mockAuthzStore{
			consentChallenge: &store.ConsentChallenge{
				ID:        "cc-handled",
				Handled:   true,
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptConsent(ctx, "cc-handled", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, ErrInvalidRequest)

		st = &mockAuthzStore{
			consentChallenge: &store.ConsentChallenge{
				ID:              "cc-scope",
				RequestedScopes: []string{"openid"},
				ExpiresAt:       time.Now().UTC().Add(time.Minute),
			},
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptConsent(ctx, "cc-scope", []string{"openid", "admin"}, false, nil)
		assert.ErrorIs(t, err, ErrInvalidScope)

		st = &mockAuthzStore{
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc-1",
				LoginChallengeID: "lc-1",
				ClientID:         "client-1",
				IdentityID:       "identity-1",
				SessionID:        "session-1",
				RequestedScopes:  []string{"openid"},
				ExpiresAt:        time.Now().UTC().Add(time.Minute),
			},
			acceptConsentErr: errors.New("accept failed"),
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptConsent(ctx, "cc-1", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, st.acceptConsentErr)

		st.acceptConsentErr = nil
		st.getLoginErr = errors.New("login missing")
		_, err = svc.AcceptConsent(ctx, "cc-1", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, st.getLoginErr)

		st = &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-1",
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc-1",
				LoginChallengeID: "lc-1",
				ClientID:         "client-1",
				IdentityID:       "identity-1",
				SessionID:        "session-1",
				RequestedScopes:  []string{"openid"},
				ExpiresAt:        time.Now().UTC().Add(time.Minute),
			},
			createAuthCodeErr: errors.New("cannot create code"),
		}
		svc = NewAuthorizationService(st)
		_, err = svc.AcceptConsent(ctx, "cc-1", []string{"openid"}, false, nil)
		assert.ErrorIs(t, err, ErrServerError)
	})

	t.Run("AcceptConsent remember branch", func(t *testing.T) {
		rememberFor := 3600
		st := &mockAuthzStore{
			loginChallenge: &store.LoginChallenge{
				ID:        "lc-rem",
				State:     ptrString("state-rem"),
				ExpiresAt: time.Now().UTC().Add(time.Minute),
			},
			consentChallenge: &store.ConsentChallenge{
				ID:               "cc-rem",
				LoginChallengeID: "lc-rem",
				ClientID:         "client-1",
				IdentityID:       "identity-1",
				SessionID:        "session-1",
				RequestedScopes:  []string{"openid"},
				ExpiresAt:        time.Now().UTC().Add(time.Minute),
			},
		}
		svc := NewAuthorizationService(st)
		resp, err := svc.AcceptConsent(ctx, "cc-rem", []string{"openid"}, true, &rememberFor)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Code)
		require.NotNil(t, st.consentSession)
		assert.True(t, st.consentSession.Remember)
		require.NotNil(t, st.consentSession.ExpiresAt)
	})

	t.Run("RejectConsent propagates store error", func(t *testing.T) {
		st := &mockAuthzStore{rejectConsentErr: errors.New("reject failed")}
		svc := NewAuthorizationService(st)
		err := svc.RejectConsent(ctx, "cc-1", "access_denied", "denied")
		assert.ErrorIs(t, err, st.rejectConsentErr)
	})
}

func TestVerifyPKCE(t *testing.T) {
	t.Run("S256Valid", func(t *testing.T) {
		verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
		err := VerifyPKCE(verifier, challenge, "S256")
		assert.NoError(t, err)
	})

	t.Run("PlainValid", func(t *testing.T) {
		verifier := "test-verifier"
		err := VerifyPKCE(verifier, verifier, "plain")
		assert.NoError(t, err)
	})

	t.Run("DefaultMethodIsPlain", func(t *testing.T) {
		err := VerifyPKCE("same", "same", "")
		assert.NoError(t, err)
	})

	t.Run("S256Invalid", func(t *testing.T) {
		verifier := "wrong"
		challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
		err := VerifyPKCE(verifier, challenge, "S256")
		assert.ErrorIs(t, err, store.ErrPKCEMismatch)
	})

	t.Run("PlainInvalid", func(t *testing.T) {
		err := VerifyPKCE("verifier", "different", "plain")
		assert.ErrorIs(t, err, store.ErrPKCEMismatch)
	})

	t.Run("UnsupportedMethod", func(t *testing.T) {
		err := VerifyPKCE("verifier", "challenge", "S512")
		assert.ErrorContains(t, err, "unsupported")
	})
}

func ptrString(v string) *string {
	return &v
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "SingleScope",
			input:    "openid",
			expected: []string{"openid"},
		},
		{
			name:     "MultipleScopes",
			input:    "openid profile email",
			expected: []string{"openid", "profile", "email"},
		},
		{
			name:     "EmptyString",
			input:    "",
			expected: []string{},
		},
		{
			name:     "ExtraSpaces",
			input:    "  openid   profile  ",
			expected: []string{"openid", "profile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseScopes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
