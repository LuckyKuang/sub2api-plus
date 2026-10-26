//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type grokOutcomeOwnerRepo struct {
	AccountRepository
	current      *Account
	legacyWrites int
	atomicWrites int
	failStorage  bool
	afterApplied func()
}

func testGrokOutcomeOwnersEqual(current, expected *Account) bool {
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type ||
		current.Status != expected.Status || current.Schedulable != expected.Schedulable || current.AutoPauseOnExpired != expected.AutoPauseOnExpired ||
		!reflect.DeepEqual(current.ProxyID, expected.ProxyID) || !sameTestCNProxySnapshot(current.Proxy, expected.Proxy) ||
		!reflect.DeepEqual(current.TempUnschedulableUntil, expected.TempUnschedulableUntil) || current.TempUnschedulableReason != expected.TempUnschedulableReason ||
		!reflect.DeepEqual(current.RateLimitedAt, expected.RateLimitedAt) || !reflect.DeepEqual(current.RateLimitResetAt, expected.RateLimitResetAt) ||
		!reflect.DeepEqual(current.OverloadUntil, expected.OverloadUntil) || !reflect.DeepEqual(current.ExpiresAt, expected.ExpiresAt) {
		return false
	}
	currentJSON, currentErr := json.Marshal(current.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected.Credentials)
	return currentErr == nil && expectedErr == nil && string(currentJSON) == string(expectedJSON)
}

func (r *grokOutcomeOwnerRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.current, nil
}

func (r *grokOutcomeOwnerRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.legacyWrites++
	if r.current.Extra == nil {
		r.current.Extra = map[string]any{}
	}
	for key, value := range updates {
		r.current.Extra[key] = value
	}
	return nil
}

func (r *grokOutcomeOwnerRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.legacyWrites++
	r.current.TempUnschedulableUntil, r.current.TempUnschedulableReason = &until, reason
	return nil
}

func (r *grokOutcomeOwnerRepo) SetRateLimited(_ context.Context, _ int64, reset time.Time) error {
	r.legacyWrites++
	now := time.Now()
	r.current.RateLimitedAt, r.current.RateLimitResetAt = &now, &reset
	return nil
}

func (r *grokOutcomeOwnerRepo) SetRateLimitedIfLater(ctx context.Context, id int64, reset time.Time) error {
	return r.SetRateLimited(ctx, id, reset)
}

// The fake compares the independent storage contract, including previous
// scheduler state. It does not call the production fingerprint or writer.
func (r *grokOutcomeOwnerRepo) ApplyGrokRequestOutcomeIfUnchanged(_ context.Context, expected *Account, outcome GrokRequestOutcome) (GrokRequestOutcomeResult, error) {
	if r.failStorage {
		return GrokRequestOutcomeResult{}, errors.New("injected Grok outcome storage failure")
	}
	current := r.current
	if !testGrokOutcomeOwnersEqual(current, expected) {
		return GrokRequestOutcomeResult{}, nil
	}
	r.atomicWrites++
	if len(outcome.Extra) > 0 {
		if current.Extra == nil {
			current.Extra = map[string]any{}
		}
		for key, value := range outcome.Extra {
			current.Extra[key] = value
		}
	}
	if outcome.TempUnschedulableUntil != nil {
		if current.TempUnschedulableUntil == nil || current.TempUnschedulableUntil.Before(*outcome.TempUnschedulableUntil) {
			value := *outcome.TempUnschedulableUntil
			current.TempUnschedulableUntil = &value
		}
		current.TempUnschedulableReason = outcome.TempUnschedulableReason
	}
	if outcome.RateLimitResetAt != nil && (current.RateLimitResetAt == nil || current.RateLimitResetAt.Before(*outcome.RateLimitResetAt)) {
		now, reset := time.Now(), *outcome.RateLimitResetAt
		current.RateLimitedAt, current.RateLimitResetAt = &now, &reset
	}
	if outcome.ClearRateLimit {
		current.RateLimitedAt, current.RateLimitResetAt = nil, nil
	}
	generation, _ := current.Extra["runtime_recovery_generation"].(string)
	if r.afterApplied != nil {
		r.afterApplied()
	}
	return GrokRequestOutcomeResult{Applied: true, RecoveryGeneration: generation}, nil
}

