//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Requirements: a shadow route must obey its acquired credential owner's CLI
// opt-in; no native WebSocket may bypass that owner's selected transport.
func TestCodexCLIBridgeShadowWSUsesAcquiredOwner(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(901, "owner-access", "owner-workspace")
	parent := owner.ID
	shadow := &Account{ID: 902, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, "owner-access")
	ctx = withRequestCredentialOwner(ctx, shadow, "owner-access")
	svc := &OpenAIGatewayService{settingService: enabledCodexCLIBridgeTestSettings()}
	c, _ := newCodexCLIExecutionContext()
	require.False(t, svc.codexUpstreamWSAllowed(ctx, shadow))
	decision := svc.codexHTTPWSDecision(ctx, c, shadow, []byte(`{"model":"gpt-5.1","stream":true,"input":"full history"}`))
	require.Equal(t, OpenAIUpstreamTransportHTTPSSE, decision.Transport)
	require.Equal(t, "official_cli_bridge", decision.Reason)
}

// Requirements: the existing WS adapter must preserve deltas, tool continuation,
// usage and the authoritative session owner while each enabled turn traverses
// the official CLI. Turning the global hop off affects the next send without
// changing that owner. A new owner must never inherit another owner's bearer,
// workspace, proxy, identity, or opaque turn state. Expected wire values below
// are independent business fixtures, not derived from the adapter output.
func TestCodexCLIBridgeWebSocketTurnsPreserveSessionAndOwnerIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingsRepo := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	settingsRepo.enabled.Store(true)
	liveAccounts := &codexBridgeHotAccountRepo{owners: make(map[int64]*Account), flags: make(map[int64]*atomic.Bool)}
	settings := &SettingService{settingRepo: settingsRepo}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 5
	store := newFakeOpenAISessionRouteStore()
	var bridgeOwners []int64
	var sentOwners []int64
	var requests []*http.Request
	var bodies [][]byte
	var proxies []string
	var upstreamTurn int
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, ownerID int64, _ int) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		upstreamTurn++
		sentOwners = append(sentOwners, ownerID)
		requests = append(requests, req.Clone(req.Context()))
		bodies = append(bodies, append([]byte(nil), body...))
		proxies = append(proxies, proxy)
		id := fmt.Sprintf("resp_owner_%d_turn_%d", ownerID, upstreamTurn)
		sse := fmt.Sprintf("data: {\"type\":\"response.output_text.delta\",\"delta\":\"owner-%d\"}\n\n", ownerID)
		output := `[]`
		if upstreamTurn == 1 {
			output = `[{"type":"function_call","id":"fc_owner_one","call_id":"call_owner_one","name":"shell","arguments":"{}"}]`
		}
		sse += fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"model\":\"gpt-5.1\",\"output\":%s,\"usage\":{\"input_tokens\":9,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":3}}}}\n\n", id, output)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Codex-Turn-State": {fmt.Sprintf("opaque-owner-%d", ownerID)}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
	}}
	svc := &OpenAIGatewayService{accountRepo: liveAccounts, cfg: cfg, settingService: settings, httpUpstream: upstream, cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector(), sessionRouteStore: store,
		codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
			// The trusted workspace is an independent fixture for the owner; no
			// inbound session field chooses this credential or destination.
			if req.Header.Get("Chatgpt-Account-Id") == "workspace-one" {
				bridgeOwners = append(bridgeOwners, 101)
			} else {
				bridgeOwners = append(bridgeOwners, 202)
			}
			return forward(ctx, req)
		}},
	}
	for _, fixture := range []struct {
		id                                int64
		access, workspace, terminal, host string
		turns                             int
	}{
		{101, "access-one", "workspace-one", "screen-256color", "proxy-one.invalid", 6},
		{202, "access-two", "workspace-two", "xterm-256color", "proxy-two.invalid", 1},
	} {
		settingsRepo.enabled.Store(true)
		account := enabledCodexCLIExecutionAccount(fixture.id, fixture.access, fixture.workspace)
		account.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) " + fixture.terminal
		account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
		account.Extra = map[string]any{"responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool}
		proxyID := fixture.id + 1000
		account.ProxyID = &proxyID
		account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: fixture.host, Port: 8080, Status: StatusActive}
		liveAccounts.owners[fixture.id] = snapshotRequestCredentialAccount(account)
		liveAccounts.flags[fixture.id] = &atomic.Bool{}
		liveAccounts.flags[fixture.id].Store(true)
		digest := OpenAISessionRouteDigest("bridge-shared-session")
		route, err := store.CreateIfAbsent(context.Background(), OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: digest, OwnerAccountID: fixture.id})
		require.NoError(t, err)
		type outcome struct {
			turn   int
			result *OpenAIForwardResult
			err    error
		}
		outcomes := make(chan outcome, 6)
		errorsCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				errorsCh <- err
				return
			}
			defer conn.CloseNow()
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			_, first, err := conn.Read(ctx)
			if err != nil {
				errorsCh <- err
				return
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = r.Clone(ctx)
			c.Request.Header.Set("User-Agent", "untrusted-client/1")
			c.Request.Header.Set("session_id", "bridge-shared-session")
			ctx = context.WithValue(ctx, openAISessionRouteAttemptKey{}, &openAISessionRouteAttempt{store: store, route: route, accountID: fixture.id})
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(turn int, result *OpenAIForwardResult, err error) { outcomes <- outcome{turn, result, err} }}
			errorsCh <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, fixture.access, first, hooks)
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		defer client.CloseNow()
		for turn := 1; turn <= fixture.turns; turn++ {
			switch turn {
			case 3:
				settingsRepo.enabled.Store(false)
			case 4:
				settingsRepo.enabled.Store(true)
			case 5:
				liveAccounts.flags[fixture.id].Store(false)
			case 6:
				liveAccounts.flags[fixture.id].Store(true)
			}
			payload := `{"type":"response.create","model":"gpt-5.1","stream":true,"input":"first"}`
			if fixture.id == 101 && turn == 2 {
				payload = `{"type":"response.create","model":"gpt-5.1","stream":true,"previous_response_id":"resp_owner_101_turn_1","input":[{"type":"function_call_output","call_id":"call_owner_one","output":"tool-ok"}]}`
			}
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
			var events [][]byte
			for {
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				events = append(events, append([]byte(nil), event...))
				if gjson.GetBytes(event, "type").String() == "response.completed" {
					break
				}
			}
			require.Equal(t, "response.output_text.delta", gjson.GetBytes(events[0], "type").String())
			require.Equal(t, fmt.Sprintf("owner-%d", fixture.id), gjson.GetBytes(events[0], "delta").String())
			if fixture.id == 101 && turn == 1 {
				require.Equal(t, "call_owner_one", gjson.GetBytes(events[len(events)-1], "response.output.0.call_id").String())
			}
			select {
			case got := <-outcomes:
				require.NoError(t, got.err)
				require.Equal(t, turn, got.turn)
				require.Equal(t, 9, got.result.Usage.InputTokens)
				require.Equal(t, 2, got.result.Usage.OutputTokens)
				require.Equal(t, 3, got.result.Usage.CacheReadInputTokens)
			case <-ctx.Done():
				t.Fatal("turn did not complete")
			}
			if fixture.id == 101 {
				require.Len(t, bridgeOwners, []int{1, 2, 2, 3, 3, 4}[turn-1], "each next turn must follow the independently specified switch transition")
				require.Len(t, sentOwners, turn, "switching transports must never replay a submitted turn")
			}
		}
		require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
		select {
		case err := <-errorsCh:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("proxy did not finish")
		}
		cancel()
		server.Close()
		current, err := store.Get(context.Background(), "group:0", digest)
		require.NoError(t, err)
		require.Equal(t, fixture.id, current.OwnerAccountID)
		if fixture.id == 101 {
			require.Equal(t, route.Generation, current.Generation, "transport switches do not migrate the existing session owner")
		} else {
			require.Equal(t, route.Generation+1, current.Generation, "the explicitly selected new owner commits only through the existing acceptance lifecycle")
		}
	}
	require.Equal(t, []int64{101, 101, 101, 101, 202}, bridgeOwners, "global and account toggles both directions take effect on the next turn")
	require.Equal(t, []int64{101, 101, 101, 101, 101, 101, 202}, sentOwners)
	for i := range requests {
		wantAccess, wantWorkspace, wantTerminal, wantProxy := "access-one", "workspace-one", "screen-256color", "http://proxy-one.invalid:8080"
		if i == 6 {
			wantAccess, wantWorkspace, wantTerminal, wantProxy = "access-two", "workspace-two", "xterm-256color", "http://proxy-two.invalid:8080"
		}
		require.Equal(t, "Bearer "+wantAccess, requests[i].Header.Get("Authorization"))
		require.Equal(t, wantWorkspace, requests[i].Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) "+wantTerminal, requests[i].Header.Get("User-Agent"))
		require.Equal(t, wantProxy, proxies[i])
	}
	require.Empty(t, requests[0].Header.Get(openAIWSTurnStateHeader))
	require.Equal(t, "opaque-owner-101", requests[1].Header.Get(openAIWSTurnStateHeader))
	require.Empty(t, requests[6].Header.Get(openAIWSTurnStateHeader), "another owner's opaque turn state cannot cross an account boundary")
	require.False(t, gjson.GetBytes(bodies[1], "previous_response_id").Exists())
	items := gjson.GetBytes(bodies[1], "input").Array()
	require.Len(t, items, 3)
	require.Equal(t, "first", items[0].String())
	require.Equal(t, "function_call", items[1].Get("type").String())
	require.Equal(t, "call_owner_one", items[1].Get("call_id").String())
	require.Equal(t, "function_call_output", items[2].Get("type").String())
	require.Equal(t, "tool-ok", items[2].Get("output").String())
}
