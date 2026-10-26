//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

type refreshCleanupRepo struct {
	AccountRepository
	current             *Account
	beforeCAS, afterCAS func()
	afterPersist        func()
	beforePersist       func(context.Context)
	err                 error
	persistErr          error
	persistCalls        int
	cleanupCalls        int
	applied             bool
}

func (r *refreshCleanupRepo) GetByID(context.Context, int64) (*Account, error) {
	return snapshotRequestCredentialAccount(r.current), nil
}

func (r *refreshCleanupRepo) UpdateOAuthRefreshCredentialsIfUnchanged(ctx context.Context, expectedOwner *Account, replacement map[string]any) (bool, error) {
	r.persistCalls++
	if r.persistErr != nil {
		return false, r.persistErr
	}
	if r.beforePersist != nil {
		r.beforePersist(ctx)
	}
	if !agentTaskTestCredentialsMatch(r.current, expectedOwner) {
		return false, nil
	}
	r.current.Credentials = cloneRequestCredentialMap(replacement)
	if r.afterPersist != nil {
		r.afterPersist()
	}
	return true, nil
}

func (r *refreshCleanupRepo) UpdateGrokOAuthCredentialsIfUnchanged(ctx context.Context, expectedOwner *Account, replacement map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, expectedOwner, replacement)
}

func (r *refreshCleanupRepo) UpdateCNOAuthCredentialsIfUnchanged(ctx context.Context, expectedOwner *Account, replacement map[string]any) (bool, error) {
	return r.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, expectedOwner, replacement)
}

func TestDirectRefreshSuccessCASMissAtAttemptDeadlineDoesNotRetry(t *testing.T) {
	current := &Account{ID: 486, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "original", "refresh_token": "original-refresh"}}
	repo := &refreshCleanupRepo{current: current}
	repo.beforePersist = func(ctx context.Context) {
		<-ctx.Done()
		current.Credentials["access_token"] = "new-login"
		// If the deadline incorrectly converts this conclusive CAS miss into a
		// retry, contain that second write so the call-count assertion reports it.
		repo.persistErr = errors.New("unexpected second persistence attempt")
	}
	cache := &tokenCacheInvalidatorStub{}
	svc := &TokenRefreshService{accountRepo: repo, cacheInvalidator: cache, cfg: &config.TokenRefreshConfig{MaxRetries: 2}, attemptTimeoutOverride: time.Second}
	refresher := &tokenRefresherStub{credentials: map[string]any{"access_token": "consumed-refresh-result", "refresh_token": "rotated"}}
	err := svc.refreshWithRetry(context.Background(), current, refresher, refresher, time.Hour)
	require.ErrorIs(t, err, errRefreshSkipped)
	require.Equal(t, 1, refresher.calls, "a known CAS miss stays terminal when the internal deadline also elapsed")
	require.Equal(t, 1, repo.persistCalls)
	require.Zero(t, repo.cleanupCalls)
	require.Zero(t, cache.calls)
	require.Equal(t, "new-login", repo.current.GetCredential("access_token"))
}

func (r *refreshCleanupRepo) ApplyCredentialStateMutation(_ context.Context, owner *Account, change CredentialStateMutation) (bool, error) {
	r.cleanupCalls++
	if r.beforeCAS != nil {
		r.beforeCAS()
	}
	if r.err != nil {
		return false, r.err
	}
	if !sameRequestCredentialGrant(owner, r.current) || !change.ClearTempUnschedulable ||
		change.ObservedTempUntil == nil || r.current.TempUnschedulableUntil == nil ||
		!change.ObservedTempUntil.Equal(*r.current.TempUnschedulableUntil) || change.ObservedTempReason != r.current.TempUnschedulableReason {
		return false, nil
	}
	r.current.TempUnschedulableUntil = nil
	r.current.TempUnschedulableReason = ""
	r.applied = true
	if r.afterCAS != nil {
		r.afterCAS()
	}
	return true, nil
}

