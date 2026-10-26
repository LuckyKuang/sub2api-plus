//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Harness adapter.ts: attribution belongs to the credential owner and native
// Messages turn, never to inbound headers or the identity version fields.
func TestDeepSeekHarnessRequestAttribution(t *testing.T) {
	for _, kind := range []string{"oauth", "apikey"} {
		t.Run(kind, func(t *testing.T) {
			a := &Account{ID: 71, Platform: PlatformDeepseek, Type: kind, Credentials: map[string]any{}}
			normalizeDeepSeekAnonymousUser(a.Platform, a.Credentials)
			user := a.GetCredential("deepseek_harness_user_id")
			id, err := uuid.Parse(user)
			require.NoError(t, err)
			require.Equal(t, uuid.Version(4), id.Version())
			normalizeDeepSeekAnonymousUser(a.Platform, a.Credentials)
			require.Equal(t, user, a.GetCredential("deepseek_harness_user_id"))
			_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
			ctx = WithOutboundIdentityScope(ctx, nil)
			var session string
			for n := 0; n < 3; n++ {
				owner := a
				if n == 2 {
					copy := *a
					copy.ID = 72
					owner = &copy
				}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/anthropic/v1/messages", nil)
				require.NoError(t, err)
				for _, name := range []string{"X-Deepseek-Harness-User-Id", "X-Deepseek-Harness-Session-Id", "X-Deepseek-Harness-Compact"} {
					req.Header.Set(name, "attacker")
					req.Header[strings.ToLower(name)] = []string{"also-attacker"}
				}
				prepareAccountOutboundRequest(req, owner)
				for _, name := range []string{"x-deepseek-harness-user-id", "x-deepseek-harness-session-id", "x-deepseek-harness-compact"} {
					require.NotContains(t, req.Header, name, "raw duplicate header casing must be removed")
				}
				require.Equal(t, "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)", req.UserAgent())
				require.Equal(t, user, req.Header.Get("X-Deepseek-Harness-User-Id"))
				require.Empty(t, req.Header.Get("X-Deepseek-Harness-Compact"))
				got := req.Header.Get("X-Deepseek-Harness-Session-Id")
				require.True(t, strings.HasPrefix(got, "session-"), "official Harness session-<UUIDv4> contract")
				id, err := uuid.Parse(strings.TrimPrefix(got, "session-"))
				require.NoError(t, err)
				require.Equal(t, uuid.Version(4), id.Version())
				if n == 0 {
					session = got
				} else if n == 1 {
					require.Equal(t, session, got)
				} else {
					require.NotEqual(t, session, got)
				}
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deepseek.com/models", nil)
			req.Header.Set("X-Deepseek-Harness-User-Id", "attacker")
			prepareAccountOutboundRequest(req, a)
			require.Empty(t, req.Header.Get("X-Deepseek-Harness-User-Id"))
		})
	}
}

func TestOutboundHeaderRemovalCoversAllRawCasing(t *testing.T) {
	// Credential-owned declarations must not coexist with an inbound or generic
	// override using a different raw casing. Unrelated headers remain intact.
	for _, name := range []string{"Authorization", "X-Api-Key", "X-Coding-Plan-Api-Key", "X-Deepseek-Harness-User-Id"} {
		h := http.Header{name: {"canonical"}, strings.ToUpper(name): {"upper"}, strings.ToLower(name): {"lower"}, "Unrelated": {"keep"}}
		deleteHeaderAllForms(h, name)
		require.Equal(t, http.Header{"Unrelated": {"keep"}}, h)
	}
}

func TestKimiOfficialReasoningCatalog(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		reasoning *bool
		levels    []string
		def       string
	}{
		{`{"type":"only","effort":{"valid":["low","high"],"default":"high"}}`, boolPtrForAgentTest(true), []string{"low", "high"}, "high"},
		{`{"type":"both","effort":{"valid":["low","high"],"default":"medium"}}`, boolPtrForAgentTest(true), []string{"low", "high"}, ""},
		{`{"type":"none"}`, boolPtrForAgentTest(false), nil, ""},
		{`{"type":"both","effort":{"valid":["low",42,"high"],"default":"high"}}`, boolPtrForAgentTest(true), []string{"low", "high"}, "high"},
		{`{"type":"only","effort":{"valid":"high","default":"high"}}`, boolPtrForAgentTest(true), nil, ""},
		{`{"type":"both","effort":"unrecognized"}`, boolPtrForAgentTest(true), nil, ""},
		{`true`, boolPtrForAgentTest(true), nil, ""},
		{`false`, boolPtrForAgentTest(false), nil, ""},
		{`{"type":"future"}`, nil, nil, ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			body := []byte(`{"data":[{"id":"kimi-k3","reasoning":` + tc.raw + `}]}`)
			_, metadata, err := extractUpstreamModelCatalog(body, false)
			require.NoError(t, err)
			got := metadata["kimi-k3"]
			require.Equal(t, tc.reasoning, got.Reasoning)
			require.ElementsMatch(t, tc.levels, got.SupportedReasoningLevels)
			require.Equal(t, tc.def, got.DefaultReasoningLevel)
		})
	}
}
func boolPtrForAgentTest(b bool) *bool { return &b }

