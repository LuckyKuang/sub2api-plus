//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// These exercise real provider account configurations through the scheduler,
// rather than assuming that a helper-level credential fence establishes sticky
// failover. A different conversation competes for the healthy primary without
// stealing the accepted replacement of the original conversation.
func TestProviderSchedulerCommitsReplacementAndKeepsConversationsIndependent(t *testing.T) {
	for _, tc := range strictProviderProxyCases[1:] { // Codex owns a separate canonical route protocol.
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, failure := range []string{"attempt-excluded", "account-disabled", "account-deleted"} {
				t.Run(tc.platform+"/"+kind+"/"+failure, func(t *testing.T) {
					proxy := &Proxy{ID: 91, Protocol: "http", Host: "primary-proxy.invalid", Port: 8080, Status: StatusActive}
					a := strictProviderProxyAccount(t, tc, kind, proxy)
					b := snapshotRequestCredentialAccount(a)
					a.ID, b.ID, a.Priority, b.Priority = 701, 702, 0, 1
					repo := schedulerTestOpenAIAccountRepo{accounts: []Account{*a, *b}}
					cache := &schedulerTestGatewayCache{}
					svc := &OpenAIGatewayService{accountRepo: repo, cache: cache, cfg: &config.Config{RunMode: config.RunModeSimple}}
					selectOwner := func(session string, excluded map[int64]struct{}, want int64) {
						t.Helper()
						selected, err := svc.selectAccountForModelWithExclusions(context.Background(), nil, tc.platform, session, "", excluded, false, 0, "", false)
						require.NoError(t, err)
						require.Equal(t, want, selected.ID)
					}
					selectOwner("first-conversation", nil, 701)
					selectOwner("first-conversation", nil, 701)
					var excluded map[int64]struct{}
					switch failure {
					case "attempt-excluded":
						excluded = map[int64]struct{}{701: {}}
					case "account-disabled":
						repo.accounts[0].Status = StatusDisabled
					case "account-deleted":
						repo.accounts[0].ID = 799
						repo.accounts[0].Status = StatusDisabled
					}
					selectOwner("first-conversation", excluded, 702)
					// The previously unavailable primary is healthy again. Recovery
					// alone cannot move an established conversation back to it.
					repo.accounts[0] = *a
					selectOwner("first-conversation", nil, 702)
					selectOwner("second-conversation", nil, 701)
					selectOwner("first-conversation", nil, 702)
					// Both physical owners are unavailable for this attempt. No
					// disabled account may be invented as an emergency fallback.
					repo.accounts[1].Status = StatusDisabled
					selected, err := svc.selectAccountForModelWithExclusions(context.Background(), nil, tc.platform, "first-conversation", "", map[int64]struct{}{701: {}}, false, 0, "", false)
					require.Error(t, err)
					require.Nil(t, selected)
					selectOwner("second-conversation", nil, 701)
				})
			}
		}
	}
}

// Step-Code's discovered restriction is user-editable. A sticky assignment is
// not permission to keep using a model removed from that account's plan.
func TestStepFunStickyOwnerRespectsEditedModelRestriction(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			a, b := stepFunTestAccount(kind, "cn"), stepFunTestAccount(kind, "cn")
			a.ID, b.ID, a.Priority, b.Priority = 711, 712, 0, 1
			for _, account := range []*Account{a, b} {
				account.Status, account.Schedulable = StatusActive, true
				account.Credentials["model_mapping"] = map[string]any{"public-step": "step-3.7-flash"}
			}
			repo := &mockAccountRepoForPlatform{accounts: []Account{*a, *b}, accountsByID: map[int64]*Account{a.ID: a, b.ID: b}}
			cache := &schedulerTestGatewayCache{}
			svc := &GatewayService{accountRepo: repo, cache: cache, cfg: testConfig()}
			selectOwner := func(want int64) {
				t.Helper()
				selected, err := svc.selectAccountForModelWithPlatform(context.Background(), nil, "step-edited-plan", "public-step", nil, PlatformStepFun)
				require.NoError(t, err)
				require.Equal(t, want, selected.ID)
				require.Equal(t, "step-3.7-flash", selected.GetMappedModel("public-step"))
			}
			selectOwner(711)
			a.Credentials["model_mapping"] = map[string]any{"other-model": "step-other"}
			repo.accounts[0] = *a
			selectOwner(712)
			a.Credentials["model_mapping"] = map[string]any{"public-step": "step-3.7-flash"}
			repo.accounts[0] = *a
			selectOwner(712)
			b.Credentials["model_mapping"] = map[string]any{"other-model": "step-other"}
			repo.accounts[1] = *b
			selectOwner(711)
		})
	}
}
