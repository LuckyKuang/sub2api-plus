//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type providerIdentityUpstream struct {
	HTTPUpstream
	check func(*http.Request)
}

func (u *providerIdentityUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	u.check(req)
	return u.HTTPUpstream.Do(req, proxy, accountID, concurrency)
}

func TestCompatibleProviderControlRequestsRetainOwnerSnapshot(t *testing.T) {
	for _, platform := range []string{PlatformCline, PlatformCommandCode} {
		for _, balanceOnly := range []bool{false, true} {
			for _, source := range []string{"account", "global", "invalid_account", "compiled_default"} {
				t.Run(platform+"/"+source+map[bool]string{false: "/quota", true: "/balance"}[balanceOnly], func(t *testing.T) {
					settings := emptyOutboundIdentitySettings()
					wantUA, wantSource := "codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color", "compiled_default"
					if source != "compiled_default" {
						settings.Defaults[platform+":apikey"] = "grok"
						settings.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "1.2.3"}
						wantUA, wantSource = "grok-shell/1.2.3 (linux; x86_64)", "global"
					}
					settingService, ctx := outboundIdentityTestSettings(t, settings)
					ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					account := commandCodeUsageAccount()
					upstream := newCommandCodeAlphaUpstream(`{"org":null}`)
					if platform == PlatformCline {
						account = clineTestAccount(78)
						upstream = newClineAccountUpstream(commandCodeAlphaResponse{status: 200, body: clineUsageLimitsBody}, `{"success":true,"data":{"balance":1000000}}`)
					}
					account.Credentials["header_override_enabled"] = true
					account.Credentials["header_overrides"] = map[string]any{"User-Agent": "untrusted-client", "Version": "99.0.0"}
					if source == "account" {
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.1.4"}
						wantUA, wantSource = "grok-shell/3.1.4 (linux; x86_64)", "account"
					} else if source == "invalid_account" {
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "unknown", Version: "bad"}
					}
					calls := 0
					transport := &providerIdentityUpstream{HTTPUpstream: upstream, check: func(req *http.Request) {
						calls++
						identity, ok := outboundidentity.FromContext(req.Context())
						assert.True(t, ok)
						assert.Equal(t, account.ID, identity.AccountID)
						assert.Equal(t, wantSource, identity.Source)
						assert.Equal(t, wantUA, req.Header.Get("User-Agent"))
						if source == "compiled_default" {
							assert.Equal(t, "0.158.0", req.Header.Get("Version"))
							assert.Equal(t, "codex_cli_rs", req.Header.Get("Originator"))
						} else {
							assert.Equal(t, identity.Version, req.Header.Get("X-Grok-Client-Version"))
							assert.Empty(t, req.Header.Get("Originator"))
						}
						// A global edit during a multi-request probe affects the next operation.
						if calls == 1 {
							assert.NoError(t, settingService.SetOutboundIdentitySettings(context.Background(), emptyOutboundIdentitySettings()))
						}
					}}
					repo := &cnBalanceProbeRepo{account: account}
					if balanceOnly {
						svc := NewCNProviderBalanceService(repo, nil, transport, &config.Config{})
						_, err := svc.QueryBalance(ctx, account.ID)
						require.NoError(t, err)
					} else {
						svc := NewCNProviderQuotaService(repo, nil, transport, &config.Config{})
						_, err := svc.QueryUsage(ctx, account.ID)
						require.NoError(t, err)
					}
					require.GreaterOrEqual(t, calls, 2)
				})
			}
		}
	}
}

