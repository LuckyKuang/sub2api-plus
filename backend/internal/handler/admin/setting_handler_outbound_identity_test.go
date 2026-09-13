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
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, "/identity", `{"profiles":{"codex":{"version":"3.9.1"}}}`).Code)
	require.Equal(t, "3.9.1", gjson.Get(repo.values[service.SettingKeyOutboundIdentity], "profiles.grok.version").String(), "rejected updates preserve the saved profile")
}
