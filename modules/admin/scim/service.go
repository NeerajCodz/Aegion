// Package scim provides SCIM 2.0 business logic.
package scim

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aegion/aegion/core/registry"
)

// Service provides SCIM 2.0 operations.
type Service struct {
	store    Store
	registry *registry.Registry
}

// Store defines the interface for SCIM persistence operations.
type Store interface {
	// User operations
	GetUserByID(ctx context.Context, id string) (*SCIMUser, error)
	GetUserByUserName(ctx context.Context, userName string) (*SCIMUser, error)
	GetUserByExternalID(ctx context.Context, externalID string) (*SCIMUser, error)
	ListUsers(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMUser, int, error)
	CreateUser(ctx context.Context, user *SCIMUser) error
	UpdateUser(ctx context.Context, user *SCIMUser) error
	PatchUser(ctx context.Context, id string, operations []PatchOperation) (*SCIMUser, error)
	DeleteUser(ctx context.Context, id string) error

	// Group operations
	GetGroupByID(ctx context.Context, id string) (*SCIMGroup, error)
	GetGroupByDisplayName(ctx context.Context, displayName string) (*SCIMGroup, error)
	ListGroups(ctx context.Context, filter *Filter, sortBy string, sortOrder SortOrder, startIndex, count int) ([]*SCIMGroup, int, error)
	CreateGroup(ctx context.Context, group *SCIMGroup) error
	UpdateGroup(ctx context.Context, group *SCIMGroup) error
	PatchGroup(ctx context.Context, id string, operations []PatchOperation) (*SCIMGroup, error)
	DeleteGroup(ctx context.Context, id string) error

	// Mapping operations
	GetSCIMMapping(ctx context.Context, id uuid.UUID) (*SCIMMapping, error)
	ListSCIMMappings(ctx context.Context) ([]*SCIMMapping, error)
	CreateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error
	UpdateSCIMMapping(ctx context.Context, mapping *SCIMMapping) error
	DeleteSCIMMapping(ctx context.Context, id uuid.UUID) error

	// Token operations
	GetSCIMTokenByPrefix(ctx context.Context, prefix string) (*SCIMToken, error)
	ListSCIMTokens(ctx context.Context) ([]*SCIMToken, error)
	CreateSCIMToken(ctx context.Context, token *SCIMToken) error
	UpdateSCIMTokenLastUsed(ctx context.Context, id uuid.UUID) error
	DeleteSCIMToken(ctx context.Context, id uuid.UUID) error
}

// NewService creates a new SCIM service.
func NewService(store Store, registry *registry.Registry) *Service {
	return &Service{
		store:    store,
		registry: registry,
	}
}

// GetServiceProviderConfig returns the SCIM service provider configuration.
func (s *Service) GetServiceProviderConfig() *ServiceProviderConfig {
	return &ServiceProviderConfig{
		Schemas:          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		DocumentationURI: "https://tools.ietf.org/html/rfc7644",
		Patch: Supported{
			Supported: true,
		},
		Bulk: BulkConfig{
			Supported:      false, // Not implemented yet
			MaxOperations:  0,
			MaxPayloadSize: 0,
		},
		Filter: FilterConfig{
			Supported:  true,
			MaxResults: 1000,
		},
		ChangePassword: Supported{
			Supported: false, // Password changes not supported via SCIM
		},
		Sort: Supported{
			Supported: true,
		},
		ETag: Supported{
			Supported: false, // Not implemented yet
		},
		AuthenticationSchemes: []AuthenticationScheme{
			{
				Type:        "httpbearer",
				Name:        "Bearer Token",
				Description: "Authentication scheme using bearer tokens",
				Primary:     true,
			},
		},
		Meta: Meta{
			ResourceType: "ServiceProviderConfig",
			Location:     "/scim/v2/ServiceProviderConfig",
		},
	}
}

// GetSchemas returns the supported SCIM resource schemas.
func (s *Service) GetSchemas() []*Schema {
	return []*Schema{
		s.getUserSchema(),
		s.getGroupSchema(),
	}
}

