// Package capability provides Discord-style admin permission management.
package capability

import (
	"github.com/google/uuid"
)

// Capability represents a specific permission.
type Capability string

// User domain capabilities
const (
	CapUsersRead     Capability = "users.read"
	CapUsersCreate   Capability = "users.create"
	CapUsersUpdate   Capability = "users.update"
	CapUsersDelete   Capability = "users.delete"
	CapUsersSuspend  Capability = "users.suspend"
	CapUsersAll      Capability = "users.*"
)

// Session domain capabilities
const (
	CapSessionsRead   Capability = "sessions.read"
	CapSessionsRevoke Capability = "sessions.revoke"
	CapSessionsAll    Capability = "sessions.*"
)

// MFA domain capabilities
const (
	CapMFARead   Capability = "mfa.read"
	CapMFAManage Capability = "mfa.manage"
	CapMFAAll    Capability = "mfa.*"
)

// OAuth2 domain capabilities
const (
	CapOAuth2ClientsRead   Capability = "oauth2.clients.read"
	CapOAuth2ClientsManage Capability = "oauth2.clients.manage"
	CapOAuth2TokensRead    Capability = "oauth2.tokens.read"
	CapOAuth2TokensRevoke  Capability = "oauth2.tokens.revoke"
	CapOAuth2All           Capability = "oauth2.*"
)

// Policy domain capabilities
const (
	CapPolicyRead   Capability = "policy.read"
	CapPolicyManage Capability = "policy.manage"
	CapPolicyAll    Capability = "policy.*"
)

// System domain capabilities
const (
	CapSystemConfig Capability = "system.config"
	CapSystemAudit  Capability = "system.audit"
	CapSystemHealth Capability = "system.health"
	CapSystemAll    Capability = "system.*"
)

// Admin team domain capabilities
const (
	CapAdminTeamRead   Capability = "admin_team.read"
	CapAdminTeamManage Capability = "admin_team.manage"
	CapAdminTeamAll    Capability = "admin_team.*"
)

// SCIM domain capabilities
const (
	CapSCIMUsersRead   Capability = "scim.users.read"
	CapSCIMUsersWrite  Capability = "scim.users.write"
	CapSCIMGroupsRead  Capability = "scim.groups.read"
	CapSCIMGroupsWrite Capability = "scim.groups.write"
	CapSCIMTokensManage Capability = "scim.tokens.manage"
	CapSCIMAll         Capability = "scim.*"
)

// Global capabilities
const (
	CapAll Capability = "*" // Superuser access
)

// Role represents a predefined set of capabilities.
type Role struct {
	ID           string       `json:"id" db:"id"`
	Name         string       `json:"name" db:"name"`
	Description  string       `json:"description" db:"description"`
	Capabilities []Capability `json:"capabilities" db:"capabilities"`
	IsSystem     bool         `json:"isSystem" db:"is_system"` // Cannot be deleted
	CreatedAt    string       `json:"createdAt" db:"created_at"`
	UpdatedAt    string       `json:"updatedAt" db:"updated_at"`
}

// AdminIdentity represents an admin's permission assignments.
type AdminIdentity struct {
	IdentityID   uuid.UUID    `json:"identityId" db:"identity_id"`
	Roles        []string     `json:"roles" db:"roles"`           // role IDs
	Grants       []Capability `json:"grants" db:"grants"`         // direct grants
	Denies       []Capability `json:"denies" db:"denies"`         // direct denies (override grants)
	CreatedAt    string       `json:"createdAt" db:"created_at"`
	UpdatedAt    string       `json:"updatedAt" db:"updated_at"`
}

// DefaultRoles defines the built-in system roles.
var DefaultRoles = map[string]*Role{
	"super_admin": {
		ID:          "super_admin",
		Name:        "Super Administrator",
		Description: "Full system access with all capabilities",
		Capabilities: []Capability{
			CapAll,
		},
		IsSystem: true,
	},
	"admin": {
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full administrative access except system configuration",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
			CapMFAAll,
			CapOAuth2All,
			CapPolicyAll,
			CapAdminTeamRead,
			CapSystemAudit,
			CapSystemHealth,
			CapSCIMAll,
		},
		IsSystem: true,
	},
	"user_manager": {
		ID:          "user_manager",
		Name:        "User Manager",
		Description: "User and session management capabilities",
		Capabilities: []Capability{
			CapUsersAll,
			CapSessionsAll,
			CapMFARead,
			CapMFAManage,
			CapSCIMUsersRead,
			CapSCIMUsersWrite,
			CapSCIMGroupsRead,
		},
		IsSystem: true,
	},
	"security_manager": {
		ID:          "security_manager",
		Name:        "Security Manager",
		Description: "Security and authentication configuration",
		Capabilities: []Capability{
			CapUsersRead,
			CapSessionsRead,
			CapSessionsRevoke,
			CapMFAAll,
			CapOAuth2All,
			CapPolicyAll,
			CapSystemAudit,
		},
		IsSystem: true,
	},
	"auditor": {
		ID:          "auditor",
		Name:        "Auditor",
		Description: "Read-only access to audit and compliance data",
		Capabilities: []Capability{
			CapUsersRead,
			CapSessionsRead,
			CapMFARead,
			CapOAuth2ClientsRead,
			CapOAuth2TokensRead,
			CapPolicyRead,
			CapSystemAudit,
			CapSystemHealth,
			CapAdminTeamRead,
		},
		IsSystem: true,
	},
	"scim_manager": {
		ID:          "scim_manager",
		Name:        "SCIM Manager",
		Description: "SCIM provisioning and integration management",
		Capabilities: []Capability{
			CapUsersRead,
			CapUsersCreate,
			CapUsersUpdate,
			CapUsersSuspend,
			CapSCIMAll,
		},
		IsSystem: true,
	},
}

