//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorIdentitySnapshotAcrossPingAndModels(t *testing.T) {
	swapMonitorHTTPClient(t)
	originalPing := monitorPingHTTPClient
	monitorPingHTTPClient = monitorHTTPClient
	t.Cleanup(func() { monitorPingHTTPClient = originalPing })
	for provider := range providerAdapters {
		t.Run(provider, func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			preset := "grok"
			switch provider {
			case "openai":
				preset = "codex"
			case "grok", "deepseek", "kimi", "stepfun":
				preset = provider
			case "zhipu":
				preset = "zcode"
			case "minimax":
				preset = "minimax_apikey"
			default:
				config.Defaults[provider+":apikey"] = "grok"
			}
			selection := func(version string) OutboundIdentitySelection {
				if preset == "stepfun" || preset == "minimax_apikey" {
					return OutboundIdentitySelection{Preset: preset}
				}
				return OutboundIdentitySelection{Preset: preset, Version: version}
			}
			if preset != "codex" {
				config.Profiles[preset] = selection("3.9.1")
			}
			svc, ctx := outboundIdentityTestSettings(t, config)
			if preset == "codex" {
				require.NoError(t, svc.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersion, "3.9.1"))
			}
			foreign := builtInOutboundIdentity("claude")
			foreign.AccountID = 88
			ctx = outboundidentity.WithIdentity(ctx, foreign)
			type capturedRequest struct {
				method   string
				protocol string
				headers  http.Header
			}
			captured := make(chan capturedRequest, 6)
			updateErrors := make(chan error, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- capturedRequest{r.Method, outboundidentity.RequestProtocol(r), r.Header.Clone()}
				if r.Method == http.MethodHead {
					config.Profiles[preset] = selection("3.9.2")
					if preset == "codex" {
						err := svc.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersion, "3.9.2")
						svc.InvalidateOpenAICodexClientVersionCache()
						updateErrors <- err
					} else {
						updateErrors <- svc.SetOutboundIdentitySettings(ctx, config)
					}
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"content":[{"type":"text","text":"ok"}],"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"output_text":"ok"}`))
			}))
			defer server.Close()
			monitor := &ChannelMonitor{Provider: provider, Endpoint: server.URL, APIKey: "test-key", PrimaryModel: "model-one", ExtraModels: []string{"model-two"},
				ExtraHeaders:     map[string]string{"uSeR-aGeNt": "untrusted/1.0", "oRiGiNaToR": "foreign", "vErSiOn": "999", "x-StAiNlEsS-pAcKaGe-VeRsIoN": "999", "x-grok-client-version": "999", "X-Custom": "preserved"},
				BodyOverrideMode: MonitorBodyOverrideModeReplace, BodyOverride: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}},
			}
			for _, version := range []string{"3.9.1", "3.9.2"} {
				results := (&ChannelMonitorService{}).runChecksConcurrent(ctx, monitor)
				require.NoError(t, <-updateErrors)
				require.Len(t, results, 2)
				for _, result := range results {
					require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
				}
				var expected outboundidentity.Identity
				if preset == "codex" {
					expected = outboundidentity.Identity{Preset: "codex", UserAgent: "codex_cli_rs/" + version + " (Ubuntu 24.04; x86_64) xterm-256color", Originator: "codex_cli_rs", Version: version,
						Headers: map[string]string{"Originator": "codex_cli_rs", "Version": version}}
				} else {
					var err error
					expected, err = buildOutboundIdentity(selection(version))
					require.NoError(t, err)
				}
				for range 3 {
					request := <-captured
					wire := expected.ForProtocol(request.protocol)
					require.Equal(t, wire.UserAgent, request.headers.Get("User-Agent"))
					for name, value := range wire.Headers {
						if name == "x-grok-client-mode" {
							continue
						}
						require.Equal(t, value, request.headers.Get(name), name)
					}
					if preset == "grok" {
						require.Equal(t, "headless", wire.Headers["x-grok-client-mode"], "the selected snapshot retains native CLI mode")
						require.Equal(t, "grok-shell/"+version+" (linux; x86_64)", request.headers.Get("User-Agent"))
						require.Equal(t, "grok-shell", request.headers.Get("X-Grok-Client-Identifier"))
						require.Equal(t, version, request.headers.Get("X-Grok-Client-Version"))
						require.Empty(t, request.headers.Get("X-Grok-Client-Mode"), "loopback monitor destinations do not receive native CLI mode")
					}
					if preset == "codex" {
						require.Equal(t, "codex_cli_rs", request.headers.Get("Originator"))
						require.Equal(t, version, request.headers.Get("Version"))
					} else {
						require.Empty(t, request.headers.Get("Originator"))
						require.Empty(t, request.headers.Get("Version"))
					}
					if request.method == http.MethodPost {
						require.Equal(t, "preserved", request.headers.Get("X-Custom"))
						for name, value := range providerAdapters[provider].buildHeaders("test-key") {
							require.Equal(t, value, request.headers.Get(name), name)
						}
					}
				}
			}
		})
	}
}

