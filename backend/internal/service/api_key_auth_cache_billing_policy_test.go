//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotBillingPolicyRoundtripAndIsolation(t *testing.T) {
	// Requirement: cache hits must use the same provider/model policy as a DB
	// read, including a free override; cached policies cannot alias a request.
	id := int64(98)
	key := &APIKey{ID: 7, UserID: 8, Key: "billing-policy-fixture", GroupID: &id,
		User: &User{ID: 8, Status: StatusActive},
		Group: &Group{ID: id, Platform: PlatformComposite, BillingPolicyVersion: 23,
			BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 1.5},
				ModelMultipliers: []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "free-model", Multiplier: 0}}}},
	}
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	key.Group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI] = 99
	require.Equal(t, 1.5, snapshot.Group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	payload, err := json.Marshal(APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))
	loaded, used, err := svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.EqualValues(t, 23, loaded.Group.BillingPolicyVersion)
	require.Equal(t, 1.5, loaded.Group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	require.Zero(t, loaded.Group.BillingRatePolicy.ModelMultipliers[0].Multiplier)
	loaded.Group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI] = 42
	require.Equal(t, 1.5, cached.Snapshot.Group.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	// A pre-policy cache generation must be a miss, never interpreted as a
	// configured policy whose default 1 would undercharge an expensive model.
	cached.Snapshot.Version = 24
	_, used, err = svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.False(t, used)
}
