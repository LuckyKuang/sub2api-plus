//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIIdentityContractDuplicateHeaders(t *testing.T) {
	identity := resolveOpenAIOutboundIdentityCandidates(testOpenAIAccountUserAgent, "")
	for _, oauth := range []bool{true, false} {
		t.Run(map[bool]string{true: "oauth", false: "apikey"}[oauth], func(t *testing.T) {
			headers := http.Header{
				"User-Agent": {"caller/1"}, "user-agent": {"caller/2"}, "USER-AGENT": {"caller/3"},
				"Originator": {"caller"}, "originator": {"caller"}, "ORIGINATOR": {"caller"},
				"Version": {"0.1.0"}, "version": {"0.2.0"}, "VERSION": {"0.3.0"},
				"Authorization": {"Bearer retained"}, "OpenAI-Beta": {"responses=experimental"},
			}
			for _, name := range managedIdentityOverrideTestNames {
				headers[strings.ToUpper(name)] = []string{"foreign-sdk/999.0.0"}
			}
			applyResolvedOpenAIOutboundIdentity(headers, identity, oauth)
			want := map[string]string{"User-Agent": identity.UserAgent}
			if oauth {
				want["Originator"] = identity.Originator
				want["Version"] = identity.Version
			}
			for name, value := range want {
				require.Equal(t, value, headers.Get(name))
			}
			for name, values := range headers {
				for _, reserved := range []string{"User-Agent", "Originator", "Version"} {
					if strings.EqualFold(name, reserved) {
						require.Equal(t, reserved, name, "remove noncanonical duplicate keys")
						require.Equal(t, []string{want[reserved]}, values)
					}
				}
			}
			if !oauth {
				require.NotContains(t, headers, "Originator")
				require.NotContains(t, headers, "Version")
			}
			for name := range headers {
				if outboundidentity.IsIdentityHeader(name) {
					require.Contains(t, want, name, "strip foreign SDK and client declarations")
				}
			}
			require.Equal(t, "Bearer retained", headers.Get("Authorization"))
			require.Equal(t, []string{"responses=experimental"}, headers["OpenAI-Beta"])
		})
	}
}

func TestOpenAIIdentityContractOversizedCandidateFallsThrough(t *testing.T) {
	oversized := testOpenAIAccountUserAgent + strings.Repeat("x", maxOpenAIAccountUserAgentLength)
	_, err := NormalizeOpenAICodexUserAgent(oversized)
	require.Error(t, err)
	identity := resolveOpenAIOutboundIdentityCandidates(oversized, testCodexCLIUserAgent)
	require.Equal(t, openAIOutboundIdentitySourceGlobal, identity.Source)
	require.Equal(t, testCodexCLIUserAgent, identity.UserAgent)
	identity = resolveOpenAIOutboundIdentityCandidates("", oversized)
	require.Equal(t, openAIOutboundIdentitySourceDefault, identity.Source)
	require.Equal(t, DefaultOpenAICodexUserAgent, identity.UserAgent)
}

func TestOpenAIIdentityContractPrivacyPreservesSelectedIdentity(t *testing.T) {
	for _, source := range []string{"account", "global"} {
		for _, client := range []string{"codex_cli_rs", "codex_app", "codex_exec", "codex_sdk_ts", "codex_vscode_copilot"} {
			t.Run(source+"/"+client, func(t *testing.T) {
				ua := client + "/0.200.1 (Ubuntu 22.4.0; x86_64) terminal"
				accountUA, globalUA := ua, ""
				if source == "global" {
					accountUA, globalUA = "", ua
				}
				identity := resolveOpenAIOutboundIdentityWithVersionAndCompatibility(accountUA, globalUA, "0.200.1", true)
				require.Equal(t, source, identity.Source)
				require.Equal(t, identity, normalizeOpenAIPrivacyIdentity(identity))
			})
		}
	}
}

