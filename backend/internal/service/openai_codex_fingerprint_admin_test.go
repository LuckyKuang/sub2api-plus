//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceUpdateAccountPreservesExplicitCodexFingerprintModeWhenOmitted(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "session",
			"unrelated":                  true,
		},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Extra: map[string]any{"unrelated": false},
	})

	require.NoError(t, err)
	require.Equal(t, "session", account.Extra[CodexFingerprintModeExtraKey])
	require.Equal(t, false, account.Extra["unrelated"])
}

func TestAdminServiceUpdateAccountRepairsMalformedCodexFingerprintModeWhenFullExtraOmitsField(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: false,
		},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Extra: map[string]any{},
	})

	require.NoError(t, err)
	require.Equal(t, "device", account.Extra[CodexFingerprintModeExtraKey])
}

func TestAdminServiceUpdateAccountRepairsMalformedCodexFingerprintModeWhenExtraOmitted(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Name:     "before",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: false,
		},
	}}
	svc := &adminServiceImpl{accountRepo: repo}

	account, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Name: "after",
	})

	require.NoError(t, err)
	require.Equal(t, "after", account.Name)
	require.Equal(t, "device", account.Extra[CodexFingerprintModeExtraKey])
}

func TestAccountServiceUpdatePreservesExplicitCodexFingerprintModeWhenReplacementOmitsField(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "session",
			"unrelated":                  true,
		},
	}}
	svc := NewAccountService(repo, nil)
	replacement := map[string]any{"unrelated": false}

	account, err := svc.Update(context.Background(), 1, UpdateAccountRequest{Extra: &replacement})

	require.NoError(t, err)
	require.Equal(t, "session", account.Extra[CodexFingerprintModeExtraKey])
	require.Equal(t, false, account.Extra["unrelated"])
}

func TestAccountServiceUpdateRepairsMalformedCodexFingerprintModeWhenReplacementOmitsField(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: false,
		},
	}}
	svc := NewAccountService(repo, nil)
	replacement := map[string]any{}

	account, err := svc.Update(context.Background(), 1, UpdateAccountRequest{Extra: &replacement})

	require.NoError(t, err)
	require.Equal(t, "device", account.Extra[CodexFingerprintModeExtraKey])
}

func TestAccountServiceUpdateRepairsMalformedCodexFingerprintModeWhenExtraOmitted(t *testing.T) {
	repo := &longContextBillingRepoStub{account: &Account{
		ID:       1,
		Name:     "before",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: false,
		},
	}}
	svc := NewAccountService(repo, nil)
	name := "after"

	account, err := svc.Update(context.Background(), 1, UpdateAccountRequest{Name: &name})

	require.NoError(t, err)
	require.Equal(t, "after", account.Name)
	require.Equal(t, "device", account.Extra[CodexFingerprintModeExtraKey])
}