func TestDirectRefreshSuccessFencesOriginalGrantBeforeCleanup(t *testing.T) {
	// Native OAuth requires the coordinator. Linked ZCode login is renewed by
	// reauthorization; its project PAT has a separate manager and lifecycle.
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, change := range []string{"unchanged", "relinked", "nested_metadata", "proxy_changed", "new_cooldown", "store_error", "missing_contract", "nil_credentials"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
				current := &Account{ID: 485, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials:            map[string]any{"oauth_provider": platform, "access_token": "original", "refresh_token": "original-refresh", "metadata": map[string]any{"grant": "original"}},
					TempUnschedulableUntil: &until, TempUnschedulableReason: "observed-before-call"}
				repo := &refreshCleanupRepo{current: current}
				var accountRepo AccountRepository = repo
				if change == "store_error" {
					repo.persistErr = errors.New("outbox unavailable")
				}
				if change == "missing_contract" {
					accountRepo = &refreshFailureUnavailableRepo{AccountRepository: repo}
				}
				cache := &tokenCacheInvalidatorStub{}
				scheduler := &tokenRefreshSchedulerCache{}
				runtime := &tokenRefreshRuntimeBlocker{}
				svc := &TokenRefreshService{accountRepo: accountRepo, cacheInvalidator: cache, schedulerCache: scheduler, runtimeBlocker: runtime,
					cfg: &config.TokenRefreshConfig{MaxRetries: 2}, refreshPolicy: DefaultBackgroundRefreshPolicy()}
				refresher := &tokenRefresherStub{credentials: map[string]any{"oauth_provider": platform, "access_token": "refresh-result", "refresh_token": "rotated"}, onRefresh: func(attempted *Account) {
					if change == "relinked" {
						attempted.Credentials["access_token"] = "new-login"
					}
					if change == "nested_metadata" {
						attempted.Credentials["metadata"].(map[string]any)["grant"] = "new-login"
					}
					if change == "proxy_changed" {
						id := int64(92)
						attempted.ProxyID = &id
					}
					if change == "new_cooldown" {
						later := until.Add(time.Hour)
						attempted.TempUnschedulableUntil = &later
						attempted.TempUnschedulableReason = "installed-during-call"
					}
				}}
				if change == "nil_credentials" {
					refresher.credentials = nil
				}
				err := svc.refreshWithRetry(context.Background(), current, refresher, refresher, time.Hour)
				require.Equal(t, 1, refresher.calls, "a provider may have consumed the refresh token; never repeat a local persistence failure")
				require.Zero(t, scheduler.setAccountCalls)
				if change == "unchanged" || change == "new_cooldown" {
					require.NoError(t, err)
					require.Equal(t, "refresh-result", repo.current.GetCredential("access_token"))
					require.Equal(t, 1, cache.calls)
					require.Equal(t, 1, scheduler.deleteAccountCalls)
					require.Equal(t, change == "unchanged", repo.applied)
					if change == "unchanged" {
						require.Nil(t, repo.current.TempUnschedulableUntil)
					} else {
						require.Equal(t, "installed-during-call", repo.current.TempUnschedulableReason)
					}
					return
				}
				require.Error(t, err)
				if change == "store_error" || change == "nil_credentials" {
					var contained *providerCycleContainmentRefreshError
					require.ErrorAs(t, err, &contained)
				} else if change == "missing_contract" {
					var contained *providerConfigurationRefreshError
					require.ErrorAs(t, err, &contained)
				} else {
					require.ErrorIs(t, err, errRefreshSkipped)
				}
				require.Zero(t, repo.cleanupCalls, "stale success cannot enter cleanup")
				require.Zero(t, cache.calls)
				require.Zero(t, scheduler.deleteAccountCalls)
				require.Zero(t, runtime.blockCalls+runtime.clearCalls)
				require.NotEqual(t, "refresh-result", repo.current.GetCredential("access_token"))
				require.NotNil(t, repo.current.TempUnschedulableUntil)
			})
		}
	}
}

type refreshCleanupDisplayCache struct {
	tempUnschedCacheStub
	reads int
}

func (s *refreshCleanupDisplayCache) GetTempUnsched(context.Context, int64) (*TempUnschedState, error) {
	s.reads++
	return s.lastState, nil
}

