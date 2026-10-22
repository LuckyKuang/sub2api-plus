//go:build unit

package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// AGENTS.md source precedence and OFFICIAL_AGENT_REQUEST_STATE.md require the
// acquired account candidate, with same-owner stable global fallback settings.
func TestCodexAcquisitionResolvesFrozenAccountWithoutRereadingGlobalSettings(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "shadow"}[shadow], func(t *testing.T) {
			repo := &openAIIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion: "0.200.1",
				SettingKeyOpenAICodexUserAgent:     "codex_cli_rs/0.180.0 (Ubuntu 24.04; x86_64) xterm-256color",
			}}
			settings := &SettingService{settingRepo: repo}
			selected := &Account{ID: 51, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old-token"}}
			owner := snapshotRequestCredentialAccount(selected)
			owner.Credentials["access_token"] = "acquired-token"
			owner.Credentials["user_agent"] = "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode"
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			before := resolveOpenAIOutboundIdentityFromSettings(ctx, selected, settings)
			require.Equal(t, "global", before.Source)
			recordRequestCredentialOwner(ctx, owner, "acquired-token")
			if shadow {
				parentID := owner.ID
				selected = &Account{ID: 52, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
			}
			ctx = withRequestCredentialOwner(ctx, selected, "acquired-token")
			repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
			settings.InvalidateOpenAICodexClientVersionCache()
			owner.Credentials["user_agent"] = "codex_cli_rs/0.180.0 (Windows 11; x86_64) terminal"
			for _, account := range []*Account{selected, owner} {
				got := resolveOpenAIOutboundIdentityFromSettings(ctx, account, settings)
				require.Equal(t, "account", got.Source)
				require.Equal(t, "codex_vscode/0.200.1 (Ubuntu 24.04; aarch64) vscode", got.UserAgent)
				require.Equal(t, "codex_vscode", got.Originator)
				require.Equal(t, "0.200.1", got.Version)
			}
		})
	}
}

func TestAcquiredPresetKeepsGlobalSnapshotAndUsesActualAccountCandidate(t *testing.T) {
	for _, changedCandidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-candidate", true: "new-account-candidate"}[changedCandidate], func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}
			settings, ctx := outboundIdentityTestSettings(t, config)
			ctx = WithOutboundIdentityScope(ctx, nil)
			selected := &Account{ID: 61, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "selected-key", outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"},
			}}
			ctx = WithAccountOutboundIdentity(ctx, selected)
			before, ok := outboundidentity.FromContext(ctx)
			require.True(t, ok)
			require.Equal(t, "3.9.1", before.Version)
			acquired := snapshotRequestCredentialAccount(selected)
			acquired.Credentials["api_key"] = "acquired-key"
			if changedCandidate {
				acquired.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.3"}
			}
			recordRequestCredentialOwner(ctx, acquired, "acquired-key")
			ctx = withRequestCredentialOwner(ctx, selected, "acquired-key")
			config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.2"}
			require.NoError(t, settings.SetOutboundIdentitySettings(ctx, config))
			selected.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.4"}
			upstream := &httpUpstreamRecorder{resp: openAIIdentityContractSuccessResponse()}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.example/v1/responses", nil)
			require.NoError(t, err)
			_, err = svc.doOpenAIUpstream(req, "", selected)
			require.NoError(t, err)
			wantVersion := "3.9.1"
			if changedCandidate {
				wantVersion = "3.9.3"
			}
			require.Len(t, upstream.requests, 1)
			require.Equal(t, wantVersion, upstream.lastReq.Header.Get("x-grok-client-version"))
			require.Equal(t, "grok-shell/"+wantVersion+" (linux; x86_64)", upstream.lastReq.Header.Get("User-Agent"))
		})
	}
}

func TestCodexAcquisitionFallbackRetainsGlobalSnapshot(t *testing.T) {
	for _, candidate := range []string{"", "curl/8.0"} {
		t.Run(candidate, func(t *testing.T) {
			repo := &openAIIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion: "0.200.1",
				SettingKeyOpenAICodexUserAgent:     "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode",
			}}
			settings := &SettingService{settingRepo: repo}
			selected := &Account{ID: 81, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "selected-token", "user_agent": "codex_cli_rs/0.180.0 (Ubuntu 24.04; x86_64) xterm-256color",
			}}
			ctx := WithOutboundIdentityScope(t.Context(), nil)
			require.Equal(t, "account", resolveOpenAIOutboundIdentityFromSettings(ctx, selected, settings).Source)
			acquired := snapshotRequestCredentialAccount(selected)
			acquired.Credentials["access_token"], acquired.Credentials["user_agent"] = "acquired-token", candidate
			recordRequestCredentialOwner(ctx, acquired, "acquired-token")
			ctx = withRequestCredentialOwner(ctx, selected, "acquired-token")
			repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
			settings.InvalidateOpenAICodexClientVersionCache()
			identity := resolveOpenAIOutboundIdentityFromSettings(ctx, selected, settings)
			require.Equal(t, "global", identity.Source, "the old row's valid account candidate cannot override acquisition")
			require.Equal(t, "codex_vscode/0.200.1 (Ubuntu 24.04; aarch64) vscode", identity.UserAgent)
		})
	}
}

