//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func codexPublicErrorHandler(t *testing.T) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	handler := NewSettingHandler(service.NewSettingService(repo, nil), nil, nil, nil, nil, nil, nil)
	return handler, repo
}

func codexPublicErrorRequest(t *testing.T, method, body string, handler func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/v1/admin/settings/codex-error-messages", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func TestCodexPublicErrorMessagesAdminContract(t *testing.T) {
	handler, _ := codexPublicErrorHandler(t)

	recorder := codexPublicErrorRequest(t, http.MethodGet, "", handler.GetCodexErrorMessages)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int                              `json:"code"`
		Data service.CodexPublicErrorSnapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Len(t, envelope.Data.Items, 76)
	require.False(t, envelope.Data.CustomEnabled)
	revision := envelope.Data.Revision

	body := `{"revision":"` + revision + `","custom_enabled":true,"changes":{"rate_limit_exceeded":"请求较多，请稍后重试。"}}`
	recorder = codexPublicErrorRequest(t, http.MethodPatch, body, handler.PatchCodexErrorMessages)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.CustomEnabled)
	require.NotEqual(t, revision, envelope.Data.Revision)
	var rateLimitItem service.CodexPublicErrorItem
	for _, item := range envelope.Data.Items {
		if item.Key == "rate_limit_exceeded" {
			rateLimitItem = item
		}
	}
	require.NotNil(t, rateLimitItem.CustomMessage)
	require.Equal(t, "请求较多，请稍后重试。", *rateLimitItem.CustomMessage)

	// A stale revision cannot overwrite the accepted text.
	recorder = codexPublicErrorRequest(t, http.MethodPatch, `{"revision":"`+revision+`","changes":{"rate_limit_exceeded":null}}`, handler.PatchCodexErrorMessages)
	require.Equal(t, http.StatusConflict, recorder.Code)

	// Unknown keys and multi-line text are client errors.
	recorder = codexPublicErrorRequest(t, http.MethodPatch, `{"revision":"`+envelope.Data.Revision+`","changes":{"unknown_key":"x"}}`, handler.PatchCodexErrorMessages)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	recorder = codexPublicErrorRequest(t, http.MethodPatch, `{"revision":"`+envelope.Data.Revision+`","changes":{"rate_limit_exceeded":"one\ntwo"}}`, handler.PatchCodexErrorMessages)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