type grokOutcomeHTTPUpstream struct {
	HTTPUpstream
	respond func(*http.Request, string, int64) (*http.Response, error)
}

func TestGrokRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit(t *testing.T) {
	for _, operation := range []string{"temporary-cooldown", "rate-limit"} {
		for _, change := range []string{"unchanged", "relinked", "proxy-edited"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
				owner, err := snapshotCNControlAccount(account)
				require.NoError(t, err)
				repo := &grokOutcomeOwnerRepo{current: account}
				repo.afterApplied = func() {
					replacement, copyErr := snapshotCNControlAccount(account)
					require.NoError(t, copyErr)
					switch change {
					case "relinked":
						replacement.Credentials["api_key"] = "post-commit-replacement-key"
					case "proxy-edited":
						replacement.Proxy.Password = "post-commit-replacement-route"
					}
					repo.current = replacement
				}
				svc := &OpenAIGatewayService{accountRepo: repo}
				if operation == "temporary-cooldown" {
					svc.tempUnscheduleGrok(context.Background(), owner, time.Minute, "captured request failure")
				} else {
					svc.rateLimitGrok(context.Background(), owner, time.Now().Add(time.Minute))
				}
				require.Equal(t, 1, repo.atomicWrites)
				require.Zero(t, repo.legacyWrites)
				require.Equal(t, change == "unchanged", svc.isOpenAIAccountRuntimeBlocked(repo.current), "a post-commit relink must not inherit the old owner's delayed runtime publication")
			})
		}
	}
}

func TestGrokRequestOutcomeRejectsCallerTransactionBeforeStorageOrRuntime(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	repo := &grokOutcomeOwnerRepo{current: account}
	svc := &OpenAIGatewayService{accountRepo: repo}
	ctx := dbent.NewTxContext(context.Background(), &dbent.Tx{})
	svc.tempUnscheduleGrok(ctx, account, time.Minute, "caller transaction cannot publish")
	require.Zero(t, repo.legacyWrites)
	require.Zero(t, repo.atomicWrites, "a statement in an uncommitted caller transaction cannot authorize runtime publication")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestGrokRuntimeOwnerEntriesExpireWithoutReplacingCurrentOwner(t *testing.T) {
	for _, operation := range []string{"current-owner-read", "current-owner-publish", "expired-owner-peek", "clear-all-owner-generations"} {
		t.Run(operation, func(t *testing.T) {
			oldOwner := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
			current, err := snapshotCNControlAccount(oldOwner)
			require.NoError(t, err)
			current.Credentials["api_key"] = "current-runtime-owner"
			svc := &OpenAIGatewayService{}
			currentUntil := time.Now().Add(time.Minute)
			svc.BlockAccountScheduling(current, currentUntil, "current-owner-block")
			oldKey := openAIAccountRuntimeOwnerKey(oldOwner)
			svc.openaiAccountRuntimeBlockUntil.Store(oldKey, time.Now().Add(-time.Hour))
			svc.openaiAccountRuntimeBlockGeneration.Store(oldKey, uint64(100))
			switch operation {
			case "current-owner-read":
				require.True(t, svc.isOpenAIAccountRuntimeBlocked(current))
			case "current-owner-publish":
				svc.BlockAccountScheduling(current, currentUntil, "preserve-current-block")
			case "expired-owner-peek":
				require.False(t, svc.peekOpenAIAccountRuntimeBlock(oldOwner).blocked)
			case "clear-all-owner-generations":
				svc.openaiAccountRuntimeBlockUntil.Delete(oldKey) // A historical generation without a deadline is stale too.
				svc.ClearAccountSchedulingBlock(current.ID)
			}
			_, retainedUntil := svc.openaiAccountRuntimeBlockUntil.Load(oldKey)
			_, retainedGeneration := svc.openaiAccountRuntimeBlockGeneration.Load(oldKey)
			require.False(t, retainedUntil, "normal current-owner activity must reclaim expired historical owners")
			require.False(t, retainedGeneration, "expired or explicitly cleared owners must not leave orphan generations")
			if operation != "clear-all-owner-generations" {
				require.True(t, svc.isOpenAIAccountRuntimeBlocked(current))
				require.WithinDuration(t, currentUntil, svc.peekOpenAIAccountRuntimeBlock(current).until, time.Millisecond)
			} else {
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(current))
			}
		})
	}
}

