//go:build unit || !integration

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Source: openai/codex 3d1001e8652212529810500facf31f63c1527d24,
// login/src/auth/api_key.rs and model-provider/src/{provider,models_endpoint}.rs.
// Issuance markers select defaults; explicit endpoints remain authoritative.
func TestOpenAIFedRAMPDefaultsFollowCredentialOwner(t *testing.T) {
	for _, tc := range []struct {
		name, key, baseURL, wantURL string
		fedRAMP                     bool
	}{
		{"project", "sk-proj-fedramp-synthetic", "", "https://gov.api.openai.com", true},
		{"service-account", "sk-svcacct-fedramp-synthetic", "", "https://gov.api.openai.com", true},
		{"unmarked", "sk-proj-synthetic", "", "https://api.openai.com", false},
		{"embedded-marker", "sk-proj-synthetic-fedramp-token", "", "https://api.openai.com", false},
		{"case-sensitive", "SK-PROJ-FEDRAMP-synthetic", "", "https://api.openai.com", false},
		{"commercial-override", "sk-proj-fedramp-synthetic", "https://api.openai.com/v1", "https://api.openai.com/v1", true},
		{"compatible-override", "sk-proj-fedramp-synthetic", "https://upstream.example/v1", "https://upstream.example/v1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount(tc.baseURL)
			account.Credentials["api_key"] = tc.key
			require.Equal(t, tc.fedRAMP, account.IsOpenAIAPIKeyFedRAMP())
			require.Equal(t, tc.wantURL, account.GetOpenAIBaseURL())
			account.Platform = PlatformDeepseek
			require.False(t, account.IsOpenAIAPIKeyFedRAMP(), "a foreign provider must not acquire OpenAI routing")
		})
	}
}

// Every finalization path must use the captured credential, including a shadow
// whose parent changes after acquisition. Failover starts a new owner's snapshot.
func TestOpenAIFedRAMPHeadersUseAcquiredOwnerAcrossFinalizers(t *testing.T) {
	for _, finalizer := range []string{"request", "headers", "gateway-and-ws"} {
		t.Run(finalizer, func(t *testing.T) {
			owner := newCodexModelsAPIKeyTestAccount("")
			owner.Credentials["api_key"] = "sk-proj-fedramp-acquired"
			parentID := owner.ID
			selected := &Account{ID: 701, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ParentAccountID: &parentID}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			recordRequestCredentialOwner(ctx, owner, "sk-proj-fedramp-acquired")
			ctx = withRequestCredentialOwner(ctx, selected, "sk-proj-fedramp-acquired")
			owner.Credentials["api_key"] = "sk-proj-replacement"
			changedParent := int64(999)
			selected.ParentAccountID = &changedParent
			headers := http.Header{"X-Openai-Fedramp": {"spoofed"}, "Authorization": {"Bearer sk-proj-fedramp-acquired"}}
			var gateway OpenAIGatewayService
			switch finalizer {
			case "request":
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gov.api.openai.com/v1/models", nil)
				require.NoError(t, err)
				req.Header = headers
				ApplyAccountOutboundIdentity(ctx, selected, req)
			case "headers":
				ApplyAccountOutboundHeaders(ctx, selected, headers)
			case "gateway-and-ws":
				gateway.applyOpenAIOutboundIdentity(ctx, selected, headers, true)
			}
			require.Equal(t, "true", headers.Get("x-openai-fedramp"))
			require.Equal(t, "Bearer sk-proj-fedramp-acquired", headers.Get("Authorization"))
			next := newCodexModelsAPIKeyTestAccount("")
			next.ID = 702
			headers.Set("x-openai-fedramp", "true")
			ApplyAccountOutboundHeaders(context.Background(), next, headers)
			require.Empty(t, headers.Get("x-openai-fedramp"), "an ordinary failover owner must not inherit regional declarations")
		})
	}
}

