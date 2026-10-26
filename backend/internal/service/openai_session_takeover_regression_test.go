//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// V05/V12/V13/V15: exercise scheduler -> store -> admission -> request builder
// -> HTTP transport -> semantic response commit, for all authentication pairs.
func TestCodexSessionTakeoverWireRegression(t *testing.T) {
	for _, from := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, to := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, pass := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-to-%s/pass=%v", from, to, pass), func(t *testing.T) {
					upstream := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
					svc := newOpenAIRejectedFieldTestService(upstream)
					makeAccount := func(id int64, kind string) Account {
						a := newOpenAIRejectedFieldTestAccount()
						if kind == AccountTypeOAuth {
							a = newOpenAIOAuthNamespaceTestAccount()
						}
						a.ID, a.Type = id, kind
						a.Status, a.Schedulable = StatusActive, true
						a.Credentials["access_token"], a.Credentials["api_key"] = fmt.Sprintf("token-%d", id), fmt.Sprintf("token-%d", id)
						a.Credentials["chatgpt_account_id"] = fmt.Sprintf("chatgpt-%d", id)
						a.Extra = map[string]any{"openai_passthrough": pass, "responses_supported": true}
						return *a
					}
					accounts := []Account{makeAccount(101, from), makeAccount(102, to)}
					accounts[0].Status, accounts[0].Schedulable = StatusError, false
					svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
					svc.cache = &schedulerTestGatewayCache{}
					svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{102: true}})
					store := newFakeOpenAISessionRouteStore()
					svc.sessionRouteStore = store
					ctx := context.Background()
					_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
					selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					require.EqualValues(t, 102, selection.Account.ID)
					ctx = ContextWithSelectionProfitGate(ctx, selection)
					require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, "session", 102))
					route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
					require.NoError(t, err)
					require.EqualValues(t, 101, route.OwnerAccountID, "admission/handshake is not takeover")
					body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello","prompt_cache_key":"session"}`)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request = c.Request.WithContext(ctx)
					_, err = svc.Forward(ctx, c, selection.Account, body)
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "Bearer token-102", upstream.lastReq.Header.Get("Authorization"))
					if to == AccountTypeOAuth {
						require.Equal(t, "chatgpt-102", upstream.lastReq.Header.Get("chatgpt-account-id"))
					} else {
						require.Empty(t, upstream.lastReq.Header.Get("chatgpt-account-id"))
					}
					route, err = store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
					require.NoError(t, err)
					require.EqualValues(t, 102, route.OwnerAccountID)
					require.EqualValues(t, 2, route.Generation)
					selection.ReleaseFunc()
					accounts[0].Status, accounts[0].Schedulable = StatusActive, true
					next, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					require.EqualValues(t, 102, next.Account.ID, "recovery of A cannot reclaim B's session")
					next.ReleaseFunc()
				})
			}
		}
	}
}

// A same-account recovery pins both the canonical owner and an uncommitted
// candidate. A closed failed Forward must not lose the original generation.
func TestCodexSameAccountRecoveryWireRegression(t *testing.T) {
	for _, migrating := range []bool{false, true} {
		t.Run(fmt.Sprintf("candidate=%v", migrating), func(t *testing.T) {
			ctx := context.Background()
			store := newFakeOpenAISessionRouteStore()
			_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
			require.NoError(t, err)
			a := *newOpenAIRejectedFieldTestAccount()
			a.ID, a.Status, a.Schedulable = 101, StatusActive, true
			b := a
			b.ID = 102
			if migrating {
				a.Status, a.Schedulable = StatusError, false
			}
			wire := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusBadGateway, `{"error":{"type":"server_error","message":"fixture failure"}}`)}
			svc := newOpenAIRejectedFieldTestService(wire)
			svc.httpUpstream = affinityContextTransport{wire}
			svc.sessionRouteStore, svc.accountRepo, svc.cache = store, schedulerTestOpenAIAccountRepo{accounts: []Account{a, b}}, &schedulerTestGatewayCache{}
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			attemptCtx := ContextWithSelectionProfitGate(ctx, selected)
			body := []byte(`{"model":"gpt-5.1","input":"complete root","stream":true}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request = c.Request.WithContext(attemptCtx)
			_, err = svc.Forward(attemptCtx, c, selected.Account, body)
			require.Error(t, err)
			selected.ReleaseFunc()
			require.NoError(t, c.Request.Context().Err(), "finished attempt must preserve the ingress context for recovery")
			retryCtx := WithOpenAISessionRecovery(c.Request.Context(), selected.Account.ID, true)
			retry, _, err := svc.SelectAccountWithScheduler(retryCtx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			defer retry.ReleaseFunc()
			require.Equal(t, selected.Account.ID, retry.Account.ID)
			wire.resp = codexLogicalIdentityAuditResponse()
			retryCtx = ContextWithSelectionProfitGate(retryCtx, retry)
			c.Request = c.Request.WithContext(retryCtx)
			_, err = svc.Forward(retryCtx, c, retry.Account, body)
			require.NoError(t, err)
			require.Len(t, wire.requests, 2)
			route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
			require.NoError(t, err)
			require.Equal(t, selected.Account.ID, route.OwnerAccountID)
		})
	}
}

func TestCodexSessionTakeoverFailureKeepsOwnerRegression(t *testing.T) {
	ctx := context.Background()
	for _, event := range []string{`{"type":"response.created"}`, `{"type":"response.failed","response":{"status":"failed"}}`, `{"type":"response.completed","response":{"output":[]}}`} {
		store := newFakeOpenAISessionRouteStore()
		route, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: "digest", OwnerAccountID: 101})
		require.NoError(t, err)
		a := &openAISessionRouteAttempt{store: store, route: route, accountID: 102, reason: "credential_invalid"}
		require.NoError(t, a.admit(ctx, 102))
		attemptCtx := context.WithValue(ctx, openAISessionRouteAttemptKey{}, a)
		require.NoError(t, commitOpenAISessionTakeoverEvent(attemptCtx, []byte(event)))
		a.close()
		route, err = store.Get(ctx, "group:0", "digest")
		require.NoError(t, err)
		require.EqualValues(t, 101, route.OwnerAccountID)
		require.Empty(t, route.LeaseID)
	}
}

func TestCodexSessionStaleAttemptCannotRebindRegression(t *testing.T) {
	ctx := context.Background()
	store := newFakeOpenAISessionRouteStore()
	route, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: "digest", OwnerAccountID: 101})
	require.NoError(t, err)
	stale := &openAISessionRouteAttempt{store: store, route: route, accountID: 102}
	_, err = store.BeginMigration(ctx, "group:0", "digest", 1, 103, "winner", "credential_invalid", time.Minute)
	require.NoError(t, err)
	_, err = store.CommitOwner(ctx, "group:0", "digest", "winner", 103)
	require.NoError(t, err)
	require.ErrorIs(t, stale.admit(ctx, 102), ErrOpenAISessionRouteLeaseLost)
	require.True(t, errors.Is(stale.commit(ctx), ErrOpenAISessionRouteLeaseLost))
}
