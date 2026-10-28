//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/stretchr/testify/require"
)

func TestCreateLiveCallDisabledSidebandNeverCreatesProviderSession(t *testing.T) {
	for _, restriction := range []string{"global", "deployment", "force-http-deployment", "oauth-deployment", "account"} {
		t.Run(restriction, func(t *testing.T) {
			proxy, wire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_create_disabled")
			account := liveCreateBusinessAccount(653, 0, "disabled-live-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", proxy)
			if restriction == "account" {
				account.Extra = map[string]any{"openai_ws_force_http": true}
			}
			svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{account})
			switch restriction {
			case "global":
				svc.settingService.settingRepo = &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexWSEnabled: "false"}}
			case "deployment":
				svc.cfg.Gateway.OpenAIWS.Enabled = false
			case "force-http-deployment":
				svc.cfg.Gateway.OpenAIWS.ForceHTTP = true
			case "oauth-deployment":
				svc.cfg.Gateway.OpenAIWS.OAuthEnabled = false
			}
			created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 753, UserID: 754}, 3)
			require.Error(t, err, "a session without an allowed control transport cannot be created")
			require.Nil(t, created)
			require.Empty(t, upstream.sends, "a disabled sideband must reject before the irreversible provider create")
			require.Empty(t, wire.headers)
			require.Empty(t, store.saveAttempts)
			require.Empty(t, concurrency.held)
			require.Equal(t, concurrency.regularAcquired, concurrency.regularReleased)
			require.Zero(t, svc.liveCreateReservations.Load(), "rejection must return the local creation reservation")
			require.Zero(t, store.claims.Load())
			require.Zero(t, directTCP.Load())
		})
	}
}

func TestCreateLiveCallShadowOwnerDisabledSidebandNeverSends(t *testing.T) {
	ownerProxy, ownerWire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_create_owner_disabled")
	selectedProxy, selectedWire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_create_selected_route")
	owner := liveCreateBusinessAccount(683, 10, "disabled-parent-owner-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", ownerProxy)
	owner.Schedulable = false
	owner.Extra = map[string]any{"openai_ws_force_http": true}
	selected := liveCreateBusinessAccount(684, 0, "unused-shadow-local-bearer", "codex_vscode/0.180.0 (Ubuntu 24.04; x86_64) vscode", selectedProxy)
	selected.ParentAccountID = &owner.ID
	selected.Credentials = nil
	svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{selected, owner})
	created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 783, UserID: 784}, 3)
	require.Error(t, err, "selected-row WS permission cannot authorize a credential owner's disabled control transport")
	require.Nil(t, created)
	require.Empty(t, upstream.sends)
	require.Empty(t, ownerWire.headers)
	require.Empty(t, selectedWire.headers)
	require.Empty(t, store.saveAttempts)
	require.Equal(t, concurrency.regularAcquired, concurrency.regularReleased)
	require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
	require.Empty(t, concurrency.held)
	require.Zero(t, svc.liveCreateReservations.Load())
	require.Zero(t, directTCP.Load())
}

// A 201 plus its valid Location is independent evidence that the provider
// created a session. A broken answer body cannot authorize another creation or
// release execution capacity before compensating that known upstream session.
func TestCreateLiveCallCreatedSessionWithBrokenSDPCompensatesWithoutReplay(t *testing.T) {
	for _, failure := range []string{"truncated", "oversized"} {
		t.Run(failure, func(t *testing.T) {
			firstHeaders := make(chan http.Header, 1)
			firstProxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstHeaders <- r.Header.Clone()
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Location", "/backend-api/codex/call_broken_sdp")
				w.Header().Set("Content-Type", "application/sdp")
				if failure == "truncated" {
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, "v=partial\r\n")
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, strings.Repeat("s", 2*1024*1024+1))
			}))
			t.Cleanup(firstProxyServer.Close)
			_, parsedProxy, err := proxyurl.Parse(firstProxyServer.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(parsedProxy.Port())
			require.NoError(t, err)
			firstProxy := &Proxy{Protocol: "http", Host: parsedProxy.Hostname(), Port: port, Status: StatusActive}
			secondProxy, secondWire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_replay_broken_sdp")
			first := liveCreateBusinessAccount(663, 0, "broken-sdp-original-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", firstProxy)
			second := liveCreateBusinessAccount(664, 10, "must-not-use-second-owner", "codex_vscode/0.180.0 (Ubuntu 24.04; x86_64) vscode", secondProxy)
			svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{first, second})
			svc.cfg.Gateway.Live.MaxSessionDurationSeconds = 5
			concurrency.leaseReleaseNotify = make(chan struct{}, 1)
			var dials atomic.Int32
			allowRecovery := make(chan struct{})
			closed := &liveCreateCleanupConn{liveTestFrameConn: newLiveTestFrameConn(), ack: true}
			var sentHeaders http.Header
			svc.openaiWSPassthroughDialer = liveCreateCleanupDialer{dial: func(ctx context.Context, target string, headers http.Header, route string) (openAIWSClientConn, int, http.Header, error) {
				if target != "wss://chatgpt.com/backend-api/codex/call_broken_sdp" || route != firstProxy.URL() || headers.Get("Authorization") != "Bearer broken-sdp-original-bearer" {
					return nil, 0, nil, errors.New("cleanup did not retain the known call and original credential owner/proxy")
				}
				if dials.Add(1) == 1 {
					sentHeaders = headers.Clone()
					return nil, http.StatusBadGateway, nil, errors.New("cleanup initially unavailable")
				}
				select {
				case <-allowRecovery:
				case <-ctx.Done():
					return nil, 0, nil, ctx.Err()
				}
				return closed, http.StatusSwitchingProtocols, nil, nil
			}}
			created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 763, UserID: 764}, 3)
			require.Error(t, err)
			if failure == "truncated" {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.Contains(t, err.Error(), "too large")
			}
			require.Nil(t, created)
			require.Len(t, upstream.sends, 1, "an already-created session cannot fail over by replaying creation")
			require.Empty(t, secondWire.headers)
			require.Empty(t, store.saveAttempts, "an unusable SDP answer cannot become a usable call mapping")
			require.Zero(t, store.claims.Load())
			require.EqualValues(t, 1, svc.liveCreateReservations.Load(), "unconfirmed compensation must keep its bounded recovery reservation")
			concurrency.mu.Lock()
			require.Len(t, concurrency.held, 1)
			require.Empty(t, concurrency.leaseReleased, "body failure does not prove the provider session ended")
			concurrency.mu.Unlock()
			originalHeaders := <-firstHeaders
			for _, key := range []string{"Authorization", "Chatgpt-Account-Id", "User-Agent", "Originator", "Version", "session-id", "thread-id", "x-oai-attestation"} {
				require.NotEmpty(t, originalHeaders.Get(key), key)
				require.Equal(t, originalHeaders.Get(key), sentHeaders.Get(key), key)
			}
			close(allowRecovery)
			select {
			case <-concurrency.leaseReleaseNotify:
			case <-time.After(3 * time.Second):
				t.Fatal("known created session did not finish recovery after the cleanup transport returned")
			}
			require.Eventually(t, func() bool { return svc.liveCreateReservations.Load() == 0 }, time.Second, time.Millisecond)
			concurrency.mu.Lock()
			require.Empty(t, concurrency.held)
			require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
			concurrency.mu.Unlock()
			require.EqualValues(t, 2, dials.Load())
			require.Len(t, upstream.sends, 1)
			require.Zero(t, directTCP.Load())
		})
	}
}

