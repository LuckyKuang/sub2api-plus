//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// A provider may put the Responses event type only in the SSE event field.
// Successful output must commit ownership before reaching the client; a failed
// or structurally empty response must retain the old owner.
func TestOpenAISessionRouteEventNamedSSEAcceptance(t *testing.T) {
	for _, pass := range []bool{false, true} {
		for _, outcome := range []string{"text", "usage-only", "empty", "failed", "json-type-wins", "json-success-wins", "unknown", "created-only"} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", pass, outcome), func(t *testing.T) {
				ctx := context.Background()
				store := newFakeOpenAISessionRouteStore()
				_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
				require.NoError(t, err)
				payload := "event: response.created\ndata: {\"response\":{\"id\":\"resp_candidate\"}}\n\n"
				switch outcome {
				case "text":
					payload += "event: response.output_text.delta\ndata: {\"delta\":\"accepted\"}\n\n" +
						"event: response.completed\ndata: {\"response\":{\"id\":\"resp_candidate\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"accepted\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
				case "usage-only":
					payload += "event: response.completed\ndata: {\"response\":{\"id\":\"resp_candidate\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
				case "empty":
					payload += "event: response.completed\ndata: {\"response\":{\"id\":\"resp_candidate\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"
				case "failed":
					payload += "event: response.failed\ndata: {\"response\":{\"id\":\"resp_candidate\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"fixture failure\"}}}\n\n"
				case "json-type-wins":
					payload += "event: response.completed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_candidate\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"fixture failure\"}}}\n\n"
				case "json-success-wins":
					payload += "event: response.failed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_candidate\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1}}}\n\n"
				case "unknown":
					payload += "event: response.future\ndata: {\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1}}}\n\n"
				case "created-only":
					payload += "data: [DONE]\n\n"
				}
				wire := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusOK, payload)}
				wire.resp.Header.Set("Content-Type", "text/event-stream")
				svc := newOpenAIRejectedFieldTestService(wire)
				a := *newOpenAIRejectedFieldTestAccount()
				a.ID, a.Extra["openai_passthrough"] = 102, pass
				svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
				svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError}, a}}
				svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
				selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				defer selected.ReleaseFunc()
				ctx = ContextWithSelectionProfitGate(ctx, selected)
				body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"complete root"}`)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request = c.Request.WithContext(ctx)
				accepted := outcome == "text" || outcome == "usage-only" || outcome == "json-success-wins"
				if accepted {
					c.Writer = &affinityCommitWriter{ResponseWriter: c.Writer, t: t, store: store}
				}
				_, forwardErr := svc.Forward(ctx, c, selected.Account, body)
				if accepted {
					require.NoError(t, forwardErr)
					require.True(t, c.Writer.Written())
				} else {
					require.Error(t, forwardErr)
				}
				selected.ReleaseFunc()
				route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
				require.NoError(t, err)
				if accepted {
					require.EqualValues(t, 102, route.OwnerAccountID)
					require.EqualValues(t, 2, route.Generation)
				} else {
					require.EqualValues(t, 101, route.OwnerAccountID)
					require.EqualValues(t, 1, route.Generation)
				}
				require.Empty(t, route.LeaseID)
				require.Len(t, wire.requests, 1, "acceptance failure cannot automatically replay upstream")
			})
		}
	}
}

// A's permitted pre-output recovery is exhausted. A can remain globally
// schedulable after a transient transport failure. If B also fails before
// acceptance, C remains eligible within the handler's existing switch budget.
func TestOpenAISessionRouteRecoveryContinuesAcrossCandidates(t *testing.T) {
	for _, retryB := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-account-retry=%v", retryB), func(t *testing.T) {
			ctx := context.Background()
			store := newFakeOpenAISessionRouteStore()
			_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
			require.NoError(t, err)
			wire := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
			svc := newOpenAIRejectedFieldTestService(wire)
			accounts := make([]Account, 3)
			for i := range accounts {
				accounts[i] = *newOpenAIRejectedFieldTestAccount()
				accounts[i].ID, accounts[i].Priority = int64(101+i), i
			}
			svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.EqualValues(t, 101, selection.Account.ID)
			ctx = ContextWithSelectionProfitGate(ctx, selection)
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"complete root"}`)
			failAttempt := func(selection *AccountSelectionResult, attemptCtx context.Context) {
				wire.resp = newOpenAIRejectedFieldTestResponse(http.StatusBadGateway, `{"error":{"type":"server_error","message":"fixture failure"}}`)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request = c.Request.WithContext(attemptCtx)
				_, err := svc.Forward(attemptCtx, c, selection.Account, body)
				require.Error(t, err)
				require.False(t, c.Writer.Written(), "pre-output failures must not emit semantic bytes")
				selection.ReleaseFunc()
			}
			failAttempt(selection, ctx)
			ctx = WithOpenAISessionRecovery(ctx, 101, false)
			excluded := map[int64]struct{}{101: {}}
			attempts := 1
			if retryB {
				attempts++
			}
			for attempt := 0; attempt < attempts; attempt++ {
				selection, _, err = svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", excluded, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.EqualValues(t, 102, selection.Account.ID)
				ctx = ContextWithSelectionProfitGate(ctx, selection)
				require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, "session", 102))
				failAttempt(selection, ctx)
				ctx = WithOpenAISessionRecovery(ctx, 102, retryB && attempt == 0)
			}
			excluded[102] = struct{}{}
			selection, _, err = svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", excluded, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err, "B's failure must not discard A's exhausted-recovery authorization")
			defer selection.ReleaseFunc()
			require.EqualValues(t, 103, selection.Account.ID)
			ctx = ContextWithSelectionProfitGate(ctx, selection)
			wire.resp = codexLogicalIdentityAuditResponse()
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request = c.Request.WithContext(ctx)
			_, err = svc.Forward(ctx, c, selection.Account, body)
			require.NoError(t, err)
			require.Len(t, wire.requests, attempts+2)
			route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
			require.NoError(t, err)
			require.EqualValues(t, 103, route.OwnerAccountID)
			require.EqualValues(t, 2, route.Generation)
		})
	}
}

