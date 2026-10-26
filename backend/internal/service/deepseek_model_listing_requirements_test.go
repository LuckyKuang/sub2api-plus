//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These literal rows follow deepseek-harness 22ddee's
// packages/llm/llm-deepseek/{src/listing.ts,src/listed-catalog.ts,
// tests/listed-catalog.spec.ts}. A declared floor is compared against the
// identity selected for this request, including prerelease ordering. Both IDs
// below are independently present in the official bundled supported catalog.
const deepseekRequirementBaselineRow = `{"id":"deepseek-v4-pro","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.0-rc.2"}}}`

func TestDeepSeekNativeModelListingUsesSelectedIdentityVersion(t *testing.T) {
	for _, tc := range []struct {
		name, accountVersion, globalVersion, environmentVersion, selectedVersion, source string
		allowed                                                                          bool
		minimumVersion                                                                   string
	}{
		{"account-at-floor", "0.2.1-rc.3", "0.2.0-rc.2", "0.2.0-rc.2", "0.2.1-rc.3", "account", true, "0.2.1-rc.3"},
		{"account-above-floor", "0.2.1-rc.4", "", "", "0.2.1-rc.4", "account", true, "0.2.1-rc.3"},
		{"account-prerelease-below-floor", "0.2.1-rc.2", "0.2.1-rc.3", "0.2.1-rc.3", "0.2.1-rc.2", "account", false, "0.2.1-rc.3"},
		{"global-at-floor", "", "0.2.1-rc.3", "0.2.0-rc.2", "0.2.1-rc.3", "global", true, "0.2.1-rc.3"},
		{"invalid-account-falls-to-global", "invalid", "0.2.1-rc.3", "0.2.0-rc.2", "0.2.1-rc.3", "global", true, "0.2.1-rc.3"},
		{"global-below-floor-cannot-adopt-newer-environment", "", "0.2.0-rc.2", "0.2.1-rc.3", "0.2.0-rc.2", "global", false, "0.2.1-rc.3"},
		{"environment-at-floor", "", "", "0.2.1-rc.3", "0.2.1-rc.3", "environment", true, "0.2.1-rc.3"},
		{"compiled-at-floor", "", "", "", "0.2.1-rc.3", "compiled_default", true, "0.2.1-rc.3"},
		{"compiled-below-floor", "", "", "", "0.2.1-rc.3", "compiled_default", false, "0.2.1-rc.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SUB2API_DEEPSEEK_HARNESS_VERSION", tc.environmentVersion)
			account := nativeControlAccount(PlatformDeepseek)
			if tc.accountVersion != "" {
				account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": tc.accountVersion}
			}
			ctx := context.Background()
			if tc.globalVersion != "" {
				settings := emptyOutboundIdentitySettings()
				settings.Profiles["deepseek"] = OutboundIdentitySelection{Preset: "deepseek", Version: tc.globalVersion}
				_, ctx = outboundIdentityTestSettings(t, settings)
			}
			body := fmt.Sprintf(`{"data":[{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":%q}}},%s]}`, tc.minimumVersion, deepseekRequirementBaselineRow)
			calls := 0
			upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https://api.deepseek.com/models", req.URL.String())
				require.Equal(t, "request-owner", req.Header.Get("X-Dsh-Auth-Token"))
				require.Equal(t, fmt.Sprintf("deepseek-harness/%s (+https://github.com/deepseek-ai/deepseek-harness)", tc.selectedVersion), req.UserAgent())
				identity, ok := outboundidentity.FromContext(req.Context())
				require.True(t, ok)
				require.Equal(t, tc.source, identity.Source)
				return nativeControlReply(body), nil
			}}
			svc := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
			models, err := svc.FetchUpstreamSupportedModels(ctx, account)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			if tc.allowed {
				require.ElementsMatch(t, []string{"deepseek-flash", "deepseek-v4-pro"}, models)
			} else {
				require.Equal(t, []string{"deepseek-v4-pro"}, models, "a listed model cannot be published when the selected client is below its declared floor")
			}
		})
	}
}