func TestRefreshCleanupFencesGrantCooldownAndRuntime(t *testing.T) {
	// OFFICIAL_AGENT_REQUEST_STATE: refresh proves only its resulting grant and
	// the pre-refresh cooldown. A relink/new cooldown/new runtime sequence wins.
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, interleaving := range []string{"unchanged", "relinked", "new_deadline", "new_reason", "new_runtime", "store_error"} {
			t.Run(platform+"/"+interleaving, func(t *testing.T) {
				ctx := context.Background()
				until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
				observed := &Account{ID: 481, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials:            map[string]any{"access_token": "refresh-result"},
					TempUnschedulableUntil: &until, TempUnschedulableReason: "observed-401"}
				repo := &refreshCleanupRepo{current: snapshotRequestCredentialAccount(observed)}
				runtime := &OpenAIGatewayService{}
				runtime.BlockAccountScheduling(observed, until, "observed-401")
				cache := &refreshCleanupDisplayCache{tempUnschedCacheStub: tempUnschedCacheStub{lastState: &TempUnschedState{UntilUnix: until.Unix(), ErrorMessage: "stale-cache"}}}
				scheduler := &tokenRefreshSchedulerCache{}
				svc := &TokenRefreshService{accountRepo: repo, runtimeBlocker: runtime, tempUnschedCache: cache, schedulerCache: scheduler}
				ctx = svc.withCredentialRefreshRuntimeSnapshot(ctx, observed)
				switch interleaving {
				case "relinked", "new_deadline", "new_reason":
					repo.beforeCAS = func() {
						if interleaving == "relinked" {
							repo.current.Credentials["access_token"] = "new-login"
						}
						if interleaving != "new_reason" {
							later := until.Add(time.Hour)
							repo.current.TempUnschedulableUntil = &later
						}
						repo.current.TempUnschedulableReason = "new-401"
						runtime.BlockAccountScheduling(repo.current, *repo.current.TempUnschedulableUntil, "new-401")
					}
				case "new_runtime":
					repo.afterCAS = func() { runtime.BlockAccountScheduling(observed, until, "new-runtime-sequence") }
				case "store_error":
					repo.err = errors.New("unavailable")
				}
				svc.postRefreshActions(ctx, observed)
				require.Zero(t, scheduler.setAccountCalls, "old refresh snapshot must never overwrite a newer login")
				require.Equal(t, 1, scheduler.deleteAccountCalls)
				require.Zero(t, cache.deleteCalls+cache.setCalls, "row-ID cache writes cannot be fenced")
				display, err := (&RateLimitService{accountRepo: repo, tempUnschedCache: cache}).GetTempUnschedStatus(ctx, observed.ID)
				require.NoError(t, err)
				require.Zero(t, cache.reads, "admin display must read durable fenced state")
				if interleaving == "unchanged" || interleaving == "new_runtime" {
					require.True(t, repo.applied)
					require.Nil(t, repo.current.TempUnschedulableUntil)
					require.Nil(t, display, "stale display cache must not resurrect a cleared cooldown")
				} else {
					require.False(t, repo.applied)
					require.NotNil(t, repo.current.TempUnschedulableUntil)
					require.Equal(t, repo.current.TempUnschedulableReason, display.ErrorMessage)
					require.Equal(t, repo.current.TempUnschedulableUntil.Unix(), display.UntilUnix)
				}
				require.Equal(t, interleaving != "unchanged", runtime.isOpenAIAccountRuntimeBlocked(repo.current), "only the unchanged runtime sequence may be cleared")
			})
		}
	}
}

func TestRefreshCleanupUsesActualPreRefreshRead(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			selected := &Account{ID: 482, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"oauth_provider": platform, "access_token": "old", "refresh_token": "refresh"}}
			current := snapshotRequestCredentialAccount(selected)
			until := time.Now().Add(time.Hour)
			current.TempUnschedulableUntil, current.TempUnschedulableReason = &until, "401-before-refresh-read"
			repo := &refreshCleanupRepo{current: current}
			svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 1}}, nil)
			svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
			refresher := &tokenRefresherStub{credentials: map[string]any{"oauth_provider": platform, "access_token": "new", "refresh_token": "rotated"}}
			err := svc.refreshWithRetry(context.Background(), selected, refresher, refresher, time.Hour)
			require.NoError(t, err)
			require.Equal(t, "new", repo.current.GetCredential("access_token"))
			require.True(t, repo.applied, "successful refresh may clear the cooldown observed by its authoritative pre-call read")
			require.Nil(t, repo.current.TempUnschedulableUntil)
		})
	}
}

