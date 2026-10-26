//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Requirement: a response belongs to the transmitted credential/proxy, never
// to whatever now occupies the same account ID. No rejected evidence may
// publish quota, corroborate a reset, notify a worker, or throttle its successor.
type codexObservationGuardRepo struct {
	AccountRepository
	current  *Account
	received chan *Account
	saved    map[string]any
	fallback int
	failure  error
}

func (r *codexObservationGuardRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.fallback++
	return errors.New("unconditional observation write is forbidden")
}
func (r *codexObservationGuardRepo) UpdateOpenAICodexUsageIfUnchanged(_ context.Context, id int64, owner *Account, updates map[string]any) (bool, error) {
	if r.received != nil {
		r.received <- owner
	}
	if r.failure != nil {
		return false, r.failure
	}
	if id != r.current.ID || owner.ID != r.current.ID || owner.Status != r.current.Status || !reflect.DeepEqual(owner.Credentials, r.current.Credentials) || !reflect.DeepEqual(owner.ProxyID, r.current.ProxyID) || !reflect.DeepEqual(owner.Proxy, r.current.Proxy) {
		return false, nil
	}
	r.saved = updates
	return true, nil
}

func TestCodexObservationRejectsStaleGrantWithoutSecondaryEffects(t *testing.T) {
	for _, state := range []string{"credentials replaced", "proxy edited", "proxy removed", "database error"} {
		t.Run(state, func(t *testing.T) {
			proxyID := int64(9)
			owner := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "sent"}, ProxyID: &proxyID, Proxy: &Proxy{ID: 9, Protocol: "http", Host: "sent.invalid", Port: 8080, Status: StatusActive}}
			current := snapshotRequestCredentialAccount(owner)
			repo := &codexObservationGuardRepo{current: current}
			switch state {
			case "credentials replaced":
				current.Credentials["access_token"] = "replacement"
			case "proxy edited":
				current.Proxy.Host = "replacement.invalid"
			case "proxy removed":
				current.ProxyID = nil
				current.Proxy = nil
			case "database error":
				repo.failure = errors.New("database unavailable")
			}
			recorder := &quotaFollowObservationRecorder{}
			observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
			setOpenAIGroupQuotaFollowResetObserver(observer)
			defer clearOpenAIGroupQuotaFollowResetObserver(observer)
			notifier := &OpenAIQuotaAutoResetService{ctx: context.Background(), queue: make(chan openAIAutoResetWorkItem, 1)}
			setOpenAIAutoResetNotifier(notifier)
			defer clearOpenAIAutoResetNotifier(notifier)
			now := time.Now().UTC()
			reset := now.Add(6 * 24 * time.Hour).Unix()
			minutes := 10080
			usage := 7.0
			snapshot := &OpenAICodexUsageSnapshot{SecondaryResetAtUnix: &reset, SecondaryWindowMinutes: &minutes, SecondaryUsedPercent: &usage}
			accepted, err := persistOpenAICodexUsageObservation(context.Background(), repo, 42, owner, buildCodexUsageExtraUpdates(snapshot, now), snapshot, true)
			require.False(t, accepted)
			if repo.failure != nil {
				require.ErrorIs(t, err, repo.failure)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, repo.saved)
			require.Zero(t, repo.fallback)
			require.Zero(t, recorder.calls)
			require.Empty(t, notifier.queue)
		})
	}
}

func TestCodexSnapshotThrottleDoesNotSuppressSuccessorOwner(t *testing.T) {
	old := &Account{ID: 9001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old"}}
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new"
	repo := &codexObservationGuardRepo{current: current, received: make(chan *Account, 2)}
	gateway := &OpenAIGatewayService{accountRepo: repo, codexSnapshotThrottle: newAccountWriteThrottle(time.Hour)}
	headers := http.Header{"X-Codex-Primary-Used-Percent": []string{"12"}}
	gateway.UpdateCodexUsageSnapshotFromHeaders(context.Background(), old, headers)
	select {
	case got := <-repo.received:
		require.Equal(t, "old", got.GetCredential("access_token"))
	case <-time.After(time.Second):
		t.Fatal("old attempt did not reach owner guard")
	}
	gateway.UpdateCodexUsageSnapshotFromHeaders(context.Background(), current, headers)
	select {
	case got := <-repo.received:
		require.Equal(t, "new", got.GetCredential("access_token"))
	case <-time.After(time.Second):
		t.Fatal("old throttle suppressed successor credential")
	}
}

func TestCodexObservationPortMissingNeverFallsBackToUpdateExtra(t *testing.T) {
	repo := &snapshotUpdateAccountRepo{updateExtraCalls: make(chan map[string]any, 1)}
	// Hide the optional guarded method behind a base repository wrapper.
	hidden := struct{ AccountRepository }{repo}
	accepted, err := persistOpenAICodexUsageObservation(context.Background(), hidden, 42, &Account{ID: 42}, map[string]any{"codex_5h_used_percent": 99}, nil, false)
	require.False(t, accepted)
	require.Error(t, err)
	require.Empty(t, repo.updateExtraCalls)
}

func TestCodexResultMustCarryCapturedOwnerEvidence(t *testing.T) {
	current := &Account{ID: 9101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "new"}}
	repo := &codexObservationGuardRepo{current: current, received: make(chan *Account, 1)}
	svc := &OpenAIGatewayService{accountRepo: repo, codexSnapshotThrottle: newAccountWriteThrottle(0)}
	svc.ApplyCodexUsageSnapshotFromResult(context.Background(), current, &OpenAIForwardResult{ResponseHeaders: http.Header{"X-Codex-Primary-Used-Percent": []string{"99"}}})
	require.Empty(t, repo.received, "handler's mutable selected account cannot stand in for actual sender evidence")
}

