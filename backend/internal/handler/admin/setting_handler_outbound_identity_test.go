//go:build unit || !integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type outboundIdentitySettingsRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *outboundIdentitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *outboundIdentitySettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := map[string]string{}
	for _, key := range keys {
		if v, ok := r.values[key]; ok {
			result[key] = v
		}
	}
	return result, nil
}
func (r *outboundIdentitySettingsRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func TestOutboundIdentitySettingsAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &outboundIdentitySettingsRepo{values: map[string]string{}}
	h := &SettingHandler{settingService: service.NewSettingService(repo, nil)}
	router := gin.New()
	router.PUT("/identity", h.UpdateOutboundIdentity)
	router.GET("/identity", h.GetOutboundIdentity)
	router.POST("/preview", h.PreviewOutboundIdentity)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	saved := request(http.MethodPut, "/identity", `{"profiles":{"grok":{"version":"3.9.1"}},"defaults":{"gemini:service_account":"grok"}}`)
	require.Equal(t, http.StatusOK, saved.Code)
	loaded := request(http.MethodGet, "/identity", "")
	require.Equal(t, "3.9.1", gjson.Get(loaded.Body.String(), "data.settings.profiles.grok.version").String())
	preview := request(http.MethodPost, "/preview", `{"platform":"gemini","type":"service_account","credentials":{"access_token":"must-not-return"}}`)
	require.Equal(t, http.StatusOK, preview.Code)
	require.Equal(t, "grok", gjson.Get(preview.Body.String(), "data.preset").String())
	require.Equal(t, "3.9.1", gjson.Get(preview.Body.String(), "data.version").String())
	require.NotContains(t, preview.Body.String(), "must-not-return")
	codexBody, err := json.Marshal(map[string]string{"platform": "openai", "type": "apikey", "user_agent": service.DefaultOpenAICodexUserAgent})
	require.NoError(t, err)
	codexPreview := request(http.MethodPost, "/preview", string(codexBody))
	require.Equal(t, http.StatusOK, codexPreview.Code)
	require.Equal(t, "account", gjson.Get(codexPreview.Body.String(), "data.source").String())
	for _, body := range []string{`{"platform":"unknown","type":"oauth"}`, `{"platform":"gemini","type":"oauth","selection":{"preset":"grok"}}`, `{"platform":"anthropic","type":"apikey","selection":{"preset":"claude","version":"invalid"}}`} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/preview", body).Code)
	}
	// MiniMax is an enumerated versionless family: the preview reports the bare
	// product token with no client version, and both an invented version and a
	// foreign User-Agent are rejected before saving.
	minimaxPreview := request(http.MethodPost, "/preview", `{"platform":"minimax","type":"apikey","selection":{"preset":"minimax"}}`)
	require.Equal(t, http.StatusOK, minimaxPreview.Code)
	require.Equal(t, "minimax", gjson.Get(minimaxPreview.Body.String(), "data.preset").String())
	require.Equal(t, "MiniMaxAgent", gjson.Get(minimaxPreview.Body.String(), "data.user_agent").String())
	require.True(t, gjson.Get(minimaxPreview.Body.String(), "data.version").Exists(), "the version key stays present and empty rather than being dropped")
	require.Equal(t, "", gjson.Get(minimaxPreview.Body.String(), "data.version").String())
	require.Equal(t, "MiniMaxAgent", gjson.Get(minimaxPreview.Body.String(), `data.headers.User-Agent`).String())
	for _, body := range []string{
		`{"platform":"minimax","type":"apikey","selection":{"preset":"minimax","version":"0.6.2"}}`,
		`{"platform":"minimax","type":"apikey","selection":{"preset":"minimax","user_agent":"MiniMaxAgent/0.6.2"}}`,
	} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/preview", body).Code, body)
	}
	minimaxSaved := request(http.MethodPut, "/identity", `{"profiles":{"grok":{"version":"3.9.1"},"minimax":{"preset":"minimax"}},"defaults":{"gemini:service_account":"grok","minimax:apikey":"minimax"}}`)
	require.Equal(t, http.StatusOK, minimaxSaved.Code)
	require.Equal(t, "minimax", gjson.Get(minimaxSaved.Body.String(), `data.settings.profiles.minimax.preset`).String())
	require.Equal(t, "MiniMaxAgent", gjson.Get(minimaxSaved.Body.String(), `data.effective.#(preset=="minimax").user_agent`).String())
	require.Equal(t, "", gjson.Get(minimaxSaved.Body.String(), `data.effective.#(preset=="minimax").version`).String())
	require.Contains(t, minimaxSaved.Body.String(), `"minimax:apikey":"minimax"`)
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/identity", `{"profiles":{"minimax":{"preset":"minimax","version":"0.6.2"}}}`).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/identity", `{"profiles":{"codex":{"version":"3.9.1"}}}`).Code)
	require.Equal(t, "3.9.1", gjson.Get(repo.values[service.SettingKeyOutboundIdentity], "profiles.grok.version").String(), "rejected updates preserve the saved profile")
	require.Equal(t, "minimax", gjson.Get(repo.values[service.SettingKeyOutboundIdentity], "profiles.minimax.preset").String())
	// ZCode is a versioned client family: the preview reports the pinned product
	// token with its client version, renders only the User-Agent declaration, and
	// rejects a User-Agent from another family.
	zcodePreview := request(http.MethodPost, "/preview", `{"platform":"zhipu","type":"apikey","selection":{"preset":"zcode"}}`)
	require.Equal(t, http.StatusOK, zcodePreview.Code)
	require.Equal(t, "zcode", gjson.Get(zcodePreview.Body.String(), "data.preset").String())
	require.Equal(t, "ZCode/3.14.3", gjson.Get(zcodePreview.Body.String(), "data.user_agent").String())
	require.Equal(t, "ZCode", gjson.Get(zcodePreview.Body.String(), "data.originator").String())
	require.Equal(t, "3.14.3", gjson.Get(zcodePreview.Body.String(), "data.version").String())
	require.Equal(t, "ZCode/3.14.3", gjson.Get(zcodePreview.Body.String(), `data.headers.User-Agent`).String())
	require.False(t, gjson.Get(zcodePreview.Body.String(), `data.headers.Originator`).Exists(), "ZCode declares no Originator header")
	require.False(t, gjson.Get(zcodePreview.Body.String(), `data.headers.Version`).Exists(), "ZCode declares no standalone version header")
	zhipuOAuthPreview := request(http.MethodPost, "/preview", `{"platform":"zhipu","type":"oauth","selection":{"preset":"codex"}}`)
	require.Equal(t, http.StatusBadRequest, zhipuOAuthPreview.Code, "OAuth accounts must retain their native client family")
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, "/preview", `{"platform":"zhipu","type":"apikey","selection":{"preset":"zcode","user_agent":"codex_cli_rs/0.158.0"}}`).Code)
	zcodeSaved := request(http.MethodPut, "/identity", `{"profiles":{"grok":{"version":"3.9.1"},"zcode":{"preset":"zcode","version":"0.16.9"}},"defaults":{"gemini:service_account":"grok","zhipu:apikey":"zcode"}}`)
	require.Equal(t, http.StatusOK, zcodeSaved.Code)
	require.Equal(t, "zcode", gjson.Get(zcodeSaved.Body.String(), `data.settings.profiles.zcode.preset`).String())
	require.Equal(t, "ZCode/0.16.9", gjson.Get(zcodeSaved.Body.String(), `data.effective.#(preset=="zcode").user_agent`).String())
	require.Contains(t, zcodeSaved.Body.String(), `"zhipu:apikey":"zcode"`)
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/identity", `{"profiles":{"zcode":{"preset":"zcode","user_agent":"codex_cli_rs/0.158.0"}}}`).Code)
	// Kimi Code declares one product token plus a device description set. The
	// preview reports the complete declaration block, the settings view reports
	// which declarations accept a configured value, and no configuration tier
	// can supply a derived or pinned declaration.
	kimiPreview := request(http.MethodPost, "/preview", `{"platform":"kimi","type":"apikey","selection":{"preset":"kimi"}}`)
	require.Equal(t, http.StatusOK, kimiPreview.Code)
	require.Equal(t, "kimi", gjson.Get(kimiPreview.Body.String(), "data.preset").String())
	require.Equal(t, "kimi-code-cli/2.1.1", gjson.Get(kimiPreview.Body.String(), "data.user_agent").String())
	require.Equal(t, "2.1.1", gjson.Get(kimiPreview.Body.String(), "data.version").String())
	require.Equal(t, "kimi_code_cli", gjson.Get(kimiPreview.Body.String(), `data.headers.X-Msh-Platform`).String())
	require.Equal(t, "2.1.1", gjson.Get(kimiPreview.Body.String(), `data.headers.X-Msh-Version`).String())
	require.False(t, gjson.Get(kimiPreview.Body.String(), `data.headers.Originator`).Exists(), "Kimi Code declares no Originator header")
	require.False(t, gjson.Get(kimiPreview.Body.String(), `data.headers.Version`).Exists(), "Kimi Code declares no standalone version header")
	for _, name := range []string{"X-Msh-Device-Name", "X-Msh-Device-Model", "X-Msh-Os-Version", "X-Msh-Device-Id"} {
		require.NotEmpty(t, gjson.Get(kimiPreview.Body.String(), "data.headers."+name).String(), name)
	}
	loadedKimi := request(http.MethodGet, "/identity", "")
	kimiDeclarations := gjson.Get(loadedKimi.Body.String(), `data.declarations.#(preset=="kimi").headers`)
	require.Len(t, kimiDeclarations.Array(), 7)
	for _, header := range kimiDeclarations.Array() {
		name := header.Get("name").String()
		require.Equal(t, header.Get("class").String() == "runtime", header.Get("editable").Bool(), name)
		require.NotEmpty(t, header.Get("builtin").String(), name)
	}
	kimiSaved := request(http.MethodPut, "/identity", `{"profiles":{"grok":{"version":"3.9.1"}},"runtime":{"kimi":{"X-Msh-Device-Name":"kimi-gateway-2","X-Msh-Device-Id":"22222222-2222-4222-8222-222222222222"}}}`)
	require.Equal(t, http.StatusOK, kimiSaved.Code)
	require.Equal(t, "kimi-gateway-2", gjson.Get(kimiSaved.Body.String(), `data.settings.runtime.kimi.X-Msh-Device-Name`).String())
	require.Equal(t, "kimi-gateway-2", gjson.Get(kimiSaved.Body.String(), `data.effective.#(preset=="kimi").headers.X-Msh-Device-Name`).String())
	require.Equal(t, "global", gjson.Get(kimiSaved.Body.String(), `data.effective.#(preset=="kimi").source`).String())
	for _, body := range []string{
		`{"runtime":{"kimi":{"X-Msh-Platform":"kimi_code_desktop"}}}`,
		`{"runtime":{"kimi":{"X-Msh-Version":"9.9.9"}}}`,
		`{"runtime":{"kimi":{"X-Msh-Device-Id":"not-a-uuid"}}}`,
		`{"runtime":{"kimi":{"X-Msh-Tool-Call-Id":"request-state"}}}`,
		`{"runtime":{"unknown":{"X-Msh-Device-Name":"x"}}}`,
		`{"profiles":{"kimi":{"preset":"kimi","headers":{"X-Msh-Platform":"kimi_code_desktop"}}}}`,
	} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/identity", body).Code, body)
	}
	require.Equal(t, "22222222-2222-4222-8222-222222222222", gjson.Get(repo.values[service.SettingKeyOutboundIdentity], "runtime.kimi.X-Msh-Device-Id").String(), "rejected updates preserve the saved runtime declarations")
	require.Equal(t, "kimi-gateway-2", gjson.Get(repo.values[service.SettingKeyOutboundIdentity], "runtime.kimi.X-Msh-Device-Name").String())
}