func TestKimiOfficialNoReasoningOverridesLegacyAndRegistry(t *testing.T) {
	// managed-kimi-code.ts: type=none is authoritative; public catalogs retain
	// the same invariant for false, regardless of stale effort/registry fields.
	for _, raw := range []string{`{"type":"none"}`, `false`} {
		t.Run(raw, func(t *testing.T) {
			_, catalog, err := extractUpstreamModelCatalog([]byte(`{"data":[{"id":"kimi-k3","reasoning":`+raw+`,"supportsThinking":true,"supported_reasoning_levels":["low","high"],"default_reasoning_level":"high"}]}`), false)
			require.NoError(t, err)
			primary := catalog["kimi-k3"]
			require.Equal(t, boolPtrForAgentTest(false), primary.Reasoning)
			require.Empty(t, primary.SupportedReasoningLevels)
			require.Empty(t, primary.DefaultReasoningLevel)
			merged, _ := mergeUpstreamModelMetadata(primary, UpstreamModelMetadata{
				Reasoning: boolPtrForAgentTest(true), SupportedReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high",
			})
			require.Equal(t, boolPtrForAgentTest(false), merged.Reasoning)
			require.Empty(t, merged.SupportedReasoningLevels)
			require.Empty(t, merged.DefaultReasoningLevel)
		})
	}
}

