//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexCLILiveAccountRepo struct {
	AccountRepository
	current *Account
	err     error
	ids     []int64
}

func (r *codexCLILiveAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.ids = append(r.ids, id)
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Second {
		panic("bridge permission read must be bounded")
	}
	return r.current, r.err
}

func TestCodexCLIBridgeAccountToggleUsesLatestFlagWithoutReplacingSnapshot(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(101, "acquired-token", "acquired-workspace")
	owner.Credentials["user_agent"] = "acquired-identity"
	current := enabledCodexCLIExecutionAccount(101, "new-token", "new-workspace")
	current.Credentials["user_agent"] = "new-identity"
	repo := &codexCLILiveAccountRepo{current: current}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: enabledCodexCLIBridgeTestSettings()}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	shadow := &Account{ID: 202, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID}
	recordRequestCredentialOwner(ctx, owner, "acquired-token")
	ctx = withRequestCredentialOwner(ctx, shadow, "acquired-token")
	for _, want := range []bool{false, true, false} {
		current.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = want
		enabled, err := svc.codexCLIBridgeEnabled(ctx, shadow)
		require.NoError(t, err)
		require.Equal(t, want, enabled)
	}
	require.Equal(t, []int64{101, 101, 101}, repo.ids)
	resolved, err := requestCredentialAccount(ctx, shadow)
	require.NoError(t, err)
	require.Equal(t, "acquired-token", resolved.Credentials["access_token"])
	require.Equal(t, "acquired-workspace", resolved.Credentials["chatgpt_account_id"])
	require.Equal(t, "acquired-identity", resolved.Credentials["user_agent"])
	require.True(t, CodexCLITaskAccountEnabled(owner))
}

func TestCodexCLIBridgeLiveSettingsFailureAndDisabledPrecedence(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(101, "token", "workspace")
	for _, test := range []struct {
		name           string
		global         string
		globalErr      bool
		accountEnabled bool
		accountErr     bool
		wantErr        bool
		wantReads      int
	}{
		{"global off skips failing account storage", "false", false, true, true, false, 0},
		{"account off tolerates global storage failure", "", true, false, false, false, 1},
		{"account on rejects global storage failure", "", true, true, false, true, 1},
		{"global on rejects account storage failure", "true", false, true, true, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := enabledCodexCLIExecutionAccount(101, "new-token", "new-workspace")
			current.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = test.accountEnabled
			repo := &codexCLILiveAccountRepo{current: current}
			if test.accountErr {
				repo.err = errors.New("private-account-storage")
			}
			settings := enabledCodexCLIBridgeTestSettings()
			if test.globalErr {
				settings = &SettingService{settingRepo: &codexCLIBridgeSettingRepo{readErr: errors.New("private-global-storage")}}
			} else {
				settings = &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: test.global}}}
			}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: settings}
			enabled, err := svc.codexCLIBridgeEnabled(context.Background(), owner)
			require.False(t, enabled)
			if test.wantErr {
				require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
				require.NotContains(t, err.Error(), "private")
			} else {
				require.NoError(t, err)
			}
			require.Len(t, repo.ids, test.wantReads)
		})
	}
}

func TestCodexCLIBridgeLatestAccountMustRemainEligible(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(101, "acquired-token", "workspace")
	for _, kind := range []string{"apikey", "pat", "shadow"} {
		t.Run(kind, func(t *testing.T) {
			current := enabledCodexCLIExecutionAccount(101, "new-token", "workspace")
			switch kind {
			case "apikey":
				current.Type = AccountTypeAPIKey
			case "pat":
				current.Credentials["auth_mode"] = "personalAccessToken"
			case "shadow":
				parent := int64(303)
				current.ParentAccountID = &parent
			}
			svc := &OpenAIGatewayService{accountRepo: &codexCLILiveAccountRepo{current: current}, settingService: enabledCodexCLIBridgeTestSettings()}
			enabled, err := svc.codexCLIBridgeEnabled(context.Background(), owner)
			require.NoError(t, err)
			require.False(t, enabled)
			require.True(t, CodexCLITaskAccountEnabled(owner))
		})
	}
}

func TestCodexCLIBridgeLatestAccountSettingBoundaries(t *testing.T) {
	for _, test := range []struct {
		name         string
		config       any
		missing      bool
		nilRow       bool
		mismatchedID bool
		want         bool
		wantErr      bool
	}{
		{name: "missing config", missing: true},
		{name: "empty config", config: map[string]any{}},
		{name: "malformed config", config: "private-malformed-config", wantErr: true},
		{name: "nil config", config: nil, wantErr: true},
		{name: "nonboolean enabled", config: map[string]any{"enabled": "true"}, wantErr: true},
		{name: "nil row", nilRow: true, wantErr: true},
		{name: "mismatched owner", mismatchedID: true, wantErr: true},
		{name: "fresh enabled overrides disabled snapshot", config: map[string]any{"enabled": true}, want: true},
		{name: "fresh disabled", config: map[string]any{"enabled": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := enabledCodexCLIExecutionAccount(101, "acquired-token", "acquired-workspace")
			owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = false
			current := enabledCodexCLIExecutionAccount(101, "new-token", "new-workspace")
			if test.missing {
				delete(current.Credentials, CodexCLIConfigCredentialKey)
			} else {
				current.Credentials[CodexCLIConfigCredentialKey] = test.config
			}
			if test.nilRow {
				current = nil
			}
			if test.mismatchedID {
				current.ID = 202
			}
			repo := &codexCLILiveAccountRepo{current: current}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: enabledCodexCLIBridgeTestSettings()}
			enabled, err := svc.codexCLIBridgeEnabled(context.Background(), owner)
			if test.wantErr {
				require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
				require.NotContains(t, err.Error(), "private")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, enabled)
			require.Equal(t, []int64{101}, repo.ids)
			require.False(t, CodexCLITaskAccountEnabled(owner))
			require.Equal(t, "acquired-token", owner.Credentials["access_token"])
		})
	}
}