// GetUserSchema returns the User resource schema.
func (s *Service) getUserSchema() *Schema {
	return &Schema{
		ID:          SchemaUser,
		Name:        "User",
		Description: "User Account",
		Attributes: []Attribute{
			{
				Name:        "userName",
				Type:        "string",
				Required:    true,
				Description: "Unique identifier for the User",
				Mutability:  "readWrite",
				Returned:    "default",
				Uniqueness:  "server",
			},
			{
				Name:        "name",
				Type:        "complex",
				Description: "The components of the user's real name",
				Mutability:  "readWrite",
				Returned:    "default",
				SubAttributes: []Attribute{
					{
						Name:        "formatted",
						Type:        "string",
						Description: "The full name",
						Mutability:  "readWrite",
						Returned:    "default",
					},
					{
						Name:        "familyName",
						Type:        "string",
						Description: "The family name",
						Mutability:  "readWrite",
						Returned:    "default",
					},
					{
						Name:        "givenName",
						Type:        "string",
						Description: "The given name",
						Mutability:  "readWrite",
						Returned:    "default",
					},
				},
			},
			{
				Name:        "displayName",
				Type:        "string",
				Description: "The name of the User, suitable for display",
				Mutability:  "readWrite",
				Returned:    "default",
			},
			{
				Name:        "emails",
				Type:        "complex",
				MultiValued: true,
				Description: "Email addresses for the user",
				Mutability:  "readWrite",
				Returned:    "default",
				SubAttributes: []Attribute{
					{
						Name:        "value",
						Type:        "string",
						Required:    true,
						Description: "Email address",
						Mutability:  "readWrite",
						Returned:    "default",
					},
					{
						Name:        "type",
						Type:        "string",
						Description: "Type of email address",
						Mutability:  "readWrite",
						Returned:    "default",
						CanonicalValues: []string{"work", "home", "other"},
					},
					{
						Name:        "primary",
						Type:        "boolean",
						Description: "Indicates if this is the primary email",
						Mutability:  "readWrite",
						Returned:    "default",
					},
				},
			},
			{
				Name:        "active",
				Type:        "boolean",
				Description: "Indicates if the user account is active",
				Mutability:  "readWrite",
				Returned:    "default",
			},
			{
				Name:        "groups",
				Type:        "complex",
				MultiValued: true,
				Description: "Groups the user belongs to",
				Mutability:  "readOnly",
				Returned:    "default",
				SubAttributes: []Attribute{
					{
						Name:           "value",
						Type:           "string",
						Description:    "The identifier of the Group",
						Mutability:     "readOnly",
						Returned:       "default",
						ReferenceTypes: []string{"Group"},
					},
					{
						Name:        "display",
						Type:        "string",
						Description: "A human-readable name for the Group",
						Mutability:  "readOnly",
						Returned:    "default",
					},
				},
			},
		},
		Meta: Meta{
			ResourceType: "Schema",
			Location:     "/scim/v2/Schemas/" + SchemaUser,
		},
	}
}

// GetGroupSchema returns the Group resource schema.
func (s *Service) getGroupSchema() *Schema {
	return &Schema{
		ID:          SchemaGroup,
		Name:        "Group",
		Description: "Group",
		Attributes: []Attribute{
			{
				Name:        "displayName",
				Type:        "string",
				Required:    true,
				Description: "A human-readable name for the Group",
				Mutability:  "readWrite",
				Returned:    "default",
			},
			{
				Name:        "members",
				Type:        "complex",
				MultiValued: true,
				Description: "A list of members of the Group",
				Mutability:  "readWrite",
				Returned:    "default",
				SubAttributes: []Attribute{
					{
						Name:           "value",
						Type:           "string",
						Required:       true,
						Description:    "The identifier of the member",
						Mutability:     "immutable",
						Returned:       "default",
						ReferenceTypes: []string{"User", "Group"},
					},
					{
						Name:        "display",
						Type:        "string",
						Description: "A human-readable name for the member",
						Mutability:  "immutable",
						Returned:    "default",
					},
					{
						Name:        "type",
						Type:        "string",
						Description: "The type of member",
						Mutability:  "immutable",
						Returned:    "default",
						CanonicalValues: []string{"User", "Group"},
					},
				},
			},
		},
		Meta: Meta{
			ResourceType: "Schema",
			Location:     "/scim/v2/Schemas/" + SchemaGroup,
		},
	}
}

// User operations

// GetUser retrieves a user by ID.
func (s *Service) GetUser(ctx context.Context, id string) (*SCIMUser, error) {
	return s.store.GetUserByID(ctx, id)
}