func TestCreateLiveCallCreatedSessionWithoutUsableLocationRetainsBoundedCapacityWithoutReplay(t *testing.T) {
	for _, failure := range []struct{ name, location string }{
		{name: "missing"},
		{name: "malformed", location: "http://[invalid"},
		{name: "without-call-id", location: "/backend-api/codex/"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			firstProxy, firstWire := liveCreateBusinessProxy(t, http.StatusCreated, failure.location)
			secondProxy, secondWire := liveCreateBusinessProxy(t, http.StatusCreated, "/backend-api/codex/must_not_replay_unknown_live")
			first := liveCreateBusinessAccount(673, 0, "unlocatable-original-bearer", "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal", firstProxy)
			second := liveCreateBusinessAccount(674, 10, "must-not-use-second-owner", "codex_vscode/0.180.0 (Ubuntu 24.04; x86_64) vscode", secondProxy)
			svc, store, concurrency, upstream, directTCP := newLiveCreateBusinessService(t, []Account{first, second})
			svc.cfg.Gateway.Live.MaxSessionDurationSeconds = 1
			concurrency.leaseReleaseNotify = make(chan struct{}, 1)
			var dials atomic.Int32
			svc.openaiWSPassthroughDialer = liveCreateCleanupDialer{dial: func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
				dials.Add(1)
				return nil, 0, nil, errors.New("an unknown call ID cannot authorize an invented cleanup target")
			}}
			started := time.Now()
			created, err := svc.CreateLiveCall(context.Background(), liveCreateBusinessRequest(), LiveCallIdentity{APIKeyID: 773, UserID: 774}, 3)
			require.Error(t, err)
			require.Nil(t, created)
			require.Len(t, firstWire.headers, 1)
			require.Len(t, upstream.sends, 1, "a successful provider status cannot authorize another create even without a usable ID")
			require.Empty(t, secondWire.headers)
			require.Empty(t, store.saveAttempts, "the gateway cannot invent a usable mapping or official call ID")
			require.Zero(t, store.claims.Load())
			require.EqualValues(t, 1, svc.liveCreateReservations.Load())
			concurrency.mu.Lock()
			require.Len(t, concurrency.held, 1)
			require.Empty(t, concurrency.leaseReleased, "unlocatable does not mean the provider session ended")
			concurrency.mu.Unlock()
			select {
			case <-concurrency.leaseReleaseNotify:
				require.GreaterOrEqual(t, time.Since(started), time.Second, "capacity must remain owned through the configured lifetime")
			case <-time.After(2 * time.Second):
				t.Fatal("unlocatable session recovery exceeded its bounded lifetime")
			}
			require.Eventually(t, func() bool { return svc.liveCreateReservations.Load() == 0 }, time.Second, time.Millisecond)
			require.Zero(t, dials.Load(), "an unlocatable call must wait with paced lease renewal instead of retrying invalid dials")
			concurrency.mu.Lock()
			require.Empty(t, concurrency.held)
			require.Equal(t, concurrency.leaseAcquired, concurrency.leaseReleased)
			concurrency.mu.Unlock()
			require.Len(t, upstream.sends, 1)
			require.Zero(t, directTCP.Load())
		})
	}
}