func TestKimiCodingMissingReasoningClearsStaleCapabilities(t *testing.T) {
	// kimi-code 67325c1b managed-kimi-code.ts: absent/null reasoning means
	// supportsThinkingType=no. Third-party metadata cannot revive old efforts.
	for _, tc := range []struct {
		name, kind, base string
		disabled         bool
	}{
		{"oauth", AccountTypeOAuth, "https://api.kimi.com/coding", true},
		{"coding-key", AccountTypeAPIKey, "https://api.kimi.com/coding", true},
		{"payg-key", AccountTypeAPIKey, "https://api.moonshot.ai/v1", false},
		{"compatible-key", AccountTypeAPIKey, "https://compatible.example/v1", false},
	} {
		for _, declaration := range []string{"", `,"reasoning":null`} {
			t.Run(tc.name+declaration, func(t *testing.T) {
				account := &Account{ID: 1, Platform: PlatformKimi, Type: tc.kind, Credentials: map[string]any{
					"api_key": "manual", "base_url": tc.base, "account_mode": AccountModeCoding,
				}}
				if tc.kind == AccountTypeOAuth {
					account.Credentials["oauth_provider"] = "kimi"
					account.Credentials["oauth_region"] = "cn"
					account.Credentials["access_token"] = "oauth-token"
					account.Credentials["refresh_token"] = "managed-refresh-token"
					account.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
				}
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"kimi-k3","limit":{"context":262144},"modalities":{"input":["text"]},"supported_reasoning_levels":["high"],"default_reasoning_level":"high"` + declaration + `}]}`))},
					{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"kimi":{"api":"https://api.kimi.com/coding","models":{"kimi-k3":{"id":"kimi-k3","reasoning":true,"limit":{"context":262144},"modalities":{"input":["text"]}}}}`))},
				}}
				repo := &upstreamModelMetadataRepoStub{}
				svc := &AccountTestService{httpUpstream: upstream, accountRepo: repo, cfg: &config.Config{}, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
				require.NoError(t, err)
				got := catalog.Metadata["kimi-k3"]
				require.Equal(t, boolPtrForAgentTest(!tc.disabled), got.Reasoning)
				var levels []string
				def := ""
				if !tc.disabled {
					levels, def = []string{"high"}, "high"
				}
				require.Equal(t, levels, got.SupportedReasoningLevels)
				require.Equal(t, def, got.DefaultReasoningLevel)
				require.NotNil(t, repo.updates)
				snapshot := account.GetUpstreamModelMetadataSnapshot()
				require.NotNil(t, snapshot)
				require.Equal(t, boolPtrForAgentTest(!tc.disabled), snapshot.Models["kimi-k3"].Reasoning)
				require.Equal(t, levels, snapshot.Models["kimi-k3"].SupportedReasoningLevels)
			})
		}
	}
}

func TestDeepSeekHarnessCatalogVersionAndMessagesGates(t *testing.T) {
	// listing.ts + resolve-catalog.ts: unknown models require a readable minimum;
	// only supported text-producing Messages capabilities can be offered.
	raw := []byte(`{"data":[
 {"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"context_window":100000,"max_output_tokens":8192},
 {"id":"new-model","client_requirements":{"deepseek-harness":{"min_version":"0.2.1-rc.2"}},"api_capabilities":{"anthropic_messages":{}},"input_modalities":["text","image"]},
 {"id":"too-new","client_requirements":{"deepseek-harness":{"min_version":"0.2.1"}},"api_capabilities":{"anthropic_messages":{}}},
 {"id":"unknown","api_capabilities":{"anthropic_messages":{}}},
 {"id":"deepseek-v4-pro","client_requirements":null,"api_capabilities":{"anthropic_messages":{}}},
 {"id":"bad-capacity","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"anthropic_messages":{}},"context_window":-1},
 {"id":"no-messages","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"chat_completions":{}}}
 ]}`)
	filtered, metadata, err := extractDeepSeekHarnessCatalog(raw, "0.2.1-rc.2")
	require.NoError(t, err)
	ids, err := extractUpstreamModelIDs(filtered)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"deepseek-flash", "new-model"}, ids)
	require.EqualValues(t, 100000, metadata["deepseek-flash"].ContextWindow)
	require.EqualValues(t, 8192, metadata["deepseek-flash"].MaxOutputTokens)
	require.Equal(t, []string{"text", "image"}, metadata["new-model"].InputModalities)
	filtered, _, err = extractDeepSeekHarnessCatalog(raw, "0.2.0-rc.2")
	require.NoError(t, err)
	ids, err = extractUpstreamModelIDs(filtered)
	require.NoError(t, err)
	require.Equal(t, []string{"deepseek-flash"}, ids)
}

