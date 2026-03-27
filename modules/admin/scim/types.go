// Package scim provides SCIM 2.0 provisioning capabilities.
package scim

import (
	"time"

	"github.com/google/uuid"
)

// SCIM 2.0 Schema URNs
const (
	SchemaUser     = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup    = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaListResponse = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError    = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaPatchOp  = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// SCIMUser represents a SCIM 2.0 User resource.
type SCIMUser struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id"`
	ExternalID  string            `json:"externalId,omitempty"`
	UserName    string            `json:"userName"`
	Name        *Name             `json:"name,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	NickName    string            `json:"nickName,omitempty"`
	ProfileURL  string            `json:"profileUrl,omitempty"`
	Title       string            `json:"title,omitempty"`
	UserType    string            `json:"userType,omitempty"`
	Locale      string            `json:"locale,omitempty"`
	Timezone    string            `json:"timezone,omitempty"`
	Active      bool              `json:"active"`
	Emails      []Email           `json:"emails,omitempty"`
	PhoneNumbers []PhoneNumber    `json:"phoneNumbers,omitempty"`
	Addresses   []Address         `json:"addresses,omitempty"`
	Groups      []GroupRef        `json:"groups,omitempty"`
	Roles       []Role            `json:"roles,omitempty"`
	Entitlements []Entitlement    `json:"entitlements,omitempty"`
	Meta        Meta              `json:"meta"`
}

// SCIMGroup represents a SCIM 2.0 Group resource.
type SCIMGroup struct {
	Schemas     []string   `json:"schemas"`
	ID          string     `json:"id"`
	ExternalID  string     `json:"externalId,omitempty"`
	DisplayName string     `json:"displayName"`
	Members     []Member   `json:"members,omitempty"`
	Meta        Meta       `json:"meta"`
}

// Name represents the name attribute of a User.
type Name struct {
	Formatted       string `json:"formatted,omitempty"`
	FamilyName      string `json:"familyName,omitempty"`
	GivenName       string `json:"givenName,omitempty"`
	MiddleName      string `json:"middleName,omitempty"`
	HonorificPrefix string `json:"honorificPrefix,omitempty"`
	HonorificSuffix string `json:"honorificSuffix,omitempty"`
}

// Email represents an email address.
type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	Display string `json:"display,omitempty"`
}

// PhoneNumber represents a phone number.
type PhoneNumber struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	Display string `json:"display,omitempty"`
}

// Address represents a physical address.
type Address struct {
	Formatted     string `json:"formatted,omitempty"`
	StreetAddress string `json:"streetAddress,omitempty"`
	Locality      string `json:"locality,omitempty"`
	Region        string `json:"region,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	Country       string `json:"country,omitempty"`
	Type          string `json:"type,omitempty"`
	Primary       bool   `json:"primary,omitempty"`
}

// GroupRef represents a group reference in a User.
type GroupRef struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
}

// Role represents a role assignment.
type Role struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Entitlement represents an entitlement.
type Entitlement struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Member represents a group member.
type Member struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Type    string `json:"type,omitempty"`
	Display string `json:"display,omitempty"`
}

// Meta contains metadata about a resource.
type Meta struct {
	ResourceType string     `json:"resourceType"`
	Created      *time.Time `json:"created,omitempty"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	Location     string     `json:"location,omitempty"`
	Version      string     `json:"version,omitempty"`
}

// ListResponse represents a SCIM 2.0 List Response.
type ListResponse struct {
	Schemas      []string    `json:"schemas"`
	TotalResults int         `json:"totalResults"`
	ItemsPerPage int         `json:"itemsPerPage"`
	StartIndex   int         `json:"startIndex"`
	Resources    interface{} `json:"Resources"`
}

// ErrorResponse represents a SCIM 2.0 Error Response.
type ErrorResponse struct {
	Schemas []string `json:"schemas"`
	ScimType string  `json:"scimType,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	Status   string  `json:"status"`
}

// PatchRequest represents a SCIM 2.0 Patch Operation Request.
type PatchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []PatchOperation `json:"Operations"`
}

