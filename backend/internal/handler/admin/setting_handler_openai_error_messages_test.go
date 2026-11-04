//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsOpenAIErrorMessagesToggleAndPartialUpdate(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	s := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	h := NewSettingHandler(s, nil, nil, nil, nil, nil, nil)
	update := func(body string, status int) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.UpdateSettings(c)
		require.Equal(t, status, rec.Code, rec.Body.String())
	}
	require.False(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	update(`{"openai_error_messages_normalized":true}`, http.StatusOK)
	require.True(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	require.False(t, s.CodexSafetyBufferingSuppressed(context.Background()))
	update(`{"openai_codex_safety_buffering_suppressed":true}`, http.StatusOK)
	require.True(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	update(`{"openai_codex_ws_enabled":false}`, http.StatusOK)
	require.NotContains(t, repo.lastUpdates, service.SettingKeyOpenAIErrorMessagesNormalized, "unrelated saves must not overwrite a concurrent toggle")
	require.True(t, s.OpenAIErrorMessagesNormalized(context.Background()), "omission must retain the enabled preference")
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAIErrorMessagesNormalized])
	update(`{"openai_error_messages_normalized":null}`, http.StatusOK)
	require.NotContains(t, repo.lastUpdates, service.SettingKeyOpenAIErrorMessagesNormalized)
	require.True(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	update(`{"openai_error_messages_normalized":"false"}`, http.StatusBadRequest)
	require.True(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	update(`{"openai_error_messages_normalized":false}`, http.StatusOK)
	require.False(t, s.OpenAIErrorMessagesNormalized(context.Background()))
	require.True(t, s.CodexSafetyBufferingSuppressed(context.Background()))
}
