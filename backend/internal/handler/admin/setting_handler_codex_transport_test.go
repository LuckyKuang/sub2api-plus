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

// User requirement: independent persistent switches; partial updates retain
// the unmentioned preference and a save immediately affects new outbound work.
func TestUpdateSettings_CodexTransportPartialUpdates(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	cfg := &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}}
	svc := service.NewSettingService(repo, cfg)
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	update := func(body string) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.UpdateSettings(c)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	require.Equal(t, service.CodexTransportSettings{WSEnabled: true}, svc.GetCodexTransportSettings(context.Background()))
	update(`{"openai_codex_http_ws_enabled":true}`)
	require.Equal(t, service.CodexTransportSettings{WSEnabled: true, HTTPWSEnabled: true}, svc.GetCodexTransportSettings(context.Background()))
	update(`{"openai_codex_ws_enabled":false}`)
	require.Equal(t, service.CodexTransportSettings{HTTPWSEnabled: true}, svc.GetCodexTransportSettings(context.Background()))
	require.Equal(t, "false", repo.values[service.SettingKeyOpenAICodexWSEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexHTTPWSEnabled])
	reloaded := service.NewSettingService(repo, cfg)
	require.Equal(t, service.CodexTransportSettings{HTTPWSEnabled: true}, reloaded.GetCodexTransportSettings(context.Background()))
}
