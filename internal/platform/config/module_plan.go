package config

import (
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strings"

	moduleassets "github.com/aegion/aegion/modules"
)

// ModuleMode describes where a module runs. Embedded modules are part of the
// core process; external modules are deployment-owned workloads.
type ModuleMode string

const (
	ModuleModeEmbedded ModuleMode = "embedded"
	ModuleModeExternal ModuleMode = "external"
)

// ModuleDeploymentConfig contains the deployment references core validates for
// an external module. CredentialFile is a mounted secret reference, never the
// credential itself.
type ModuleDeploymentConfig struct {
	Image          string `yaml:"image"`
	PublicURL      string `yaml:"public_url"`
	DatabaseURL    string `yaml:"database_url"`
	CACertFile     string `yaml:"ca_cert_file"`
	ClientCertFile string `yaml:"client_cert_file"`
	ClientKeyFile  string `yaml:"client_key_file"`
	CredentialFile string `yaml:"credential_file"`
}

// ModuleMigration identifies the embedded SQL source owned by the core
// migrator. External workloads use it only for schema verification.
type ModuleMigration struct {
	Filesystem fs.FS
	BasePath   string
}

// ModuleRoute is a canonical public route owned by a module. Routes are
// compile-time catalog data; registration metadata cannot create or change one.
type ModuleRoute struct {
	Method string
	Prefix string
}

// ResolvedModule is the immutable deployment and migration contract for one
// enabled module.
type ResolvedModule struct {
	ID                  string
	Mode                ModuleMode
	Version             string
	DependsOn           []string
	Migration           ModuleMigration
	PublicRoutes        []ModuleRoute
	InternalPermissions []string
	Image               string
}

// ModulePlan is the deterministic enabled module graph. Modules are in
// dependency order and do not include the implicit core root.
type ModulePlan struct {
	Modules []ResolvedModule
}

// Module returns an enabled module by ID.
func (p ModulePlan) Module(id string) (ResolvedModule, bool) {
	for _, module := range p.Modules {
		if module.ID == id {
			return cloneResolvedModule(module), true
		}
	}
	return ResolvedModule{}, false
}

// Enabled reports whether an ID is present in this plan.
func (p ModulePlan) Enabled(id string) bool {
	_, ok := p.Module(id)
	return ok
}

type moduleDefinition struct {
	id                  string
	mode                ModuleMode
	dependencies        []string
	publicRoutes        []ModuleRoute
	internalPermissions []string
}

var moduleCatalog = map[string]moduleDefinition{
	"admin": {
		id:           "admin",
		mode:         ModuleModeExternal,
		dependencies: []string{"core", "policy"},
		publicRoutes: []ModuleRoute{{Method: "*", Prefix: "/aegion"}},
		internalPermissions: []string{
			"admin:manage",
		},
	},
	"analytics": {
		id:           "analytics",
		mode:         ModuleModeExternal,
		dependencies: []string{"core"},
		publicRoutes: []ModuleRoute{
			{Method: "*", Prefix: "/api/v1/analytics"},
			{Method: "*", Prefix: "/graphql/analytics"},
		},
	},
	"introspection": {
		id:           "introspection",
		mode:         ModuleModeExternal,
		dependencies: []string{"oauth2"},
		publicRoutes: []ModuleRoute{{Method: "POST", Prefix: "/api/v1/introspection/token"}},
		internalPermissions: []string{
			"oauth2:introspect",
		},
	},
	"magic_link": {
		id:           "magic_link",
		mode:         ModuleModeEmbedded,
		dependencies: []string{"core"},
	},
	"mfa": {
		id:           "mfa",
		mode:         ModuleModeExternal,
		dependencies: []string{"core", "password"},
		publicRoutes: []ModuleRoute{{Method: "*", Prefix: "/api/v1/self-service/mfa"}},
		internalPermissions: []string{
			"session:step_up",
		},
	},
	"oauth2": {
		id:           "oauth2",
		mode:         ModuleModeExternal,
		dependencies: []string{"core"},
		publicRoutes: []ModuleRoute{
			{Method: "GET", Prefix: "/.well-known/openid-configuration"},
			{Method: "GET", Prefix: "/.well-known/jwks.json"},
			{Method: "*", Prefix: "/oidc/userinfo"},
			{Method: "*", Prefix: "/oauth2"},
		},
		internalPermissions: []string{"identity:read"},
	},
	"passkeys": {
		id:           "passkeys",
		mode:         ModuleModeExternal,
		dependencies: []string{"core"},
		publicRoutes: []ModuleRoute{{Method: "*", Prefix: "/api/v1/self-service/passkeys"}},
		internalPermissions: []string{
			"session:create",
		},
	},
	"password": {
		id:           "password",
		mode:         ModuleModeEmbedded,
		dependencies: []string{"core"},
	},
	"policy": {
		id:           "policy",
		mode:         ModuleModeEmbedded,
		dependencies: []string{"core"},
		internalPermissions: []string{
			"policy:check",
		},
	},
	"proxy": {
		id:                  "proxy",
		mode:                ModuleModeExternal,
		dependencies:        []string{"core"},
		internalPermissions: []string{"proxy:manage"},
	},
	"social": {
		id:           "social",
		mode:         ModuleModeExternal,
		dependencies: []string{"core"},
		publicRoutes: []ModuleRoute{{Method: "*", Prefix: "/api/v1/self-service/social"}},
		internalPermissions: []string{
			"identity:provision",
		},
	},
	"sso": {
		id:           "sso",
		mode:         ModuleModeExternal,
		dependencies: []string{"core"},
		publicRoutes: []ModuleRoute{{Method: "*", Prefix: "/api/v1/self-service/sso"}},
		internalPermissions: []string{
			"identity:provision",
		},
	},
}