func TestGrokRuntimeOwnerPublicationAndRollbackAreIndependent(t *testing.T) {
	oldOwner := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
	current, err := snapshotCNControlAccount(oldOwner)
	require.NoError(t, err)
	current.Credentials["api_key"] = "new-runtime-owner"
	svc := &OpenAIGatewayService{}
	currentUntil, oldUntil := time.Now().Add(time.Minute), time.Now().Add(time.Hour)
	svc.BlockAccountScheduling(current, currentUntil, "current-owner-block")
	svc.BlockAccountScheduling(oldOwner, oldUntil, "delayed-old-owner-block")
	require.WithinDuration(t, currentUntil, svc.peekOpenAIAccountRuntimeBlock(current).until, time.Millisecond)
	require.WithinDuration(t, oldUntil, svc.peekOpenAIAccountRuntimeBlock(oldOwner).until, time.Millisecond)
	svc.clearOpenAIAccountRuntimeBlockIfUnchanged(oldOwner.ID, svc.peekOpenAIAccountRuntimeBlock(oldOwner))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(oldOwner))
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(current), "clearing an old generation must retain the replacement owner's block")
}

func TestGrokNonFailoverErrorsDoNotReenterGenericAccountMutation(t *testing.T) {
	accountID := int64(81200)
	for _, path := range []string{"responses", "chat"} {
		for _, kind := range []string{"model-capacity", "content-policy"} {
			for _, change := range []string{"unchanged", "relinked", "proxy-edited"} {
				t.Run(path+"/"+kind+"/"+change, func(t *testing.T) {
					accountID++
					account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
					account.ID = accountID
					repo := &grokOutcomeOwnerRepo{current: account}
					status, responseBody := http.StatusTooManyRequests, `{"error":{"message":"The model is currently at capacity due to high demand"}}`
					if kind == "content-policy" {
						status, responseBody = http.StatusForbidden, `{"error":{"code":"new_sensitive","message":"image is sensitive"}}`
					}
					upstream := &grokOutcomeHTTPUpstream{respond: func(_ *http.Request, _ string, _ int64) (*http.Response, error) {
						if change == "relinked" {
							account.Credentials["api_key"] = "replacement-nonfailover-owner"
						} else if change == "proxy-edited" {
							account.Proxy.Password = "replacement-nonfailover-route"
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}, nil
					}}
					cfg := &config.Config{}
					svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, httpUpstream: upstream,
						rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil)}
					var err error
					if path == "responses" {
						_, err = svc.Forward(context.Background(), grokAuxiliaryInbound(http.MethodPost, "/v1/responses"), account, []byte(`{"model":"grok-4.5","input":"hello","stream":false}`))
					} else {
						_, err = svc.forwardAsRawChatCompletions(context.Background(), grokAuxiliaryInbound(http.MethodPost, "/v1/chat/completions"), account, []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`), "")
					}
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover), "capacity and request content policy do not fail over the account")
					require.Zero(t, repo.legacyWrites, "the generic formatter cannot repeat the shared Grok policy through ID-only writers")
					require.Nil(t, account.TempUnschedulableUntil)
					require.Nil(t, account.RateLimitResetAt, "model capacity cannot become account quota exhaustion")
					require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
					if kind == "model-capacity" && change == "unchanged" {
						require.Equal(t, 1, repo.atomicWrites, "one upstream error owns one observation transaction")
						require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()))
					} else {
						require.Zero(t, repo.atomicWrites)
						require.False(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()))
					}
				})
			}
		}
	}
}

func TestGrokChatResponsesBridgeRetainsErrorObservationOwner(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway} {
		for _, change := range []string{"unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure"} {
			t.Run(http.StatusText(status)+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
				repo := &grokOutcomeOwnerRepo{current: account, failStorage: change == "storage-failure"}
				otherUntil := time.Now().Add(6 * time.Hour)
				calls := 0
				upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, proxy string, _ int64) (*http.Response, error) {
					calls++
					require.Equal(t, "/v1/responses", request.URL.Path, "exercise the independent Chat-to-Responses sender")
					require.Equal(t, "Bearer private-grok-token", request.Header.Get("Authorization"))
					require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", proxy)
					switch change {
					case "relinked":
						account.Credentials["access_token"] = "replacement-bridge-owner"
					case "proxy-edited":
						account.Proxy.Password = "replacement-bridge-route"
					case "manual-pause":
						account.Schedulable = false
					case "other-cooldown":
						account.TempUnschedulableUntil, account.TempUnschedulableReason = &otherUntil, "independent-bridge-cooldown"
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"isolated upstream failure"}}`))}, nil
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream,
					grokTokenProvider: NewGrokTokenProvider(repo, nil)}
				inbound := grokAuxiliaryInbound(http.MethodPost, "/v1/chat/completions")
				inbound.Set("api_key", &APIKey{ID: 7933})
				_, err := svc.ForwardAsChatCompletions(context.Background(), inbound, account,
					[]byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`), "bridge-owner-session", "")
				require.Error(t, err)
				require.Equal(t, 1, calls)
				require.Zero(t, repo.legacyWrites)
				if change == "unchanged" {
					require.Equal(t, 1, repo.atomicWrites)
					require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
				} else {
					require.Zero(t, repo.atomicWrites, "bridge results retain their sent credential/proxy/scheduling owner")
					require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
					require.Empty(t, account.Extra)
					require.Nil(t, account.RateLimitResetAt)
					if change == "other-cooldown" {
						require.Equal(t, &otherUntil, account.TempUnschedulableUntil)
						require.Equal(t, "independent-bridge-cooldown", account.TempUnschedulableReason)
					} else {
						require.Nil(t, account.TempUnschedulableUntil)
					}
				}
			})
		}
	}
}

func TestGrokChatResponsesBridgeRejectsBrokenProxyBeforeCredentialLookup(t *testing.T) {
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			account := grokStrictProxyAccount(AccountTypeOAuth, mode)
			cache := &grokAuxiliaryTokenCache{}
			calls := 0
			upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, proxy string, _ int64) (*http.Response, error) {
				calls++
				require.Equal(t, "/v1/responses", request.URL.Path)
				require.Equal(t, "Bearer private-grok-token", request.Header.Get("Authorization"))
				if mode == "explicitly-unbound" {
					require.Empty(t, proxy)
				} else {
					require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", proxy)
				}
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid isolated request"}}`))}, nil
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream,
				grokTokenProvider: NewGrokTokenProvider(nil, cache)}
			inbound := grokAuxiliaryInbound(http.MethodPost, "/v1/chat/completions")
			inbound.Set("api_key", &APIKey{ID: 7933})
			_, err := svc.ForwardAsChatCompletions(context.Background(), inbound, account,
				[]byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`), "bridge-route-session", "")
			require.Error(t, err)
			if mode == "valid-bound" || mode == "explicitly-unbound" {
				require.Equal(t, 1, cache.reads)
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, cache.reads, "validate the bridge route before any OAuth credential lookup")
				require.Zero(t, calls, "a broken bridge route cannot send a bearer directly")
				grokAuxiliaryAssertSafeError(t, err)
			}
		})
	}
}

func TestGrokSuccessfulHTTPRequestRecoversOnlyObservedRateLimitOwner(t *testing.T) {
	for _, path := range []string{"voice", "media", "responses", "chat"} {
		for _, change := range []string{"unchanged", "relinked", "proxy-edited", "newer-rate-limit", "storage-failure"} {
			t.Run(path+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
				observedAt, observedReset := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Minute)
				account.RateLimitedAt, account.RateLimitResetAt = &observedAt, &observedReset
				repo := &grokOutcomeOwnerRepo{current: account, failStorage: change == "storage-failure"}
				newAt, newReset := time.Now(), time.Now().Add(time.Hour)
				calls := 0
				upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, _ string, _ int64) (*http.Response, error) {
					calls++
					require.Equal(t, "Bearer private-grok-token", request.Header.Get("Authorization"))
					switch change {
					case "relinked":
						account.Credentials["access_token"] = "replacement-success-owner"
					case "proxy-edited":
						account.Proxy.Password = "replacement-success-route"
					case "newer-rate-limit":
						account.RateLimitedAt, account.RateLimitResetAt = &newAt, &newReset
					}
					body := `{"data":[{"url":"https://images.example.test/result.png"}],"text":"hello"}`
					if path == "responses" {
						body = `{"id":"response-owner","object":"response","status":"completed","model":"grok-4.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
					} else if path == "chat" {
						body = `{"id":"chat-owner","object":"chat.completion","model":"grok-4.5","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
						"Content-Type": {"application/json"}, "X-Ratelimit-Limit-Requests": {"100"}, "X-Ratelimit-Remaining-Requests": {"90"},
					}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil)}
				var err error
				ctx := context.Background()
				switch path {
				case "voice":
					_, err = svc.ForwardGrokVoice(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/tts"), account, "tts", []byte(`{"text":"hello"}`), "application/json")
				case "media":
					_, err = svc.ForwardGrokMedia(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/images/generations"), account, GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"grok-imagine-image","prompt":"a tree"}`), "application/json")
				case "responses":
					_, err = svc.Forward(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/responses"), account, []byte(`{"model":"grok-4.5","input":"hello","stream":false}`))
				case "chat":
					_, err = svc.forwardAsRawChatCompletions(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/chat/completions"), account, []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`), "")
				}
				require.NoError(t, err, "a successful provider response remains usable when its observation cannot be saved")
				require.Equal(t, 1, calls)
				require.Zero(t, repo.legacyWrites)
				if change == "unchanged" {
					require.Equal(t, 1, repo.atomicWrites, "quota snapshot and successful recovery use one transaction")
					require.Nil(t, account.RateLimitedAt)
					require.Nil(t, account.RateLimitResetAt)
					require.Contains(t, account.Extra, grokQuotaSnapshotExtraKey)
				} else {
					require.Zero(t, repo.atomicWrites)
					require.Empty(t, account.Extra)
					if change == "newer-rate-limit" {
						require.Equal(t, &newAt, account.RateLimitedAt)
						require.Equal(t, &newReset, account.RateLimitResetAt)
					} else {
						require.Equal(t, &observedAt, account.RateLimitedAt)
						require.Equal(t, &observedReset, account.RateLimitResetAt)
					}
				}
			})
		}
	}
}

