//go:build unit || !integration

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/gemini"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiNativeModelsUsesAccountMappings(t *testing.T) {
	for _, tt := range []struct {
		name                     string
		forced, mixed, allowlist bool
		status                   int
	}{
		{"mixed", false, true, false, 200},
		{"mixed allowlist", false, true, true, 200},
		{"disabled mixed", false, false, false, 503},
		{"forced without mixed opt-in", true, false, false, 200},
		{"forced allowlist", true, false, true, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			groupID := int64(45)
			repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				groupID: {{ID: 1, Platform: service.PlatformAntigravity,
					Extra:       map[string]any{"mixed_scheduling": tt.mixed},
					Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high", "claude-custom": "claude-sonnet-4-6"}}}},
			}}}
			h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, nil, nil, nil)}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformGemini,
				ModelAllowlist: service.GroupModelAllowlist{Enabled: tt.allowlist, Models: []string{"gemini-synced-custom"}},
			}})
			if tt.forced {
				c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)
			}
			prepareGatewayMetadataTest(h, c)
			h.GeminiV1BetaListModels(c)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.status != 200 {
				return
			}
			var got gemini.ModelsListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			names := []string{}
			for _, model := range got.Models {
				names = append(names, model.Name)
				require.Contains(t, model.SupportedGenerationMethods, "generateContent")
			}
			require.Contains(t, names, "models/gemini-synced-custom")
			require.NotContains(t, names, "models/claude-custom")
			require.NotContains(t, names, "models/gemini-2.0-flash")
			if tt.allowlist {
				require.Equal(t, []string{"models/gemini-synced-custom"}, names)
			}
		})
	}
}

func TestAppendUpstreamGeminiModelsPreservesMetadata(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}}],"nextPageToken":"next","other":42}`)
	extra := []gemini.Model{gemini.FallbackModel("gemini-native"), gemini.FallbackModel("gemini-synced-custom"), gemini.FallbackModel("gemini-synced-custom")}
	merged, ok := appendUpstreamGeminiModels(body, extra)
	require.True(t, ok)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-native","inputTokenLimit":123,"custom":{"a":true}},{"name":"models/gemini-synced-custom","supportedGenerationMethods":["generateContent","streamGenerateContent"]}],"nextPageToken":"next","other":42}`, string(merged))
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(merged, service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-synced-*"}})
	require.True(t, ok)
	require.True(t, dropped)
	require.NotContains(t, string(filtered), "gemini-native")
	require.Contains(t, string(filtered), "gemini-synced-custom")
	for _, invalid := range []string{`null`, `not-json`, `{"error":"bad"}`, `{"models":{}}`} {
		got, ok := appendUpstreamGeminiModels([]byte(invalid), extra)
		require.False(t, ok)
		require.Equal(t, invalid, string(got))
	}
}

// Exercise the real handler's native-upstream branch, including scope fallback.
type geminiMixedModelsUpstream struct {
	service.HTTPUpstream
	status int
	body   string
}