func TestOpenAIIdentityContractPrivacyPreservesSynchronizedVersionForms(t *testing.T) {
	base := "codex_cli_rs/0.147.0 "
	maximumUA := base + strings.Repeat("x", maxOpenAIAccountUserAgentLength-len(base))
	for _, version := range []string{"0.200", "0.2000.1"} {
		t.Run(version, func(t *testing.T) {
			identity := resolveOpenAIOutboundIdentityWithVersion("", maximumUA, version)
			require.Equal(t, openAIOutboundIdentitySourceGlobal, identity.Source)
			require.Equal(t, version, identity.Version)
			require.Equal(t, identity, normalizeOpenAIPrivacyIdentity(identity))
		})
	}
}

func TestOpenAIIdentityContractBulkCodexUserAgentValidation(t *testing.T) {
	for _, raw := range []any{"curl/8.0", "codex_cli_rs/invalid", strings.Repeat("x", 513), 42, "codex_exec/0.200.1"} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		}}
		_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Credentials: map[string]any{"user_agent": raw}})
		requireApplicationErrorReason(t, err, "OPENAI_CODEX_USER_AGENT_INVALID")
		require.Zero(t, repo.bulkUpdateCalls)
	}
	for _, tc := range []struct{ raw, want any }{
		{"  " + testOpenAIAccountUserAgent + "  ", testOpenAIAccountUserAgent},
		{nil, nil}, {"   ", nil},
	} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
		_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{2}, Credentials: map[string]any{"user_agent": tc.raw, "retained": "value"}})
		require.NoError(t, err)
		require.Equal(t, tc.want, repo.lastBulkUpdate.Credentials["user_agent"])
		require.Contains(t, repo.lastBulkUpdate.Credentials, "user_agent")
		require.Equal(t, "value", repo.lastBulkUpdate.Credentials["retained"])
	}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	svc := &adminServiceImpl{accountRepo: repo, settingService: &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyCodexLegacyClientProfileCompatibilityEnabled: "true"}}}}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{2}, Credentials: map[string]any{"user_agent": "codex_exec/0.200.1"}})
	require.NoError(t, err)
	require.Equal(t, "codex_exec/0.200.1", repo.lastBulkUpdate.Credentials["user_agent"])
}

