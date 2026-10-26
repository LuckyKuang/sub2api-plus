//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func invalidRawCodexIdentityCandidates() []struct{ name, raw string } {
	return []struct{ name, raw string }{
		{"leading LF", "\ncodex-tui/0.164.0 (Windows)"},
		{"trailing LF", "codex-tui/0.164.0 (Windows)\n"},
		{"leading CR", "\rcodex-tui/0.164.0 (Windows)"},
		{"trailing CRLF", "codex-tui/0.164.0 (Windows)\r\n"},
		{"NUL", "codex-tui/0.164.0 (Windows)\x00"},
		{"control-only", "\r\n"},
	}
}

// Invalid raw candidates must never become valid by stripping control bytes.
// This applies to account writes, global writes, and persisted legacy values.
func TestCodexRawIdentityCandidatesRejectedBeforeManagementWrites(t *testing.T) {
	for _, tc := range invalidRawCodexIdentityCandidates() {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				account := &Account{ID: 21, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{}}
				repo := &accountRepoStubForBulkUpdate{
					getByIDAccounts: map[int64]*Account{21: account}, getByIDsAccounts: []*Account{account},
				}
				svc := &adminServiceImpl{accountRepo: repo}
				_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
					Platform: PlatformOpenAI, Type: kind, SkipDefaultGroupBind: true,
					Credentials: map[string]any{"user_agent": tc.raw},
				})
				requireApplicationErrorReason(t, err, "OPENAI_CODEX_USER_AGENT_INVALID")
				require.Nil(t, repo.createAccount)
				_, err = svc.UpdateAccount(context.Background(), 21, &UpdateAccountInput{Credentials: map[string]any{"user_agent": tc.raw}})
				requireApplicationErrorReason(t, err, "OPENAI_CODEX_USER_AGENT_INVALID")
				require.Empty(t, repo.updatedAccounts)
				_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{21}, Credentials: map[string]any{"user_agent": tc.raw}})
				requireApplicationErrorReason(t, err, "OPENAI_CODEX_USER_AGENT_INVALID")
				require.Zero(t, repo.bulkUpdateCalls)
			})
		}
		t.Run(tc.name+"/global", func(t *testing.T) {
			repo := &authSourceDefaultsRepoStub{values: map[string]string{}}
			settings := &SettingService{settingRepo: repo, cfg: &config.Config{}}
			parsed := settings.parseSettings(map[string]string{SettingKeyOpenAICodexUserAgent: tc.raw})
			require.Equal(t, tc.raw, parsed.OpenAICodexUserAgent, "admin reads must not repair a persisted invalid candidate")
			err := settings.UpdateSettings(context.Background(), &SystemSettings{OpenAICodexUserAgent: tc.raw})
			requireApplicationErrorReason(t, err, "OPENAI_CODEX_USER_AGENT_INVALID")
			require.Empty(t, repo.updates)
		})
	}
	for _, raw := range []string{"", "   ", "\t", "  codex-tui/0.164.0 (Windows)  "} {
		_, err := NormalizeOpenAICodexUserAgent(raw)
		require.NoError(t, err, "ordinary surrounding spaces and explicit clears remain supported")
	}
}

func TestCodexRawIdentitySettingsRefreshPreservesSource(t *testing.T) {
	previousVersionBounds := versionBoundsCache.Load()
	previousBackendMode := backendModeCache.Load()
	previousForwarding := gatewayForwardingCache.Load()
	previousScheduler := openAIAdvancedSchedulerSettingCache.Load()
	previousThresholds := accountSchedulingThresholdsCache.Load()
	t.Cleanup(func() {
		if previousVersionBounds != nil {
			versionBoundsCache.Store(previousVersionBounds)
		} else {
			versionBoundsCache.Store(&cachedVersionBounds{})
		}
		if previousBackendMode != nil {
			backendModeCache.Store(previousBackendMode)
		} else {
			backendModeCache.Store(&cachedBackendMode{})
		}
		if previousForwarding != nil {
			gatewayForwardingCache.Store(previousForwarding)
		} else {
			gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{})
		}
		if previousScheduler != nil {
			openAIAdvancedSchedulerSettingCache.Store(previousScheduler)
		} else {
			openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{})
		}
		if previousThresholds != nil {
			accountSchedulingThresholdsCache.Store(previousThresholds)
		} else {
			accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
		}
	})
	for _, tc := range []struct{ raw, source string }{
		{"", "compiled_default"},
		{"codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", "global"},
		{"\ncodex-tui/0.164.0 (Windows)", "compiled_default"},
	} {
		settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}, cfg: &config.Config{}}
		settings.refreshCachedSettings(&SystemSettings{OpenAICodexUserAgent: tc.raw})
		raw, _ := settings.GetOpenAICodexOutboundProfile(context.Background())
		require.Equal(t, tc.raw, raw)
		identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), nil, settings)
		require.Equal(t, tc.source, identity.Source)
		require.Equal(t, "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", identity.UserAgent)
		require.Equal(t, "codex_cli_rs", identity.Originator)
		require.Equal(t, "0.164.0", identity.Version)
	}
}

