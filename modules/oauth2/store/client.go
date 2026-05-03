package store

import (
	"context"
	"encoding/json"
	"time"
)

// Client represents an OAuth2 client.
type Client struct {
	ID                       string            `json:"id"`
	SecretHash               *string           `json:"-"` // never expose
	Name                     string            `json:"name"`
	Description              *string           `json:"description,omitempty"`
	LogoURI                  *string           `json:"logo_uri,omitempty"`
	ClientURI                *string           `json:"client_uri,omitempty"`
	PolicyURI                *string           `json:"policy_uri,omitempty"`
	TOSURI                   *string           `json:"tos_uri,omitempty"`
	RedirectURIs             []string          `json:"redirect_uris"`
	PostLogoutRedirectURIs   []string          `json:"post_logout_redirect_uris,omitempty"`
	GrantTypes               []string          `json:"grant_types"`
	ResponseTypes            []string          `json:"response_types"`
	Scopes                   []string          `json:"scopes"`
	Audience                 []string          `json:"audience,omitempty"`
	TokenEndpointAuthMethod  string            `json:"token_endpoint_auth_method"`
	JWKSURI                  *string           `json:"jwks_uri,omitempty"`
	JWKS                     json.RawMessage   `json:"jwks,omitempty"`
	SectorIdentifierURI      *string           `json:"sector_identifier_uri,omitempty"`
	SubjectType              string            `json:"subject_type"`
	IDTokenSignedResponseAlg string            `json:"id_token_signed_response_alg"`
	AccessTokenStrategy      string            `json:"access_token_strategy"`
	AccessTokenTTL           int               `json:"access_token_ttl"`
	RefreshTokenTTL          int               `json:"refresh_token_ttl"`
	IDTokenTTL               int               `json:"id_token_ttl"`
	AuthCodeTTL              int               `json:"auth_code_ttl"`
	RequirePKCE              bool              `json:"require_pkce"`
	RequireConsent           bool              `json:"require_consent"`
	AllowOfflineAccess       bool              `json:"allow_offline_access"`
	Metadata                 map[string]string `json:"metadata,omitempty"`
	OwnerID                  *string           `json:"owner_id,omitempty"`
	CreatedAt                time.Time         `json:"created_at"`
	UpdatedAt                time.Time         `json:"updated_at"`
}

// CreateClient creates a new OAuth2 client.
func (s *Store) CreateClient(ctx context.Context, client *Client) error {
	if client.ID == "" {
		var err error
		client.ID, err = GenerateClientID()
		if err != nil {
			return err
		}
	}
	now := nowUTC()
	client.CreatedAt = now
	client.UpdatedAt = now

	metadata, _ := json.Marshal(client.Metadata)

	_, err := s.db.Exec(ctx, `
		INSERT INTO oa2_clients (
			id, secret_hash, name, description, logo_uri, client_uri, policy_uri, tos_uri,
			redirect_uris, post_logout_redirect_uris, grant_types, response_types, scopes,
			audience, token_endpoint_auth_method, jwks_uri, jwks, sector_identifier_uri,
			subject_type, id_token_signed_response_alg, access_token_strategy,
			access_token_ttl, refresh_token_ttl, id_token_ttl, auth_code_ttl,
			require_pkce, require_consent, allow_offline_access, metadata, owner_id,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			$19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32
		)
	`,
		client.ID, client.SecretHash, client.Name, client.Description,
		client.LogoURI, client.ClientURI, client.PolicyURI, client.TOSURI,
		client.RedirectURIs, client.PostLogoutRedirectURIs, client.GrantTypes,
		client.ResponseTypes, client.Scopes, client.Audience,
		client.TokenEndpointAuthMethod, client.JWKSURI, client.JWKS,
		client.SectorIdentifierURI, client.SubjectType, client.IDTokenSignedResponseAlg,
		client.AccessTokenStrategy, client.AccessTokenTTL, client.RefreshTokenTTL,
		client.IDTokenTTL, client.AuthCodeTTL, client.RequirePKCE, client.RequireConsent,
		client.AllowOfflineAccess, metadata, client.OwnerID, client.CreatedAt, client.UpdatedAt,
	)

	if isDuplicateKeyError(err) {
		return ErrAlreadyExists
	}
	return err
}