func TestChannelMonitorRejectsManagedIdentityExtraHeaders(t *testing.T) {
	for _, name := range []string{"User-Agent", "uSeR-aGeNt", "Originator", "Version", "x-app", "x-goog-api-client", "X-Stainless-Package-Version", "x-grok-client-version", "x-grok-client-identifier"} {
		require.ErrorIs(t, validateExtraHeaders(map[string]string{name: ""}), ErrChannelMonitorTemplateHeaderForbidden, name)
	}
	require.NoError(t, validateExtraHeaders(map[string]string{"Authorization": "Bearer test", "X-Custom": "ok"}))
}

func TestChannelMonitorDefaultIdentityForProviders(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, test := range []struct{ provider, preset, mode string }{
		{"openai", "codex", MonitorAPIModeChatCompletions}, {"openai", "codex", MonitorAPIModeResponses},
		{"anthropic", "claude", ""}, {"gemini", "gemini", ""}, {"grok", "grok", ""},
		// Channel-monitor egress shares the platform default preset, so the
		// Kimi, DeepSeek and MiniMax providers send the same pinned identity as
		// their platform accounts.
		{"kimi", "kimi", ""}, {"zhipu", "zcode", ""}, {"deepseek", "deepseek", ""}, {"minimax", "minimax_apikey", ""},
		{"opencode_go", "codex", ""}, {"stepfun", "stepfun", ""},
	} {
		t.Run(test.provider+"/"+test.mode, func(t *testing.T) {
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				_, _ = w.Write([]byte(`{"output_text":"ok"}`))
			}))
			defer server.Close()
			ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return outboundidentity.Identity{} })
			result, err := callProvider(ctx, test.provider, server.URL, "test-key", "test-model", "hello", &CheckOptions{APIMode: test.mode}, time.Now())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.StatusCode)
			headers := <-captured
			if test.mode == MonitorAPIModeResponses {
				require.Equal(t, "remote_compaction_v2", headers.Get("x-codex-beta-features"))
			} else {
				require.Empty(t, headers.Get("x-codex-beta-features"), "other endpoint protocols keep their own capability policy")
			}
			expected := builtInOutboundIdentity(test.preset).ForProtocol("chat_completions")
			require.Equal(t, expected.UserAgent, headers.Get("User-Agent"))
			for name, value := range expected.Headers {
				if name == "x-grok-client-mode" {
					continue
				}
				require.Equal(t, value, headers.Get(name), name)
			}
			if test.preset == "grok" {
				require.Equal(t, "headless", expected.Headers["x-grok-client-mode"], "the selected snapshot retains native CLI mode")
				require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", headers.Get("User-Agent"))
				require.Equal(t, "grok-shell", headers.Get("X-Grok-Client-Identifier"))
				require.Equal(t, "1.0.45", headers.Get("X-Grok-Client-Version"))
				require.Empty(t, headers.Get("X-Grok-Client-Mode"), "loopback monitor destinations do not receive native CLI mode")
			}
		})
	}
}

func TestChannelMonitorStreamingIdentityProtected(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, mode := range []string{MonitorAPIModeChatCompletions, MonitorAPIModeResponses} {
		captured := make(chan http.Header, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured <- r.Header.Clone()
			w.Header().Set("Content-Type", "text/event-stream")
			if mode == MonitorAPIModeResponses {
				_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"ok\"}}\n\n"))
			} else {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}
		}))
		ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return outboundidentity.Identity{} })
		result, err := callProvider(ctx, "openai", server.URL, "test-key", "test-model", "hi", &CheckOptions{APIMode: mode, StreamProbe: true, ExtraHeaders: map[string]string{"User-Agent": "untrusted/1.0", "Originator": "untrusted", "Version": "999", "Accept": "application/json"}}, time.Now())
		server.Close()
		require.NoError(t, err)
		require.Equal(t, "ok", result.Text)
		headers := <-captured
		require.Equal(t, builtInOutboundIdentity("codex").UserAgent, headers.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
		require.Equal(t, "0.164.0", headers.Get("Version"))
		if mode == MonitorAPIModeResponses {
			require.Equal(t, "remote_compaction_v2", headers.Get("x-codex-beta-features"))
		} else {
			require.Empty(t, headers.Get("x-codex-beta-features"))
		}
		require.Equal(t, "text/event-stream", headers.Get("Accept"))
		require.Equal(t, "Bearer test-key", headers.Get("Authorization"))
	}
}
