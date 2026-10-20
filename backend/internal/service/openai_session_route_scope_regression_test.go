//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Requirements: impact-review P1 and CODEX_CLIENT_PROFILES.md, "Session
// ownership and credential state". Durable text ownership must not change
// another platform's TTL failover or become its availability dependency.
type scopeObservedRouteStore struct {
	*fakeOpenAISessionRouteStore
	reads int
}

func (s *scopeObservedRouteStore) Get(ctx context.Context, domain, digest string) (OpenAISessionRoute, error) {
	s.reads++
	return s.fakeOpenAISessionRouteStore.Get(ctx, domain, digest)
}

func newScopeRegressionService(t *testing.T, platform, mode string, store OpenAISessionRouteStore, wire HTTPUpstream) *OpenAIGatewayService {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	settings := &openAIAdvancedSchedulerSettingRepoStub{values: map[string]string{
		openAIAdvancedSchedulerSettingKey:                      fmt.Sprint(mode != "legacy"),
		SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled: fmt.Sprint(mode == "weighted"),
	}}
	cfg := &config.Config{}
	accounts := make([]Account, 0, 2)
	for _, id := range []int64{101, 102} {
		a := newOpenAIRejectedFieldTestAccount()
		a.ID, a.Platform = id, platform
		a.Credentials["api_key"] = fmt.Sprintf("fixture-key-%d", id)
		if platform != PlatformOpenAI {
			a.Credentials["api_protocol"] = "chat_completions"
			a.Extra["responses_mode"] = "force_chat_completions"
		}
		accounts = append(accounts, *a)
	}
	svc := newOpenAIRejectedFieldTestService(nil)
	svc.httpUpstream = wire
	svc.cfg, svc.accountRepo = cfg, schedulerTestOpenAIAccountRepo{accounts: accounts}
	svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
	svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
	svc.rateLimitService = &RateLimitService{settingService: NewSettingService(settings, cfg)}
	return svc
}