// GetClient retrieves a client by ID.
func (s *Store) GetClient(ctx context.Context, id string) (*Client, error) {
	client := &Client{}
	var metadata []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, secret_hash, name, description, logo_uri, client_uri, policy_uri, tos_uri,
			redirect_uris, post_logout_redirect_uris, grant_types, response_types, scopes,
			audience, token_endpoint_auth_method, jwks_uri, jwks, sector_identifier_uri,
			subject_type, id_token_signed_response_alg, access_token_strategy,
			access_token_ttl, refresh_token_ttl, id_token_ttl, auth_code_ttl,
			require_pkce, require_consent, allow_offline_access, metadata, owner_id,
			created_at, updated_at
		FROM oa2_clients WHERE id = $1
	`, id).Scan(
		&client.ID, &client.SecretHash, &client.Name, &client.Description,
		&client.LogoURI, &client.ClientURI, &client.PolicyURI, &client.TOSURI,
		&client.RedirectURIs, &client.PostLogoutRedirectURIs, &client.GrantTypes,
		&client.ResponseTypes, &client.Scopes, &client.Audience,
		&client.TokenEndpointAuthMethod, &client.JWKSURI, &client.JWKS,
		&client.SectorIdentifierURI, &client.SubjectType, &client.IDTokenSignedResponseAlg,
		&client.AccessTokenStrategy, &client.AccessTokenTTL, &client.RefreshTokenTTL,
		&client.IDTokenTTL, &client.AuthCodeTTL, &client.RequirePKCE, &client.RequireConsent,
		&client.AllowOfflineAccess, &metadata, &client.OwnerID, &client.CreatedAt, &client.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &client.Metadata)
	}

	return client, nil
}

// UpdateClient updates an existing client.
func (s *Store) UpdateClient(ctx context.Context, client *Client) error {
	client.UpdatedAt = nowUTC()
	metadata, _ := json.Marshal(client.Metadata)

	result, err := s.db.Exec(ctx, `
		UPDATE oa2_clients SET
			name = $2, description = $3, logo_uri = $4, client_uri = $5,
			policy_uri = $6, tos_uri = $7, redirect_uris = $8, post_logout_redirect_uris = $9,
			grant_types = $10, response_types = $11, scopes = $12, audience = $13,
			token_endpoint_auth_method = $14, jwks_uri = $15, jwks = $16,
			sector_identifier_uri = $17, subject_type = $18, id_token_signed_response_alg = $19,
			access_token_strategy = $20, access_token_ttl = $21, refresh_token_ttl = $22,
			id_token_ttl = $23, auth_code_ttl = $24, require_pkce = $25, require_consent = $26,
			allow_offline_access = $27, metadata = $28, updated_at = $29
		WHERE id = $1
	`,
		client.ID, client.Name, client.Description, client.LogoURI, client.ClientURI,
		client.PolicyURI, client.TOSURI, client.RedirectURIs, client.PostLogoutRedirectURIs,
		client.GrantTypes, client.ResponseTypes, client.Scopes, client.Audience,
		client.TokenEndpointAuthMethod, client.JWKSURI, client.JWKS,
		client.SectorIdentifierURI, client.SubjectType, client.IDTokenSignedResponseAlg,
		client.AccessTokenStrategy, client.AccessTokenTTL, client.RefreshTokenTTL,
		client.IDTokenTTL, client.AuthCodeTTL, client.RequirePKCE, client.RequireConsent,
		client.AllowOfflineAccess, metadata, client.UpdatedAt,
	)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateClientSecret updates a client's secret hash.
func (s *Store) UpdateClientSecret(ctx context.Context, clientID string, secretHash string) error {
	result, err := s.db.Exec(ctx, `
		UPDATE oa2_clients SET secret_hash = $2, updated_at = NOW() WHERE id = $1
	`, clientID, secretHash)

	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteClient deletes a client.
func (s *Store) DeleteClient(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, "DELETE FROM oa2_clients WHERE id = $1", id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListClients lists all clients, optionally filtered by owner.
func (s *Store) ListClients(ctx context.Context, ownerID *string, limit, offset int) ([]*Client, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	var rows interface {
		Close()
		Next() bool
		Scan(dest ...interface{}) error
		Err() error
	}
	var err error

	if ownerID != nil {
		rows, err = s.db.Query(ctx, `
			SELECT id, name, description, redirect_uris, grant_types, scopes,
				token_endpoint_auth_method, require_pkce, created_at, updated_at
			FROM oa2_clients WHERE owner_id = $1
			ORDER BY created_at DESC LIMIT $2 OFFSET $3
		`, *ownerID, limit, offset)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT id, name, description, redirect_uris, grant_types, scopes,
				token_endpoint_auth_method, require_pkce, created_at, updated_at
			FROM oa2_clients
			ORDER BY created_at DESC LIMIT $1 OFFSET $2
		`, limit, offset)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []*Client
	for rows.Next() {
		c := &Client{}
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Description, &c.RedirectURIs, &c.GrantTypes,
			&c.Scopes, &c.TokenEndpointAuthMethod, &c.RequirePKCE,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}

	return clients, rows.Err()
}

// ValidateRedirectURI checks if the given URI is allowed for the client.
func (c *Client) ValidateRedirectURI(uri string) bool {
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

// HasGrantType checks if the client supports the given grant type.
func (c *Client) HasGrantType(grantType string) bool {
	for _, gt := range c.GrantTypes {
		if gt == grantType {
			return true
		}
	}
	return false
}

// HasScope checks if the client supports the given scope.
func (c *Client) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// IsPublic returns true if the client is a public client (no secret).
func (c *Client) IsPublic() bool {
	return c.TokenEndpointAuthMethod == "none"
}