func TestCodexWeeklyBaselinePreservesKnownPrincipalOnly(t *testing.T) {
	owner := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "principal-A", "chatgpt_user_id": "verified-user", "access_token": "old"}}
	fresh := snapshotRequestCredentialAccount(owner)
	fresh.Credentials["access_token"] = "refreshed"
	require.Equal(t, OpenAICodexObservationGeneration(owner), OpenAICodexObservationGeneration(fresh), "normal token rotation keeps the known upstream principal baseline")
	fresh.Credentials["chatgpt_account_id"] = "principal-B"
	require.NotEqual(t, OpenAICodexObservationGeneration(owner), OpenAICodexObservationGeneration(fresh))
	sameTeam := snapshotRequestCredentialAccount(owner)
	sameTeam.Credentials["chatgpt_user_id"] = "another-user"
	require.NotEqual(t, OpenAICodexObservationGeneration(owner), OpenAICodexObservationGeneration(sameTeam), "users sharing one team ID do not share quota-reset ownership")
	noUser := snapshotRequestCredentialAccount(owner)
	delete(noUser.Credentials, "chatgpt_user_id")
	rotated := snapshotRequestCredentialAccount(noUser)
	rotated.Credentials["access_token"] = "replacement"
	require.NotEqual(t, OpenAICodexObservationGeneration(noUser), OpenAICodexObservationGeneration(rotated), "a team ID without user evidence must use the exact grant")
	delete(owner.Credentials, "chatgpt_account_id")
	fresh = snapshotRequestCredentialAccount(owner)
	fresh.Credentials["access_token"] = "replacement"
	require.NotEqual(t, OpenAICodexObservationGeneration(owner), OpenAICodexObservationGeneration(fresh), "unknown principals cannot authorize cross-token reset evidence")
}

func TestCodexResetNotificationCannotSuppressSuccessorGeneration(t *testing.T) {
	svc := &OpenAIQuotaAutoResetService{ctx: context.Background(), queue: make(chan openAIAutoResetWorkItem, 3)}
	old := &Account{ID: 42, Credentials: map[string]any{"access_token": "old"}}
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new"
	svc.notifyOwner(old.ID, old)
	svc.notifyOwner(current.ID, current)
	svc.notifyOwner(old.ID, old)
	require.Len(t, svc.queue, 2, "only the exact same generation is deduplicated")
	require.Equal(t, "old", (<-svc.queue).owner.GetCredential("access_token"))
	require.Equal(t, "new", (<-svc.queue).owner.GetCredential("access_token"))
}

func TestCodexObservationInMemoryMergeClearsOnlyOldObservationState(t *testing.T) {
	account := &Account{Extra: map[string]any{OpenAICodexUsageOwnerGenerationExtraKey: "old", "codex_usage_updated_at": "2099-01-01T00:00:00Z", "codex_7d_used_percent": 99.0, "openai_compact_supported": false, "openai_ws_mode": "ctx_pool", OpenAIAutoResetCreditEnabledExtraKey: true, "user_agent": "configured"}}
	updates := map[string]any{OpenAICodexUsageOwnerGenerationExtraKey: "new", "codex_5h_used_percent": 7.0}
	mergeAccountExtra(account, updates)
	require.NotContains(t, account.Extra, "codex_7d_used_percent")
	require.NotContains(t, account.Extra, "openai_compact_supported")
	require.Equal(t, "ctx_pool", account.Extra["openai_ws_mode"])
	require.Equal(t, true, account.Extra[OpenAIAutoResetCreditEnabledExtraKey])
	require.Equal(t, "configured", account.Extra["user_agent"])
	require.Equal(t, 7.0, account.Extra["codex_5h_used_percent"])
}

type codex429GuardRepo struct {
	codexObservationGuardRepo
	cooldowns int
	plans     int
}

