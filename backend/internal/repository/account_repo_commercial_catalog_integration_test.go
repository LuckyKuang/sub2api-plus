//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestListCommercialModelMappingsKeepsConfiguredStateAndExactGroupBindings(t *testing.T) {
	// Catalog configuration is independent of live health: disabled, manually
	// unschedulable and deleted accounts are excluded; cooldowns and expiry
	// remain visible. Only explicitly requested group bindings contribute.
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	allowed := mustCreateGroup(t, client, &service.Group{Name: "commercial-projection-allowed"})
	other := mustCreateGroup(t, client, &service.Group{Name: "commercial-projection-other"})
	create := func(name string, groupIDs ...int64) *service.Account {
		a := mustCreateAccount(t, client, &service.Account{
			Name: name, Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"access_token": "never-projected", "model_mapping": map[string]any{name: "private-target"}},
		})
		for _, groupID := range groupIDs {
			_, err := client.AccountGroup.Create().SetAccountID(a.ID).SetGroupID(groupID).SetPriority(50).Save(ctx)
			require.NoError(t, err)
		}
		return a
	}
	create("active", allowed.ID)
	create("shared", allowed.ID, other.ID)
	create("other-only", other.ID)
	create("ungrouped")
	disabled := create("disabled", allowed.ID)
	_, err := client.Account.UpdateOneID(disabled.ID).SetStatus(service.StatusDisabled).Save(ctx)
	require.NoError(t, err)
	unschedulable := create("unschedulable", allowed.ID)
	_, err = client.Account.UpdateOneID(unschedulable.ID).SetSchedulable(false).Save(ctx)
	require.NoError(t, err)
	deleted := create("deleted", allowed.ID)
	_, err = client.Account.UpdateOneID(deleted.ID).SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	cooldown := create("cooldown-and-expired", allowed.ID)
	_, err = client.Account.UpdateOneID(cooldown.ID).
		SetRateLimitResetAt(time.Now().Add(time.Hour)).
		SetOverloadUntil(time.Now().Add(time.Hour)).
		SetTempUnschedulableUntil(time.Now().Add(time.Hour)).
		SetExpiresAt(time.Now().Add(-time.Hour)).SetAutoPauseOnExpired(true).Save(ctx)
	require.NoError(t, err)
	noMapping := create("no-mapping", allowed.ID)
	_, err = client.Account.UpdateOneID(noMapping.ID).SetCredentials(map[string]any{"access_token": "not-a-model"}).Save(ctx)
	require.NoError(t, err)

	out, err := repo.ListCommercialModelMappings(ctx, []int64{allowed.ID})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.ElementsMatch(t, []service.CommercialModelMapping{
		{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"active": "private-target"}},
		{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"shared": "private-target"}},
		{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"cooldown-and-expired": "private-target"}},
		{Platform: service.PlatformOpenAI},
	}, out[allowed.ID])

	out, err = repo.ListCommercialModelMappings(ctx, []int64{allowed.ID, other.ID})
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.ElementsMatch(t, []service.CommercialModelMapping{
		{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"shared": "private-target"}},
		{Platform: service.PlatformOpenAI, ModelMapping: map[string]any{"other-only": "private-target"}},
	}, out[other.ID])
	// Pure catalog reads must not recover, pause or otherwise mutate accounts.
	stored, err := client.Account.Get(ctx, cooldown.ID)
	require.NoError(t, err)
	require.True(t, stored.Schedulable)
	require.NotNil(t, stored.RateLimitResetAt)
	require.Equal(t, "never-projected", stored.Credentials["access_token"])
}
