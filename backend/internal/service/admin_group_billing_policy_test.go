//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// Requirements: provider/model selling policy is independent of the common
// group rate; omission preserves it, an explicit empty policy clears it, and
// invalid prices must never be written. Examples are operator inputs, not
// values computed by the resolver being tested.
func TestAdminGroupBillingPolicyCreateAndUpdate(t *testing.T) {
	policy := domain.BillingRatePolicy{
		PlatformMultipliers: map[string]float64{PlatformOpenAI: 1.5, PlatformStepFun: 0},
		ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformOpenAI, Model: "premium-*", Multiplier: 2}},
	}
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	created, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "All providers", Platform: PlatformComposite, RateMultiplier: .8, BillingRatePolicy: policy,
	})
	require.NoError(t, err)
	require.Equal(t, .8, created.RateMultiplier)
	require.Equal(t, 1.5, created.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	require.Zero(t, created.BillingRatePolicy.PlatformMultipliers[PlatformStepFun])
	policy.PlatformMultipliers[PlatformOpenAI] = 99
	policy.ModelMultipliers[0].Multiplier = 99
	require.Equal(t, 1.5, created.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	require.Equal(t, 2.0, created.BillingRatePolicy.ModelMultipliers[0].Multiplier)

	repo.getByID = created
	_, err = svc.UpdateGroup(context.Background(), created.ID, &UpdateGroupInput{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, 1.5, repo.updated.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	empty := domain.BillingRatePolicy{}
	_, err = svc.UpdateGroup(context.Background(), created.ID, &UpdateGroupInput{BillingRatePolicy: &empty})
	require.NoError(t, err)
	require.Empty(t, repo.updated.BillingRatePolicy.PlatformMultipliers)
	require.Empty(t, repo.updated.BillingRatePolicy.ModelMultipliers)
}

func TestAdminGroupBillingPolicyInvalidDoesNotPersist(t *testing.T) {
	for name, factor := range map[string]float64{"negative": -1, "nan": math.NaN(), "infinite": math.Inf(1)} {
		t.Run(name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByID: &Group{ID: 8, Name: "All", Platform: PlatformComposite, RateMultiplier: 1}}
			svc := &adminServiceImpl{groupRepo: repo}
			policy := domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: factor}}
			_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "All", Platform: PlatformComposite, RateMultiplier: 1, BillingRatePolicy: policy})
			require.Error(t, err)
			require.Equal(t, "INVALID_BILLING_RATE_POLICY", infraerrors.FromError(err).Reason)
			require.Nil(t, repo.created)
			_, err = svc.UpdateGroup(context.Background(), 8, &UpdateGroupInput{BillingRatePolicy: &policy})
			require.Error(t, err)
			require.Nil(t, repo.updated)
		})
	}
}

func TestDuplicateGroupBillingPolicyIsIndependent(t *testing.T) {
	source := &Group{BillingPolicyVersion: 19, BillingRatePolicy: domain.BillingRatePolicy{
		PlatformMultipliers: map[string]float64{PlatformCline: .5},
		ModelMultipliers:    []domain.BillingModelRate{{Platform: PlatformCline, Model: "same-model", Multiplier: 2}},
	}}
	copy := cloneGroupForDuplicate(source, "billing-copy")
	copy.BillingRatePolicy.PlatformMultipliers[PlatformCline] = 3
	copy.BillingRatePolicy.ModelMultipliers[0].Multiplier = 4
	require.Equal(t, .5, source.BillingRatePolicy.PlatformMultipliers[PlatformCline])
	require.Equal(t, 2.0, source.BillingRatePolicy.ModelMultipliers[0].Multiplier)
	require.Zero(t, copy.BillingPolicyVersion, "the new group must receive its own persisted version")
}

func TestAdminGroupBillingPolicyStaleEditorCannotOverwriteCurrentRules(t *testing.T) {
	current := &Group{ID: 8, Platform: PlatformComposite, RateMultiplier: 1, BillingPolicyVersion: 9,
		BillingRatePolicy: domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 2}}}
	repo := &groupRepoStubForAdmin{getByID: current}
	svc := &adminServiceImpl{groupRepo: repo}
	stale := int64(8)
	old := domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: .5}}
	_, err := svc.UpdateGroup(context.Background(), current.ID, &UpdateGroupInput{BillingRatePolicy: &old, ExpectedBillingPolicyVersion: &stale})
	require.Error(t, err)
	require.Equal(t, "GROUP_CONFIGURATION_CHANGED", infraerrors.FromError(err).Reason)
	require.Nil(t, repo.updated)
	require.Equal(t, 2.0, current.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
	valid := int64(9)
	_, err = svc.UpdateGroup(context.Background(), current.ID, &UpdateGroupInput{BillingRatePolicy: &old, ExpectedBillingPolicyVersion: &valid})
	require.NoError(t, err)
	require.Equal(t, .5, repo.updated.BillingRatePolicy.PlatformMultipliers[PlatformOpenAI])
}