func TestDeepSeekHarnessListingSkipsUnknownRowsAndModalities(t *testing.T) {
	// listing.ts accepts mixed arrays, skips unreadable rows, keeps the first
	// model ID and exact supported modalities. npm semver requires X.Y.Z.
	raw := []byte(`{"data":[null,42,"future",[],
	 {"id":"deepseek-flash","api_capabilities":{"anthropic_messages":{}},"input_modalities":[false,"image","future","text","image"],"output_modalities":[1,"text"]},
	 {"id":"deepseek-flash","api_capabilities":{}},
	 {"id":"partial-version","client_requirements":{"deepseek-harness":{"min_version":"0.2"}},"api_capabilities":{"anthropic_messages":{}}},
	 {"id":"prefixed-version","client_requirements":{"deepseek-harness":{"min_version":"v0.2.0"}},"api_capabilities":{"anthropic_messages":{}}},
	 {"id":"uppercase-only","client_requirements":{"deepseek-harness":{"min_version":"0.2.0"}},"api_capabilities":{"anthropic_messages":{}},"input_modalities":["TEXT"]}
	]}`)
	filtered, metadata, err := extractDeepSeekHarnessCatalog(raw, "0.2.1-rc.2")
	require.NoError(t, err)
	ids, err := extractUpstreamModelIDs(filtered)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"deepseek-flash", "prefixed-version"}, ids)
	require.Equal(t, []string{"image", "text"}, metadata["deepseek-flash"].InputModalities)
	_, _, err = extractDeepSeekHarnessCatalog([]byte(`{"data":[null,42]}`), "0.2.1-rc.2")
	require.Error(t, err, "a listing without a readable model ID is invalid")
}

func TestDomesticOAuthNativeModelDiscovery(t *testing.T) {
	for _, tc := range []struct{ platform, region, target, header, auth, ua string }{
		{"deepseek", "cn", "https://api.deepseek.com/models", "x-dsh-auth-token", "secret", "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)"},
		{"kimi", "cn", "https://api.kimi.com/coding/v1/models", "Authorization", "Bearer secret", "kimi-code-cli/2.3.1"},
		{"kimi", "global", "https://api.kimi.ai/coding/v1/models", "Authorization", "Bearer secret", "kimi-code-cli/2.3.1"},
	} {
		t.Run(tc.platform+tc.region, func(t *testing.T) {
			a := &Account{ID: 1, Platform: tc.platform, Type: "oauth", Credentials: map[string]any{"oauth_provider": tc.platform, "oauth_region": tc.region, "access_token": "secret", "base_url": "https://attacker.test"}}
			if tc.platform == PlatformKimi {
				a.Credentials["refresh_token"] = "managed-refresh-token"
				a.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			}
			_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
			req, err := svc.buildUpstreamModelsRequest(ctx, a)
			require.NoError(t, err)
			_, err = svc.doUpstreamModelsRequest(req, a)
			require.NoError(t, err)
			require.Equal(t, tc.target, upstream.lastReq.URL.String())
			require.Equal(t, tc.auth, upstream.lastReq.Header.Get(tc.header))
			require.Contains(t, upstream.lastReq.UserAgent(), tc.ua)
			require.Empty(t, upstream.lastReq.Header.Get("x-api-key"))
			require.Empty(t, upstream.lastReq.Header.Get("X-Deepseek-Harness-Session-Id"))
		})
	}
}

func TestGrokAPIKeyRejectsForeignIdentityCandidates(t *testing.T) {
	settings := emptyOutboundIdentitySettings()
	settings.Defaults["grok:apikey"] = "claude"
	settings.Profiles["grok"] = OutboundIdentitySelection{Preset: "grok", Version: "1.0.45"}
	svc := NewSettingService(&outboundIdentityTestRepo{values: map[string]string{}}, nil)
	require.Error(t, svc.SetOutboundIdentitySettings(context.Background(), settings))
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	svc = NewSettingService(&outboundIdentityTestRepo{values: map[string]string{SettingKeyOutboundIdentity: string(raw)}}, nil)
	ctx := outboundidentity.WithResolver(context.Background(), svc.resolveOutboundIdentityKey)
	a := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "claude", Version: "3.9.1"}}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.ai/v1/chat/completions", nil)
	prepareAccountOutboundRequest(req, a)
	require.Contains(t, req.UserAgent(), "grok-shell/1.0.45")
	require.Equal(t, "1.0.45", req.Header.Get("X-Grok-Client-Version"))
	require.Empty(t, req.Header.Get("X-Grok-Client-Mode"))
}