func TestSessionRouteScopeCompatibleFailoverWireRegression(t *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformStepFun, PlatformOpenCodeGo, PlatformCommandCode, PlatformCline} {
		for _, mode := range []string{"legacy", "advanced", "weighted"} {
			for _, unavailable := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/store-unavailable=%v", platform, mode, unavailable), func(t *testing.T) {
					ctx := context.Background()
					store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
					// A stale durable owner must not influence this provider, even
					// if a pre-fix deployment wrote it under the same session hash.
					original, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
					if unavailable {
						store.fail = errors.New("fixture route database unavailable")
					}
					wire := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"chat-fixture","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)}
					svc := newScopeRegressionService(t, platform, mode, store, wire)
					selectAccount := func(excluded int64) *AccountSelectionResult {
						sel, _, selectErr := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "session", "", map[int64]struct{}{excluded: {}}, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, false, false, platform)
						require.NoError(t, selectErr)
						require.NotNil(t, sel)
						return sel
					}
					a := selectAccount(102)
					require.EqualValues(t, 101, a.Account.ID)
					require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ContextWithSelectionProfitGate(ctx, a), nil, "session", 101))
					a.ReleaseFunc()
					b := selectAccount(101)
					defer b.ReleaseFunc()
					require.EqualValues(t, 102, b.Account.ID)
					admissionCtx := ContextWithSelectionProfitGate(ctx, b)
					require.NoError(t, svc.BindStickySessionAfterProfitAdmission(admissionCtx, nil, "session", 102))
					body := []byte(`{"model":"fixture-model","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request = c.Request.WithContext(admissionCtx)
					result, err := svc.ForwardAsChatCompletions(admissionCtx, c, b.Account, body, "", "")
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, wire.requests, 1)
					require.Equal(t, "Bearer fixture-key-102", wire.lastReq.Header.Get("Authorization"))
					require.Equal(t, http.StatusOK, c.Writer.Status())
					require.Equal(t, 1, result.Usage.OutputTokens)
					cache, ok := svc.cache.(*schedulerTestGatewayCache)
					require.True(t, ok)
					require.EqualValues(t, 102, cache.sessionBindings["openai:session"])
					require.Zero(t, store.reads, "a compatible request must not query the durable route")
					require.Equal(t, map[string]OpenAISessionRoute{fakeRouteKey("group:0", OpenAISessionRouteDigest("session")): original}, store.routes)
				})
			}
		}
	}
}

// Images use their existing affinity and complete independently of a text
// session. A successful image must not leave a candidate lease on that route.
func TestSessionRouteScopeImagesWireRegression(t *testing.T) {
	for _, mode := range []string{"legacy", "advanced", "weighted"} {
		for _, unavailable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/store-unavailable=%v", mode, unavailable), func(t *testing.T) {
				ctx := context.Background()
				store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
				original, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
				require.NoError(t, err)
				if unavailable {
					store.fail = errors.New("fixture route database unavailable")
				}
				wire := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
				svc := newScopeRegressionService(t, PlatformOpenAI, mode, store, wire)
				repo, ok := svc.accountRepo.(schedulerTestOpenAIAccountRepo)
				require.True(t, ok)
				repo.accounts[0].Status, repo.accounts[0].Schedulable = StatusError, false
				body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)
				c, response := newOpenAIImagesTestContext(t, body)
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				selection, _, err := svc.SelectAccountWithSchedulerForImages(ctx, nil, "session", parsed.Model, map[int64]struct{}{101: {}}, parsed.RequiredCapability)
				require.NoError(t, err)
				require.NotNil(t, selection)
				defer selection.ReleaseFunc()
				require.EqualValues(t, 102, selection.Account.ID)
				admissionCtx := ContextWithSelectionProfitGate(ctx, selection)
				require.NoError(t, svc.BindStickySessionAfterProfitAdmission(admissionCtx, nil, "session", 102))
				c.Request = c.Request.WithContext(admissionCtx)
				result, err := svc.ForwardImages(admissionCtx, c, selection.Account, body, parsed, "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 1, result.ImageCount)
				require.Equal(t, http.StatusOK, response.Code)
				require.Contains(t, response.Body.String(), `"b64_json":"aGVsbG8="`)
				require.Len(t, wire.requests, 1)
				require.Equal(t, "Bearer fixture-key-102", wire.lastReq.Header.Get("Authorization"))
				require.Equal(t, map[string]OpenAISessionRoute{fakeRouteKey("group:0", OpenAISessionRouteDigest("session")): original}, store.routes, "image selection/admission/output must not mutate a text owner or lease")
				require.Zero(t, store.reads)
			})
		}
	}
}

func TestSessionRouteScopeCompatibleStickyEscapeRegression(t *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformStepFun, PlatformOpenCodeGo, PlatformCommandCode, PlatformCline} {
		t.Run(platform, func(t *testing.T) {
			ctx := context.Background()
			store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
			original, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
			require.NoError(t, err)
			svc := newScopeRegressionService(t, platform, "advanced", store, nil)
			cache, ok := svc.cache.(*schedulerTestGatewayCache)
			require.True(t, ok)
			cache.sessionBindings = map[string]int64{"openai:session": 101}
			svc.cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
			svc.cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 15000
			svc.openaiAccountStats = newOpenAIAccountRuntimeStats()
			slowTTFT := 20000
			for i := 0; i < 3; i++ {
				svc.openaiAccountStats.report(101, true, &slowTTFT)
			}
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "session", "", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityChatCompletions, false, false, false, platform)
			require.NoError(t, err)
			require.NotNil(t, selection)
			defer selection.ReleaseFunc()
			require.EqualValues(t, 102, selection.Account.ID, "the provider's ordinary TTFT escape must survive a stale durable owner")
			require.Zero(t, store.reads)
			require.Equal(t, map[string]OpenAISessionRoute{fakeRouteKey("group:0", OpenAISessionRouteDigest("session")): original}, store.routes)
		})
	}
}

func TestSessionRouteScopeAuxiliarySelectionRegression(t *testing.T) {
	for _, capability := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityEmbeddings, OpenAIEndpointCapabilityAlphaSearch, OpenAIEndpointCapabilityLive} {
		t.Run(string(capability), func(t *testing.T) {
			store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
			store.fail = errors.New("fixture route database unavailable")
			svc := newScopeRegressionService(t, PlatformOpenAI, "advanced", store, nil)
			// Isolate the request's scope from provider capability eligibility;
			// the actual selector still binds through shared production code.
			ctx := context.Background()
			selection, _, err := svc.selectAccountWithCanonicalRoute(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "session", RequiredCapability: capability}, func(candidateCtx context.Context, session string, _ map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
				require.NoError(t, svc.BindStickySession(candidateCtx, nil, session, 102))
				account, accountErr := svc.getSchedulableAccount(candidateCtx, 102)
				return &AccountSelectionResult{Account: account}, OpenAIAccountScheduleDecision{}, accountErr
			})
			require.NoError(t, err)
			require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ContextWithSelectionProfitGate(ctx, selection), nil, "session", 102))
			require.Zero(t, store.reads)
			require.Empty(t, store.routes)
		})
	}
	t.Run("legacy-compact", func(t *testing.T) {
		store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
		store.fail = errors.New("fixture route database unavailable")
		svc := newScopeRegressionService(t, PlatformOpenAI, "advanced", store, nil)
		ctx := context.Background()
		selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "", nil, OpenAIUpstreamTransportAny, true)
		require.NoError(t, err)
		require.NotNil(t, selection)
		defer selection.ReleaseFunc()
		require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ContextWithSelectionProfitGate(ctx, selection), nil, "session", selection.Account.ID))
		require.Zero(t, store.reads)
		require.Empty(t, store.routes)
	})
	t.Run("token-count", func(t *testing.T) {
		store := &scopeObservedRouteStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
		store.fail = errors.New("fixture route database unavailable")
		svc := newScopeRegressionService(t, PlatformOpenAI, "legacy", store, nil)
		account, err := svc.SelectAccountForTokenCount(context.Background(), nil, "session", "", OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI)
		require.NoError(t, err)
		require.NotNil(t, account)
		require.Zero(t, store.reads)
		require.Empty(t, store.routes)
	})
}