func (u *geminiMixedModelsUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"native-id"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func TestGeminiNativeModelsMergesNativeUpstream(t *testing.T) {
	for _, tt := range []struct {
		name           string
		status         int
		body           string
		expectedNative string
	}{
		{"native", 200, `{"models":[{"name":"models/gemini-native","inputTokenLimit":123}],"nextPageToken":"next"}`, "models/gemini-native"},
		{"scope fallback", 403, `{"error":"insufficient authentication scopes"}`, "models/gemini-3.5-flash"},
		{"upstream error", 429, `{"error":"rate limited"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := int64(46)
			repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{id: {
				{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}},
				{ID: 2, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high"}}},
			}}}}
			upstream := &geminiMixedModelsUpstream{status: tt.status, body: tt.body}
			h := &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, nil, nil, upstream, nil, &config.Config{})}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformGemini}})
			prepareGatewayMetadataTest(h, c)
			h.GeminiV1BetaListModels(c)
			if tt.status == 429 {
				require.Equal(t, 429, rec.Code)
				require.JSONEq(t, tt.body, rec.Body.String())
				return
			}
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.expectedNative)
			require.Contains(t, rec.Body.String(), "models/gemini-synced-custom")
			if tt.status == 200 {
				require.Contains(t, rec.Body.String(), `"inputTokenLimit":123`)
				require.Contains(t, rec.Body.String(), `"nextPageToken":"next"`)
				require.Equal(t, "native-id", rec.Header().Get("X-Request-Id"))
			}
		})
	}
}

type geminiModelsUpstreamCounter struct {
	service.HTTPUpstream
	calls   int
	respond func(*http.Request) (int, string)
}

func (u *geminiModelsUpstreamCounter) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	status, body := http.StatusOK, `{"models":[{"name":"gemini-4-argon-high"}]}`
	if u.respond != nil {
		status, body = u.respond(req)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

type geminiDeclaredModelsFixture struct {
	t        *testing.T
	groupID  int64
	handler  *GatewayHandler
	upstream *geminiModelsUpstreamCounter
}

func newGeminiDeclaredModelsFixture(t *testing.T, accounts []service.Account) *geminiDeclaredModelsFixture {
	groupID := int64(47)
	repo := &geminiAllowlistAccountRepoStub{gatewayModelsAccountRepoStub: gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: accounts}}}
	upstream := &geminiModelsUpstreamCounter{}
	return &geminiDeclaredModelsFixture{
		t:        t,
		groupID:  groupID,
		handler:  &GatewayHandler{geminiCompatService: service.NewGeminiMessagesCompatService(repo, nil, nil, nil, service.NewGeminiTokenProvider(nil, nil, nil), nil, upstream, nil, &config.Config{})},
		upstream: upstream,
	}
}

func (f *geminiDeclaredModelsFixture) context(path, model string, allowlist service.GroupModelAllowlist) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	if model != "" {
		c.Params = gin.Params{{Key: "model", Value: model}}
	}
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{GroupID: &f.groupID, Group: &service.Group{ID: f.groupID, Platform: service.PlatformGemini, ModelAllowlist: allowlist}})
	prepareGatewayMetadataTest(f.handler, c)
	return c, rec
}

func (f *geminiDeclaredModelsFixture) list(allowlist service.GroupModelAllowlist) []string {
	c, rec := f.context("/v1beta/models", "", allowlist)
	f.handler.GeminiV1BetaListModels(c)
	require.Equal(f.t, http.StatusOK, rec.Code, rec.Body.String())
	var got gemini.ModelsListResponse
	require.NoError(f.t, json.Unmarshal(rec.Body.Bytes(), &got))
	names := []string{}
	for _, model := range got.Models {
		names = append(names, model.Name)
	}
	return names
}

func (f *geminiDeclaredModelsFixture) get(model string) *httptest.ResponseRecorder {
	c, rec := f.context("/v1beta/models/"+model, model, service.GroupModelAllowlist{})
	f.handler.GeminiV1BetaGetModel(c)
	return rec
}

// 分组内 Gemini 账号都声明了 model_mapping 时，列表与详情都按声明的模型应答，不透传选中账号的上游列表。
func TestGeminiNativeModelsUsesDeclaredMappings(t *testing.T) {
	f := newGeminiDeclaredModelsFixture(t, []service.Account{
		{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Priority: 10, Credentials: map[string]any{"api_key": "a", "model_mapping": map[string]any{"gemini-4-argon": "gemini-4-argon"}}},
		{ID: 2, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Priority: 11, Credentials: map[string]any{"api_key": "b", "model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash", "gemini-3.1-pro-preview": "gemini-3.1-pro-preview", "gemini-3.8-*": "gemini-3.8-flash"}}},
		{ID: 3, Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-synced-custom": "gemini-3.8-flash-high"}}},
	})

	names := f.list(service.GroupModelAllowlist{})
	require.Subset(t, names, []string{"models/gemini-2.5-flash", "models/gemini-3.1-pro-preview", "models/gemini-3.8-*", "models/gemini-4-argon", "models/gemini-synced-custom"})
	require.NotContains(t, names, "models/gemini-4-argon-high")

	// 白名单与 /v1/models 同口径：通配符映射键展开为白名单中的具体模型。
	require.Equal(t, []string{"models/gemini-2.5-flash", "models/gemini-3.8-flash", "models/gemini-synced-custom"},
		f.list(service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-2.5-flash", "gemini-3.8-flash", "gemini-synced-custom"}}))

	for model, status := range map[string]int{
		"gemini-4-argon":                     http.StatusOK,
		"gemini-3.8-flash":                   http.StatusOK,
		"gemini-3.1-pro-preview-customtools": http.StatusOK,
		"gemini-synced-custom":               http.StatusOK,
		"gemini-4-argon-high":                http.StatusNotFound,
	} {
		rec := f.get(model)
		require.Equal(t, status, rec.Code, model+": "+rec.Body.String())
		if status == http.StatusOK {
			require.JSONEq(t, `{"name":"models/`+model+`","supportedGenerationMethods":["generateContent","streamGenerateContent"]}`, rec.Body.String())
		} else {
			require.Contains(t, rec.Body.String(), "models/"+model+" is not found")
		}
	}
	require.Zero(t, f.upstream.calls)
}

// 分组内有 Gemini 账号未配置映射（接受任意模型）时，模型接口仍由选中账号的上游应答。
func TestGeminiNativeModelsUnmappedAccountKeepsUpstream(t *testing.T) {
	f := newGeminiDeclaredModelsFixture(t, []service.Account{
		{ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Priority: 10, Credentials: map[string]any{"api_key": "a", "model_mapping": map[string]any{"gemini-4-argon": "gemini-4-argon"}}},
		{ID: 2, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Priority: 11, Credentials: map[string]any{"api_key": "b"}},
	})

	require.Equal(t, []string{"gemini-4-argon-high"}, f.list(service.GroupModelAllowlist{}))
	rec := f.get("gemini-2.5-pro")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 2, f.upstream.calls)
}

func TestGeminiNativeModelsOAuthTranslationAliasesStayUnavailable(t *testing.T) {
	for _, oauthType := range []string{"ai_studio", "code_assist", "google_one"} {
		t.Run(oauthType, func(t *testing.T) {
			f := newGeminiDeclaredModelsFixture(t, []service.Account{{
				ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"oauth_type": oauthType, "model_mapping": map[string]any{
					"customer-alias": "gemini-2.5-flash", "gemini-2.5-flash": "gemini-2.5-flash",
				}},
			}})

			require.Equal(t, []string{"models/gemini-2.5-flash"}, f.list(service.GroupModelAllowlist{}), "OAuth discovery must preserve native names without promising translated aliases")
			alias := f.get("customer-alias")
			require.Equal(t, http.StatusNotFound, alias.Code, "OAuth ForwardNative does not translate account mapping aliases")
			require.Contains(t, alias.Body.String(), "models/customer-alias is not found")
			native := f.get("gemini-2.5-flash")
			require.Equal(t, http.StatusOK, native.Code, native.Body.String())
			require.JSONEq(t, `{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent","streamGenerateContent"]}`, native.Body.String())
			require.Zero(t, f.upstream.calls)
		})
	}
}

func TestGeminiNativeModelsOAuthAliasOnlyMappingKeepsNativeDiscovery(t *testing.T) {
	f := newGeminiDeclaredModelsFixture(t, []service.Account{{
		ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "ai_studio", "access_token": "oauth-discovery-token", "expires_at": "2099-01-01T00:00:00Z",
			"model_mapping": map[string]any{"customer-alias": "gemini-2.5-flash"},
		},
	}})
	const listBody = `{"models":[{"name":"models/gemini-2.5-flash","inputTokenLimit":123}],"nextPageToken":"native-page"}`
	const nativeBody = `{"name":"models/gemini-2.5-flash","inputTokenLimit":123,"displayName":"Native Gemini"}`
	const missingBody = `{"error":{"code":404,"message":"unknown native model","status":"NOT_FOUND"}}`
	var paths []string
	f.upstream.respond = func(req *http.Request) (int, string) {
		paths = append(paths, req.URL.Path)
		require.Equal(t, "Bearer oauth-discovery-token", req.Header.Get("Authorization"))
		switch req.URL.Path {
		case "/v1beta/models":
			return http.StatusOK, listBody
		case "/v1beta/models/gemini-2.5-flash":
			return http.StatusOK, nativeBody
		default:
			return http.StatusNotFound, missingBody
		}
	}

	c, listed := f.context("/v1beta/models", "", service.GroupModelAllowlist{})
	f.handler.GeminiV1BetaListModels(c)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.JSONEq(t, listBody, listed.Body.String(), "OAuth discovery must retain native model metadata")
	alias := f.get("customer-alias")
	require.Equal(t, http.StatusNotFound, alias.Code, "an OAuth-only translated alias must not acquire a synthetic successful descriptor")
	require.JSONEq(t, missingBody, alias.Body.String())
	native := f.get("gemini-2.5-flash")
	require.Equal(t, http.StatusOK, native.Code, native.Body.String())
	require.JSONEq(t, nativeBody, native.Body.String())
	require.Equal(t, []string{"/v1beta/models", "/v1beta/models/customer-alias", "/v1beta/models/gemini-2.5-flash"}, paths)
	require.Equal(t, 3, f.upstream.calls)
}

