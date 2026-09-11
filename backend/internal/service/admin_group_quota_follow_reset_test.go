//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type quotaResetAccountRepoStub struct {
	AccountRepository
	account *Account
}

func (s *quotaResetAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if s.account == nil || s.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return s.account, nil
}

type quotaResetGroupRepoStub struct {
	*groupRepoStubForAdmin
}

func TestAdminServiceCreateGroupConfiguresOpenAIOAuthQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "Primary OAuth", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	groupRepo := &quotaResetGroupRepoStub{groupRepoStubForAdmin: &groupRepoStubForAdmin{
		createID:    7,
		getByIDByID: map[int64]*Group{99: {ID: 99, Platform: PlatformOpenAI}},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			require.Equal(t, []int64{99}, groupIDs)
			return []int64{account.ID}, nil
		},
		bindAccountsToGroupFn: func(_ int64, accountIDs []int64) error {
			require.Equal(t, []int64{account.ID}, accountIDs)
			return nil
		},
	}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}
	monthlyLimit := 100.0

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Follow upstream", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription,
		RateMultiplier: 1, MonthlyLimitUSD: &monthlyLimit,
		CopyAccountsFromGroupIDs:  []int64{99},
		QuotaResetSourceAccountID: &account.ID, QuotaResetIncludeMonthly: true,
	})

	require.NoError(t, err)
	require.Equal(t, account.ID, *group.QuotaResetSourceAccountID)
	require.Equal(t, account.Name, group.QuotaResetSourceAccountName)
	require.Nil(t, group.QuotaResetSourceResetAt, "baseline is established by the persistence transaction")
	require.Equal(t, int64(1), group.QuotaResetConfigVersion)
	require.True(t, group.QuotaResetIncludeMonthly)
}

func TestAdminServiceCreateGroupRejectsUnboundQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "Primary OAuth", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	groupRepo := &quotaResetGroupRepoStub{groupRepoStubForAdmin: &groupRepoStubForAdmin{createID: 7}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Unbound source", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription,
		RateMultiplier: 1, QuotaResetSourceAccountID: &account.ID,
	})

	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_RESET_SOURCE", infraerrors.Reason(err))
}

func TestAdminServiceCreateGroupRejectsNonOAuthQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "API Key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	groupRepo := &quotaResetGroupRepoStub{groupRepoStubForAdmin: &groupRepoStubForAdmin{}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Invalid source", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription,
		RateMultiplier: 1, QuotaResetSourceAccountID: &account.ID,
	})

	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_RESET_SOURCE", infraerrors.Reason(err))
}

func TestAdminServiceUpdateGroupRejectsUnboundQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "Primary OAuth", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	existing := &Group{
		ID: 7, Name: "Follow upstream", Platform: PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
	}
	groupRepo := &groupRepoStubForAdmin{
		getByID: existing,
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			require.Equal(t, []int64{existing.ID}, groupIDs)
			return []int64{99}, nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		QuotaResetSourceAccountIDSet: true,
		QuotaResetSourceAccountID:    &account.ID,
	})

	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_RESET_SOURCE", infraerrors.Reason(err))
}

func TestAdminServiceUpdateGroupRejectsCopiedAccountsMissingQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "Primary OAuth", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	existing := &Group{
		ID: 7, Name: "Follow upstream", Platform: PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
		QuotaResetSourceAccountID: &account.ID, QuotaResetSourceAccountName: account.Name,
		QuotaResetConfigVersion: 3, QuotaResetSourceValid: true,
	}
	groupRepo := &groupRepoStubForAdmin{
		getByID: existing,
		getByIDByID: map[int64]*Group{
			7:  existing,
			99: {ID: 99, Platform: PlatformOpenAI},
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			require.Equal(t, []int64{99}, groupIDs)
			return []int64{100}, nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		CopyAccountsFromGroupIDs:     []int64{99},
		QuotaResetSourceAccountIDSet: true,
		QuotaResetSourceAccountID:    &account.ID,
	})

	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_RESET_SOURCE", infraerrors.Reason(err))
}

func TestAdminServiceUpdateGroupClearsQuotaResetSourceAndAdvancesVersion(t *testing.T) {
	accountID := int64(42)
	baseline := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	existing := &Group{
		ID: 7, Name: "Follow upstream", Platform: PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
		QuotaResetSourceAccountID: &accountID, QuotaResetSourceAccountName: "Primary OAuth",
		QuotaResetSourceResetAt: &baseline, QuotaResetIncludeMonthly: true,
		QuotaResetConfigVersion: 3, QuotaResetSourceValid: true,
	}
	groupRepo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: groupRepo}

	group, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		QuotaResetSourceAccountIDSet: true,
	})

	require.NoError(t, err)
	require.Nil(t, group.QuotaResetSourceAccountID)
	require.Empty(t, group.QuotaResetSourceAccountName)
	require.Nil(t, group.QuotaResetSourceResetAt)
	require.False(t, group.QuotaResetIncludeMonthly)
	require.Equal(t, int64(4), group.QuotaResetConfigVersion)
}

func TestAdminServiceUpdateGroupCopyPreservesUnchangedQuotaResetSource(t *testing.T) {
	account := &Account{ID: 42, Name: "Primary OAuth", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	baseline := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	existing := &Group{
		ID: 7, Name: "Follow upstream", Platform: PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
		QuotaResetSourceAccountID: &account.ID, QuotaResetSourceAccountName: account.Name,
		QuotaResetSourceResetAt: &baseline, QuotaResetConfigVersion: 3, QuotaResetSourceValid: true,
	}
	lookups := 0
	groupRepo := &groupRepoStubForAdmin{
		deleteAccountGroupsByGroupIDFn: func(groupID int64) (int64, error) {
			require.Equal(t, existing.ID, groupID)
			return 1, nil
		},
		getByID:     existing,
		getByIDByID: map[int64]*Group{7: existing, 99: {ID: 99, Platform: PlatformOpenAI}},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			lookups++
			require.Equal(t, []int64{99}, groupIDs)
			return []int64{account.ID}, nil
		},
		bindAccountsToGroupFn: func(_ int64, accountIDs []int64) error {
			require.Equal(t, []int64{account.ID}, accountIDs)
			return nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}
	group, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		CopyAccountsFromGroupIDs:     []int64{99},
		QuotaResetSourceAccountIDSet: true, QuotaResetSourceAccountID: &account.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), group.QuotaResetConfigVersion, "copying members must not invalidate pending reset events")
	require.False(t, group.QuotaResetSourceChanged)
	require.Equal(t, baseline, *group.QuotaResetSourceResetAt)
	require.Equal(t, 1, lookups, "validate and bind the same membership snapshot")
}