func TestOpenAIIdentityContractHTTPRetrySnapshot(t *testing.T) {
	const globalUA = "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode"
	const legacyUA = "codex_exec/0.180.0 (Ubuntu 22.4.0; x86_64) terminal"
	candidates := []struct {
		name, accountUA, globalUA, selectedUA, originator string
		legacy                                            bool
	}{
		{"account", testOpenAIAccountUserAgent, globalUA, testOpenAIAccountUserAgent, "codex_cli_rs", false},
		{"global", "", globalUA, globalUA, "codex_vscode", false},
		{"invalid-account", "curl/8.0", globalUA, globalUA, "codex_vscode", false},
		{"compiled-default", "invalid", "invalid", DefaultOpenAICodexUserAgent, "codex-tui", false},
		{"legacy-disabled", legacyUA, globalUA, globalUA, "codex_vscode", false},
		{"legacy-enabled", legacyUA, globalUA, legacyUA, "codex_exec", true},
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, forceCLI := range []bool{false, true} {
				for _, candidate := range candidates {
					t.Run(fmt.Sprintf("%s/passthrough=%t/force-cli=%t/%s", accountType, passthrough, forceCLI, candidate.name), func(t *testing.T) {
						body := []byte(`{"model":"gpt-5.5","stream":true,"instructions":"test","input":[{"type":"message","role":"user","status":"completed","content":"hello"}]}`)
						repo := &openAIIdentitySettingRepoStub{values: map[string]string{
							SettingKeyOpenAICodexClientVersion:                     "0.200.1",
							SettingKeyOpenAICodexUserAgent:                         candidate.globalUA,
							SettingKeyCodexLegacyClientProfileCompatibilityEnabled: strconv.FormatBool(candidate.legacy),
						}}
						settings := &SettingService{settingRepo: repo}
						var headers []http.Header
						upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							headers = append(headers, req.Header.Clone())
							if len(headers) == 1 {
								repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
								settings.InvalidateOpenAICodexClientVersionCache()
								return newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].status'.","param":"input[0].status"}}`), nil
							}
							return openAIIdentityContractSuccessResponse(), nil
						}}
						svc := newOpenAIRejectedFieldTestService(nil)
						svc.httpUpstream, svc.settingService = upstream, settings
						svc.cfg.Gateway.ForceCodexCLI = forceCLI
						account := newOpenAIOAuthNamespaceTestAccount()
						wantAuth := "Bearer oauth-token"
						if accountType == AccountTypeAPIKey {
							account = newOpenAIRejectedFieldTestAccount()
							wantAuth = "Bearer sk-test"
						}
						if account.Extra == nil {
							account.Extra = map[string]any{}
						}
						account.Extra["openai_passthrough"] = passthrough
						account.Credentials["user_agent"] = candidate.accountUA
						overrides := map[string]any{"X-Route": "retained"}
						for _, name := range managedIdentityOverrideTestNames {
							overrides[strings.ToLower(name)] = "injected/999.0.0"
						}
						account.Credentials[credKeyHeaderOverrideEnabled] = true
						account.Credentials[credKeyHeaderOverrides] = overrides
						c := newOpenAIIdentityContractContaminatedContext(body)
						_, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err)
						require.Len(t, headers, 2, "exercise the real rejected-field retry")
						// Build expected declarations independently of the resolver under test.
						slash := strings.IndexByte(candidate.selectedUA, '/')
						end := strings.IndexByte(candidate.selectedUA[slash+1:], ' ')
						suffix := ""
						if end >= 0 {
							suffix = candidate.selectedUA[slash+1+end:]
						}
						wantUA := candidate.originator + "/0.200.1" + suffix
						want := http.Header{"User-Agent": {wantUA}}
						if accountType == AccountTypeOAuth {
							want.Set("Originator", candidate.originator)
							want.Set("Version", "0.200.1")
						}
						for _, header := range headers {
							requireOpenAIIdentityContractHeaders(t, header, want, wantAuth)
							if accountType == AccountTypeAPIKey {
								require.Equal(t, "retained", getHeaderRaw(header, "x-route"))
							}
						}
						// The HTTP switch and request classification must also leave the
						// WS identity contract and the retained request scope intact.
						wsHeaders, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, strings.TrimPrefix(wantAuth, "Bearer "), OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, forceCLI, "", "", "", "gpt-5.5", "")
						require.NoError(t, err)
						requireOpenAIIdentityContractHeaders(t, wsHeaders, want, wantAuth)
						require.Equal(t, openAIWSBetaV2Value, wsHeaders.Get("OpenAI-Beta"))

						account.Credentials[credKeyHeaderOverrideEnabled] = false
						_, err = svc.Forward(context.Background(), newOpenAIIdentityContractContaminatedContext(body), account, body)
						require.NoError(t, err)
						require.Len(t, headers, 3)
						want.Set("User-Agent", strings.Replace(wantUA, "/0.200.1", "/0.200.2", 1))
						if accountType == AccountTypeOAuth {
							want.Set("Version", "0.200.2")
						}
						requireOpenAIIdentityContractHeaders(t, headers[2], want, wantAuth)
					})
				}
			}
		}
	}
}

func newOpenAIIdentityContractContaminatedContext(body []byte) *gin.Context {
	c := newOpenAIRejectedFieldTestContext(body)
	for _, name := range managedIdentityOverrideTestNames {
		c.Request.Header[name] = []string{"caller/999.0.0"}
		c.Request.Header[strings.ToUpper(name)] = []string{"caller/888.0.0"}
	}
	return c
}

func openAIIdentityContractSuccessResponse() *http.Response {
	response := newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n")
	response.Header.Set("Content-Type", "text/event-stream")
	return response
}

