package main

import (
	"fmt"
	"strings"

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
