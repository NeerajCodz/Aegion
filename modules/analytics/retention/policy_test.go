package retention

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetentionPolicyAppliesDefaults(t *testing.T) {
	policy := &RetentionPolicy{}

	require.NoError(t, policy.Validate())
	assert.Equal(t, "tiered", policy.DefaultPolicy)
	assert.Equal(t, 7, policy.Hot.TTLDays)
	assert.Equal(t, 90, policy.Warm.TTLDays)
	assert.Equal(t, 730, policy.Cold.TTLDays)
	assert.Equal(t, CompressionSnappy, policy.Warm.Compression)
}

func TestRetentionPolicyHonorsCategoryOverrides(t *testing.T) {
	policy := DefaultRetentionPolicy()
	policy.Categories["audit_events"] = CategoryRetention{
		HotDays:  30,
		WarmDays: 180,
		ColdDays: 365,
	}

	assert.Equal(t, TierHot, policy.GetTierForTimestamp("audit_events", time.Now().Add(-15*24*time.Hour)))
	assert.Equal(t, TierWarm, policy.GetTierForTimestamp("audit_events", time.Now().Add(-90*24*time.Hour)))
	assert.True(t, policy.IsExpired("audit_events", time.Now().Add(-366*24*time.Hour)))
	assert.False(t, policy.IsExpired("audit_events", time.Now().Add(-300*24*time.Hour)))
}

func TestRetentionPolicyNextTierAndConfig(t *testing.T) {
	policy := DefaultRetentionPolicy()

	assert.Equal(t, TierWarm, policy.NextTier(TierHot))
	assert.Equal(t, TierCold, policy.NextTier(TierWarm))
	assert.Equal(t, TierCold, policy.NextTier(TierCold))

	require.NotNil(t, policy.GetTierConfig(TierHot))
	require.NotNil(t, policy.GetTierConfig(TierWarm))
	require.NotNil(t, policy.GetTierConfig(TierCold))
	assert.Nil(t, policy.GetTierConfig(TierType("unknown")))
}