// AllCapabilities returns all defined capabilities grouped by domain.
var AllCapabilities = map[string][]Capability{
	"users": {
		CapUsersRead,
		CapUsersCreate,
		CapUsersUpdate,
		CapUsersDelete,
		CapUsersSuspend,
		CapUsersAll,
	},
	"sessions": {
		CapSessionsRead,
		CapSessionsRevoke,
		CapSessionsAll,
	},
	"mfa": {
		CapMFARead,
		CapMFAManage,
		CapMFAAll,
	},
	"oauth2": {
		CapOAuth2ClientsRead,
		CapOAuth2ClientsManage,
		CapOAuth2TokensRead,
		CapOAuth2TokensRevoke,
		CapOAuth2All,
	},
	"policy": {
		CapPolicyRead,
		CapPolicyManage,
		CapPolicyAll,
	},
	"system": {
		CapSystemConfig,
		CapSystemAudit,
		CapSystemHealth,
		CapSystemAll,
	},
	"admin_team": {
		CapAdminTeamRead,
		CapAdminTeamManage,
		CapAdminTeamAll,
	},
	"scim": {
		CapSCIMUsersRead,
		CapSCIMUsersWrite,
		CapSCIMGroupsRead,
		CapSCIMGroupsWrite,
		CapSCIMTokensManage,
		CapSCIMAll,
	},
	"global": {
		CapAll,
	},
}

// CapabilityInfo provides metadata about a capability.
type CapabilityInfo struct {
	Name        Capability `json:"name"`
	Domain      string     `json:"domain"`
	Description string     `json:"description"`
	IsWildcard  bool       `json:"isWildcard"`
}

// AllCapabilityInfo provides detailed information about all capabilities.
var AllCapabilityInfo = map[Capability]CapabilityInfo{
	// Users domain
	CapUsersRead:    {CapUsersRead, "users", "View user information and list users", false},
	CapUsersCreate:  {CapUsersCreate, "users", "Create new user accounts", false},
	CapUsersUpdate:  {CapUsersUpdate, "users", "Update user profile and settings", false},
	CapUsersDelete:  {CapUsersDelete, "users", "Delete user accounts", false},
	CapUsersSuspend: {CapUsersSuspend, "users", "Suspend and unsuspend user accounts", false},
	CapUsersAll:     {CapUsersAll, "users", "All user management capabilities", true},

	// Sessions domain
	CapSessionsRead:   {CapSessionsRead, "sessions", "View active user sessions", false},
	CapSessionsRevoke: {CapSessionsRevoke, "sessions", "Revoke user sessions", false},
	CapSessionsAll:    {CapSessionsAll, "sessions", "All session management capabilities", true},

	// MFA domain
	CapMFARead:   {CapMFARead, "mfa", "View MFA settings and status", false},
	CapMFAManage: {CapMFAManage, "mfa", "Configure MFA settings and reset MFA devices", false},
	CapMFAAll:    {CapMFAAll, "mfa", "All MFA management capabilities", true},

	// OAuth2 domain
	CapOAuth2ClientsRead:   {CapOAuth2ClientsRead, "oauth2", "View OAuth2 clients and applications", false},
	CapOAuth2ClientsManage: {CapOAuth2ClientsManage, "oauth2", "Create and manage OAuth2 clients", false},
	CapOAuth2TokensRead:    {CapOAuth2TokensRead, "oauth2", "View OAuth2 tokens and grants", false},
	CapOAuth2TokensRevoke:  {CapOAuth2TokensRevoke, "oauth2", "Revoke OAuth2 tokens", false},
	CapOAuth2All:           {CapOAuth2All, "oauth2", "All OAuth2 management capabilities", true},

	// Policy domain
	CapPolicyRead:   {CapPolicyRead, "policy", "View policies and access rules", false},
	CapPolicyManage: {CapPolicyManage, "policy", "Create and modify policies", false},
	CapPolicyAll:    {CapPolicyAll, "policy", "All policy management capabilities", true},

	// System domain
	CapSystemConfig: {CapSystemConfig, "system", "Modify system configuration", false},
	CapSystemAudit:  {CapSystemAudit, "system", "View audit logs and compliance data", false},
	CapSystemHealth: {CapSystemHealth, "system", "View system health and metrics", false},
	CapSystemAll:    {CapSystemAll, "system", "All system management capabilities", true},

	// Admin team domain
	CapAdminTeamRead:   {CapAdminTeamRead, "admin_team", "View admin team members and roles", false},
	CapAdminTeamManage: {CapAdminTeamManage, "admin_team", "Manage admin team members and permissions", false},
	CapAdminTeamAll:    {CapAdminTeamAll, "admin_team", "All admin team management capabilities", true},

	// SCIM domain
	CapSCIMUsersRead:    {CapSCIMUsersRead, "scim", "Read SCIM user resources", false},
	CapSCIMUsersWrite:   {CapSCIMUsersWrite, "scim", "Create, update, and delete SCIM users", false},
	CapSCIMGroupsRead:   {CapSCIMGroupsRead, "scim", "Read SCIM group resources", false},
	CapSCIMGroupsWrite:  {CapSCIMGroupsWrite, "scim", "Create, update, and delete SCIM groups", false},
	CapSCIMTokensManage: {CapSCIMTokensManage, "scim", "Manage SCIM API tokens", false},
	CapSCIMAll:          {CapSCIMAll, "scim", "All SCIM management capabilities", true},

	// Global
	CapAll: {CapAll, "global", "All system capabilities (superuser)", true},
}