package service

import (
	"context"
	"errors"
	"testing"

	bcrypt "github.com/aegion/aegion/internal/platform/bcryptcompat"

	"github.com/aegion/aegion/modules/oauth2/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClientStore struct {
	client         *store.Client
	clients        []*store.Client
	createErr      error
	getErr         error
	updateErr      error
	updateHashErr  error
	deleteErr      error
	listErr        error
	lastCreated    *store.Client
	lastUpdated    *store.Client
	lastSecretID   string
	lastSecretHash string
	lastDeletedID  string
}

func (m *mockClientStore) CreateClient(ctx context.Context, client *store.Client) error {
	m.lastCreated = client
	return m.createErr
}

func (m *mockClientStore) GetClient(ctx context.Context, id string) (*store.Client, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.client != nil {
		return m.client, nil
	}
	return nil, store.ErrNotFound
}

func (m *mockClientStore) UpdateClient(ctx context.Context, client *store.Client) error {
	m.lastUpdated = client
	return m.updateErr
}

func (m *mockClientStore) UpdateClientSecret(ctx context.Context, clientID string, secretHash string) error {
	m.lastSecretID = clientID
	m.lastSecretHash = secretHash
	return m.updateHashErr
}

func (m *mockClientStore) DeleteClient(ctx context.Context, id string) error {
	m.lastDeletedID = id
	return m.deleteErr
}

func (m *mockClientStore) ListClients(ctx context.Context, ownerID *string, limit, offset int) ([]*store.Client, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.clients, nil
}