func TestCompatibleProviderForwardProbeAndDiscoveryIdentity(t *testing.T) {
	for _, platform := range []string{PlatformCline, PlatformCommandCode} {
		protocols := []string{APIProtocolChatCompletions}
		if platform == PlatformCommandCode {
			protocols = append(protocols, APIProtocolResponses, APIProtocolAnthropic)
		}
		for _, protocol := range protocols {
			t.Run(platform+"/"+protocol, func(t *testing.T) {
				account := commandCodeTestAccount(995)
				if platform == PlatformCline {
					account = clineTestAccount(996)
				}
				account.Credentials["api_protocol"] = protocol
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.1.4"}
				account.Credentials["header_override_enabled"] = true
				account.Credentials["header_overrides"] = map[string]any{"User-Agent": "untrusted", "Version": "99.0.0"}
				check := func(req *http.Request) {
					t.Helper()
					identity, ok := outboundidentity.FromContext(req.Context())
					require.True(t, ok)
					require.Equal(t, account.ID, identity.AccountID)
					require.Equal(t, "account", identity.Source)
					require.Equal(t, "grok-shell/3.1.4 (linux; x86_64)", req.UserAgent())
					require.Equal(t, "3.1.4", req.Header.Get("X-Grok-Client-Version"))
					require.Empty(t, req.Header.Get("Version"))
					require.Empty(t, req.Header.Get("Originator"))
				}
				for _, ingress := range routingMatrixIngresses() {
					upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					body := routingMatrixCase{ingress: ingress, model: "example-model"}.body()
					c := adaptiveProtocolTestContext(ingress.path, body)
					c.Request.Header.Set("User-Agent", "inbound-client/99")
					_ = ingress.forward(svc, c, account, body)
					require.NotEmpty(t, upstream.requests, ingress.name)
					for _, req := range upstream.requests {
						check(req)
					}
				}
				response := adaptiveCNChatTestResponse()
				if protocol == APIProtocolResponses {
					response = adaptiveCNResponsesTestResponse()
				} else if protocol == APIProtocolAnthropic {
					response = adaptiveCNAnthropicTestResponse()
				}
				probe, upstream := adaptiveCNAccountTestService(account, response)
				c, _ := newTestContext()
				require.NoError(t, probe.TestAccountConnection(c, account.ID, "example-model", "hi", AccountTestModeDefault))
				require.NotEmpty(t, upstream.requests)
				for _, req := range upstream.requests {
					check(req)
				}
				discovery := &httpUpstreamRecorder{err: errors.New("stop after capture")}
				modelSvc := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: discovery}
				_, err := modelSvc.FetchUpstreamSupportedModels(t.Context(), account)
				require.Error(t, err)
				require.NotEmpty(t, discovery.requests)
				for _, req := range discovery.requests {
					check(req)
				}
			})
		}
	}
}

func TestModelProtocolCatalogInvalidatesCredentialTenantIdentityAndProxy(t *testing.T) {
	account := commandCodeTestAccount(991)
	ctx := context.Background()
	url := "https://api.commandcode.ai/provider/v1/models"
	headers := modelProtocolCatalogHeaders(ctx, account)
	key := modelProtocolCatalogKey(account, url, headers, "")
	require.NotContains(t, key, "user_test_key")
	for _, header := range []string{"Authorization", "X-Tenant", "User-Agent", "Version"} {
		changed := headers.Clone()
		changed.Set(header, "changed")
		require.NotEqual(t, key, modelProtocolCatalogKey(account, url, changed, ""), header)
	}
	require.NotEqual(t, key, modelProtocolCatalogKey(account, url, headers, "http://proxy.example:8080"))
	require.Equal(t, key, modelProtocolCatalogKey(account, url, headers.Clone(), ""))
}

func TestModelProtocolCatalogRefreshAfterCredentialTenantAndIdentityChanges(t *testing.T) {
	settings := emptyOutboundIdentitySettings()
	settings.Defaults["command_code:apikey"] = "grok"
	settings.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "1.2.3"}
	_, ctx := outboundIdentityTestSettings(t, settings)
	account := commandCodeTestAccount(997)
	account.Credentials["api_key"] = "invalid-key"
	upstream := &modelCatalogAccountUpstream{failureStatus: http.StatusUnauthorized}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	require.Nil(t, svc.modelCatalogProtocols(ctx, account, "vendor/model"))
	account.Credentials["api_key"] = "rotated-key"
	require.Equal(t, []string{APIProtocolChatCompletions}, svc.modelCatalogProtocols(ctx, account, "vendor/model"), "credential rotation must not inherit the earlier auth backoff")
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"X-Tenant": "responses-tenant", "User-Agent": "untrusted"}
	require.Equal(t, []string{APIProtocolResponses}, svc.modelCatalogProtocols(ctx, account, "vendor/model"), "tenant rotation must reload capabilities")
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.1.4"}
	require.Equal(t, []string{APIProtocolResponses}, svc.modelCatalogProtocols(ctx, account, "vendor/model"))
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.Len(t, upstream.requests, 4)
	for i, req := range upstream.requests {
		identity, ok := outboundidentity.FromContext(req.Context())
		require.True(t, ok)
		require.Equal(t, account.ID, identity.AccountID)
		if i < 3 {
			require.Equal(t, "grok-shell/1.2.3 (linux; x86_64)", req.UserAgent())
			require.Equal(t, "global", identity.Source)
		} else {
			require.Equal(t, "grok-shell/3.1.4 (linux; x86_64)", req.UserAgent())
			require.Equal(t, "account", identity.Source)
		}
		require.Equal(t, identity.Version, req.Header.Get("X-Grok-Client-Version"))
	}
}
