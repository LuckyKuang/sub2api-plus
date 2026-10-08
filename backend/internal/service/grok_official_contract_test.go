//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGrokOfficialForwardDoesNotInjectOrConvertSearchTools(t *testing.T) {
	// Official responses.rs preserves client functions and explicitly declared
	// hosted tools; a cache key grants no additional tools, even on Free OAuth.
	for _, tools := range []string{
		"",
		`,"tools":[],"tool_choice":"none"`,
		`,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`,
		`,"tools":[{"type":"function","name":"web_search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}],"tool_choice":"auto"`,
		`,"tools":[{"type":"web_search"},{"type":"function","name":"lookup","parameters":{"type":"object"}}]`,
	} {
		t.Run(tools, func(t *testing.T) {
			body := []byte(`{"model":"grok","prompt_cache_key":"parent","input":"hi"` + tools + `}`)
			c := newGrokCacheTestContext(705)
			c.Request.Header.Set("X-Grok-Client-Tool-Cache", "prefer-cache")
			account := healthyGrokOAuthGatewayTestAccount(705, "access-token")
			account.Credentials["subscription_tier"] = "free"
			repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{account.ID: account}}}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"r","object":"response","status":"completed","model":"grok-4.5","output":[],"usage":{"input_tokens":10,"output_tokens":1}}`))}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}
			_, err := svc.forwardGrokResponses(context.Background(), c, account, body, "grok", false, time.Now())
			require.NoError(t, err)
			for _, field := range []string{"tools", "tool_choice"} {
				if field == "tool_choice" && len(gjson.GetBytes(body, "tools").Array()) == 0 {
					choice := gjson.GetBytes(upstream.lastBody, field)
					require.True(t, !choice.Exists() || choice.String() == "none", "empty tool sets cannot grant tool execution")
					continue
				}
				require.JSONEq(t, `{"value":`+grokJSONFieldOrNull(body, field)+`}`, `{"value":`+grokJSONFieldOrNull(upstream.lastBody, field)+`}`, field)
			}
			require.Empty(t, upstream.lastReq.Header.Get("X-Grok-Client-Tool-Cache"), "unconfigured inbound extras are not automatically forwarded")
			require.NotEmpty(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
		})
	}
}

func grokJSONFieldOrNull(body []byte, field string) string {
	value := gjson.GetBytes(body, field)
	if !value.Exists() {
		return "null"
	}
	return value.Raw
}

// Contract: grok-build 2bdd1d6a, responses.rs::From and
// side_call.rs::parent_cached_request. Expectations are protocol invariants,
// independently of gateway helper algorithms.
func TestGrokOfficialCacheFieldDoesNotGrantTools(t *testing.T) {
	for _, body := range []string{
		`{"input":"hi"}`,
		`{"input":"hi","tools":[],"tool_choice":"none"}`,
		`{"tools":[{"type":"function","name":"web_search","parameters":{"type":"object"}}],"tool_choice":"auto"}`,
		`{"tools":[{"type":"function","name":"lookup"},{"type":"web_search"}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			got, err := applyGrokResponsesCacheIdentity([]byte(body), "tenant-cache")
			require.NoError(t, err)
			require.Equal(t, "tenant-cache", gjson.GetBytes(got, "prompt_cache_key").String())
			for _, field := range []string{"tools", "tool_choice", "input"} {
				require.Equal(t, gjson.Get(body, field).Raw, gjson.GetBytes(got, field).Raw, field)
			}
		})
	}
}

func TestGrokOfficialSideCallKeepsParentSessionAndIndependentConversation(t *testing.T) {
	main := newGrokCacheTestContext(701)
	main.Request.Header.Set("X-Grok-Session-Id", "parent")
	main.Request.Header.Set("X-Grok-Conv-Id", "parent")
	side := newGrokCacheTestContext(701)
	side.Request.Header.Set("X-Grok-Session-Id", "parent")
	side.Request.Header.Set("X-Grok-Conv-Id", "recap-child")
	side.Request.Header.Set("X-Claude-Code-Session-Id", "foreign-session")
	body := []byte(`{"model":"grok-4.5","prompt_cache_key":"parent","input":"summary"}`)
	mainKey := resolveGrokCacheIdentity(main, body, "", "grok-4.5")
	sideKey := resolveGrokCacheIdentity(side, body, "", "grok-4.5")
	require.Equal(t, mainKey, sideKey, "explicit official cache key outranks any session/header seed")
	svc := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	mainReq, err := svc.buildGrokResponsesRequest(context.Background(), main, account, body, "key", mainKey, nil)
	require.NoError(t, err)
	sideReq, err := svc.buildGrokResponsesRequest(context.Background(), side, account, body, "key", sideKey, nil)
	require.NoError(t, err)
	require.Equal(t, mainReq.Header.Get("X-Grok-Session-Id"), sideReq.Header.Get("X-Grok-Session-Id"))
	require.Equal(t, mainReq.Header.Get("X-Grok-Conv-Group-Id"), sideReq.Header.Get("X-Grok-Conv-Group-Id"))
	require.NotEqual(t, mainReq.Header.Get("X-Grok-Conv-Id"), sideReq.Header.Get("X-Grok-Conv-Id"))
	require.NotEqual(t, mainReq.Header.Get("X-Grok-Req-Id"), sideReq.Header.Get("X-Grok-Req-Id"))
	for _, header := range []string{"X-Grok-Session-Id", "X-Grok-Conv-Id"} {
		require.NotContains(t, sideReq.Header.Get(header), "parent")
		require.NotContains(t, sideReq.Header.Get(header), "recap-child")
	}
	other := newGrokCacheTestContext(702)
	other.Request.Header = side.Request.Header.Clone()
	otherReq, err := svc.buildGrokResponsesRequest(context.Background(), other, account, body, "key", resolveGrokCacheIdentity(other, body, "", "grok-4.5"), nil)
	require.NoError(t, err)
	require.NotEqual(t, sideReq.Header.Get("X-Grok-Session-Id"), otherReq.Header.Get("X-Grok-Session-Id"))
}

