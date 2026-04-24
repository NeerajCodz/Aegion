package graphql

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalResolverCreateWebhookUsesEventTypes(t *testing.T) {
	store := NewMockStore()
	resolver := NewResolver(zerolog.Nop(), store)

	payload, err := resolver.CreateWebhook(context.Background(), &CreateWebhookInput{
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
	resolver := NewResolver(zerolog.Nop(), store)

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