func TestGrokDiagnosticObservationsRequireCurrentCredentialProxyAndSchedulerOwner(t *testing.T) {
	for _, path := range []string{"active-quota", "billing", "admin-voice"} {
		for _, status := range []int{http.StatusOK, http.StatusForbidden} {
			for _, change := range []string{"unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure"} {
				t.Run(path+"/"+http.StatusText(status)+"/"+change, func(t *testing.T) {
					account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
					repo := &grokOutcomeOwnerRepo{current: account, failStorage: change == "storage-failure"}
					var once sync.Once
					var calls atomic.Int64
					otherUntil := time.Now().Add(6 * time.Hour)
					upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, proxy string, _ int64) (*http.Response, error) {
						calls.Add(1)
						require.Equal(t, "Bearer private-grok-token", request.Header.Get("Authorization"))
						require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", proxy)
						once.Do(func() {
							switch change {
							case "relinked":
								account.Credentials["access_token"] = "replacement-diagnostic-owner"
							case "proxy-edited":
								account.Proxy.Password = "replacement-diagnostic-route"
							case "manual-pause":
								account.Schedulable = false
							case "other-cooldown":
								account.TempUnschedulableUntil, account.TempUnschedulableReason = &otherUntil, "independent-diagnostic-cooldown"
							}
						})
						body := `{"config":{"currentPeriod":{"type":"WEEKLY","start":"2026-10-19T00:00:00Z","end":"2026-10-26T00:00:00Z"},"creditUsagePercent":12}}`
						if status >= 400 {
							body = `{"error":{"message":"isolated entitlement denied"}}`
						} else if path == "admin-voice" {
							body = "ID3isolated-audio"
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"audio/mpeg"},
							"X-Ratelimit-Limit-Requests": {"100"}, "X-Ratelimit-Remaining-Requests": {"90"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					}}
					provider := NewGrokTokenProvider(repo, nil)
					var result *GrokQuotaProbeResult
					if path == "admin-voice" {
						svc := &AccountTestService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream, grokTokenProvider: provider}
						_ = svc.testGrokAccountConnection(grokAuxiliaryInbound(http.MethodPost, "/admin/accounts/test"), account, "", "hello", AccountTestModeGrokTTS, AccountTestOptions{})
					} else {
						svc := NewGrokQuotaService(repo, nil, provider, upstream, &config.Config{})
						if path == "active-quota" {
							result, _ = svc.ProbeUsage(context.Background(), account.ID)
						} else {
							result, _ = svc.ProbeBilling(context.Background(), account.ID)
						}
					}
					require.Positive(t, calls.Load(), "the actual diagnostic request must reach the upstream boundary")
					require.Zero(t, repo.legacyWrites, "diagnostic snapshots and readiness are request observations too")
					if change == "unchanged" {
						if status == http.StatusOK || path != "active-quota" {
							require.Equal(t, 1, repo.atomicWrites, "save the complete diagnostic result with one owner CAS")
						}
						if result != nil && status == http.StatusOK {
							require.True(t, result.Persisted)
						}
					} else {
						require.Zero(t, repo.atomicWrites)
						require.Empty(t, account.Extra)
						if result != nil {
							require.False(t, result.Persisted)
						}
						if change == "other-cooldown" {
							require.Equal(t, &otherUntil, account.TempUnschedulableUntil)
							require.Equal(t, "independent-diagnostic-cooldown", account.TempUnschedulableReason)
						} else {
							require.Nil(t, account.TempUnschedulableUntil)
						}
					}
				})
			}
		}
	}
}