func TestGrokOfficialMediaIdentityUsesSameSelectedVersion(t *testing.T) {
	// Official media_tool_config.rs: xai-grok-build/<version>;
	// sampler/client.rs: grok-shell/<version> (linux; x86_64).
	for _, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		account := &Account{ID: 501, Platform: PlatformGrok, Type: typ, Credentials: map[string]any{
			outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok", UserAgent: "grok-shell/1.0.46 (linux; x86_64)"},
		}}
		ctx := WithAccountOutboundIdentity(context.Background(), account)
		for path, ua := range map[string]string{
			"/v1/responses":          "grok-shell/1.0.46 (linux; x86_64)",
			"/v1/images/generations": "xai-grok-build/1.0.46",
			"/v1/videos/job-1":       "xai-grok-build/1.0.46",
		} {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.ai"+path, nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", "untrusted-client")
			prepareAccountOutboundRequest(req, account)
			require.Equal(t, ua, req.UserAgent())
			require.Equal(t, "1.0.46", req.Header.Get("X-Grok-Client-Version"))
			require.Equal(t, "grok-shell", req.Header.Get("X-Grok-Client-Identifier"))
			require.Empty(t, req.Header.Get("X-Grok-Client-Mode"), "public API omits CLI proxy mode")
		}
	}
}

func TestGrokOfficialHostedSearchWinsOnlyForExplicitCollision(t *testing.T) {
	body := []byte(`{"model":"grok","tools":[{"type":"function","name":"web_search","parameters":{"type":"object"}},{"type":"function","name":"lookup","parameters":{"type":"object"}},{"type":"web_search","filters":{"allowed_domains":["example.com"]}}]}`)
	got, err := patchGrokResponsesBody(body, "grok-4.5")
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"function","name":"lookup","parameters":{"type":"object"}},{"type":"web_search","filters":{"allowed_domains":["example.com"]}}]`, gjson.GetBytes(got, "tools").Raw)
}

func TestGrokOfficialMediaPreservesSourcePrioritySnapshotAndFailover(t *testing.T) {
	// Repository source contract applies equally to sampler and Imagine families.
	t.Setenv(xai.CLIVersionEnv, "1.0.46")
	for _, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, tc := range []struct {
			name, globalVersion, accountVersion, version, source string
		}{
			{"environment", "", "", "1.0.46", "environment"},
			{"global", "1.0.47", "", "1.0.47", "global"},
			{"account", "1.0.47", "1.0.48", "1.0.48", "account"},
			{"invalid account falls through", "1.0.47", "invalid", "1.0.47", "global"},
		} {
			t.Run(typ+"/"+tc.name, func(t *testing.T) {
				config := emptyOutboundIdentitySettings()
				if tc.globalVersion != "" {
					config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: tc.globalVersion}
				}
				svc, ctx := outboundIdentityTestSettings(t, config)
				account := &Account{ID: 801, Platform: PlatformGrok, Type: typ, Credentials: map[string]any{}}
				if tc.accountVersion != "" {
					account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: tc.accountVersion}
				}
				snapshot := WithAccountOutboundIdentity(ctx, account)
				config.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "1.0.49"}
				require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
				for _, path := range []string{"/v1/images/generations", "/v1/videos/job"} {
					req, err := http.NewRequestWithContext(snapshot, http.MethodPost, "https://api.x.ai"+path, nil)
					require.NoError(t, err)
					prepareAccountOutboundRequest(req, account)
					require.Equal(t, "xai-grok-build/"+tc.version, req.UserAgent())
					require.Equal(t, tc.version, req.Header.Get("X-Grok-Client-Version"))
					selected, ok := outboundidentity.FromContext(req.Context())
					require.True(t, ok)
					require.Equal(t, tc.source, selected.Source)
					require.Equal(t, account.ID, selected.AccountID)
				}
				other := &Account{ID: 802, Platform: PlatformGrok, Type: typ, Credentials: map[string]any{}}
				req, err := http.NewRequestWithContext(snapshot, http.MethodGet, "https://api.x.ai/v1/videos/job", nil)
				require.NoError(t, err)
				prepareAccountOutboundRequest(req, other)
				require.Equal(t, "xai-grok-build/1.0.49", req.UserAgent(), "failover resolves the new credential owner")
			})
		}
	}
}

func TestOutboundPolicyRejectionIsLocalAndDoesNotScheduleFailover(t *testing.T) {
	c := newGrokCacheTestContext(713)
	account := &Account{ID: 713, Platform: PlatformGrok, Type: AccountTypeOAuth}
	policy := &brandidentity.Violation{Reason: "protected_header"}
	for _, handle := range []func() error{
		func() error {
			return (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(context.Background(), c, account, policy, false)
		},
		func() error {
			return (&GatewayService{}).handleUpstreamTransportError(context.Background(), c, account, policy, OpsUpstreamErrorEvent{})
		},
	} {
		require.ErrorIs(t, handle(), brandidentity.ErrBrandedOutboundHeader)
		events, exists := c.Get(OpsUpstreamErrorsKey)
		require.True(t, exists)
		final := events.([]*OpsUpstreamErrorEvent)
		last := final[len(final)-1]
		require.Equal(t, "gateway", last.Scope)
		require.Equal(t, "outbound_policy", last.Stage)
		require.Zero(t, last.UpstreamStatusCode)
		require.Equal(t, "outbound_policy_protected_header", last.Reason)
	}
}
