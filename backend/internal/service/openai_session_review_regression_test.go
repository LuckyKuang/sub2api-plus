//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// CODEX_CLIENT_PROFILES.md, "Session ownership and credential state": a
// migrating candidate's policy rejection remains request-scoped (review P1).
// It must reach the client and AfterTurn with its cyber evidence, without a
// retryable route/failover error, credential penalty, or owner commit.
func TestCodexWSMigrationCyberPolicyRegression(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, eventType := range []string{"error", "response.failed"} {
			t.Run(accountType+"/"+eventType, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				upstream := newStagedPassthroughConn()
				event := `{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"rate limit exceeded by policy"},"usage":{"input_tokens":5,"output_tokens":1}}`
				if eventType == "response.failed" {
					event = `{"type":"response.failed","response":{"id":"resp_policy","status":"failed","error":{"type":"authentication_error","code":"cyber_policy","status_code":401,"message":"blocked by policy"},"usage":{"input_tokens":5,"output_tokens":1}}}`
				}
				upstream.Send(`{"type":"response.created","response":{"id":"resp_policy"}}`)
				// Unknown frames with a policy-looking field are structural;
				// only recognized failure events may bypass acceptance staging.
				upstream.Send(`{"type":"fixture.unknown","error":{"code":"cyber_policy"}}`)
				upstream.Send(event)
				if eventType == "error" {
					upstream.Send(`{"type":"response.failed","response":{"id":"resp_policy","status":"failed","error":{"code":"cyber_policy","message":"paired policy rejection"}}}`)
				}
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				svc := newPassthroughLifecycleService(cfg, upstream)
				account := passthroughLifecycleAccount()
				account.Type = accountType
				if accountType == AccountTypeOAuth {
					// The ingress fixture transmits sk-test; bind an actual OAuth
					// grant so migration reaches upstream policy classification.
					account.Credentials = map[string]any{"access_token": "sk-test", "chatgpt_account_id": "migration-policy-owner", "chatgpt_user_id": "migration-policy-user"}
				}
				account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
				svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{
					{ID: 900, Platform: PlatformOpenAI, Type: accountType, Status: StatusError}, *account,
				}}
				svc.cache = &schedulerTestGatewayCache{}
				svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
				penalties := &openAIStream403AccountRepo{}
				svc.rateLimitService = NewRateLimitService(penalties, nil, cfg, nil, nil)
				store := newFakeOpenAISessionRouteStore()
				svc.sessionRouteStore = store
				_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 900})
				require.NoError(t, err)
				selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				defer selection.ReleaseFunc()
				attemptCtx := ContextWithSelectionProfitGate(ctx, selection)
				marks := make(chan *CyberPolicyMark, 2)
				turnErrors := make(chan error, 2)
				server, serverErr := startPassthroughLifecycleServerWithHooks(t, attemptCtx, svc, selection.Account, func(c *gin.Context) *OpenAIWSIngressHooks {
					return &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, turnErr error) {
						marks <- GetOpsCyberPolicy(c)
						turnErrors <- turnErr
					}}
				})
				defer server.Close()
				client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"gpt-5.1","input":"complete root","store":false}`)
				defer func() { _ = client.CloseNow() }()
				var mark *CyberPolicyMark
				select {
				case mark = <-marks:
				case <-ctx.Done():
					t.Fatal("policy rejection did not settle its turn")
				}
				require.NotNil(t, mark, "migration must classify upstream policy before rejecting candidate acceptance")
				require.Equal(t, "cyber_policy", mark.Code)
				require.Equal(t, 5, mark.UpstreamInTok)
				require.Equal(t, 1, mark.UpstreamOutTok)
				turnErr := <-turnErrors
				require.False(t, IsOpenAISessionRouteError(turnErr))
				var retry *UpstreamFailoverError
				require.False(t, errors.As(turnErr, &retry))
				for {
					payload, readErr := readPassthroughLifecycleFrame(t, client, time.Second)
					require.NoError(t, readErr)
					require.NotEqual(t, "fixture.unknown", gjson.GetBytes(payload, "type").String())
					if gjson.GetBytes(payload, "type").String() == eventType {
						require.JSONEq(t, event, string(payload))
						break
					}
				}
				require.Zero(t, penalties.setErrorCalls)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				_ = client.Close(coderws.StatusNormalClosure, "done")
				select {
				case proxyErr := <-serverErr:
					require.False(t, IsOpenAISessionRouteError(proxyErr))
					require.False(t, errors.As(proxyErr, &retry))
				case <-ctx.Done():
					t.Fatal("policy migration did not exit")
				}
				selection.ReleaseFunc()
				route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
				require.NoError(t, err)
				require.EqualValues(t, 900, route.OwnerAccountID)
				require.EqualValues(t, 1, route.Generation)
				require.Empty(t, route.LeaseID)
			})
		}
	}
}

type affinityParentLookupRepo struct {
	schedulerTestOpenAIAccountRepo
	parentID  int64
	err       error
	reads     int
	failAfter int
}

func (r *affinityParentLookupRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if id == r.parentID {
		r.reads++
		if r.err != nil && r.reads > r.failAfter {
			return nil, r.err
		}
	}
	return r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
}

// CODEX_CLIENT_PROFILES.md, "Session ownership and credential state": inability
// to confirm a credential owner's health is not evidence
// that the session owner is unavailable. Exercise selection through actual HTTP
// forwarding so a swallowed lookup error exposes both the send and commit.
func TestCodexShadowParentLookupStopsMigrationRegression(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		lookupErr error
		status    string
		wantOwner int64
	}{
		{"repository-failure", errors.New("fixture parent query failed"), StatusActive, 0},
		{"deadline", context.DeadlineExceeded, StatusActive, 0},
		{"not-found", ErrAccountNotFound, StatusActive, 102},
		{"unhealthy", nil, StatusError, 102},
		{"healthy", nil, StatusActive, 101},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx := context.Background()
			parentID := int64(100)
			owner := *newOpenAIRejectedFieldTestAccount()
			owner.ID, owner.Type, owner.Status, owner.Schedulable = 101, AccountTypeOAuth, StatusActive, true
			owner.ParentAccountID, owner.QuotaDimension = &parentID, QuotaDimensionSpark
			candidate := *newOpenAIRejectedFieldTestAccount()
			candidate.ID, candidate.Status, candidate.Schedulable = 102, StatusActive, true
			parent := Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: fixture.status, Schedulable: false,
				Credentials: map[string]any{"access_token": "fixture-parent-token", "chatgpt_account_id": "fixture-parent"}}
			wire := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
			svc := newOpenAIRejectedFieldTestService(wire)
			svc.accountRepo = &affinityParentLookupRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{owner, candidate, parent}}, parentID: parentID, err: fixture.lookupErr}
			svc.cache = &schedulerTestGatewayCache{}
			var acquired []int64
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})
			store := newFakeOpenAISessionRouteStore()
			svc.sessionRouteStore = store
			_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
			require.NoError(t, err)
			selection, _, selectionErr := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			if selection != nil {
				defer selection.ReleaseFunc()
				attemptCtx := ContextWithSelectionProfitGate(ctx, selection)
				body := []byte(`{"model":"gpt-5.1","input":"complete root","stream":true}`)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request = c.Request.WithContext(attemptCtx)
				_, err = svc.Forward(attemptCtx, c, selection.Account, body)
				require.NoError(t, err)
				selection.ReleaseFunc()
			}
			route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
			require.NoError(t, err)
			if fixture.wantOwner == 0 {
				require.Empty(t, wire.requests, "query failure must stop before any candidate upstream send")
				require.Empty(t, acquired)
				require.EqualValues(t, 101, route.OwnerAccountID)
				require.EqualValues(t, 1, route.Generation)
				require.Empty(t, route.LeaseID)
				require.Nil(t, selection)
				require.ErrorIs(t, selectionErr, ErrOpenAISessionRouteStoreUnavailable)
				return
			}
			require.NoError(t, selectionErr)
			require.NotNil(t, selection)
			require.Len(t, wire.requests, 1)
			require.Equal(t, fixture.wantOwner, selection.Account.ID)
			require.Equal(t, fixture.wantOwner, route.OwnerAccountID)
		})
	}
}

// The same unknown parent state must stop every scheduler layer, including
// failures discovered after taking a slot; those slots must be released.
func TestOpenAIShadowParentQueryFailureSelectionPathsRegression(t *testing.T) {
	for _, path := range []string{"candidate-filter", "acquired-refresh", "acquired-recheck", "wait-fallback", "wait-recheck", "weighted-sticky", "weighted-recheck", "sticky-recheck"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			parentID := int64(100)
			shadow := Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID,
				QuotaDimension: QuotaDimensionSpark, Status: StatusActive, Schedulable: true, Concurrency: 1}
			candidate := Account{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}
			failure := errors.New("fixture parent query failed")
			parent := Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
			repo := &affinityParentLookupRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{shadow, candidate, parent}}, parentID: parentID, err: failure}
			var acquired, released []int64
			svc := &OpenAIGatewayService{accountRepo: repo, cache: &schedulerTestGatewayCache{},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released})}
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportAny}
			order := []openAIAccountCandidateScore{{account: &shadow}, {account: &candidate}}
			var result *AccountSelectionResult
			var err error
			switch path {
			case "candidate-filter":
				result, _, _, _, err = scheduler.selectByLoadBalance(ctx, req)
			case "acquired-refresh":
				result, _, err = scheduler.tryAcquireOpenAISelectionOrder(ctx, req, order)
			case "acquired-recheck":
				repo.failAfter = 2
				result, _, err = scheduler.tryAcquireOpenAISelectionOrder(ctx, req, order)
			case "wait-fallback":
				result, _, _, _, err = scheduler.finishLoadBalanceSelectionFallback(ctx, req,
					openAIAccountLoadSelectionAttempt{selectionOrder: order}, newOpenAISelectionProbeBudget(), openAISelectionFilterStats{})
			case "wait-recheck":
				repo.failAfter = 2
				result, _, _, _, err = scheduler.finishLoadBalanceSelectionFallback(ctx, req,
					openAIAccountLoadSelectionAttempt{selectionOrder: order}, newOpenAISelectionProbeBudget(), openAISelectionFilterStats{})
			case "weighted-sticky":
				req.StickyWeighted, req.StickyAccountID, req.StickyPreviousAccountID = true, 101, 102
				result, err = scheduler.tryFallbackToWeightedSticky(ctx, req)
			case "weighted-recheck":
				repo.failAfter = 1
				req.StickyWeighted, req.StickyAccountID, req.StickyPreviousAccountID = true, 101, 102
				result, err = scheduler.tryFallbackToWeightedSticky(ctx, req)
			case "sticky-recheck":
				repo.failAfter = 1
				req.StickyAccountID, req.SessionHash = 101, "session"
				result, _, err = scheduler.selectBySessionHash(ctx, req)
			}
			if result != nil && result.ReleaseFunc != nil {
				result.ReleaseFunc()
			}
			require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
			require.ErrorIs(t, err, failure)
			require.Nil(t, result)
			if path == "acquired-refresh" || path == "acquired-recheck" {
				require.Equal(t, []int64{101}, acquired)
				require.Equal(t, []int64{101}, released)
			} else {
				require.Empty(t, acquired)
				require.Empty(t, released)
			}
		})
	}
}