func TestRefreshCleanupCannotClearRuntimeFailureInstalledAfterTokenPersistence(t *testing.T) {
	for _, unified := range []bool{false, true} {
		platforms := []string{PlatformOpenAI, PlatformGrok}
		if unified {
			// Native grant rotations use their coordinator. Step Passport keeps
			// the inference model key and has a dedicated native regression below.
			platforms = []string{PlatformOpenAI, PlatformGrok, PlatformKimi, PlatformMiniMax}
		}
		for _, platform := range platforms {
			t.Run(fmt.Sprintf("%s/unified=%t", platform, unified), func(t *testing.T) {
				until := time.Now().Add(time.Hour).Truncate(time.Second)
				current := &Account{ID: 489, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials:            map[string]any{"oauth_provider": platform, "access_token": "old", "refresh_token": "refresh"},
					TempUnschedulableUntil: &until, TempUnschedulableReason: "observed-before-refresh"}
				repo := &refreshCleanupRepo{current: current}
				runtime := &OpenAIGatewayService{}
				runtime.BlockAccountScheduling(current, until, "original-failure")
				var failureOwner *Account
				repo.afterPersist = func() {
					// A request using the newly committed token fails before the
					// older refresh enters cleanup. Its durable write may fail, so
					// only this generation's immediate runtime quarantine remains.
					failureOwner = snapshotRequestCredentialAccount(repo.current)
					runtime.BlockAccountScheduling(failureOwner, until, "new-token-failure")
				}
				svc := &TokenRefreshService{accountRepo: repo, runtimeBlocker: runtime, cfg: &config.TokenRefreshConfig{MaxRetries: 1}}
				if unified {
					svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
				}
				refresher := &tokenRefresherStub{credentials: map[string]any{"oauth_provider": platform, "access_token": "rotated", "refresh_token": "rotated-refresh"}}
				require.NoError(t, svc.refreshWithRetry(t.Context(), current, refresher, refresher, time.Hour))
				require.True(t, repo.applied, "the unchanged pre-refresh durable cooldown may be cleared")
				require.NotNil(t, failureOwner)
				require.True(t, runtime.peekOpenAIAccountRuntimeBlock(failureOwner).blocked, "an older refresh cannot clear a later failure of the new token")
			})
		}
	}
}

func TestRefreshCleanupKeepsStateInstalledDuringRefresh(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformKimi, PlatformMiniMax} {
		for _, change := range []string{"new_deadline", "same_deadline_new_reason", "relinked_after_persist", "attempt_snapshot_mutated"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
				current := &Account{ID: 484, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials:            map[string]any{"oauth_provider": platform, "access_token": "old", "refresh_token": "refresh"},
					TempUnschedulableUntil: &until, TempUnschedulableReason: "observed-before-call"}
				repo := &refreshCleanupRepo{current: current}
				selected := snapshotRequestCredentialAccount(current)
				svc := &TokenRefreshService{accountRepo: repo, refreshAPI: NewOAuthRefreshAPI(repo, nil), cfg: &config.TokenRefreshConfig{MaxRetries: 1}}
				refresher := &tokenRefresherStub{credentials: map[string]any{"oauth_provider": platform, "access_token": "rotated", "refresh_token": "rotated-refresh"}}
				refresher.onRefresh = func(attempted *Account) {
					switch change {
					case "new_deadline":
						later := until.Add(time.Hour)
						repo.current.TempUnschedulableUntil = &later
					case "same_deadline_new_reason":
						repo.current.TempUnschedulableReason = "installed-during-call"
					case "attempt_snapshot_mutated":
						// The executor's mutable input must not rewrite the frozen
						// cooldown evidence recorded immediately before the call.
						*attempted.TempUnschedulableUntil = until.Add(time.Hour)
						later := until.Add(time.Hour)
						repo.current.TempUnschedulableUntil = &later
					}
				}
				if change == "relinked_after_persist" {
					repo.afterPersist = func() {
						repo.current.Credentials["access_token"] = "new-login"
						repo.current.TempUnschedulableReason = "new-login-cooldown"
					}
				}
				err := svc.refreshWithRetry(context.Background(), selected, refresher, refresher, time.Hour)
				if change == "relinked_after_persist" && current.IsDomesticOAuth() {
					require.Error(t, err, "a replacement login after persistence cannot authorize refresh cleanup")
				} else {
					require.NoError(t, err)
				}
				require.False(t, repo.applied, "refresh may clear only its pre-call cooldown on its own resulting grant")
				require.NotNil(t, repo.current.TempUnschedulableUntil)
				if change == "relinked_after_persist" {
					require.Equal(t, "new-login", repo.current.GetCredential("access_token"))
				} else {
					require.Equal(t, "rotated", repo.current.GetCredential("access_token"))
				}
			})
		}
	}
}

