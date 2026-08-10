package main

import (
	"testing"

	"github.com/aegion/aegion/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestModuleRouteTableUsesResolvedExternalModules(t *testing.T) {
	plan, err := config.ResolveModulePlan(&config.Config{ModuleVersions: map[string]string{
		"oauth2": "v1.2.3",
		"policy": "v1.2.3",
	}})
	require.NoError(t, err)

	table, err := NewModuleRouteTable(plan)
	require.NoError(t, err)

	moduleID, ok := table.Match("GET", "/.well-known/jwks.json")
	require.True(t, ok)
	require.Equal(t, "oauth2", moduleID)

	moduleID, ok = table.Match("POST", "/oauth2/token")
	require.True(t, ok)
	require.Equal(t, "oauth2", moduleID)

	_, ok = table.Match("POST", "/.well-known/jwks.json")
	require.False(t, ok)
	_, ok = table.Match("GET", "/internal/registry/register")
	require.False(t, ok)
}

func TestModuleRouteTableExcludesDisabledModuleRoutes(t *testing.T) {
	plan, err := config.ResolveModulePlan(&config.Config{ModuleVersions: map[string]string{"oauth2": "off"}})
	require.NoError(t, err)

	table, err := NewModuleRouteTable(plan)
	require.NoError(t, err)
	_, ok := table.Match("GET", "/.well-known/jwks.json")
	require.False(t, ok)
}
