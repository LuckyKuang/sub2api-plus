//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The durable-owner contract applies across all scheduling modes. A policy
// rejection is not health evidence or authority to replace a healthy owner.
func TestCodexClientPolicySelectionRegression(t *testing.T) {
	for _, mode := range []string{"legacy", "advanced", "weighted"} {
		for _, scenario := range []string{"new-session", "all-denied", "owner-denied", "excluded-owner", "concurrent-denied-winner"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				accounts := []Account{
					{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, Extra: map[string]any{"codex_cli_only": true}},
					{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2},
					{ID: 103, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 3, Extra: map[string]any{"codex_cli_only": true}},
				}
				if scenario == "all-denied" {
					accounts = accounts[:1]
				}
				store := &affinityFaultStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
				if scenario == "concurrent-denied-winner" {
					store.stage = "winner"
				}
				if scenario == "owner-denied" || scenario == "excluded-owner" {
					_, err := store.CreateIfAbsent(context.Background(), OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
				}
				var released []int64
				cfg := &config.Config{RunMode: config.RunModeSimple}
				// A one-candidate Top-K makes the business precondition explicit:
				// the restricted A must be considered before the eligible B.
				cfg.Gateway.OpenAIWS.LBTopK = 1
				svc := &OpenAIGatewayService{cfg: cfg, sessionRouteStore: store, cache: &schedulerTestGatewayCache{},
					accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{101: true, 102: true, 103: true}, releasedIDs: &released})}
				enabled, weighted := "true", "false"
				if mode == "legacy" {
					enabled = "false"
				}
				if mode == "weighted" {
					weighted = "true"
				}
				svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService(enabled, weighted)
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("User-Agent", "curl/8.0")
				ctx := WithOpenAIClientRestriction(c.Request.Context(), c, []byte(`{"input":"hello"}`))
				var excluded map[int64]struct{}
				if scenario == "excluded-owner" {
					excluded = map[int64]struct{}{101: {}}
				}
				selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", excluded, OpenAIUpstreamTransportAny, false)
				if scenario == "new-session" {
					require.NoError(t, err)
					require.EqualValues(t, 102, selected.Account.ID)
					require.Contains(t, released, int64(101), "rejected candidate must release its acquired slot")
					selected.ReleaseFunc()
				} else {
					require.Nil(t, selected)
					if scenario == "excluded-owner" {
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
					} else {
						var policyErr *CodexClientRestrictionError
						require.ErrorAs(t, err, &policyErr)
						require.Equal(t, CodexClientRestrictionReasonNotMatchedProfile, policyErr.Result.Reason)
					}
				}
				route, getErr := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
				if scenario == "all-denied" {
					require.ErrorIs(t, getErr, ErrOpenAISessionRouteNotFound)
					require.Equal(t, []int64{101}, released)
					return
				}
				require.NoError(t, getErr)
				wantOwner := int64(101)
				switch scenario {
				case "new-session":
					wantOwner = 102
				case "concurrent-denied-winner":
					wantOwner = 103
					require.ElementsMatch(t, []int64{101, 102, 103}, released)
				case "owner-denied":
					require.Equal(t, []int64{101}, released)
				}
				require.Equal(t, wantOwner, route.OwnerAccountID)
				require.EqualValues(t, 1, route.Generation)
				require.Empty(t, route.LeaseID)
				require.Zero(t, route.CandidateAccountID)
			})
		}
	}
}

func TestCodexClientPolicyBackendFailureIsNotPolicyDenialRegression(t *testing.T) {
	store := newFakeOpenAISessionRouteStore()
	svc := &OpenAIGatewayService{sessionRouteStore: store}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx := WithOpenAIClientRestriction(c.Request.Context(), c, nil)
	calls, releases := 0, 0
	selected, _, err := svc.selectAccountWithCanonicalRoute(ctx, OpenAIAccountScheduleRequest{SessionHash: "session"},
		func(context.Context, string, map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
			calls++
			if calls == 1 {
				return &AccountSelectionResult{Account: &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{"codex_cli_only": true}},
					Acquired: true, ReleaseFunc: func() { releases++ }}, OpenAIAccountScheduleDecision{}, nil
			}
			return nil, OpenAIAccountScheduleDecision{}, ErrOpenAISessionRouteStoreUnavailable
		})
	require.Nil(t, selected)
	require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
	require.Equal(t, 1, releases)
	require.Empty(t, store.routes)
}

func TestCodexClientPolicyIngressSnapshotRegression(t *testing.T) {
	for _, official := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "allowed"}[official], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			setProfile := func(allowed bool) {
				c.Request.Header.Set("User-Agent", "curl/8.0")
				if allowed {
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.150.0 (Ubuntu 24.04; x86_64) xterm-256color")
				}
				c.Request.Header.Set("originator", "codex_cli_rs")
				c.Request.Header.Set("x-codex-window-id", "window-1")
			}
			setProfile(official)
			ctx := WithOpenAIClientRestriction(c.Request.Context(), c, nil)
			setProfile(!official)
			err := (&OpenAIGatewayService{}).checkOpenAIClientRestriction(ctx, &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_cli_only": true}})
			if official {
				require.NoError(t, err)
			} else {
				var policyErr *CodexClientRestrictionError
				require.ErrorAs(t, err, &policyErr)
			}
		})
	}
}