func refreshCleanupNativeAccount(platform string) *Account {
	account := cnRecoveryAccount(platform)
	account.Credentials["metadata"] = map[string]any{"grant": "original"}
	if platform == PlatformStepFun {
		// Passport renews its own account session, preserving the model key.
		delete(account.Credentials, "refresh_token")
		delete(account.Credentials, "expires_at")
		for key, value := range map[string]any{"step_account_access_token": "passport-access", "step_account_refresh_token": "passport-refresh", "step_account_device_id": "passport-device", "step_account_app_id": "passport-app"} {
			account.Credentials[key] = value
		}
	}
	return account
}

func TestNativeBackgroundRefreshRequiresCoordinator(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		t.Run(platform, func(t *testing.T) {
			account := refreshCleanupNativeAccount(platform)
			repo := &refreshCleanupRepo{current: snapshotRequestCredentialAccount(account)}
			cache, runtime := &tokenCacheInvalidatorStub{}, &tokenRefreshRuntimeBlocker{}
			svc := &TokenRefreshService{accountRepo: repo, cacheInvalidator: cache, runtimeBlocker: runtime, cfg: &config.TokenRefreshConfig{MaxRetries: 2}}
			refresher := &tokenRefresherStub{credentials: map[string]any{"access_token": "forbidden-direct-result"}}
			err := svc.refreshWithRetry(t.Context(), account, refresher, refresher, time.Hour)
			var configuration *providerConfigurationRefreshError
			require.ErrorAs(t, err, &configuration)
			require.Zero(t, refresher.calls, "native grants cannot fall through to a legacy direct refresh")
			require.Zero(t, repo.persistCalls+repo.cleanupCalls+cache.calls+runtime.blockCalls+runtime.clearCalls)
			require.Equal(t, account.Credentials, repo.current.Credentials)
		})
	}
}

