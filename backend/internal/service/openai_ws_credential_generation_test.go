//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

const wsCredentialRejectedPayload = `{"type":"response.failed","response":{"status":"failed","error":{"code":"token_revoked","message":"token revoked"}}}`

func newWSCredentialGenerationServer(t *testing.T) (string, <-chan http.Header, *atomic.Int32) {
	t.Helper()
	observed := make(chan http.Header, 16)
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		count.Add(1)
		observed <- req.Header.Clone()
		for {
			if _, _, err := conn.Read(req.Context()); err != nil {
				return
			}
			if err := conn.Write(req.Context(), websocket.MessageText, []byte(wsCredentialRejectedPayload)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses", observed, count
}

func wsCredentialGenerationAttempt(owner *Account, token string) context.Context {
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, token)
	return withRequestCredentialOwner(ctx, owner, token)
}

func wsCredentialGenerationRequest(selected *Account, target, token string) openAIWSAcquireRequest {
	return openAIWSAcquireRequest{Account: selected, WSURL: target, Headers: http.Header{
		"Authorization": {"Bearer " + token}, "User-Agent": {"codex_cli_rs/0.163.0"},
		"Originator": {"codex_cli_rs"}, "Version": {"0.163.0"},
	}}
}

func TestOpenAIWSCredentialGenerationSeparatesRealSockets(t *testing.T) {
	for _, scenario := range []struct {
		name                                             string
		sameToken, preferred, strict, pinned, atCapacity bool
	}{
		{name: "ordinary"},
		{name: "same-token-new-grant", sameToken: true},
		{name: "preferred", preferred: true},
		{name: "strict-preferred", preferred: true, strict: true},
		{name: "pinned-preferred", preferred: true, strict: true, pinned: true},
		{name: "at-capacity", atCapacity: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			target, observed, handshakes := newWSCredentialGenerationServer(t)
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
			if scenario.atCapacity {
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			old := newOpenAIAuthRecoveryTestAccount()
			old.Credentials["access_token"] = "socket-T0"
			oldCtx := wsCredentialGenerationAttempt(old, "socket-T0")
			oldReq := wsCredentialGenerationRequest(old, target, "socket-T0")
			lease, err := pool.Acquire(oldCtx, oldReq)
			require.NoError(t, err)
			oldConn := lease.ConnID()
			if scenario.pinned {
				require.True(t, pool.PinConn(old.ID, oldConn))
			}
			lease.Release()
			oldHeaders := <-observed
			require.Equal(t, "Bearer socket-T0", oldHeaders.Get("Authorization"))
			fresh := snapshotRequestCredentialAccount(old)
			freshToken := "socket-T1"
			if scenario.sameToken {
				freshToken = "socket-T0"
			}
			fresh.Credentials["access_token"], fresh.Credentials["refresh_token"] = freshToken, "new-grant-S1"
			freshCtx := wsCredentialGenerationAttempt(fresh, freshToken)
			// The public forwarder keeps its selection snapshot while acquisition
			// records the newer owner. Pool identity must follow that sent grant.
			req := wsCredentialGenerationRequest(old, target, freshToken)
			if scenario.preferred {
				req.PreferredConnID = oldConn
			}
			req.ForcePreferredConn = scenario.strict
			if scenario.strict {
				unavailable, acquireErr := pool.Acquire(freshCtx, req)
				require.ErrorIs(t, acquireErr, errOpenAIWSPreferredConnUnavailable)
				require.Nil(t, unavailable)
				require.Equal(t, int32(1), handshakes.Load())
				req.PreferredConnID, req.ForcePreferredConn = "", false
			}
			current, err := pool.Acquire(freshCtx, req)
			require.NoError(t, err)
			require.NotEqual(t, oldConn, current.ConnID())
			require.False(t, current.Reused())
			currentConn := current.ConnID()
			current.Release()
			freshHeaders := <-observed
			require.Equal(t, "Bearer "+freshToken, freshHeaders.Get("Authorization"))
			require.Equal(t, oldHeaders.Get("User-Agent"), freshHeaders.Get("User-Agent"))
			require.Equal(t, oldHeaders.Get("Originator"), freshHeaders.Get("Originator"))
			require.Equal(t, oldHeaders.Get("Version"), freshHeaders.Get("Version"))
			reused, err := pool.Acquire(freshCtx, wsCredentialGenerationRequest(old, target, freshToken))
			require.NoError(t, err)
			require.True(t, reused.Reused())
			require.Equal(t, currentConn, reused.ConnID(), "the same actual grant should retain session affinity")
			reused.Release()
			require.Equal(t, int32(2), handshakes.Load())
		})
	}
}

func TestOpenAIWSCredentialGenerationRejectsLatePrewarmAndReconnectsCurrentGrant(t *testing.T) {
	target, observed, handshakes := newWSCredentialGenerationServer(t)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 3
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	old := newOpenAIAuthRecoveryTestAccount()
	old.Credentials["access_token"] = "socket-T0"
	oldCtx := wsCredentialGenerationAttempt(old, "socket-T0")
	oldLease, err := pool.Acquire(oldCtx, wsCredentialGenerationRequest(old, target, "socket-T0"))
	require.NoError(t, err)
	ap, ok := pool.getAccountPool(old.ID)
	require.True(t, ok)
	ap.mu.Lock()
	remembered := cloneOpenAIWSAcquireRequest(*ap.lastAcquire)
	generation := ap.generation
	ap.creating++
	ap.mu.Unlock()
	fresh := snapshotRequestCredentialAccount(old)
	fresh.Credentials["access_token"], fresh.Credentials["refresh_token"] = "socket-T1", "new-grant-S1"
	freshCtx := wsCredentialGenerationAttempt(fresh, "socket-T1")
	freshReq := wsCredentialGenerationRequest(old, target, "socket-T1")
	current, err := pool.Acquire(freshCtx, freshReq)
	require.NoError(t, err)
	currentConn := current.ConnID()
	oldLease.MarkBroken()
	oldLease.Release()
	// Complete an old generation's real background handshake only after the
	// new grant has become the account's remembered prewarm target.
	pool.prewarmConns(old.ID, remembered, 1, generation)
	ap.mu.Lock()
	resident := len(ap.conns)
	_, hasCurrent := ap.conns[currentConn]
	ap.mu.Unlock()
	require.Equal(t, 1, resident)
	require.True(t, hasCurrent)
	current.Release()
	reused, err := pool.Acquire(freshCtx, freshReq)
	require.NoError(t, err)
	require.Equal(t, currentConn, reused.ConnID())
	require.True(t, reused.Reused())
	reused.MarkBroken()
	reused.Release()
	reconnected, err := pool.Acquire(freshCtx, freshReq)
	require.NoError(t, err)
	require.NotEqual(t, currentConn, reconnected.ConnID())
	reconnected.Release()
	for _, token := range []string{"socket-T0", "socket-T1", "socket-T0", "socket-T1"} {
		require.Equal(t, "Bearer "+token, (<-observed).Get("Authorization"))
	}
	require.Equal(t, int32(4), handshakes.Load())
}

func TestOpenAIWSCredentialGenerationPreservesAgentIdentitySignedReuse(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		name := "credential-owner"
		if shadow {
			name = "credential-shadow"
		}
		t.Run(name, func(t *testing.T) {
			target, observed, handshakes := newWSCredentialGenerationServer(t)
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			key, privateKey := newTestAgentIdentityKey(t)
			owner := &Account{ID: 641, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID,
				"agent_private_key": privateKey, "task_id": key.taskID,
			}}
			selected := owner
			if shadow {
				parentID := owner.ID
				selected = &Account{ID: 642, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
			}
			// Agent Identity acquires no bearer, so its per-attempt evidence is
			// deliberately empty. Per-dial signatures must retain normal reuse.
			ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), selected, "")
			firstAssertion, err := buildAgentAssertion(key, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			nextAssertion, err := buildAgentAssertion(key, time.Date(2026, 7, 14, 0, 0, 1, 0, time.UTC))
			require.NoError(t, err)
			require.NotEqual(t, firstAssertion, nextAssertion)
			factoryCalls := 0
			req := wsCredentialGenerationRequest(selected, target, "")
			req.Headers.Set("Authorization", firstAssertion)
			req.HeadersFactory = func(_ context.Context, headers http.Header) (http.Header, error) {
				factoryCalls++
				assertion := firstAssertion
				if factoryCalls > 1 {
					assertion = nextAssertion
				}
				headers.Set("Authorization", assertion)
				return headers, nil
			}
			first, err := pool.Acquire(ctx, req)
			require.NoError(t, err)
			firstConn := first.ConnID()
			first.Release()
			firstHeaders := <-observed
			require.Equal(t, firstAssertion, firstHeaders.Get("Authorization"))
			require.Equal(t, "codex_cli_rs/0.163.0", firstHeaders.Get("User-Agent"))
			require.Equal(t, "codex_cli_rs", firstHeaders.Get("Originator"))
			require.Equal(t, "0.163.0", firstHeaders.Get("Version"))
			req.Headers.Set("Authorization", nextAssertion)
			reused, err := pool.Acquire(ctx, req)
			require.NoError(t, err)
			require.Equal(t, firstConn, reused.ConnID())
			require.True(t, reused.Reused())
			require.Equal(t, 1, factoryCalls)
			reused.MarkBroken()
			reused.Release()
			reconnected, err := pool.Acquire(ctx, req)
			require.NoError(t, err)
			require.NotEqual(t, firstConn, reconnected.ConnID())
			reconnected.Release()
			require.Equal(t, nextAssertion, (<-observed).Get("Authorization"))
			require.Equal(t, 2, factoryCalls)
			require.Equal(t, int32(2), handshakes.Load())
		})
	}
}

