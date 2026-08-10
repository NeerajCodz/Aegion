package config

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveModulePlan(t *testing.T) {
	cfg := &Config{ModuleVersions: map[string]string{
		"admin":         "v1.2.3",
		"analytics":     "v1.2.3",
		"introspection": "v1.2.3",
		"oauth2":        "v1.2.3",
		"policy":        "v1.2.3",
	}}

	plan, err := ResolveModulePlan(cfg)
	require.NoError(t, err)

	var got []string
	for _, module := range plan.Modules {
		got = append(got, module.ID)
		_, err := fs.Stat(module.Migration.Filesystem, module.Migration.BasePath)
		require.NoErrorf(t, err, "migration source for %s", module.ID)
	}
	require.Equal(t, []string{"analytics", "oauth2", "introspection", "policy", "admin"}, got)

	policy, ok := plan.Module("policy")
	require.True(t, ok)
	require.Equal(t, ModuleModeEmbedded, policy.Mode)
	require.Empty(t, policy.Image)

	oauth2, ok := plan.Module("oauth2")
	require.True(t, ok)
	require.Equal(t, ModuleModeExternal, oauth2.Mode)
	require.Equal(t, "aegion/aegion-oauth2:v1.2.3", oauth2.Image)
	require.Contains(t, oauth2.InternalPermissions, "identity:read")
	require.Contains(t, oauth2.PublicRoutes, ModuleRoute{Method: "*", Prefix: "/oauth2"})
}

func TestResolveModulePlanRejectsUnknownAndMissingDependencies(t *testing.T) {
	_, err := ResolveModulePlan(&Config{ModuleVersions: map[string]string{"unknown": "v1.0.0"}})
	require.ErrorContains(t, err, `unknown module "unknown"`)

	_, err = ResolveModulePlan(&Config{ModuleVersions: map[string]string{"introspection": "v1.0.0"}})
	require.ErrorContains(t, err, `module "introspection" requires "oauth2"`)
}

func TestResolveModulePlanRecognizesAllDisabledVersions(t *testing.T) {
	for _, disabled := range []string{"", "disabled", "disable", "off", "false", "0", "none", " OFF "} {
		t.Run(strings.TrimSpace(disabled), func(t *testing.T) {
			plan, err := ResolveModulePlan(&Config{ModuleVersions: map[string]string{"oauth2": disabled}})
			require.NoError(t, err)
			require.Empty(t, plan.Modules)
		})
	}
}

func TestResolveModulePlanProductionDeploymentRequirements(t *testing.T) {
	t.Setenv("AEGION_ENV", "production")
	t.Setenv("AEGION_ENVIRONMENT", "")

	base := &Config{
		ModuleVersions: map[string]string{"oauth2": "v1.2.3"},
		Modules: map[string]ModuleDeploymentConfig{
			"oauth2": {
				Image:          "ghcr.io/aegion/aegion-oauth2:v1.2.3",
				PublicURL:      "https://oauth.example.test",
				DatabaseURL:    "postgres://oauth:secret@postgres/oauth?sslmode=verify-full",
				CACertFile:     "/run/secrets/core-ca.pem",
				CredentialFile: "/run/secrets/oauth2-credential",
			},
		},
	}

	plan, err := ResolveModulePlan(base)
	require.NoError(t, err)
	require.Len(t, plan.Modules, 1)

	missingDatabase := *base
	missingDatabase.Modules = map[string]ModuleDeploymentConfig{"oauth2": {
		Image:          "ghcr.io/aegion/aegion-oauth2:v1.2.3",
		PublicURL:      "https://oauth.example.test",
		CACertFile:     "/run/secrets/core-ca.pem",
		CredentialFile: "/run/secrets/oauth2-credential",
	}}
	_, err = ResolveModulePlan(&missingDatabase)
	require.ErrorContains(t, err, "modules.oauth2.database_url is required")

	latest := *base
	latest.ModuleVersions = map[string]string{"oauth2": "latest"}
	_, err = ResolveModulePlan(&latest)
	require.ErrorContains(t, err, "module_versions.oauth2 must be pinned in production")

	invalidURL := *base
	invalidURL.Modules = map[string]ModuleDeploymentConfig{"oauth2": {
		Image:          "ghcr.io/aegion/aegion-oauth2:v1.2.3",
		PublicURL:      "http://oauth.example.test",
		DatabaseURL:    "postgres://oauth:secret@postgres/oauth?sslmode=verify-full",
		CACertFile:     "/run/secrets/core-ca.pem",
		CredentialFile: "/run/secrets/oauth2-credential",
	}}
	_, err = ResolveModulePlan(&invalidURL)
	require.ErrorContains(t, err, "modules.oauth2.public_url must be an absolute https URL in production")
}

func TestModuleRouteValidationRejectsReservedAndOverlappingRoutes(t *testing.T) {
	require.ErrorContains(t, validateModuleRoute("test", ModuleRoute{Method: "GET", Prefix: "/internal/test"}), "reserved core prefix")
	require.True(t, routesOverlap(
		ModuleRoute{Method: "*", Prefix: "/oauth2"},
		ModuleRoute{Method: "POST", Prefix: "/oauth2/token"},
	))
	require.False(t, routesOverlap(
		ModuleRoute{Method: "GET", Prefix: "/oauth2"},
		ModuleRoute{Method: "POST", Prefix: "/oauth2"},
	))
}