func TestNativeRefreshCoordinatorFencesSuccessBeforeCleanup(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		for _, change := range []string{"unchanged", "relinked", "nested-metadata", "proxy-changed", "new-cooldown", "store-error", "new-runtime"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				account := refreshCleanupNativeAccount(platform)
				until := time.Now().Add(time.Hour).Truncate(time.Second)
				account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, "observed-before-call"
				repo := &refreshCleanupRepo{current: account}
				if change == "store-error" {
					repo.persistErr = errors.New("independent outbox storage failure")
				}
				cache, scheduler, runtime := &tokenCacheInvalidatorStub{}, &tokenRefreshSchedulerCache{}, &OpenAIGatewayService{}
				runtime.BlockAccountScheduling(account, until, "observed-before-call")
				var newFailureOwner *Account
				if change == "new-runtime" {
					repo.afterPersist = func() {
						newFailureOwner = snapshotRequestCredentialAccount(repo.current)
						runtime.BlockAccountScheduling(newFailureOwner, until, "new-session-failure")
					}
				}
				svc := &TokenRefreshService{accountRepo: repo, refreshAPI: NewOAuthRefreshAPI(repo, nil), cacheInvalidator: cache, schedulerCache: scheduler, runtimeBlocker: runtime, cfg: &config.TokenRefreshConfig{MaxRetries: 2}}
				refresher := NewCNTokenRefresher(platform)
				refresher.force = true
				calls := 0
				refresher.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(request *http.Request) (*http.Response, error) {
					calls++
					if platform == PlatformStepFun {
						require.Equal(t, "Oasis-Token=passport-access...passport-refresh", request.Header.Get("Cookie"))
					} else {
						require.NoError(t, request.ParseForm())
						require.Equal(t, "refresh", request.Form.Get("refresh_token"))
					}
					switch change {
					case "relinked":
						repo.current.Credentials["access_token"] = "replacement-login"
					case "nested-metadata":
						repo.current.Credentials["metadata"].(map[string]any)["grant"] = "replacement-login"
					case "proxy-changed":
						id := int64(92)
						repo.current.ProxyID = &id
					case "new-cooldown":
						later := until.Add(time.Hour)
						repo.current.TempUnschedulableUntil, repo.current.TempUnschedulableReason = &later, "installed-during-call"
					}
					if platform == PlatformStepFun {
						return cnRecoveryResponse(http.StatusOK, `{"accessToken":{"raw":"renewed-passport-access","mode":2,"duration":1800}}`), nil
					}
					return cnRecoveryResponse(http.StatusOK, `{"access_token":"renewed-inference-access","refresh_token":"rotated-refresh","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
				})}
				err := svc.refreshWithRetry(t.Context(), account, refresher, refresher, time.Hour)
				require.Equal(t, 1, calls, "the issuer may consume a refresh token before persistence fails")
				require.Equal(t, 1, repo.persistCalls)
				require.Zero(t, scheduler.setAccountCalls)
				success := change == "unchanged" || change == "new-cooldown" || change == "new-runtime"
				if !success {
					require.Error(t, err)
					require.Zero(t, repo.cleanupCalls+cache.calls+scheduler.deleteAccountCalls)
					require.NotNil(t, repo.current.TempUnschedulableUntil)
					if platform == PlatformStepFun {
						require.Equal(t, "passport-access", repo.current.GetCredential("step_account_access_token"))
					} else {
						require.NotEqual(t, "renewed-inference-access", repo.current.GetCredential("access_token"))
					}
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, cache.calls)
				require.Equal(t, 1, scheduler.deleteAccountCalls)
				if platform == PlatformStepFun {
					require.Equal(t, "old", repo.current.GetCredential("access_token"), "Passport cannot replace the inference model key")
					require.Equal(t, "renewed-passport-access", repo.current.GetCredential("step_account_access_token"))
					require.Equal(t, "passport-refresh", repo.current.GetCredential("step_account_refresh_token"))
				} else {
					require.Equal(t, "renewed-inference-access", repo.current.GetCredential("access_token"))
				}
				if change == "new-cooldown" {
					require.False(t, repo.applied)
					require.Equal(t, "installed-during-call", repo.current.TempUnschedulableReason)
				} else {
					require.True(t, repo.applied)
					require.Nil(t, repo.current.TempUnschedulableUntil)
				}
				if change == "new-runtime" {
					require.NotNil(t, newFailureOwner)
					require.True(t, runtime.peekOpenAIAccountRuntimeBlock(newFailureOwner).blocked, "an earlier refresh cannot clear a later failure of its renewed session")
				}
			})
		}
	}
}

func TestNativeBackgroundRefreshFailureCannotMutateGrantOrScheduling(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		for _, failure := range []string{"invalid-grant", "provider-unavailable", "late-old-login", "missing-contract"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				account := refreshCleanupNativeAccount(platform)
				until := time.Now().Add(time.Hour).Truncate(time.Second)
				account.TempUnschedulableUntil, account.TempUnschedulableReason = &until, "retained-cooldown"
				repo := &refreshCleanupRepo{current: account}
				var accountRepo AccountRepository = repo
				if failure == "missing-contract" {
					accountRepo = &refreshFailureUnavailableRepo{AccountRepository: repo}
				}
				cache, scheduler, runtime := &tokenCacheInvalidatorStub{}, &tokenRefreshSchedulerCache{}, &tokenRefreshRuntimeBlocker{}
				svc := &TokenRefreshService{accountRepo: accountRepo, refreshAPI: NewOAuthRefreshAPI(accountRepo, nil), cacheInvalidator: cache, schedulerCache: scheduler, runtimeBlocker: runtime, cfg: &config.TokenRefreshConfig{MaxRetries: 2}}
				calls := 0
				refresher := NewCNTokenRefresher(platform)
				refresher.force = true
				refresher.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
					calls++
					if failure == "late-old-login" {
						repo.current.Credentials["access_token"] = "replacement-login"
					}
					if failure == "provider-unavailable" {
						return nil, errors.New("independent provider network failure")
					}
					return cnRecoveryResponse(http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
				})}
				err := svc.refreshWithRetry(t.Context(), account, refresher, refresher, time.Hour)
				var contained *providerCycleContainmentRefreshError
				require.ErrorAs(t, err, &contained)
				if failure == "missing-contract" {
					require.Zero(t, calls, "a missing owner CAS must be rejected before consuming the issuer grant")
				} else {
					require.Equal(t, 1, calls, "native issuer failure is contained without a generic retry")
				}
				require.Zero(t, repo.persistCalls+repo.cleanupCalls+cache.calls+runtime.blockCalls+runtime.clearCalls+scheduler.deleteAccountCalls+scheduler.setAccountCalls)
				require.Equal(t, StatusActive, repo.current.Status)
				require.Equal(t, &until, repo.current.TempUnschedulableUntil)
				require.Equal(t, "retained-cooldown", repo.current.TempUnschedulableReason)
				if failure == "late-old-login" {
					require.Equal(t, "replacement-login", repo.current.GetCredential("access_token"))
				}
			})
		}
	}
}