func TestDeepSeekNativePublicModelCacheKeepsSelectedVersionGate(t *testing.T) {
	t.Setenv("SUB2API_DEEPSEEK_HARNESS_VERSION", "")
	account := nativeControlAccount(PlatformDeepseek)
	account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": "0.2.0-rc.2"}
	body := `{"data":[{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.1-rc.3"}}},` + deepseekRequirementBaselineRow + `]}`
	calls := 0
	upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://api.deepseek.com/models", req.URL.String())
		return nativeControlReply(body), nil
	}}
	gateway := &OpenAIGatewayService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
	first, err := gateway.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"deepseek-v4-pro"}, gjson.GetBytes(first.Body, "data.#.id").Value())
	account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": "0.2.1-rc.3"}
	second, err := gateway.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"deepseek-flash", "deepseek-v4-pro"}, gjson.GetBytes(second.Body, "data.#.id").Value())
	require.Equal(t, 2, calls, "the selected identity version must partition the cached filtered response")
	_, err = gateway.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "the same identity can reuse its filtered catalog")
}

func TestDeepSeekNativeMetadataCannotPublishVersionRejectedModel(t *testing.T) {
	t.Setenv("SUB2API_DEEPSEEK_HARNESS_VERSION", "")
	account := nativeControlAccount(PlatformDeepseek)
	account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": "0.2.0-rc.2"}
	repo := &nativeModelMetadataRepo{owner: account}
	body := `{"data":[{"id":"deepseek-flash","input_modalities":["text"],"context_window":64000,"api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.1-rc.3"}}},{"id":"deepseek-v4-pro","input_modalities":["text"],"context_window":64000,"api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"0.2.0-rc.2"}}}]}`
	calls := 0
	upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() == modelsDevRegistryURL {
			// Harness listing facts do not declare reasoning. The supported
			// registry shape supplies that independent completeness fact for
			// both IDs, but cannot readmit the version-rejected native row.
			return nativeControlReply(`{"deepseek":{"api":"https://api.deepseek.com","models":{"deepseek-flash":{"id":"deepseek-flash","reasoning":false,"limit":{"context":64000},"modalities":{"input":["text"]}},"deepseek-v4-pro":{"id":"deepseek-v4-pro","reasoning":false,"limit":{"context":64000},"modalities":{"input":["text"]}}}}}`), nil
		}
		require.Equal(t, "https://api.deepseek.com/models", req.URL.String())
		return nativeControlReply(body), nil
	}}
	svc := &AccountTestService{accountRepo: repo, cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
	catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "native listing and independent capability enrichment must both execute")
	require.Equal(t, []string{"deepseek-v4-pro"}, catalog.Models)
	require.Contains(t, catalog.Metadata, "deepseek-v4-pro")
	require.NotContains(t, catalog.Metadata, "deepseek-flash")
	snapshot := account.GetUpstreamModelMetadataSnapshot()
	require.NotNil(t, snapshot)
	require.Contains(t, snapshot.Models, "deepseek-v4-pro")
	require.NotContains(t, snapshot.Models, "deepseek-flash")
}

func TestDeepSeekNativeModelListingRejectsUnreadableOrUnusableRows(t *testing.T) {
	for _, tc := range []struct{ name, row string }{
		{"unreadable-semver", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":"soon"}}}`},
		{"malformed-requirements", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":"bad"}`},
		{"malformed-product-requirement", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":"bad"}}`},
		{"numeric-floor", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":3}}}`},
		{"empty-floor", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"client_requirements":{"deepseek-harness":{"min_version":""}}}`},
		{"no-messages-capability", `{"id":"deepseek-flash","api_capabilities":{"chat_completions":{}}}`},
		{"null-messages-capability", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":null}}`},
		{"malformed-messages-capability", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":[]}}`},
		{"no-text-output", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"output_modalities":["audio"]}`},
		{"unsupported-input-modality", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"input_modalities":["audio"]}`},
		{"invalid-context-capacity", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"context_window":0}`},
		{"fractional-output-capacity", `{"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"max_output_tokens":1.5}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SUB2API_DEEPSEEK_HARNESS_VERSION", "")
			account := nativeControlAccount(PlatformDeepseek)
			account.Credentials["outbound_identity"] = map[string]any{"preset": "deepseek", "version": "0.2.1-rc.3"}
			body := `{"data":[` + tc.row + `,` + deepseekRequirementBaselineRow + `]}`
			upstream := nativeControlOwnerUpstream{respond: func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "https://api.deepseek.com/models", req.URL.String())
				return nativeControlReply(body), nil
			}}
			svc := &AccountTestService{cfg: upstreamModelSyncTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
			models, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
			require.NoError(t, err, "a malformed sibling cannot hide independently usable models")
			require.Equal(t, []string{"deepseek-v4-pro"}, models)
		})
	}
}