func TestClientService_CreateClient(t *testing.T) {
	t.Run("success with defaults", func(t *testing.T) {
		st := &mockClientStore{}
		svc := NewClientService(st)

		resp, err := svc.CreateClient(context.Background(), &CreateClientRequest{
			Name:         "My App",
			RedirectURIs: []string{"https://app.example.com/callback"},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Client)
		assert.NotEmpty(t, resp.Client.ID)
		assert.Equal(t, []string{"authorization_code"}, resp.Client.GrantTypes)
		assert.Equal(t, []string{"code"}, resp.Client.ResponseTypes)
		assert.Equal(t, []string{"openid"}, resp.Client.Scopes)
		assert.Equal(t, "client_secret_basic", resp.Client.TokenEndpointAuthMethod)
		assert.True(t, resp.Client.RequirePKCE)
		assert.True(t, resp.Client.RequireConsent)
		assert.True(t, resp.Client.AllowOfflineAccess)
		require.NotNil(t, resp.Client.SecretHash)
		require.NotNil(t, resp.ClientSecret)
		require.NoError(t, bcrypt.CompareHashAndPassword([]byte(*resp.Client.SecretHash), []byte(*resp.ClientSecret)))
	})

	t.Run("public client has no secret", func(t *testing.T) {
		st := &mockClientStore{}
		svc := NewClientService(st)

		resp, err := svc.CreateClient(context.Background(), &CreateClientRequest{
			Name:                    "Public App",
			RedirectURIs:            []string{"https://app.example.com/callback"},
			TokenEndpointAuthMethod: "none",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Nil(t, resp.Client.SecretHash)
		assert.Nil(t, resp.ClientSecret)
	})

	t.Run("validation and unsupported auth method", func(t *testing.T) {
		st := &mockClientStore{}
		svc := NewClientService(st)

		_, err := svc.CreateClient(context.Background(), &CreateClientRequest{
			RedirectURIs: []string{"https://app.example.com/callback"},
		})
		assert.ErrorContains(t, err, "client name is required")

		_, err = svc.CreateClient(context.Background(), &CreateClientRequest{
			Name:         "x",
			RedirectURIs: nil,
		})
		assert.ErrorContains(t, err, "at least one redirect URI is required")

		_, err = svc.CreateClient(context.Background(), &CreateClientRequest{
			Name:         "x",
			RedirectURIs: []string{"https://*.example.com/callback"},
		})
		assert.ErrorContains(t, err, "wildcards")

		_, err = svc.CreateClient(context.Background(), &CreateClientRequest{
			Name:                    "x",
			RedirectURIs:            []string{"https://app.example.com/callback"},
			TokenEndpointAuthMethod: "unknown",
		})
		assert.ErrorIs(t, err, ErrUnsupportedAuthMethod)
	})
}

func TestClientService_GetUpdateDeleteList(t *testing.T) {
	ctx := context.Background()

	t.Run("get maps not found", func(t *testing.T) {
		st := &mockClientStore{getErr: store.ErrNotFound}
		svc := NewClientService(st)
		_, err := svc.GetClient(ctx, "missing")
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("get propagates non-notfound errors", func(t *testing.T) {
		st := &mockClientStore{getErr: errors.New("db unavailable")}
		svc := NewClientService(st)
		_, err := svc.GetClient(ctx, "any")
		assert.ErrorContains(t, err, "db unavailable")
	})

	t.Run("update success and invalid redirect", func(t *testing.T) {
		st := &mockClientStore{
			client: &store.Client{
				ID:           "client-1",
				Name:         "Old",
				RedirectURIs: []string{"https://old.example.com/cb"},
				GrantTypes:   []string{"authorization_code"},
				Scopes:       []string{"openid"},
			},
		}
		svc := NewClientService(st)

		newName := "New"
		enablePKCE := false
		updated, err := svc.UpdateClient(ctx, "client-1", &CreateClientRequest{
			Name:         newName,
			RedirectURIs: []string{"https://new.example.com/cb"},
			RequirePKCE:  &enablePKCE,
		})
		require.NoError(t, err)
		assert.Equal(t, newName, updated.Name)
		assert.Equal(t, []string{"https://new.example.com/cb"}, updated.RedirectURIs)
		assert.False(t, updated.RequirePKCE)
		require.NotNil(t, st.lastUpdated)

		_, err = svc.UpdateClient(ctx, "client-1", &CreateClientRequest{
			RedirectURIs: []string{"https://bad.example.com/cb#frag"},
		})
		assert.ErrorContains(t, err, "fragments")
	})

	t.Run("update optional fields", func(t *testing.T) {
		desc := "updated description"
		requirePKCE := false
		requireConsent := false
		allowOffline := false
		st := &mockClientStore{
			client: &store.Client{
				ID:                 "client-2",
				Name:               "Old",
				Description:        ptrString("old"),
				RedirectURIs:       []string{"https://old.example.com/cb"},
				GrantTypes:         []string{"authorization_code"},
				Scopes:             []string{"openid"},
				RequirePKCE:        true,
				RequireConsent:     true,
				AllowOfflineAccess: true,
				Metadata:           map[string]string{"old": "1"},
			},
		}
		svc := NewClientService(st)
		updated, err := svc.UpdateClient(ctx, "client-2", &CreateClientRequest{
			Description:        &desc,
			GrantTypes:         []string{"refresh_token"},
			Scopes:             []string{"profile"},
			RequirePKCE:        &requirePKCE,
			RequireConsent:     &requireConsent,
			AllowOfflineAccess: &allowOffline,
			Metadata:           map[string]string{"new": "1"},
		})
		require.NoError(t, err)
		require.NotNil(t, updated.Description)
		assert.Equal(t, desc, *updated.Description)
		assert.Equal(t, []string{"refresh_token"}, updated.GrantTypes)
		assert.Equal(t, []string{"profile"}, updated.Scopes)
		assert.False(t, updated.RequirePKCE)
		assert.False(t, updated.RequireConsent)
		assert.False(t, updated.AllowOfflineAccess)
		assert.Equal(t, map[string]string{"new": "1"}, updated.Metadata)
	})

	t.Run("delete and list passthrough", func(t *testing.T) {
		owner := "owner-1"
		st := &mockClientStore{
			clients: []*store.Client{{ID: "c1"}, {ID: "c2"}},
		}
		svc := NewClientService(st)

		err := svc.DeleteClient(ctx, "c1")
		require.NoError(t, err)
		assert.Equal(t, "c1", st.lastDeletedID)

		clients, err := svc.ListClients(ctx, &owner, 10, 0)
		require.NoError(t, err)
		assert.Len(t, clients, 2)
	})
}

func TestClientService_RotateAndAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("rotate secret for confidential client", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("old-secret"), bcrypt.DefaultCost)
		require.NoError(t, err)
		st := &mockClientStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_basic",
				SecretHash:              ptrString(string(hash)),
			},
		}
		svc := NewClientService(st)

		secret, err := svc.RotateClientSecret(ctx, "client-1")
		require.NoError(t, err)
		assert.NotEmpty(t, secret)
		assert.Equal(t, "client-1", st.lastSecretID)
		assert.NotEmpty(t, st.lastSecretHash)
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(st.lastSecretHash), []byte(secret)))
	})

	t.Run("rotate secret for public client fails", func(t *testing.T) {
		st := &mockClientStore{
			client: &store.Client{ID: "pub", TokenEndpointAuthMethod: "none"},
		}
		svc := NewClientService(st)
		_, err := svc.RotateClientSecret(ctx, "pub")
		assert.ErrorContains(t, err, "public client")
	})

	t.Run("rotate secret get client error", func(t *testing.T) {
		st := &mockClientStore{getErr: errors.New("db error")}
		svc := NewClientService(st)
		_, err := svc.RotateClientSecret(ctx, "x")
		assert.ErrorContains(t, err, "db error")
	})

	t.Run("authenticate confidential and public clients", func(t *testing.T) {
		secret := "top-secret"
		hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		require.NoError(t, err)
		conf := &store.Client{
			ID:                      "conf-1",
			TokenEndpointAuthMethod: "client_secret_post",
			SecretHash:              ptrString(string(hash)),
		}

		st := &mockClientStore{client: conf}
		svc := NewClientService(st)

		client, err := svc.AuthenticateClient(ctx, "conf-1", secret, "client_secret_post")
		require.NoError(t, err)
		assert.Equal(t, "conf-1", client.ID)

		_, err = svc.AuthenticateClient(ctx, "conf-1", "wrong", "client_secret_post")
		assert.ErrorIs(t, err, ErrInvalidSecret)

		_, err = svc.AuthenticateClient(ctx, "conf-1", secret, "client_secret_basic")
		assert.ErrorContains(t, err, "mismatch")

		st.client = &store.Client{ID: "pub-1", TokenEndpointAuthMethod: "none"}
		client, err = svc.AuthenticateClient(ctx, "pub-1", "", "none")
		require.NoError(t, err)
		assert.Equal(t, "pub-1", client.ID)

		_, err = svc.AuthenticateClient(ctx, "pub-1", "", "client_secret_post")
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("authenticate not found maps error", func(t *testing.T) {
		st := &mockClientStore{getErr: store.ErrNotFound}
		svc := NewClientService(st)
		_, err := svc.AuthenticateClient(ctx, "missing", "x", "client_secret_post")
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	t.Run("authenticate propagates store errors and missing secret hash", func(t *testing.T) {
		st := &mockClientStore{getErr: errors.New("db timeout")}
		svc := NewClientService(st)
		_, err := svc.AuthenticateClient(ctx, "client-1", "x", "client_secret_post")
		assert.ErrorContains(t, err, "db timeout")

		st = &mockClientStore{
			client: &store.Client{
				ID:                      "client-1",
				TokenEndpointAuthMethod: "client_secret_post",
				SecretHash:              nil,
			},
		}
		svc = NewClientService(st)
		_, err = svc.AuthenticateClient(ctx, "client-1", "x", "client_secret_post")
		assert.ErrorContains(t, err, "not set")
	})
}

func TestClientHelpers(t *testing.T) {
	assert.NoError(t, validateRedirectURI("https://app.example.com/callback"))
	assert.NoError(t, validateRedirectURI("custom://callback"))
	assert.Error(t, validateRedirectURI(""))
	assert.ErrorContains(t, validateRedirectURI("callback"), "absolute")
	assert.ErrorContains(t, validateRedirectURI("https://*.example.com/cb"), "wildcards")
	assert.ErrorContains(t, validateRedirectURI("https://a.example.com/cb#f"), "fragments")

	assert.True(t, isValidAuthMethod("none"))
	assert.True(t, isValidAuthMethod("private_key_jwt"))
	assert.False(t, isValidAuthMethod("bad"))

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	require.NoError(t, err)
	assert.True(t, VerifyClientSecret(string(hash), "secret"))
	assert.False(t, VerifyClientSecret(string(hash), "nope"))
}

func TestClientService_StoreErrors(t *testing.T) {
	ctx := context.Background()
	st := &mockClientStore{
		createErr:     errors.New("create"),
		updateErr:     errors.New("update"),
		updateHashErr: errors.New("secret"),
		deleteErr:     errors.New("delete"),
		listErr:       errors.New("list"),
		client: &store.Client{
			ID:                      "client-1",
			TokenEndpointAuthMethod: "client_secret_basic",
			SecretHash:              ptrString("$2a$10$TzK8dwU4uM1z6l4gXFfS8e0CW8QmIQsNv2Q7E2Q4hET3tK5QhHjFS"),
		},
	}
	svc := NewClientService(st)

	_, err := svc.CreateClient(ctx, &CreateClientRequest{Name: "x", RedirectURIs: []string{"https://a.example.com/cb"}})
	assert.ErrorContains(t, err, "failed to create client")

	_, err = svc.UpdateClient(ctx, "client-1", &CreateClientRequest{Name: "y"})
	assert.ErrorIs(t, err, st.updateErr)

	_, err = svc.RotateClientSecret(ctx, "client-1")
	assert.ErrorIs(t, err, st.updateHashErr)

	err = svc.DeleteClient(ctx, "client-1")
	assert.ErrorIs(t, err, st.deleteErr)

	_, err = svc.ListClients(ctx, nil, 1, 0)
	assert.ErrorIs(t, err, st.listErr)
}

func ptrString(v string) *string {
	return &v
}
