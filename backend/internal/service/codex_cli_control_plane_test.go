//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestCodexCLIBridgeModelsUsesCurrentOwnerFlagsForBothAccountTypes(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			owner := enabledCodexCLIExecutionAccount(801, "captured-access", "captured-workspace")
			token := "captured-access"
			if kind == AccountTypeAPIKey {
				owner = codexBridgeAPIKeyFixture()
				token = "acquired-api-key"
			}
			global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
			flag := &atomic.Bool{}
			rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{owner.ID: owner}, flags: map[int64]*atomic.Bool{owner.ID: flag}}
			writes, hops := 0, 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes++
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/custom/models?version=trusted", r.URL.RequestURI())
				require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", r.Header.Get("User-Agent"))
				require.Equal(t, "etag-owned", r.Header.Get("If-None-Match"))
				if kind == AccountTypeAPIKey {
					require.Empty(t, r.Header.Get("Chatgpt-Account-Id"))
				} else {
					require.Equal(t, "captured-workspace", r.Header.Get("Chatgpt-Account-Id"))
				}
				_, _ = io.WriteString(w, `{"models":[{"slug":"gpt-5.1","display_name":"Owned Model"}]}`)
			}))
			defer provider.Close()
			svc := &OpenAIGatewayService{accountRepo: rows, settingService: &SettingService{settingRepo: global}, httpUpstream: &codexModelsHTTPUpstreamStub{do: func(r *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				require.Equal(t, owner.ID, id)
				return http.DefaultClient.Do(r)
			}}, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
				hops++
				return forward(ctx, r)
			}}}
			ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, token)
			header := http.Header{"Authorization": {"Bearer " + token}, "User-Agent": {"codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"}}
			if kind == AccountTypeOAuth {
				header.Set("Chatgpt-Account-Id", "captured-workspace")
			}
			request := openAIModelsRequest{url: provider.URL + "/custom/models?version=trusted", headers: header, credentialAccount: owner, accountID: owner.ID, accountConcurrency: 1, useAPIKeyUpstream: kind == AccountTypeAPIKey}
			for step, state := range []struct{ global, account, bridge bool }{{false, true, false}, {true, true, true}, {true, false, false}, {true, true, true}, {false, true, false}} {
				global.enabled.Store(state.global)
				flag.Store(state.account)
				before := hops
				result, err := svc.fetchOpenAIModelsUpstream(ctx, request, "etag-owned")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, step+1, writes, "models requests must not replay")
				if state.bridge {
					require.Equal(t, before+1, hops)
				} else {
					require.Equal(t, before, hops)
				}
			}
		})
	}
}