func (r *codex429GuardRepo) ApplyCredentialStateMutation(_ context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	if owner == nil || owner.Status != r.current.Status || !reflect.DeepEqual(owner.Credentials, r.current.Credentials) || !reflect.DeepEqual(owner.Proxy, r.current.Proxy) || !reflect.DeepEqual(owner.ProxyID, r.current.ProxyID) {
		return false, nil
	}
	if mutation.RateLimitUntil != nil {
		r.cooldowns++
	}
	if mutation.OpenAIPlanType != "" {
		r.plans++
	}
	return true, nil
}

type codex429RuntimeBlocker struct{ calls int }

func (b *codex429RuntimeBlocker) BlockAccountScheduling(*Account, time.Time, string) { b.calls++ }
func (*codex429RuntimeBlocker) ClearAccountSchedulingBlock(int64)                    {}

func TestCodex429RejectsOldOwnerQuotaCooldownPlanAndRuntimeEffects(t *testing.T) {
	for _, route := range []string{"quota headers", "usage body", "fallback"} {
		for _, change := range []string{"unchanged", "reauthorized", "proxy edit", "proxy unbound", "disabled"} {
			t.Run(route+"/"+change, func(t *testing.T) {
				proxyID := int64(9)
				owner := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "sent", "plan_type": "plus"}, ProxyID: &proxyID, Proxy: &Proxy{ID: 9, Protocol: "http", Host: "sent.invalid", Port: 8080, Status: StatusActive}}
				current := snapshotRequestCredentialAccount(owner)
				switch change {
				case "reauthorized":
					current.Credentials["access_token"] = "new"
				case "proxy edit":
					current.Proxy.Host = "new.invalid"
				case "proxy unbound":
					current.ProxyID = nil
					current.Proxy = nil
				case "disabled":
					current.Status = "inactive"
				}
				repo := &codex429GuardRepo{codexObservationGuardRepo: codexObservationGuardRepo{current: current}}
				blocker := &codex429RuntimeBlocker{}
				service := &RateLimitService{accountRepo: repo, runtimeBlocker: blocker}
				headers := make(http.Header)
				body := []byte(`{"error":{"type":"usage_limit_reached","plan_type":"free"}}`)
				switch route {
				case "quota headers":
					headers.Set("x-codex-primary-used-percent", "100")
					headers.Set("x-codex-primary-window-minutes", "300")
					headers.Set("x-codex-primary-reset-after-seconds", "3600")
				case "usage body":
					body = []byte(`{"error":{"type":"usage_limit_reached","plan_type":"free","resets_in_seconds":3600}}`)
				}
				ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, "sent")
				service.handle429(ctx, owner, headers, body)
				expected := 0
				if change == "unchanged" {
					expected = 1
				}
				require.Equal(t, expected, repo.cooldowns)
				require.Equal(t, expected, repo.plans)
				require.Equal(t, expected, blocker.calls)
				require.Zero(t, repo.fallback, "no obsolete observation can fall back to account-ID writes")
				if change != "unchanged" {
					require.Nil(t, repo.saved)
				}
			})
		}
	}
}

func TestCodexPlanMetadataNeverLearnsChatGPTPlanForAPIKey(t *testing.T) {
	owner := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": "sent"}}
	repo := &codex429GuardRepo{codexObservationGuardRepo: codexObservationGuardRepo{current: owner}}
	persistOpenAI429PlanType(context.Background(), repo, owner, []byte(`{"error":{"type":"usage_limit_reached","plan_type":"pro"}}`))
	require.Zero(t, repo.plans)
	require.NotContains(t, owner.Credentials, "plan_type")
}

func (r *codex429GuardRepo) SetRateLimited(context.Context, int64, time.Time) error {
	r.fallback++
	return nil
}
func (r *codex429GuardRepo) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	r.fallback++
	return 1, nil
}
func (r *codex429GuardRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	r.fallback++
	return nil
}

// Give each quota fixture explicit provenance; expected quota decisions remain
// independently specified by the business scenario.
func verifiedCodexUsageForTest(account *Account) *Account {
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[OpenAICodexUsageOwnerGenerationExtraKey] = credentialStateGeneration(account)
	return account
}