func requireOpenAIIdentityContractHeaders(t *testing.T, headers, want http.Header, auth string) {
	t.Helper()
	actual := http.Header{}
	for name, values := range headers {
		if outboundidentity.IsIdentityHeader(name) {
			require.Equal(t, http.CanonicalHeaderKey(name), name, "no duplicate noncanonical identity keys")
			actual[name] = values
		}
	}
	require.Equal(t, want, actual, "final wire identity must contain only the selected protocol declarations")
	require.Equal(t, auth, headers.Get("Authorization"))
}

func TestOpenAIIdentityContractHTTPPassthroughCompatiblePresets(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, preset := range []string{"claude", "gemini", "grok", "antigravity"} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", preset, passthrough), func(t *testing.T) {
				account := newOpenAIRejectedFieldTestAccount()
				account.Extra["openai_passthrough"] = passthrough
				account.Credentials[outboundIdentityCredential] = map[string]any{"preset": preset}
				account.Credentials[credKeyHeaderOverrideEnabled] = true
				overrides := map[string]any{}
				for _, name := range managedIdentityOverrideTestNames {
					overrides[name] = "injected/999.0.0"
				}
				account.Credentials[credKeyHeaderOverrides] = overrides
				var headers http.Header
				svc := newOpenAIRejectedFieldTestService(nil)
				svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					headers = req.Header.Clone()
					return openAIIdentityContractSuccessResponse(), nil
				}}
				body := []byte(`{"model":"gpt-5.5","stream":true,"instructions":"test","input":"hello"}`)
				c := newOpenAIIdentityContractContaminatedContext(body)
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				want := http.Header{}
				builtInOutboundIdentity(preset).Apply(want)
				requireOpenAIIdentityContractHeaders(t, headers, want, "Bearer sk-test")
				wsHeaders, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, account, "sk-test", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, "", "", "", "gpt-5.5", "")
				require.NoError(t, err)
				requireOpenAIIdentityContractHeaders(t, wsHeaders, want, "Bearer sk-test")
			})
		}
	}
}

func TestOpenAIIdentityContractShadowAndWSRetrySnapshot(t *testing.T) {
	repo := &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexClientVersion: "0.200.1"}}
	settings := &SettingService{settingRepo: repo}
	parentID := int64(11)
	parent := &Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"user_agent": testOpenAIAccountUserAgent}}
	shadow := &Account{ID: 12, ParentAccountID: &parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"user_agent": "codex_vscode/0.150.0 ignored-shadow"}}
	svc := &OpenAIGatewayService{settingService: settings, accountRepo: &codexAccountIdentityRepoStub{account: parent}}
	c := newOpenAIRejectedFieldTestContext(nil)
	ctx := WithOutboundIdentityScope(context.Background(), c)
	want := svc.resolveOpenAIOutboundIdentity(ctx, parent)
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
	settings.InvalidateOpenAICodexClientVersionCache()
	require.Equal(t, want, svc.resolveOpenAIOutboundIdentity(ctx, shadow), "the parent and its shadow share the credential owner's snapshot")
	for range 2 {
		// Re-enter with the original context, as handler-level WS retries do.
		headers, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, shadow, "parent-token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, "", "", "", "gpt-5.5", "")
		require.NoError(t, err)
		require.Equal(t, want.UserAgent, headers.Get("User-Agent"))
		require.Equal(t, want.Originator, headers.Get("Originator"))
		require.Equal(t, want.Version, headers.Get("Version"))
		require.Equal(t, "Bearer parent-token", headers.Get("Authorization"))
	}
	next := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"user_agent": "codex_vscode/0.150.0 (Ubuntu 24.04; x86_64) vscode"}}
	header, _, err := svc.buildOpenAIWSHeaders(context.Background(), c, next, "next-token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, "", "", "", "gpt-5.5", "")
	require.NoError(t, err)
	require.Equal(t, "codex_vscode", header.Get("Originator"))
	require.Equal(t, "0.200.2", header.Get("Version"))
	fresh := WithOutboundIdentityScope(context.Background(), newOpenAIRejectedFieldTestContext(nil))
	require.Equal(t, "0.200.2", svc.resolveOpenAIOutboundIdentity(fresh, parent).Version)
}