// PatchOperation represents a single patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`    // "add", "remove", "replace"
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

// ServiceProviderConfig represents SCIM service provider configuration.
type ServiceProviderConfig struct {
	Schemas               []string              `json:"schemas"`
	DocumentationURI      string                `json:"documentationUri,omitempty"`
	Patch                 Supported             `json:"patch"`
	Bulk                  BulkConfig            `json:"bulk"`
	Filter                FilterConfig          `json:"filter"`
	ChangePassword        Supported             `json:"changePassword"`
	Sort                  Supported             `json:"sort"`
	ETag                  Supported             `json:"etag"`
	AuthenticationSchemes []AuthenticationScheme `json:"authenticationSchemes"`
	Meta                  Meta                  `json:"meta"`
}

// Supported represents a boolean capability with support.
type Supported struct {
	Supported bool `json:"supported"`
}

// BulkConfig represents bulk operation configuration.
type BulkConfig struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations,omitempty"`
	MaxPayloadSize int  `json:"maxPayloadSize,omitempty"`
}

// FilterConfig represents filter configuration.
type FilterConfig struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults,omitempty"`
}

// AuthenticationScheme represents an authentication scheme.
type AuthenticationScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	SpecURI          string `json:"specUri,omitempty"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// Schema represents a SCIM resource schema.
type Schema struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Attributes  []Attribute `json:"attributes"`
	Meta        Meta        `json:"meta"`
}

// Attribute represents a schema attribute.
type Attribute struct {
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	MultiValued     bool        `json:"multiValued,omitempty"`
	Description     string      `json:"description,omitempty"`
	Required        bool        `json:"required,omitempty"`
	CanonicalValues []string    `json:"canonicalValues,omitempty"`
	CaseExact       bool        `json:"caseExact,omitempty"`
	Mutability      string      `json:"mutability,omitempty"`
	Returned        string      `json:"returned,omitempty"`
	Uniqueness      string      `json:"uniqueness,omitempty"`
	SubAttributes   []Attribute `json:"subAttributes,omitempty"`
	ReferenceTypes  []string    `json:"referenceTypes,omitempty"`
}

// Internal mapping types

// SCIMMapping represents the mapping between SCIM and Aegion identity fields.
type SCIMMapping struct {
	ID               uuid.UUID          `db:"id"`
	Name             string             `db:"name"`
	Description      string             `db:"description"`
	UserNameSource   string             `db:"username_source"`   // "email", "preferred_username", "custom"
	UserNameCustom   string             `db:"username_custom"`   // custom field name if UserNameSource is "custom"
	EmailSource      string             `db:"email_source"`      // which email to use: "primary", "work", "personal"
	NameMapping      map[string]string  `db:"name_mapping"`      // SCIM name fields to Aegion profile fields
	AttributeMapping map[string]string  `db:"attribute_mapping"` // custom attribute mapping
	GroupMapping     map[string]string  `db:"group_mapping"`     // SCIM group to Aegion role mapping
	CreatedAt        time.Time          `db:"created_at"`
	UpdatedAt        time.Time          `db:"updated_at"`
}

// SCIMToken represents an API token for SCIM endpoints.
type SCIMToken struct {
	ID          uuid.UUID  `db:"id"`
	Name        string     `db:"name"`
	Description string     `db:"description"`
	TokenHash   string     `db:"token_hash"`
	Prefix      string     `db:"prefix"`
	Permissions []string   `db:"permissions"` // e.g., ["users:read", "users:write", "groups:read"]
	CreatedBy   uuid.UUID  `db:"created_by"`
	CreatedAt   time.Time  `db:"created_at"`
	ExpiresAt   *time.Time `db:"expires_at"`
	LastUsedAt  *time.Time `db:"last_used_at"`
	Active      bool       `db:"active"`
}

// Filter represents a SCIM filter expression.
type Filter struct {
	Attribute string      `json:"attribute"`
	Operator  string      `json:"operator"`
	Value     interface{} `json:"value"`
}

// SortOrder represents sort direction.
type SortOrder string

const (
	SortAscending  SortOrder = "ascending"
	SortDescending SortOrder = "descending"
)