func TestOpenAIWSCredentialGenerationLate401CannotQuarantineReplacementGrant(t *testing.T) {
	target, _, _ := newWSCredentialGenerationServer(t)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	old := newOpenAIAuthRecoveryTestAccount()
	old.Credentials["access_token"] = "socket-T0"
	oldCtx := wsCredentialGenerationAttempt(old, "socket-T0")
	oldLease, err := pool.Acquire(oldCtx, wsCredentialGenerationRequest(old, target, "socket-T0"))
	require.NoError(t, err)
	defer oldLease.Release()
	fresh := snapshotRequestCredentialAccount(old)
	fresh.Credentials["access_token"], fresh.Credentials["refresh_token"] = "socket-T1", "new-grant-S1"
	freshCtx := wsCredentialGenerationAttempt(fresh, "socket-T1")
	newLease, err := pool.Acquire(freshCtx, wsCredentialGenerationRequest(old, target, "socket-T1"))
	require.NoError(t, err)
	defer newLease.Release()
	require.NotEqual(t, oldLease.ConnID(), newLease.ConnID())
	repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(fresh)}
	blocker := &runtimeBlockRecorder{}
	rl := NewRateLimitService(repo, nil, cfg, nil, nil)
	rl.SetAccountRuntimeBlocker(blocker)
	svc := &OpenAIGatewayService{rateLimitService: rl, accountRepo: repo}
	for index, attempt := range []struct {
		ctx   context.Context
		lease *openAIWSConnLease
	}{{oldCtx, oldLease}, {freshCtx, newLease}} {
		require.NoError(t, attempt.lease.WriteJSONWithContextTimeout(attempt.ctx, map[string]any{"type": "response.create"}, time.Second))
		payload, readErr := attempt.lease.ReadMessageWithContextTimeout(attempt.ctx, time.Second)
		require.NoError(t, readErr)
		require.Equal(t, wsCredentialRejectedPayload, string(payload))
		require.True(t, svc.handleOpenAIWSFailureAccountSideEffects(attempt.ctx, old, "gpt-5.6-sol", nil, payload))
		if index == 0 {
			require.Equal(t, StatusActive, repo.current.Status)
			require.Empty(t, blocker.accounts)
			require.Equal(t, "socket-T0", repo.attempted.GetOpenAIAccessToken())
		} else {
			require.Equal(t, StatusError, repo.current.Status)
			require.Len(t, blocker.accounts, 1)
			require.Equal(t, "socket-T1", repo.attempted.GetOpenAIAccessToken())
		}
	}
}