// ListUsers retrieves a list of users with optional filtering and pagination.
func (s *Service) ListUsers(ctx context.Context, filter string, sortBy string, sortOrder SortOrder, startIndex, count int) (*ListResponse, error) {
	// Parse filter if provided
	var parsedFilter *Filter
	if filter != "" {
		var err error
		parsedFilter, err = s.parseFilter(filter)
		if err != nil {
			return nil, fmt.Errorf("invalid filter: %w", err)
		}
	}

	// Set defaults
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 {
		count = 20
	}
	if count > 1000 {
		count = 1000
	}

	users, total, err := s.store.ListUsers(ctx, parsedFilter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		return nil, err
	}

	return &ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: total,
		ItemsPerPage: len(users),
		StartIndex:   startIndex,
		Resources:    users,
	}, nil
}

// CreateUser creates a new user.
func (s *Service) CreateUser(ctx context.Context, user *SCIMUser) (*SCIMUser, error) {
	// Generate ID if not provided
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set schemas
	user.Schemas = []string{SchemaUser}

	// Set meta information
	now := time.Now().UTC()
	user.Meta = Meta{
		ResourceType: "User",
		Created:      &now,
		LastModified: &now,
		Location:     "/scim/v2/Users/" + user.ID,
		Version:      "1",
	}

	// Validate required fields
	if user.UserName == "" {
		return nil, fmt.Errorf("userName is required")
	}

	err := s.store.CreateUser(ctx, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateUser replaces a user (PUT).
func (s *Service) UpdateUser(ctx context.Context, id string, user *SCIMUser) (*SCIMUser, error) {
	// Preserve ID
	user.ID = id

	// Set schemas
	user.Schemas = []string{SchemaUser}

	// Update meta information
	now := time.Now().UTC()
	existing, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}

	user.Meta = existing.Meta
	user.Meta.LastModified = &now
	user.Meta.Location = "/scim/v2/Users/" + id

	err = s.store.UpdateUser(ctx, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// PatchUser applies patch operations to a user.
func (s *Service) PatchUser(ctx context.Context, id string, operations []PatchOperation) (*SCIMUser, error) {
	return s.store.PatchUser(ctx, id, operations)
}

// DeleteUser deletes a user.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	return s.store.DeleteUser(ctx, id)
}

// Group operations

// GetGroup retrieves a group by ID.
func (s *Service) GetGroup(ctx context.Context, id string) (*SCIMGroup, error) {
	return s.store.GetGroupByID(ctx, id)
}

// ListGroups retrieves a list of groups with optional filtering and pagination.
func (s *Service) ListGroups(ctx context.Context, filter string, sortBy string, sortOrder SortOrder, startIndex, count int) (*ListResponse, error) {
	// Parse filter if provided
	var parsedFilter *Filter
	if filter != "" {
		var err error
		parsedFilter, err = s.parseFilter(filter)
		if err != nil {
			return nil, fmt.Errorf("invalid filter: %w", err)
		}
	}

	// Set defaults
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 {
		count = 20
	}
	if count > 1000 {
		count = 1000
	}

	groups, total, err := s.store.ListGroups(ctx, parsedFilter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		return nil, err
	}

	return &ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: total,
		ItemsPerPage: len(groups),
		StartIndex:   startIndex,
		Resources:    groups,
	}, nil
}

// CreateGroup creates a new group.
func (s *Service) CreateGroup(ctx context.Context, group *SCIMGroup) (*SCIMGroup, error) {
	// Generate ID if not provided
	if group.ID == "" {
		group.ID = uuid.New().String()
	}

	// Set schemas
	group.Schemas = []string{SchemaGroup}

	// Set meta information
	now := time.Now().UTC()
	group.Meta = Meta{
		ResourceType: "Group",
		Created:      &now,
		LastModified: &now,
		Location:     "/scim/v2/Groups/" + group.ID,
		Version:      "1",
	}

	// Validate required fields
	if group.DisplayName == "" {
		return nil, fmt.Errorf("displayName is required")
	}

	err := s.store.CreateGroup(ctx, group)
	if err != nil {
		return nil, err
	}

	return group, nil
}

// UpdateGroup replaces a group (PUT).
func (s *Service) UpdateGroup(ctx context.Context, id string, group *SCIMGroup) (*SCIMGroup, error) {
	// Preserve ID
	group.ID = id

	// Set schemas
	group.Schemas = []string{SchemaGroup}

	// Update meta information
	now := time.Now().UTC()
	existing, err := s.store.GetGroupByID(ctx, id)
	if err != nil {
		return nil, err
	}

	group.Meta = existing.Meta
	group.Meta.LastModified = &now
	group.Meta.Location = "/scim/v2/Groups/" + id

	err = s.store.UpdateGroup(ctx, group)
	if err != nil {
		return nil, err
	}

	return group, nil
}

// PatchGroup applies patch operations to a group.
func (s *Service) PatchGroup(ctx context.Context, id string, operations []PatchOperation) (*SCIMGroup, error) {
	return s.store.PatchGroup(ctx, id, operations)
}

// DeleteGroup deletes a group.
func (s *Service) DeleteGroup(ctx context.Context, id string) error {
	return s.store.DeleteGroup(ctx, id)
}

// Token management

// CreateSCIMToken creates a new SCIM API token.
func (s *Service) CreateSCIMToken(ctx context.Context, name, description string, permissions []string, expiresAt *time.Time, createdBy uuid.UUID) (*SCIMToken, string, error) {
	// Generate token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("failed to generate token: %w", err)
	}
	token := "aegion_scim_" + base64.RawURLEncoding.EncodeToString(tokenBytes)

	// Hash token for storage
	hash := sha256.Sum256([]byte(token))
	tokenHash := base64.StdEncoding.EncodeToString(hash[:])

	// Generate prefix for lookup (first 12 chars after "aegion_scim_")
	prefix := token[12:24]

	scimToken := &SCIMToken{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		TokenHash:   tokenHash,
		Prefix:      prefix,
		Permissions: permissions,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   expiresAt,
		Active:      true,
	}

	err := s.store.CreateSCIMToken(ctx, scimToken)
	if err != nil {
		return nil, "", err
	}

	return scimToken, token, nil
}

