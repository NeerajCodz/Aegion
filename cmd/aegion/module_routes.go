package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/platform/config"
)

// ModuleRouteTable is the core-owned, static public API map. It is generated
// only from the resolved module plan; a module registration cannot add or
// rewrite a public route.
type ModuleRouteTable struct {
	routes []moduleGatewayRoute
}

type moduleGatewayRoute struct {
	moduleID string
	method   string
	prefix   string
}

// NewModuleRouteTable produces the public gateway table for enabled external
// modules. The plan catalog has already rejected duplicate and reserved routes.
func NewModuleRouteTable(plan config.ModulePlan) (ModuleRouteTable, error) {
	table := ModuleRouteTable{}
	for _, module := range plan.Modules {
		if module.Mode != config.ModuleModeExternal {
			continue
		}
		for _, route := range module.PublicRoutes {
			if strings.TrimSpace(route.Method) == "" || strings.TrimSpace(route.Prefix) == "" {
				return ModuleRouteTable{}, fmt.Errorf("module %q has an incomplete public route", module.ID)
			}
			table.routes = append(table.routes, moduleGatewayRoute{
				moduleID: module.ID,
				method:   route.Method,
				prefix:   route.Prefix,
			})
		}
	}
	return table, nil
}

// Match returns the uniquely owning module for a method/path pair.
func (t ModuleRouteTable) Match(method, requestPath string) (string, bool) {
	var (
		moduleID string
		matchLen int
	)
	for _, route := range t.routes {
		if route.method != "*" && route.method != method {
			continue
		}
		if requestPath != route.prefix && !strings.HasPrefix(requestPath, route.prefix+"/") {
			continue
		}
		if len(route.prefix) <= matchLen {
			continue
		}
		moduleID = route.moduleID
		matchLen = len(route.prefix)
	}
	return moduleID, moduleID != ""
}

func (t ModuleRouteTable) owns(moduleID, requestPath string) bool {
	for _, route := range t.routes {
		if route.moduleID != moduleID {
			continue
		}
		if requestPath == route.prefix || strings.HasPrefix(requestPath, route.prefix+"/") {
			return true
		}
	}
	return false
}

func (s *Server) moduleEndpointURL(moduleID, modulePath string) (*url.URL, error) {
	if !s.moduleRoutes.owns(moduleID, modulePath) {
		return nil, fmt.Errorf("module %q does not own route %q", moduleID, modulePath)
	}
	plannedModule, ok := s.modulePlan.Module(moduleID)
	if !ok || plannedModule.Mode != config.ModuleModeExternal {
		return nil, fmt.Errorf("module %q is not enabled as an external workload", moduleID)
	}

	module, err := s.registry.GetModule(moduleID)
	if err != nil {
		return nil, err
	}
	if module.Status != registry.StatusHealthy {
		return nil, errors.New("module is not healthy")
	}

	for _, endpoint := range module.Endpoints {
		if endpoint.Type != registry.EndpointHTTP {
			continue
		}
		parsed, parseErr := url.Parse(strings.TrimSpace(endpoint.URL))
		if parseErr != nil {
			return nil, parseErr
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("module endpoint must use http or https")
		}
		if parsed.Host == "" || parsed.User != nil {
			return nil, errors.New("module endpoint is missing a valid host")
		}
		parsed.Path = modulePath
		parsed.RawPath = ""
		return parsed, nil
	}

	return nil, errors.New("module has no HTTP endpoint")
}