func TestCodexCLIBridgeAPIKeyImageAndChatProbesCannotBypassHop(t *testing.T) {
	for _, path := range []string{"image", "chat"} {
		t.Run(path, func(t *testing.T) {
			owner := codexBridgeAPIKeyFixture()
			global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
			flag := &atomic.Bool{}
			rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{owner.ID: owner}, flags: map[int64]*atomic.Bool{owner.ID: flag}}
			writes, hops := 0, 0
			upstream := &codexModelsHTTPUpstreamStub{do: func(r *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				writes++
				require.Equal(t, owner.ID, id)
				require.Equal(t, "Bearer acquired-api-key", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", r.Header.Get("User-Agent"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), "owned prompt")
				if path == "image" {
					require.Equal(t, "https://compatible.example/v1/images/generations", r.URL.String())
					require.JSONEq(t, `{"model":"gpt-image-2","prompt":"owned prompt","n":1,"response_format":"b64_json"}`, string(body))
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`))}, nil
				}
				require.Equal(t, "https://compatible.example/v1/chat/completions", r.URL.String())
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"))}, nil
			}}
			gateway := &OpenAIGatewayService{accountRepo: rows, settingService: &SettingService{settingRepo: global}, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
				hops++
				return forward(ctx, r)
			}}}
			probe := &AccountTestService{accountRepo: rows, settingService: gateway.settingService, openaiGatewayService: gateway, httpUpstream: upstream, cfg: &config.Config{}}
			for step, state := range []struct{ global, account, bridge bool }{{false, true, false}, {true, true, true}, {true, false, false}, {true, true, true}, {false, true, false}} {
				global.enabled.Store(state.global)
				flag.Store(state.account)
				before := hops
				c, _ := newCodexCLIExecutionContext()
				var err error
				if path == "image" {
					err = probe.testOpenAIImageAPIKey(c, c.Request.Context(), owner, "gpt-image-2", "owned prompt")
				} else {
					err = probe.testOpenAIChatCompletionsConnection(c, owner, "custom-chat", "owned prompt", "https://compatible.example/v1", "acquired-api-key")
				}
				require.NoError(t, err)
				if state.bridge {
					require.Equal(t, before+1, hops)
				} else {
					require.Equal(t, before, hops)
				}
				require.Equal(t, step+1, writes, "probe transport selection must not replay")
			}
		})
	}
}

func TestCodexCLIBridgeQuotaClientPreservesPreparedWHAMRequestsAndHotSwitches(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(802, "captured-access", "captured-workspace")
	global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	flag := &atomic.Bool{}
	rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{owner.ID: owner}, flags: map[int64]*atomic.Bool{owner.ID: flag}}
	hops, writes := 0, 0
	gateway := &OpenAIGatewayService{accountRepo: rows, settingService: &SettingService{settingRepo: global}, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		hops++
		return forward(ctx, r)
	}}}
	native := req.C()
	native.Transport.WrapRoundTripFunc(func(_ http.RoundTripper) req.HttpRoundTripFunc {
		return func(r *http.Request) (*http.Response, error) {
			writes++
			require.Equal(t, "chatgpt.com", r.URL.Host)
			require.Equal(t, "Bearer captured-access", r.Header.Get("Authorization"))
			require.Equal(t, "captured-workspace", r.Header.Get("Chatgpt-Account-Id"))
			require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", r.Header.Get("User-Agent"))
			require.Empty(t, r.Header.Get("Originator"))
			require.Empty(t, r.Header.Get("Version"))
			if r.Method == http.MethodPost {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"redeem_request_id":"owned-reset"}`, string(body))
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
		}
	})
	quota := &OpenAIQuotaService{accountRepo: rows, openAIIdentityResolver: gateway}
	client := quota.codexCLIQuotaClient(native, owner)
	ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, "captured-access")
	for step, state := range []struct{ global, account, bridge bool }{{false, true, false}, {true, true, true}, {true, false, false}, {true, true, true}, {false, true, false}} {
		global.enabled.Store(state.global)
		flag.Store(state.account)
		before := hops
		for _, endpoint := range []string{chatGPTUsageURL, chatGPTRateLimitCreditsURL, chatGPTRateLimitResetURL} {
			request := client.R().SetContext(ctx).SetHeaders(map[string]string{"Authorization": "Bearer captured-access", "Chatgpt-Account-Id": "captured-workspace", "User-Agent": "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"})
			var err error
			if endpoint == chatGPTRateLimitResetURL {
				_, err = request.SetBody(map[string]string{"redeem_request_id": "owned-reset"}).Post(endpoint)
			} else {
				_, err = request.Get(endpoint)
			}
			require.NoError(t, err)
		}
		if state.bridge {
			require.Equal(t, before+3, hops)
		} else {
			require.Equal(t, before, hops)
		}
		require.Equal(t, (step+1)*3, writes)
	}
	// Wrapping one operation must not mutate a factory's shared native client.
	global.enabled.Store(true)
	flag.Store(true)
	before := hops
	_, err := native.R().SetContext(ctx).SetHeaders(map[string]string{"Authorization": "Bearer captured-access", "Chatgpt-Account-Id": "captured-workspace", "User-Agent": "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"}).Get(chatGPTUsageURL)
	require.NoError(t, err)
	require.Equal(t, before, hops)
}

func TestCodexCLIBridgeUsageProbeUsesLiveFlagsWithoutChangingPreparedInference(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(803, "captured-access", "captured-workspace")
	global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	flag := &atomic.Bool{}
	rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{owner.ID: owner}, flags: map[int64]*atomic.Bool{owner.ID: flag}}
	hops, writes := 0, 0
	gateway := &OpenAIGatewayService{accountRepo: rows, settingService: &SettingService{settingRepo: global}, codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		hops++
		return forward(ctx, r)
	}}}
	usage := &AccountUsageService{accountRepo: rows, openAIIdentityResolver: gateway}
	ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, "captured-access")
	for step, state := range []struct{ global, account, bridge bool }{{false, true, false}, {true, true, true}, {true, false, false}, {true, true, true}, {false, true, false}} {
		global.enabled.Store(state.global)
		flag.Store(state.account)
		before := hops
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, strings.NewReader(`{"model":"gpt-5.1","input":"usage probe","stream":true}`))
		require.NoError(t, err)
		r.Header = http.Header{"Authorization": {"Bearer captured-access"}, "Chatgpt-Account-Id": {"captured-workspace"}, "User-Agent": {"codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"}, "Originator": {"codex_cli_rs"}, "Version": {"0.165.0"}}
		response, err := usage.doOpenAICodexUsageProbe(r, owner, func(actual *http.Request) (*http.Response, error) {
			writes++
			require.Equal(t, chatgptCodexURL, actual.URL.String())
			require.Equal(t, r.Header, actual.Header)
			body, err := io.ReadAll(actual.Body)
			require.NoError(t, err)
			require.Equal(t, `{"model":"gpt-5.1","input":"usage probe","stream":true}`, string(body))
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("accepted")), Header: make(http.Header)}, nil
		})
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		if state.bridge {
			require.Equal(t, before+1, hops)
		} else {
			require.Equal(t, before, hops)
		}
		require.Equal(t, step+1, writes)
	}
}
