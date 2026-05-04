package graphql

import (
	"context"
	"testing"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/aegion/aegion/modules/analytics/rbac"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalResolverCreateWebhookUsesEventTypes(t *testing.T) {
	store := NewMockStore()
	resolver := NewResolver(logger.TestLogger(), store)

	payload, err := resolver.CreateWebhook(withResolverRole("analyst-1", rbac.RoleAnalyst), &CreateWebhookInput{
		URL:       "https://example.com/webhook",
		EventType: "auth.login",
		Active:    boolPtr(true),
	})
	require.NoError(t, err)
	require.NotNil(t, payload.Webhook)
	require.Len(t, store.webhooks, 1)
	assert.Equal(t, []string{"auth.login"}, store.webhooks[0].EventTypes)
	assert.Equal(t, "auth.login", payload.Webhook.EventType)
}

func TestAdditionalResolverDashboardRequiresUserContext(t *testing.T) {
	store := NewMockStore()
	resolver := NewResolver(logger.TestLogger(), store)

	payload, err := resolver.CreateDashboard(context.Background(), &CreateDashboardInput{
		Name:   "No User",
		Config: map[string]interface{}{"layout": "grid"},
	})
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Nil(t, payload.Dashboard)
	require.NotEmpty(t, payload.Errors)
	assert.Contains(t, payload.Errors[0].Message, "unauthorized")
}

func TestAdditionalResolverCreateWebhookRequiresPermission(t *testing.T) {
	store := NewMockStore()
	resolver := NewResolver(logger.TestLogger(), store)

	payload, err := resolver.CreateWebhook(withResolverRole("viewer-1", rbac.RoleViewer), &CreateWebhookInput{
		URL:       "https://example.com/webhook",
		EventType: "auth.login",
	})
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Nil(t, payload.Webhook)
	require.NotEmpty(t, payload.Errors)
	assert.Contains(t, payload.Errors[0].Message, "missing permission")
}

func withResolverRole(userID string, role rbac.Role) context.Context {
	ctx := context.WithValue(context.Background(), "userID", userID)
	manager := rbac.NewManager()
	_ = manager.SetUserRole(userID, role)
	return rbac.WithManager(ctx, manager)
}
