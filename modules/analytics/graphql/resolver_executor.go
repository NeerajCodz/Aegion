package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ResolverExecutor is the bounded production executor for this module's
// single-root-operation GraphQL contract. It deliberately permits one root
// field per request; batching and subscriptions are not exposed on the HTTP
// endpoint, preventing a request from multiplying database work.
type ResolverExecutor struct {
	resolver *Resolver
}

func NewResolverExecutor(resolver *Resolver) (*ResolverExecutor, error) {
	if resolver == nil {
		return nil, errors.New("graphql resolver is required")
	}
	return &ResolverExecutor{resolver: resolver}, nil
}

func (e *ResolverExecutor) Execute(ctx context.Context, query, operationName string, variables map[string]interface{}) (*ExecutionResult, error) {
	operation, field, err := rootOperation(query)
	if err != nil {
		return graphqlExecutionError(err), nil
	}
	if operationName != "" {
		// This executor accepts one root operation. A supplied operation name is
		// meaningful only as a client trace label, not a selector for hidden work.
		operationName = strings.TrimSpace(operationName)
	}

	var value interface{}
	switch operation {
	case "query":
		switch field {
		case "events":
			var filter *EventFilter
			var first *int
			var after *string
			var sort *SortInput
			if err := decodeVariable(variables, "filter", &filter); err != nil { return graphqlExecutionError(err), nil }
			if err := decodeVariable(variables, "first", &first); err != nil { return graphqlExecutionError(err), nil }
			if err := decodeVariable(variables, "after", &after); err != nil { return graphqlExecutionError(err), nil }
			if err := decodeVariable(variables, "sort", &sort); err != nil { return graphqlExecutionError(err), nil }
			value, err = e.resolver.Events(ctx, filter, first, after, sort)
		case "event":
			var id string; err = requiredVariable(variables, "id", &id); if err == nil { value, err = e.resolver.Event(ctx, id) }
		case "dashboards":
			var isDefault, public *bool
			if err = decodeVariable(variables, "isDefault", &isDefault); err == nil { err = decodeVariable(variables, "public", &public) }
			if err == nil { value, err = e.resolver.Dashboards(ctx, isDefault, public) }
		case "dashboard":
			var id string; err = requiredVariable(variables, "id", &id); if err == nil { value, err = e.resolver.Dashboard(ctx, id) }
		case "queries":
			var limit, offset *int
			if err = decodeVariable(variables, "limit", &limit); err == nil { err = decodeVariable(variables, "offset", &offset) }
			if err == nil { value, err = e.resolver.Queries(ctx, limit, offset) }
		case "query":
			var id string; err = requiredVariable(variables, "id", &id); if err == nil { value, err = e.resolver.Query(ctx, id) }
		case "health":
			value, err = e.resolver.Health(ctx)
		case "stats":
			value, err = e.resolver.Stats(ctx)
		case "metrics":
			var category *string; var timeRange *TimeRangeInput
			if err = decodeVariable(variables, "category", &category); err == nil { err = decodeVariable(variables, "timeRange", &timeRange) }
			if err == nil { value, err = e.resolver.Metrics(ctx, category, timeRange) }
		default:
			err = fmt.Errorf("unsupported query field %q", field)
		}
	case "mutation":
		switch field {
		case "createDashboard":
			var input CreateDashboardInput; err = requiredVariable(variables, "input", &input); if err == nil { value, err = e.resolver.CreateDashboard(ctx, &input) }
		case "updateDashboard":
			var id string; var input UpdateDashboardInput
			err = requiredVariable(variables, "id", &id); if err == nil { err = requiredVariable(variables, "input", &input) }; if err == nil { value, err = e.resolver.UpdateDashboard(ctx, id, &input) }
		case "deleteDashboard":
			var id string; err = requiredVariable(variables, "id", &id); if err == nil { value, err = e.resolver.DeleteDashboard(ctx, id) }
		case "saveQuery":
			var input SaveQueryInput; err = requiredVariable(variables, "input", &input); if err == nil { value, err = e.resolver.SaveQuery(ctx, &input) }
		case "deleteQuery":
			var id string; err = requiredVariable(variables, "id", &id); if err == nil { value, err = e.resolver.DeleteQuery(ctx, id) }
		case "createWebhook":
			var input CreateWebhookInput; err = requiredVariable(variables, "input", &input); if err == nil { value, err = e.resolver.CreateWebhook(ctx, &input) }
		case "executeQuery":
			var statement string; var timeout *int
			err = requiredVariable(variables, "sql", &statement); if err == nil { err = decodeVariable(variables, "timeout", &timeout) }; if err == nil { value, err = e.resolver.ExecuteQuery(ctx, statement, timeout) }
		case "createReport":
			err = errors.New("createReport is not available")
		default:
			err = fmt.Errorf("unsupported mutation field %q", field)
		}
	default:
		err = fmt.Errorf("unsupported operation %q", operation)
	}
	if err != nil { return graphqlExecutionError(err), nil }
	return &ExecutionResult{Data: map[string]interface{}{field: value}}, nil
}

func graphqlExecutionError(err error) *ExecutionResult {
	return &ExecutionResult{Errors: []*GraphQLError{{Message: "request could not be completed", Extensions: map[string]interface{}{"code": "GRAPHQL_EXECUTION_FAILED"}}}}
}

func requiredVariable(variables map[string]interface{}, name string, destination interface{}) error {
	if variables == nil { return fmt.Errorf("variable %q is required", name) }
	if _, ok := variables[name]; !ok { return fmt.Errorf("variable %q is required", name) }
	return decodeVariable(variables, name, destination)
}

func decodeVariable(variables map[string]interface{}, name string, destination interface{}) error {
	if variables == nil { return nil }
	value, ok := variables[name]
	if !ok || value == nil { return nil }
	encoded, err := json.Marshal(value)
	if err != nil { return fmt.Errorf("encode variable %q: %w", name, err) }
	if err := json.Unmarshal(encoded, destination); err != nil { return fmt.Errorf("invalid variable %q", name) }
	return nil
}

func rootOperation(query string) (string, string, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" { return "", "", errors.New("query is required") }
	operation := "query"
	if strings.HasPrefix(trimmed, "mutation") { operation, trimmed = "mutation", strings.TrimSpace(strings.TrimPrefix(trimmed, "mutation"))
	} else if strings.HasPrefix(trimmed, "query") { trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "query"))
	} else if strings.HasPrefix(trimmed, "subscription") { return "", "", errors.New("subscriptions are not available over HTTP") }
	open := strings.IndexByte(trimmed, '{')
	if open < 0 { return "", "", errors.New("operation selection is required") }
	field := strings.TrimSpace(trimmed[open+1:])
	if field == "" { return "", "", errors.New("operation field is required") }
	end := 0
	for end < len(field) && ((field[end] >= 'a' && field[end] <= 'z') || (field[end] >= 'A' && field[end] <= 'Z') || (field[end] >= '0' && field[end] <= '9') || field[end] == '_') { end++ }
	if end == 0 { return "", "", errors.New("invalid operation field") }
	return operation, field[:end], nil
}