// ValidateToken validates a SCIM API token and returns the token info.
func (s *Service) ValidateToken(ctx context.Context, token string) (*SCIMToken, error) {
	if !strings.HasPrefix(token, "aegion_scim_") {
		return nil, fmt.Errorf("invalid token format")
	}

	if len(token) < 24 {
		return nil, fmt.Errorf("invalid token length")
	}

	// Extract prefix for lookup
	prefix := token[12:24]

	scimToken, err := s.store.GetSCIMTokenByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("token not found: %w", err)
	}

	// Check if active
	if !scimToken.Active {
		return nil, fmt.Errorf("token is inactive")
	}

	// Check expiration
	if scimToken.ExpiresAt != nil && time.Now().UTC().After(*scimToken.ExpiresAt) {
		return nil, fmt.Errorf("token has expired")
	}

	// Validate token hash
	hash := sha256.Sum256([]byte(token))
	tokenHash := base64.StdEncoding.EncodeToString(hash[:])

	if tokenHash != scimToken.TokenHash {
		return nil, fmt.Errorf("invalid token")
	}

	// Update last used timestamp (best effort)
	go func() {
		_ = s.store.UpdateSCIMTokenLastUsed(context.Background(), scimToken.ID)
	}()

	return scimToken, nil
}

// HasPermission checks if a token has the specified permission.
func (s *Service) HasPermission(token *SCIMToken, permission string) bool {
	for _, perm := range token.Permissions {
		if perm == "*" || perm == permission {
			return true
		}
		// Check wildcard permissions (e.g., "users:*" matches "users:read")
		if strings.HasSuffix(perm, ":*") {
			prefix := strings.TrimSuffix(perm, "*")
			if strings.HasPrefix(permission, prefix) {
				return true
			}
		}
	}
	return false
}

// parseFilter parses a SCIM filter expression.
// This is a simplified implementation - a full implementation would need a proper parser.
func (s *Service) parseFilter(filter string) (*Filter, error) {
	// Simple parsing for common patterns like 'userName eq "john"'
	parts := strings.Split(filter, " ")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid filter format")
	}

	// Remove quotes from value
	value := strings.Trim(parts[2], `"'`)

	return &Filter{
		Attribute: parts[0],
		Operator:  parts[1],
		Value:     value,
	}, nil
}