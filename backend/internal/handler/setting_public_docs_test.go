//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The docs switches must reach both the public JSON response and the SSR
// injection payload; a zero-valued DTO previously made a login-required center
// look public.
func TestSettingHandler_GetPublicSettings_ExposesDocsSwitches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		values      map[string]string
		wantEnabled bool
		wantAuth    bool
	}{
		{name: "missing defaults to disabled and login required", values: map[string]string{}, wantAuth: true},
		{
			name: "enabled and login required",
			values: map[string]string{
				service.SettingKeyDocsEnabled:     "true",
				service.SettingKeyDocsRequireAuth: "true",
			},
			wantEnabled: true,
			wantAuth:    true,
		},
		{
			name: "enabled and public",
			values: map[string]string{
				service.SettingKeyDocsEnabled:     "true",
				service.SettingKeyDocsRequireAuth: "false",
			},
			wantEnabled: true,
			wantAuth:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := service.NewSettingService(&settingHandlerPublicRepoStub{values: tc.values}, &config.Config{})
			h := NewSettingHandler(svc, "test-version")
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
			h.GetPublicSettings(c)
			require.Equal(t, http.StatusOK, recorder.Code)

			var resp struct {
				Code int `json:"code"`
				Data struct {
					Enabled     *bool `json:"docs_enabled"`
					RequireAuth *bool `json:"docs_require_auth"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
			require.Equal(t, 0, resp.Code)
			require.NotNil(t, resp.Data.Enabled)
			require.NotNil(t, resp.Data.RequireAuth)
			require.Equal(t, tc.wantEnabled, *resp.Data.Enabled)
			require.Equal(t, tc.wantAuth, *resp.Data.RequireAuth)

			injected, err := svc.GetPublicSettingsForInjection(c.Request.Context())
			require.NoError(t, err)
			payload := injected.(*service.PublicSettingsInjectionPayload)
			require.Equal(t, tc.wantEnabled, payload.DocsEnabled)
			require.Equal(t, tc.wantAuth, payload.DocsRequireAuth)
		})
	}
}