func TestOpenAISessionRouteActiveMigrationStillFencesConcurrentSelection(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "concurrent-request")))
	store := newFakeOpenAISessionRouteStore()
	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
	require.NoError(t, err)
	_, err = store.BeginMigration(ctx, "group:0", OpenAISessionRouteDigest("session"), 1, 102, "lease", "owner_unavailable", time.Minute)
	require.NoError(t, err)
	svc := &OpenAIGatewayService{sessionRouteStore: store}
	selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)
	require.Nil(t, selection)
	route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
	require.NoError(t, err)
	require.EqualValues(t, 101, route.OwnerAccountID)
	require.Equal(t, "lease", route.LeaseID)
	warnings := logs.FilterMessage("openai_session_route_failed").All()
	require.Len(t, warnings, 1)
	fields := warnings[0].ContextMap()
	require.Equal(t, "selection", fields["stage"])
	require.Equal(t, "migration_in_progress", fields["reason"])
	require.Equal(t, "concurrent-request", fields["request_id"])
	require.EqualValues(t, 101, fields["owner_account_id"])
	require.EqualValues(t, 102, fields["candidate_account_id"])
}

// Exhaustion evidence authorizes only the original logical session and
// generation. Reusing an excluded account ID is not sufficient authority.
func TestOpenAISessionRouteRecoveryRejectsUnrelatedOrStaleEvidence(t *testing.T) {
	for _, scenario := range []string{"no-recovery", "missing-attempt", "mismatched-account", "another-session", "another-group", "new-generation", "new-owner"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store := newFakeOpenAISessionRouteStore()
			original, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("original"), OwnerAccountID: 101})
			require.NoError(t, err)
			if scenario != "no-recovery" {
				if scenario != "missing-attempt" {
					accountID := int64(101)
					if scenario == "mismatched-account" {
						accountID = 102
					}
					ctx = context.WithValue(ctx, openAISessionRouteAttemptKey{}, &openAISessionRouteAttempt{store: store, route: original, accountID: accountID})
				}
				ctx = WithOpenAISessionRecovery(ctx, 101, false)
			}
			session := "original"
			var groupID *int64
			if scenario == "another-session" || scenario == "another-group" {
				domain := "group:0"
				if scenario == "another-session" {
					session = "unrelated"
				} else {
					id := int64(7)
					groupID, domain = &id, "group:7"
				}
				_, err = store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: domain, SessionDigest: OpenAISessionRouteDigest(session), OwnerAccountID: 101})
				require.NoError(t, err)
			}
			if scenario == "new-generation" || scenario == "new-owner" {
				store.mu.Lock()
				changed := original
				changed.Generation++
				if scenario == "new-owner" {
					changed.OwnerAccountID = 103
				}
				store.routes[fakeRouteKey(original.AuthzDomain, original.SessionDigest)] = changed
				store.mu.Unlock()
			}
			var acquired []int64
			svc := &OpenAIGatewayService{sessionRouteStore: store, cache: &schedulerTestGatewayCache{},
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
					{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1},
					{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1},
				}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})}
			selection, _, err := svc.SelectAccountWithScheduler(ctx, groupID, "", session, "gpt-6.1-sol", map[int64]struct{}{101: {}}, OpenAIUpstreamTransportAny, false)
			if selection != nil && selection.ReleaseFunc != nil {
				defer selection.ReleaseFunc()
			}
			if scenario == "new-generation" || scenario == "new-owner" {
				require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)
			} else {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
			}
			require.Nil(t, selection)
			require.Empty(t, acquired, "unrelated or stale recovery must stop before acquiring a candidate")
		})
	}
}

// A same-account retry must retain its selected route generation even while
// the canonical owner ID happens to be unchanged.
func TestOpenAISessionRouteSameAccountRetryRejectsUnrelatedOrStaleAttempt(t *testing.T) {
	for _, scenario := range []string{"missing-attempt", "another-session", "another-group", "new-generation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store := newFakeOpenAISessionRouteStore()
			route, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101, Generation: 2})
			require.NoError(t, err)
			if scenario != "missing-attempt" {
				previous := route
				switch scenario {
				case "another-session":
					previous.SessionDigest = OpenAISessionRouteDigest("other")
				case "another-group":
					previous.AuthzDomain = "group:7"
				case "new-generation":
					previous.Generation--
				}
				ctx = context.WithValue(ctx, openAISessionRouteAttemptKey{}, &openAISessionRouteAttempt{store: store, route: previous, accountID: 101, closed: true})
			}
			ctx = WithOpenAISessionRecovery(ctx, 101, true)
			var acquired []int64
			svc := &OpenAIGatewayService{sessionRouteStore: store, cache: &schedulerTestGatewayCache{},
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
					{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1},
				}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})}
			selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
			if selection != nil && selection.ReleaseFunc != nil {
				defer selection.ReleaseFunc()
			}
			require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)
			require.Nil(t, selection)
			require.Empty(t, acquired, "a stale retry must stop before account acquisition")
		})
	}
}
