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
	groupRepo := &quotaResetGroupRepoStub{groupRepoStubForAdmin: &groupRepoStubForAdmin{createID: 7}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: &quotaResetAccountRepoStub{account: account}}
	monthlyLimit := 100.0

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "Follow upstream", Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription,
		RateMultiplier: 1, MonthlyLimitUSD: &monthlyLimit,
		QuotaResetSourceAccountID: &account.ID, QuotaResetIncludeMonthly: true,
	})

	require.NoError(t, err)
	require.Equal(t, account.ID, *group.QuotaResetSourceAccountID)
	require.Equal(t, account.Name, group.QuotaResetSourceAccountName)
	require.Nil(t, group.QuotaResetSourceResetAt, "baseline is established by the persistence transaction")
	require.Equal(t, int64(1), group.QuotaResetConfigVersion)
	require.True(t, group.QuotaResetIncludeMonthly)
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