func TestCodexRawIdentityCandidatesFallThroughWithCachedGlobalReads(t *testing.T) {
	const globalUA = "codex_vscode/0.164.0 (Ubuntu 24.04; x86_64) vscode"
	for _, tc := range invalidRawCodexIdentityCandidates() {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
				account := &Account{ID: 21, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{"user_agent": tc.raw}}
				settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexUserAgent: globalUA}}}
				for range 2 {
					identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, settings)
					require.Equal(t, "global", identity.Source)
					require.Equal(t, globalUA, identity.UserAgent)
					require.Equal(t, "codex_vscode", identity.Originator)
					require.Equal(t, "0.164.0", identity.Version)
				}
				settings = &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexUserAgent: tc.raw}}}
				for range 2 {
					raw, _ := settings.GetOpenAICodexOutboundProfile(context.Background())
					require.Equal(t, tc.raw, raw, "cache must retain the invalid raw candidate")
					identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, settings)
					require.Equal(t, "compiled_default", identity.Source)
					require.Equal(t, "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", identity.UserAgent)
					require.Equal(t, "codex_cli_rs", identity.Originator)
					require.Equal(t, "0.164.0", identity.Version)
				}
			}
		})
	}
}

func TestCodexRawIdentityFallbackHTTPAndWebSocketWire(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, tc := range []struct{ name, accountUA, globalUA, wantUA, wantOriginator string }{
				{"invalid account", "codex-tui/0.164.0 (Windows)\n", "codex_vscode/0.164.0 (Ubuntu 24.04; x86_64) vscode", "codex_vscode/0.164.0 (Ubuntu 24.04; x86_64) vscode", "codex_vscode"},
				{"invalid global", "", "\rcodex-tui/0.164.0 (Windows)", "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs"},
			} {
				t.Run(kind+"/passthrough="+map[bool]string{true: "on", false: "off"}[passthrough]+"/"+tc.name, func(t *testing.T) {
					captured := make(chan http.Header, 2)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captured <- r.Header.Clone()
						if websocket.IsWebSocketUpgrade(r) {
							conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
							if err == nil {
								_ = conn.Close()
							}
						}
					}))
					defer upstream.Close()
					settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexUserAgent: tc.globalUA}}}
					svc := &OpenAIGatewayService{cfg: codexTransportTestConfig(), settingService: settings}
					account := &Account{ID: 21, Platform: PlatformOpenAI, Type: kind, Concurrency: 1, Credentials: map[string]any{
						"api_key": "fixture", "access_token": "fixture", "base_url": upstream.URL, "user_agent": tc.accountUA,
					}}
					body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello","instructions":"test"}`)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					c.Request.Header.Set("User-Agent", "codex-tui/0.164.0 (Windows)")
					var req *http.Request
					var err error
					if passthrough {
						req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "fixture")
					} else {
						req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "fixture", true, "", true)
					}
					require.NoError(t, err)
					target, err := url.Parse(upstream.URL)
					require.NoError(t, err)
					req.URL.Scheme, req.URL.Host, req.Host = target.Scheme, target.Host, target.Host
					resp, err := http.DefaultClient.Do(prepareAccountOutboundRequest(req, account))
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					headers, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "fixture", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", "", "gpt-6.1-sol", "")
					require.NoError(t, err)
					conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(context.Background(), "ws"+strings.TrimPrefix(upstream.URL, "http"), headers, "")
					require.NoError(t, err)
					require.NoError(t, conn.Close())
					for range 2 {
						h := <-captured
						require.Equal(t, tc.wantUA, h.Get("User-Agent"))
						require.Equal(t, tc.wantOriginator, h.Get("Originator"))
						require.Equal(t, "0.164.0", h.Get("Version"))
						require.Equal(t, "Bearer fixture", h.Get("Authorization"))
					}
				})
			}
		}
	}
}