func TestCodexQuotaDecisionsRequireCurrentObservationOwner(t *testing.T) {
	for _, transition := range []string{"unmarked legacy", "reauthorized", "proxy edited", "proxy removed", "current", "slim current"} {
		t.Run(transition, func(t *testing.T) {
			now := time.Now().UTC()
			proxyID := int64(8)
			account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"access_token": "observed-grant"}, ProxyID: &proxyID,
				Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "observed.invalid", Port: 8080, Status: StatusActive},
				Extra: map[string]any{"auto_pause_5h_threshold": 0.9, "codex_5h_used_percent": 100.0,
					"codex_7d_used_percent": 90.0, "codex_usage_updated_at": now.Format(time.RFC3339Nano),
					"codex_5h_reset_at": now.Add(time.Hour).Format(time.RFC3339Nano)},
			}
			verifiedCodexUsageForTest(account)
			switch transition {
			case "unmarked legacy":
				delete(account.Extra, OpenAICodexUsageOwnerGenerationExtraKey)
			case "reauthorized":
				account.Credentials["access_token"] = "successor-grant"
			case "proxy edited":
				account.Proxy.Host = "successor.invalid"
			case "proxy removed":
				account.ProxyID, account.Proxy = nil, nil
			case "slim current":
				account.CredentialGeneration = credentialStateGeneration(account)
				account.Credentials, account.Proxy = nil, nil
			}
			paused, _ := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
			if transition == "current" || transition == "slim current" {
				require.True(t, paused)
				require.InDelta(t, 0.05, openAIQuotaHeadroomFactor(account, now), 1e-9)
				_, resetKnown := openAISchedulingResetWindowEnd(account, now)
				require.True(t, resetKnown)
			} else {
				require.False(t, paused, "unproved upstream quota cannot block its successor")
				require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(account, now))
				_, resetKnown := openAISchedulingResetWindowEnd(account, now)
				require.False(t, resetKnown)
				manualEnd := now.Add(2 * time.Hour)
				account.SessionWindowEnd = &manualEnd
				end, exists := openAISchedulingResetWindowEnd(account, now)
				require.True(t, exists, "manual session window remains independent of quota observation")
				require.Equal(t, manualEnd, end)
			}
			require.Equal(t, 0.9, account.Extra["auto_pause_5h_threshold"], "operator configuration survives")
		})
	}
}

func TestCodexSparkQuotaUsesCurrentParentGrantAndOwnWindow(t *testing.T) {
	ctx := context.Background()
	parentID := int64(72)
	parent := verifiedCodexUsageForTest(&Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "parent-grant"},
		Extra:       map[string]any{"auto_pause_5h_threshold": 0.9, "codex_5h_used_percent": 100.0}})
	shadow := &Account{ID: 73, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, ParentAccountID: &parentID,
		Extra: map[string]any{"auto_pause_5h_threshold": 0.9, "codex_5h_used_percent": 10.0,
			OpenAICodexUsageOwnerGenerationExtraKey: credentialStateGeneration(parent)}}
	lookup := func(int64) *Account { return parent }
	require.True(t, parentHealthyForShadow(shadow, lookup, ctx), "parent global exhaustion must not block healthy Spark quota")
	shadow.Extra["codex_5h_used_percent"] = 100.0
	require.False(t, parentHealthyForShadow(shadow, lookup, ctx), "verified Spark exhaustion blocks Spark")
	parent.Credentials["access_token"] = "successor-grant"
	require.True(t, parentHealthyForShadow(shadow, lookup, ctx), "old parent grant quota must not block new grant")
	paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, shadow)
	require.False(t, paused, "shadow credentials cannot prove parent quota ownership")
}

func TestCodexAutoResetIgnoresUnprovedQuotaAndFailureBackoff(t *testing.T) {
	for _, transition := range []string{"legacy", "reauthorized", "proxy edited"} {
		t.Run(transition, func(t *testing.T) {
			service, repo, quota := newResetThresholdTestFixture(t, &OpenAIAutoResetCreditState{
				Status: OpenAIAutoResetStatusFailed, TriggerWindow: "7d", CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
				ErrorCode: "RESET_CREDIT_QUERY_FAILED", AvailableCount: 9,
			}, 1)
			switch transition {
			case "legacy":
				delete(repo.account.Extra, OpenAICodexUsageOwnerGenerationExtraKey)
			case "reauthorized":
				repo.account.Credentials = map[string]any{"access_token": "successor-grant"}
			case "proxy edited":
				proxyID := int64(8)
				repo.account.ProxyID = &proxyID
				repo.account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "new.invalid", Port: 8080, Status: StatusActive}
			}
			quota.usage.RateLimit.SecondaryWindow.UsedPercent = 10
			require.NoError(t, service.evaluateAccount(context.Background(), repo.account.ID))
			require.Equal(t, int32(1), quota.queryCalls.Load(), "old failed probe backoff cannot suppress a current-owner probe")
			require.Zero(t, quota.resetCalls.Load(), "old exhaustion cannot consume a current owner's credit")
			state := repo.stateForTest()
			require.NotNil(t, state)
			require.Equal(t, OpenAIAutoResetStatusAvailable, state.Status)
			require.Empty(t, state.TriggerWindow)
			require.True(t, ResolveOpenAIAutoResetCreditConfig(repo.account).Enabled)
		})
	}
}
