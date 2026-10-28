//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupBillingPolicyPersistsVersionsAndZero(t *testing.T) {
	// Requirements: SQL remains authoritative for policy versions; explicit
	// free factors survive a round trip, and old empty policies stay empty.
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	ctx := context.Background()
	g := &service.Group{Name: "billing-policy-persistence", Platform: service.PlatformComposite,
		Status: service.StatusActive, RateMultiplier: .8, SubscriptionType: service.SubscriptionTypeStandard,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{service.PlatformOpenAI: 1.5, service.PlatformStepFun: 0}},
	}
	require.NoError(t, repo.Create(ctx, g))
	require.EqualValues(t, 1, g.BillingPolicyVersion)
	loaded, err := repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	require.Equal(t, 1.5, loaded.BillingRatePolicy.PlatformMultipliers[service.PlatformOpenAI])
	free, exists := loaded.BillingRatePolicy.PlatformMultipliers[service.PlatformStepFun]
	require.True(t, exists)
	require.Zero(t, free)
	loaded.BillingRatePolicy.PlatformMultipliers[service.PlatformOpenAI] = 2
	require.NoError(t, repo.Update(ctx, loaded))
	require.EqualValues(t, 2, loaded.BillingPolicyVersion)
	loaded, err = repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	require.Equal(t, 2.0, loaded.BillingRatePolicy.PlatformMultipliers[service.PlatformOpenAI])
	require.EqualValues(t, 2, loaded.BillingPolicyVersion)
	loaded.BillingRatePolicy = domain.BillingRatePolicy{}
	require.NoError(t, repo.Update(ctx, loaded))
	loaded, err = repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, loaded.BillingPolicyVersion)
	require.Empty(t, loaded.BillingRatePolicy.PlatformMultipliers)
}

func TestGroupBillingPolicyRejectsStaleWriter(t *testing.T) {
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	ctx := context.Background()
	g := &service.Group{Name: "billing-policy-cas", Platform: service.PlatformComposite,
		Status: service.StatusActive, RateMultiplier: 1, SubscriptionType: service.SubscriptionTypeStandard}
	require.NoError(t, repo.Create(ctx, g))
	first, err := repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	stale, err := repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	first.BillingRatePolicy.PlatformMultipliers = map[string]float64{service.PlatformOpenAI: 2}
	require.NoError(t, repo.Update(ctx, first))
	// An unrelated edit from an old admin form must not restore the old policy.
	stale.Description = "stale description"
	err = repo.Update(ctx, stale)
	require.Error(t, err)
	require.Equal(t, "GROUP_CONFIGURATION_CHANGED", infraerrors.FromError(err).Reason)
	stored, err := repo.GetByID(ctx, g.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, stored.BillingPolicyVersion)
	require.Equal(t, 2.0, stored.BillingRatePolicy.PlatformMultipliers[service.PlatformOpenAI])
	require.NotEqual(t, stale.Description, stored.Description)
}

func TestGroupBillingPolicyRejectsStaleWriterAfterAlternateConfigurationUpdate(t *testing.T) {
	for _, writer := range []string{"sort_order", "simple_mode_image_backfill"} {
		t.Run(writer, func(t *testing.T) {
			tx := testEntTx(t)
			repo := newGroupRepositoryWithSQL(tx.Client(), tx)
			ctx := context.Background()
			g := &service.Group{Name: service.PlatformGrok + "-default", Description: simpleModeDefaultGroupDescription,
				Platform: service.PlatformGrok, Status: service.StatusActive,
				RateMultiplier: 1, SubscriptionType: service.SubscriptionTypeStandard}
			require.NoError(t, repo.Create(ctx, g))
			stale, err := repo.GetByID(ctx, g.ID)
			require.NoError(t, err)
			require.False(t, stale.AllowImageGeneration)

			if writer == "sort_order" {
				require.NoError(t, repo.UpdateSortOrders(ctx, []service.GroupSortOrderUpdate{{ID: g.ID, SortOrder: 70}, {ID: g.ID, SortOrder: 73}}))
			} else {
				require.NoError(t, backfillSimpleModeGrokDefaultImageGeneration(ctx, tx.Client()))
			}
			changed, err := repo.GetByID(ctx, g.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, changed.BillingPolicyVersion, "every persisted configuration edit advances the opening revision")

			// An editor opened before another configuration writer must reload;
			// it cannot overwrite the new configuration with its old group copy.
			stale.RateMultiplier = 2
			err = repo.Update(ctx, stale)
			require.Error(t, err)
			require.Equal(t, "GROUP_CONFIGURATION_CHANGED", infraerrors.FromError(err).Reason)
			stored, err := repo.GetByID(ctx, g.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, stored.BillingPolicyVersion)
			require.Equal(t, 1.0, stored.RateMultiplier)
			if writer == "sort_order" {
				require.Equal(t, 73, stored.SortOrder)
				err = repo.UpdateSortOrders(ctx, []service.GroupSortOrderUpdate{{ID: g.ID, SortOrder: 99}, {ID: 1 << 60, SortOrder: 1}})
				require.ErrorIs(t, err, service.ErrGroupNotFound)
				unchanged, err := repo.GetByID(ctx, g.ID)
				require.NoError(t, err)
				require.Equal(t, 73, unchanged.SortOrder)
				require.EqualValues(t, 2, unchanged.BillingPolicyVersion, "a rejected batch must not advance any group revision")
			} else {
				require.True(t, stored.AllowImageGeneration)
				require.NoError(t, backfillSimpleModeGrokDefaultImageGeneration(ctx, tx.Client()))
				unchanged, err := repo.GetByID(ctx, g.ID)
				require.NoError(t, err)
				require.EqualValues(t, 2, unchanged.BillingPolicyVersion, "an already applied backfill does not create a new edit")
			}
		})
	}
}