func (u *grokOutcomeHTTPUpstream) Do(request *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	return u.respond(request, proxy, id)
}

func (u *grokOutcomeHTTPUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, id, concurrency)
}

// These are the actual independent HTTP senders. A valid response from the old
// login/route must not update the new owner or its in-memory scheduling overlay.
func TestGrokHTTPResponseCannotPublishAnotherOwnersErrorState(t *testing.T) {
	for _, path := range []string{"voice", "media", "responses", "chat"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway} {
			for _, change := range []string{"unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure"} {
				t.Run(path+"/"+http.StatusText(status)+"/"+change, func(t *testing.T) {
					account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
					repo := &grokOutcomeOwnerRepo{current: account, failStorage: change == "storage-failure"}
					otherUntil := time.Now().Add(6 * time.Hour)
					calls := 0
					upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, proxy string, id int64) (*http.Response, error) {
						calls++
						require.Equal(t, account.ID, id)
						require.Equal(t, "Bearer private-grok-token", request.Header.Get("Authorization"))
						require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", proxy)
						switch change {
						case "relinked":
							account.Credentials["api_key"] = "replacement-grok-owner"
						case "proxy-edited":
							account.Proxy.Password = "replacement-grok-route"
						case "manual-pause":
							account.Schedulable = false
						case "other-cooldown":
							account.TempUnschedulableUntil, account.TempUnschedulableReason = &otherUntil, "independent-cooldown"
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"isolated upstream failure"}}`))}, nil
					}}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
					ctx := context.Background()
					var err error
					switch path {
					case "voice":
						_, err = svc.ForwardGrokVoice(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/tts"), account, "tts", []byte(`{"text":"hello"}`), "application/json")
					case "media":
						_, err = svc.ForwardGrokMedia(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/images/generations"), account, GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"grok-imagine-image","prompt":"a tree"}`), "application/json")
					case "responses":
						_, err = svc.Forward(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/responses"), account, []byte(`{"model":"grok-4.5","input":"hello","stream":false}`))
					case "chat":
						_, err = svc.forwardAsRawChatCompletions(ctx, grokAuxiliaryInbound(http.MethodPost, "/v1/chat/completions"), account, []byte(`{"model":"grok-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`), "")
					}
					require.Error(t, err)
					require.Equal(t, 1, calls)
					require.Zero(t, repo.legacyWrites, "a request observation requires a full credential/proxy/scheduler CAS")
					if change == "unchanged" {
						require.Positive(t, repo.atomicWrites)
						require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
					} else {
						require.Zero(t, repo.atomicWrites)
						require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "a rejected stale result cannot publish a runtime block")
						require.Empty(t, account.Extra)
						require.Nil(t, account.RateLimitResetAt)
						if change == "other-cooldown" {
							require.Equal(t, &otherUntil, account.TempUnschedulableUntil)
							require.Equal(t, "independent-cooldown", account.TempUnschedulableReason)
						} else {
							require.Nil(t, account.TempUnschedulableUntil)
						}
					}
				})
			}
		}
	}
}