func TestAmbiguousAcquiredOwnerStopsTransport(t *testing.T) {
	account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "same-key"}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, account, "same-key")
	conflicting := snapshotRequestCredentialAccount(account)
	conflicting.Credentials["base_url"] = "https://another-provider.example"
	recordRequestCredentialOwner(ctx, conflicting, "same-key")
	ctx = withRequestCredentialOwner(ctx, account, "same-key")
	upstream := &httpUpstreamRecorder{resp: openAIIdentityContractSuccessResponse()}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.example/v1/responses", nil)
	require.NoError(t, err)
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.Error(t, err)
	require.Empty(t, upstream.requests)
}

func TestAuxiliaryRequestsUseAcquiredDestinationAndHeaderOverrides(t *testing.T) {
	for _, endpoint := range []string{"images/generations", "responses/input_tokens", "embeddings"} {
		t.Run(endpoint, func(t *testing.T) {
			account := b64BackfillAccount(true)
			account.Credentials["base_url"] = "https://acquired.example/v1"
			account.Credentials[credKeyHeaderOverrideEnabled] = true
			account.Credentials[credKeyHeaderOverrides] = map[string]any{"X-Route": "acquired"}
			ctx := WithOutboundIdentityScope(t.Context(), nil)
			recordRequestCredentialOwner(ctx, account, "sk-test")
			ctx = withRequestCredentialOwner(ctx, account, "sk-test")
			account.Credentials["base_url"] = "https://replacement.example/v1"
			account.Credentials[credKeyHeaderOverrides] = map[string]any{"X-Route": "replacement"}
			upstream := &httpUpstreamRecorder{resp: b64BackfillImageResponse(http.StatusOK, "application/json", []byte(`{"data":[],"usage":{"prompt_tokens":1,"total_tokens":1}}`))}
			svc := newOpenAIRejectedFieldTestService(upstream)
			body := []byte(`{"model":"gpt-image-2","prompt":"test"}`)
			c := newOpenAIRejectedFieldTestContext(body)
			if endpoint == "embeddings" {
				_, err := svc.ForwardEmbeddings(ctx, c, account, []byte(`{"model":"text-embedding-3-small","input":"test"}`), "")
				require.NoError(t, err)
			} else {
				var req *http.Request
				var err error
				if strings.HasPrefix(endpoint, "images/") {
					req, err = svc.buildOpenAIImagesRequest(ctx, c, account, body, "application/json", "sk-test", openAIImagesGenerationsEndpoint)
				} else {
					req, err = svc.buildInputTokensUpstreamRequest(ctx, c, account, body, "sk-test")
				}
				require.NoError(t, err)
				_, err = svc.doOpenAIUpstream(req, "", account)
				require.NoError(t, err)
			}
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "https://acquired.example/v1/"+endpoint, upstream.lastReq.URL.String())
			require.Equal(t, "acquired", getHeaderRaw(upstream.lastReq.Header, "x-route"))
			require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
		})
	}
}

func TestAcquiredPresetFallbackUsesSameSettingsAcrossTypedAndPresetKeys(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"}
	settings, ctx := outboundIdentityTestSettings(t, config)
	ctx = WithOutboundIdentityScope(ctx, nil)
	selected := &Account{ID: 91, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "selected-key"}}
	ctx = WithAccountOutboundIdentity(ctx, selected)
	before, ok := outboundidentity.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "3.9.1", before.Version)
	owner := snapshotRequestCredentialAccount(selected)
	owner.Credentials["api_key"] = "acquired-key"
	owner.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok"}
	recordRequestCredentialOwner(ctx, owner, "acquired-key")
	ctx = withRequestCredentialOwner(ctx, selected, "acquired-key")
	config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.2"}
	require.NoError(t, settings.SetOutboundIdentitySettings(ctx, config))
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, selected))
	require.True(t, ok)
	require.Equal(t, "account", identity.Source)
	require.Equal(t, "3.9.1", identity.Version, "changing account candidate does not authorize rereading global settings through a different key")
}

// OUTBOUND_IDENTITY.md: failover resolves the replacement owner's current
// defaults, including a compatible account whose chosen family is Codex.
func TestCompatibleCodexDefaultsAreScopedToEachCredentialOwner(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Defaults[PlatformGemini+":"+AccountTypeAPIKey] = "codex"
	settings, ctx := outboundIdentityTestSettings(t, config)
	repo := settings.settingRepo.(*outboundIdentityTestRepo)
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.1"
	ctx = WithOutboundIdentityScope(ctx, nil)
	first := &Account{ID: 101, Platform: PlatformGemini, Type: AccountTypeAPIKey}
	ctx = WithAccountOutboundIdentity(ctx, first)
	before, ok := outboundidentity.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "codex", before.Preset)
	require.Equal(t, "0.200.1", before.Version)
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
	settings.InvalidateOpenAICodexClientVersionCache()
	retry, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, first))
	require.True(t, ok)
	require.Equal(t, before, retry)
	next := &Account{ID: 102, Platform: PlatformGemini, Type: AccountTypeAPIKey}
	failover, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, next))
	require.True(t, ok)
	require.EqualValues(t, 102, failover.AccountID)
	require.Equal(t, "codex", failover.Preset)
	require.Equal(t, "0.200.2", failover.Version)
}