func TestOpenAIIdentityContractPresetSelectionSurvivesMappingChanges(t *testing.T) {
	settings, original := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	account := &Account{ID: 51, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	ctx := WithAccountOutboundIdentity(original, account)
	_, selectedPreset := outboundidentity.FromContext(ctx)
	require.False(t, selectedPreset, "native Codex uses its existing resolver")
	require.NoError(t, settings.SetOutboundIdentitySettings(original, OutboundIdentitySettings{Defaults: map[string]string{"openai:apikey": "grok"}}))
	_, selectedPreset = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.False(t, selectedPreset, "a retry cannot switch from Codex to another preset")
	identity, selectedPreset := outboundidentity.FromContext(WithAccountOutboundIdentity(original, account))
	require.True(t, selectedPreset)
	require.Equal(t, "grok", identity.Preset)
}

type identityContractOAuthClient struct {
	openAIIdentityOAuthClientStub
	after func()
}

func (s *identityContractOAuthClient) ExchangeCodeWithIdentity(ctx context.Context, code, verifier, redirect, proxy, clientID, ua, originator, version string) (*openai.TokenResponse, error) {
	result, err := s.openAIIdentityOAuthClientStub.ExchangeCodeWithIdentity(ctx, code, verifier, redirect, proxy, clientID, ua, originator, version)
	result.IDToken = identityContractIDToken()
	s.after()
	return result, err
}

func (s *identityContractOAuthClient) RefreshTokenWithClientIDAndIdentity(ctx context.Context, refresh, proxy, clientID, ua, originator, version string) (*openai.TokenResponse, error) {
	result, err := s.openAIIdentityOAuthClientStub.RefreshTokenWithClientIDAndIdentity(ctx, refresh, proxy, clientID, ua, originator, version)
	result.IDToken = identityContractIDToken()
	s.after()
	return result, err
}

func identityContractIDToken() string {
	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"test-personal"}}`
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".test-signature"
}

func TestOpenAIIdentityContractOAuthMetadataUsesExchangeSnapshot(t *testing.T) {
	for _, operation := range []string{"exchange", "refresh"} {
		t.Run(operation, func(t *testing.T) {
			repo := &openAIIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion:                     "0.200.1",
				SettingKeyOpenAICodexUserAgent:                         "codex_exec/0.150.0 (Ubuntu 22.4.0; x86_64) terminal",
				SettingKeyCodexLegacyClientProfileCompatibilityEnabled: "true",
			}}
			settings := &SettingService{settingRepo: repo}
			want := resolveOpenAIOutboundIdentityFromSettings(context.Background(), nil, settings)
			client := &identityContractOAuthClient{after: func() {
				repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
				settings.InvalidateOpenAICodexClientVersionCache()
			}}
			captured := make(chan http.Header, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"accounts":{}}`))
			}))
			defer server.Close()
			svc := NewOpenAIOAuthService(nil, client)
			defer svc.Stop()
			svc.SetSettingService(settings)
			svc.SetPrivacyClientFactory(newQuotaRedirectingFactory(server))
			if operation == "exchange" {
				auth, err := svc.GenerateAuthURL(context.Background(), nil, "", PlatformOpenAI)
				require.NoError(t, err)
				session, ok := svc.sessionStore.Get(auth.SessionID)
				require.True(t, ok)
				_, err = svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: auth.SessionID, State: session.State, Code: "test-code"})
				require.NoError(t, err)
				require.Equal(t, want.UserAgent, client.exchangeUserAgent)
			} else {
				_, err := svc.RefreshToken(context.Background(), "test-refresh", "")
				require.NoError(t, err)
				require.Equal(t, want.UserAgent, client.refreshUserAgent)
			}
			require.Len(t, captured, 3, "account, subscription and privacy requests must all use the selected identity")
			for range 3 {
				header := <-captured
				require.Equal(t, want.UserAgent, header.Get("User-Agent"))
				require.Equal(t, want.Originator, header.Get("Originator"))
				require.Equal(t, want.Version, header.Get("Version"))
			}
		})
	}
}