func TestGeminiNativeModelsGoogleOneKeepsNativeWhitelist(t *testing.T) {
	f := newGeminiDeclaredModelsFixture(t, []service.Account{{
		ID: 1, Platform: service.PlatformGemini, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"oauth_type": "google_one"},
	}})

	require.Equal(t, []string{"models/gemini-2.0-flash", "models/gemini-2.5-flash", "models/gemini-2.5-pro"}, f.list(service.GroupModelAllowlist{}))
	require.Equal(t, http.StatusOK, f.get("gemini-2.5-pro").Code)
	require.Equal(t, http.StatusNotFound, f.get("gemini-3.5-flash").Code)
	require.Zero(t, f.upstream.calls)
}

func TestGeminiNativeModelsMappedCredentialTypesKeepDeclaredAliases(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeAPIKey, service.AccountTypeServiceAccount} {
		t.Run(accountType, func(t *testing.T) {
			f := newGeminiDeclaredModelsFixture(t, []service.Account{{
				ID: 1, Platform: service.PlatformGemini, Type: accountType,
				Credentials: map[string]any{"model_mapping": map[string]any{"customer-alias": "gemini-2.5-flash"}},
			}})

			require.Equal(t, []string{"models/customer-alias"}, f.list(service.GroupModelAllowlist{}))
			rec := f.get("customer-alias")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.JSONEq(t, `{"name":"models/customer-alias","supportedGenerationMethods":["generateContent","streamGenerateContent"]}`, rec.Body.String())
			require.Equal(t, http.StatusNotFound, f.get("gemini-2.5-flash").Code)
			require.Zero(t, f.upstream.calls)
		})
	}
}