// ResolveModulePlan resolves the only supported module topology. It validates
// module IDs, versions, dependencies, canonical public route ownership, and
// production deployment prerequisites before workloads are started.
func ResolveModulePlan(cfg *Config) (ModulePlan, error) {
	if cfg == nil {
		return ModulePlan{}, fmt.Errorf("module plan requires configuration")
	}

	enabled := make(map[string]struct{}, len(cfg.ModuleVersions))
	for moduleID, version := range cfg.ModuleVersions {
		if !moduleVersionEnabled(version) {
			continue
		}
		if _, ok := moduleCatalog[moduleID]; !ok {
			return ModulePlan{}, fmt.Errorf("unknown module %q", moduleID)
		}
		enabled[moduleID] = struct{}{}
	}

	if err := validateEnabledDependencies(enabled); err != nil {
		return ModulePlan{}, err
	}
	if err := validateCatalogRoutes(); err != nil {
		return ModulePlan{}, err
	}

	order, err := orderedEnabledModules(enabled)
	if err != nil {
		return ModulePlan{}, err
	}

	plan := ModulePlan{Modules: make([]ResolvedModule, 0, len(order))}
	for _, moduleID := range order {
		definition := moduleCatalog[moduleID]
		version := strings.TrimSpace(cfg.ModuleVersions[moduleID])
		module := ResolvedModule{
			ID:                  definition.id,
			Mode:                definition.mode,
			Version:             version,
			DependsOn:           slices.Clone(definition.dependencies),
			Migration:           ModuleMigration{Filesystem: moduleassets.MigrationFiles, BasePath: path.Join(moduleID, "migrations")},
			PublicRoutes:        slices.Clone(definition.publicRoutes),
			InternalPermissions: slices.Clone(definition.internalPermissions),
		}
		if module.Mode == ModuleModeExternal {
			deployment := cfg.Modules[module.ID]
			module.Image = configuredModuleImage(cfg.ModuleRegistry.BaseURL, module.ID, version, deployment.Image)
			if isProductionEnvironment() {
				if err := validateProductionDeployment(module, deployment); err != nil {
					return ModulePlan{}, err
				}
			}
		}
		plan.Modules = append(plan.Modules, module)
	}

	return plan, nil
}

func moduleVersionEnabled(version string) bool {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "", "disabled", "disable", "off", "false", "0", "none":
		return false
	default:
		return true
	}
}

func validateEnabledDependencies(enabled map[string]struct{}) error {
	for moduleID := range enabled {
		definition := moduleCatalog[moduleID]
		for _, dependency := range definition.dependencies {
			if dependency == "core" {
				continue
			}
			if _, ok := enabled[dependency]; !ok {
				return fmt.Errorf("module %q requires %q", moduleID, dependency)
			}
		}
	}
	return nil
}

func orderedEnabledModules(enabled map[string]struct{}) ([]string, error) {
	indegree := make(map[string]int, len(enabled))
	dependents := make(map[string][]string, len(enabled))
	for moduleID := range enabled {
		indegree[moduleID] = 0
	}
	for moduleID := range enabled {
		for _, dependency := range moduleCatalog[moduleID].dependencies {
			if dependency == "core" {
				continue
			}
			indegree[moduleID]++
			dependents[dependency] = append(dependents[dependency], moduleID)
		}
	}

	ready := make([]string, 0, len(enabled))
	for moduleID, degree := range indegree {
		if degree == 0 {
			ready = append(ready, moduleID)
		}
	}
	slices.Sort(ready)

	order := make([]string, 0, len(enabled))
	for len(ready) > 0 {
		moduleID := ready[0]
		ready = ready[1:]
		order = append(order, moduleID)
		for _, dependent := range dependents[moduleID] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
		slices.Sort(ready)
	}
	if len(order) != len(enabled) {
		return nil, fmt.Errorf("resolving module plan: cyclic dependency detected")
	}
	return order, nil
}