func TestOpenAIIdentityContractLiveRegistrationSnapshot(t *testing.T) {
	for _, operation := range []string{"create", "sideband"} {
		t.Run(operation, func(t *testing.T) {
			key, privateKey := newTestAgentIdentityKey(t)
			account := &Account{ID: 61, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID,
				"agent_private_key": privateKey, "chatgpt_account_id": "test-live-account", "user_agent": testOpenAIAccountUserAgent,
			}}
			repo := &agentIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexClientVersion: "0.200.1"}}
			settings := &SettingService{settingRepo: repo}
			registered := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				registered <- r.Header.Clone()
				repo.mu.Lock()
				repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
				repo.mu.Unlock()
				settings.InvalidateOpenAICodexClientVersionCache()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"task_id":"test-live-task"}`))
			}))
			defer server.Close()
			original := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = server.URL
			defer func() { openAIAgentIdentityAuthAPIBaseURL = original }()
			upstream := &liveHTTPUpstreamStub{}
			cipher := newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "identity-contract-test-only"}})
			svc := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings,
				accountRepo:  &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}},
				httpUpstream: upstream, liveAttestationCipher: cipher}
			var headers http.Header
			if operation == "create" {
				_, err := svc.createUpstreamLiveCall(context.Background(), account, &LiveCallRequest{SDP: "v=offer\r\n", Session: []byte(`{"model":"gpt-live-test"}`)}, "test-attestation")
				require.NoError(t, err)
				headers = upstream.request.Header
			} else {
				encrypted, err := cipher.Encrypt("test-attestation")
				require.NoError(t, err)
				headers, err = svc.liveSidebandHeaders(context.Background(), account, &LiveCallRecord{AttestationCiphertext: encrypted})
				require.NoError(t, err)
			}
			require.Len(t, registered, 1)
			registration := <-registered
			for _, name := range []string{"User-Agent", "Originator", "Version"} {
				require.Equal(t, registration.Get(name), headers.Get(name), name)
			}
			require.Equal(t, "0.200.1", headers.Get("Version"))
			require.Equal(t, "quicksilver=v2", headers.Get("OpenAI-Alpha"))
			require.Empty(t, headers.Get("OpenAI-Beta"))
			require.Equal(t, "test-attestation", headers.Get(liveAttestationHeader))
		})
	}
}

func TestOpenAIIdentityContractPoolDoesNotReuseAnotherTriple(t *testing.T) {
	for _, name := range []string{"User-Agent", "Originator", "Version"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			dialer := &openAIWSCountingDialer{}
			pool.setClientDialerForTest(dialer)
			request := openAIWSAcquireRequest{Account: &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, WSURL: "wss://example.com/v1/responses", Headers: make(http.Header)}
			applyResolvedOpenAIOutboundIdentity(request.Headers, resolveOpenAIOutboundIdentityCandidates(testOpenAIAccountUserAgent, ""), true)
			first, err := pool.Acquire(context.Background(), request)
			require.NoError(t, err)
			firstID := first.ConnID()
			first.Release()
			same, err := pool.Acquire(context.Background(), request)
			require.NoError(t, err)
			require.True(t, same.Reused())
			same.Release()
			changed := request
			changed.Headers = request.Headers.Clone()
			changed.Headers.Set(name, changed.Headers.Get(name)+"-changed")
			require.False(t, sameOpenAIWSPrewarmTarget(request, changed), "prewarm must follow the new triple")
			next, err := pool.Acquire(context.Background(), changed)
			require.NoError(t, err)
			require.False(t, next.Reused(), "a previous handshake cannot override the new identity")
			require.NotEqual(t, firstID, next.ConnID())
			next.Release()
			require.Equal(t, 2, dialer.DialCount())
		})
	}
}
