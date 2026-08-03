package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettings_OpenAICodexUserAgentValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	update := func(t *testing.T, userAgent string) *httptest.ResponseRecorder {
		t.Helper()
		repo := &settingHandlerRepoStub{values: map[string]string{}}
		handler := NewSettingHandler(service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}}), nil, nil, nil, nil, nil, nil)
		body, err := json.Marshal(map[string]any{"openai_codex_user_agent": userAgent})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.UpdateSettings(c)
		return recorder
	}

	invalid := update(t, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "supported Codex User-Agent")

	valid := update(t, "codex-tui/0.145.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.145.0)")
	require.Equal(t, http.StatusOK, valid.Code)
}