func TestOpenAIFedRAMPHeadersCannotBeSelectedByOverrides(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken} {
		for _, fedRAMP := range []bool{false, true} {
			account := &Account{Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{
				"api_key": "sk-proj-commercial", "chatgpt_account_is_fedramp": fedRAMP,
				"header_overrides": map[string]any{"x-openai-fedramp": "injected"},
			}}
			if accountType == AccountTypeAPIKey && fedRAMP {
				account.Credentials["api_key"] = "sk-svcacct-fedramp-synthetic"
			}
			headers := http.Header{"X-Openai-Fedramp": {"inbound"}, "x-openai-fedramp": {"raw-inbound"}}
			account.applyOpenAIHeaderOverrides(headers)
			var gateway OpenAIGatewayService
			gateway.applyOpenAIOutboundIdentity(context.Background(), account, headers, true)
			if fedRAMP {
				require.Equal(t, "true", headers.Get("x-openai-fedramp"))
			} else {
				require.Empty(t, headers.Get("x-openai-fedramp"))
			}
			require.NotContains(t, headers, "x-openai-fedramp", "raw header casing must not survive finalization")
		}
	}
	require.True(t, isOpenAIProtectedHeaderOverrideName("X-OpenAI-Fedramp"))
	account := newCodexModelsAPIKeyTestAccount("")
	account.Credentials["chatgpt_account_is_fedramp"] = true
	headers := http.Header{"X-Openai-Fedramp": {"injected"}}
	applyOpenAIRegionalRoutingHeaders(context.Background(), account, headers)
	require.Empty(t, headers.Get("x-openai-fedramp"), "API-key routing must not consume OAuth workspace claims")
}

func TestOpenAIFedRAMPSetupTokenRetainsAcquiredClaim(t *testing.T) {
	owner := &Account{ID: 811, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Credentials: map[string]any{
		"access_token": "setup-acquired", "chatgpt_account_is_fedramp": true,
	}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, "setup-acquired")
	ctx = withRequestCredentialOwner(ctx, owner, "setup-acquired")
	owner.Credentials["chatgpt_account_is_fedramp"] = false
	headers := http.Header{"x-openai-fedramp": {"injected"}}
	var gateway OpenAIGatewayService
	gateway.applyOpenAIOutboundIdentity(ctx, owner, headers, true)
	require.Equal(t, "true", headers.Get("x-openai-fedramp"), "setup-token forwarding uses the acquired owner's workspace claim")
	ApplyAccountOutboundHeaders(context.Background(), owner, headers)
	require.Empty(t, headers.Get("x-openai-fedramp"), "a later ordinary owner snapshot clears regional routing")
}

func TestFetchCodexModelsManifestFedRAMPDefaultAndOverrides(t *testing.T) {
	for _, tc := range []struct{ name, baseURL, expectedURL string }{
		{"default", "", "https://gov.chatgpt.com/backend-api/codex/models?client_version=0.165.0"},
		{"explicit-commercial", "https://api.openai.com/v1", "https://api.openai.com/v1/models?client_version=0.165.0"},
		{"explicit-regional", "https://gov.api.openai.com/v1", "https://gov.api.openai.com/v1/models?client_version=0.165.0"},
		{"compatible", "https://upstream.example/custom/v1", "https://upstream.example/custom/v1/models?client_version=0.165.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount(tc.baseURL)
			account.Credentials["api_key"] = "sk-svcacct-fedramp-synthetic"
			var captured *http.Request
			svc := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				captured = req
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-6.1-terra"}]}`))}, nil
			}})
			_, err := svc.FetchCodexModelsManifest(context.Background(), account, "untrusted", "")
			require.NoError(t, err)
			require.NotNil(t, captured)
			require.Equal(t, tc.expectedURL, captured.URL.String())
			require.Equal(t, "Bearer sk-svcacct-fedramp-synthetic", captured.Header.Get("Authorization"))
			require.Equal(t, "true", captured.Header.Get("x-openai-fedramp"))
			require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", captured.UserAgent())
			require.Equal(t, "0.165.0", captured.Header.Get("Version"))
		})
	}
}

func TestFetchOpenAIModelsListFedRAMPUsesSelectedRegionalAPI(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("")
	account.Credentials["api_key"] = "sk-proj-fedramp-synthetic"
	var captured *http.Request
	svc := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		captured = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"object":"list","data":[{"id":"gpt-6.1-terra"}]}`))}, nil
	}})
	_, err := svc.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "https://gov.api.openai.com/v1/models", captured.URL.String())
	require.Equal(t, "true", captured.Header.Get("x-openai-fedramp"))
	require.Equal(t, "Bearer sk-proj-fedramp-synthetic", captured.Header.Get("Authorization"))
}