func validateCatalogRoutes() error {
	type owner struct {
		moduleID string
		route    ModuleRoute
	}
	seen := make([]owner, 0)
	for _, definition := range moduleCatalog {
		for _, route := range definition.publicRoutes {
			if err := validateModuleRoute(definition.id, route); err != nil {
				return err
			}
			for _, existing := range seen {
				if existing.moduleID != definition.id && routesOverlap(existing.route, route) {
					return fmt.Errorf("public route %s %q is claimed by both %q and %q", route.Method, route.Prefix, existing.moduleID, definition.id)
				}
			}
			seen = append(seen, owner{moduleID: definition.id, route: route})
		}
	}
	return nil
}

func validateModuleRoute(moduleID string, route ModuleRoute) error {
	if strings.TrimSpace(route.Method) == "" || strings.TrimSpace(route.Prefix) == "" {
		return fmt.Errorf("module %q has an incomplete public route", moduleID)
	}
	if !strings.HasPrefix(route.Prefix, "/") || strings.Contains(route.Prefix, "//") {
		return fmt.Errorf("module %q has invalid public route prefix %q", moduleID, route.Prefix)
	}
	for _, reserved := range []string{"/internal", "/health", "/metrics"} {
		if routeHasPrefix(route.Prefix, reserved) {
			return fmt.Errorf("module %q public route %q overlaps reserved core prefix %q", moduleID, route.Prefix, reserved)
		}
	}
	return nil
}

func routesOverlap(left, right ModuleRoute) bool {
	if left.Method != "*" && right.Method != "*" && left.Method != right.Method {
		return false
	}
	return routeHasPrefix(left.Prefix, right.Prefix) || routeHasPrefix(right.Prefix, left.Prefix)
}

func routeHasPrefix(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func configuredModuleImage(registry, moduleID, version, configured string) string {
	if image := strings.TrimSpace(configured); image != "" {
		return image
	}
	base := strings.TrimSuffix(strings.TrimSpace(registry), "/")
	imageName := "aegion/aegion-" + strings.ReplaceAll(moduleID, "_", "-")
	if base != "" {
		imageName = base + "/" + imageName
	}
	return imageName + ":" + version
}

func validateProductionDeployment(module ResolvedModule, deployment ModuleDeploymentConfig) error {
	if !isPinnedVersion(module.Version) {
		return fmt.Errorf("module_versions.%s must be pinned in production", module.ID)
	}
	if !imageMatchesVersion(module.Image, module.Version) {
		return fmt.Errorf("modules.%s.image tag must match module_versions.%s in production", module.ID, module.ID)
	}
	if !isPinnedImage(module.Image) {
		return fmt.Errorf("modules.%s.image must be a pinned image reference in production", module.ID)
	}
	if strings.TrimSpace(deployment.Image) == "" {
		return fmt.Errorf("modules.%s.image is required in production", module.ID)
	}
	if strings.TrimSpace(deployment.DatabaseURL) == "" {
		return fmt.Errorf("modules.%s.database_url is required in production", module.ID)
	}
	if strings.TrimSpace(deployment.CACertFile) == "" {
		return fmt.Errorf("modules.%s.ca_cert_file is required in production", module.ID)
	}
	if strings.TrimSpace(deployment.ClientCertFile) == "" || strings.TrimSpace(deployment.ClientKeyFile) == "" {
		return fmt.Errorf("modules.%s.client_cert_file and modules.%s.client_key_file are required in production", module.ID, module.ID)
	}
	if strings.TrimSpace(deployment.CredentialFile) == "" {
		return fmt.Errorf("modules.%s.credential_file is required in production", module.ID)
	}
	if len(module.PublicRoutes) != 0 {
		publicURL := strings.TrimSpace(deployment.PublicURL)
		if publicURL == "" {
			return fmt.Errorf("modules.%s.public_url is required in production", module.ID)
		}
		parsed, err := url.Parse(publicURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("modules.%s.public_url must be an absolute https URL in production", module.ID)
		}
	}
	return nil
}

func isPinnedVersion(version string) bool {
	version = strings.TrimSpace(version)
	return version != "" && !strings.EqualFold(version, "latest") && !strings.Contains(version, "${")
}

func imageMatchesVersion(image, version string) bool {
	image = strings.TrimSpace(image)
	version = strings.TrimSpace(version)
	return strings.HasSuffix(image, ":"+version)
}

func isPinnedImage(image string) bool {
	image = strings.TrimSpace(image)
	if image == "" || strings.Contains(image, "@sha256:placeholder") {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash || lastColon == len(image)-1 {
		return false
	}
	tag := image[lastColon+1:]
	return !strings.EqualFold(tag, "latest") && !strings.Contains(tag, "${")
}

func cloneResolvedModule(module ResolvedModule) ResolvedModule {
	module.DependsOn = slices.Clone(module.DependsOn)
	module.PublicRoutes = slices.Clone(module.PublicRoutes)
	module.InternalPermissions = slices.Clone(module.InternalPermissions)
	return module
}
