//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceAccountWritesValidateEgressCountry(t *testing.T) {
	t.Run("create normalizes assigned code", func(t *testing.T) {
		repo := &accountRepoStubForBulkUpdate{createID: 1}
		svc := &adminServiceImpl{accountRepo: repo}
		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name: "account", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{}, Extra: map[string]any{CodexEgressCountryExtraKey: " us "},
			SkipDefaultGroupBind: true,
		})
		require.NoError(t, err)
		require.Equal(t, "US", account.Extra[CodexEgressCountryExtraKey])
	})

	t.Run("create rejects unassigned code before write", func(t *testing.T) {
		repo := &accountRepoStubForBulkUpdate{}
		svc := &adminServiceImpl{accountRepo: repo}
		account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
			Name: "account", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{}, Extra: map[string]any{CodexEgressCountryExtraKey: "ZZ"},
			SkipDefaultGroupBind: true,
		})
		require.Nil(t, account)
		requireApplicationErrorReason(t, err, "INVALID_EGRESS_COUNTRY")
		require.Nil(t, repo.createAccount)
	})

	t.Run("update rejects unassigned code before write", func(t *testing.T) {
		repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
			1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{}},
		}}
		svc := &adminServiceImpl{accountRepo: repo}
		account, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
			Extra: map[string]any{CodexEgressCountryExtraKey: "ZZ"},
		})
		require.Nil(t, account)
		requireApplicationErrorReason(t, err, "INVALID_EGRESS_COUNTRY")
		require.Empty(t, repo.updatedAccounts)
	})

	t.Run("extra and bulk updates reject before write", func(t *testing.T) {
		repo := &accountRepoStubForBulkUpdate{}
		svc := &adminServiceImpl{accountRepo: repo}
		err := svc.UpdateAccountExtra(context.Background(), 1, map[string]any{CodexEgressCountryExtraKey: "ZZ"})
		requireApplicationErrorReason(t, err, "INVALID_EGRESS_COUNTRY")

		result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
			AccountIDs: []int64{1}, Extra: map[string]any{CodexEgressCountryExtraKey: "ZZ"},
		})
		require.Nil(t, result)
		requireApplicationErrorReason(t, err, "INVALID_EGRESS_COUNTRY")
		require.Zero(t, repo.bulkUpdateCalls)
	})
}
