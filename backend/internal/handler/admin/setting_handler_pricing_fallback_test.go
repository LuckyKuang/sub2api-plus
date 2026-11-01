//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func pricingFallbackResponseModels(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body struct {
		Data struct {
			PricingFallbackModels map[string]string `json:"pricing_fallback_models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data.PricingFallbackModels, 14)
	return body.Data.PricingFallbackModels
}

func TestUpdateSettingsPricingFallbackModelsPreservesOmittedAndNull(t *testing.T) {
	const stored = `{"openai":"gpt-6.1-terra","grok":""}`
	for _, body := range []map[string]any{
		{"site_name": "Gateway"},
		{"pricing_fallback_models": nil},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyPricingFallbackModels: stored})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, stored, repo.values[service.SettingKeyPricingFallbackModels])
		require.NotContains(t, repo.lastUpdates, service.SettingKeyPricingFallbackModels)
		models := pricingFallbackResponseModels(t, rec)
		require.Equal(t, "gpt-6.1-terra", models["openai"])
		require.Empty(t, models["grok"], "an omitted/null map must preserve the disabled platform")
	}
}

func TestUpdateSettingsPricingFallbackModelsResetsDefaultsAndDisablesOnePlatform(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyPricingFallbackModels: `{"openai":"gpt-6.1-terra","grok":""}`,
	})
	rec := doUpdateSettings(t, h, map[string]any{"pricing_fallback_models": map[string]string{}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	models := pricingFallbackResponseModels(t, rec)
	require.Equal(t, "gpt-6.1-sol", models["openai"])
	require.Equal(t, "claude-opus-5-5", models["anthropic"])
	require.Equal(t, "deepseek-v4.1-flash", models["deepseek"])
	require.Equal(t, "deepseek-v4.1-flash", models["command_code"])
	require.Equal(t, "deepseek-v4.1-flash", models["cline"])
	require.Equal(t, "grok-4.6", models["grok"])

	rec = doUpdateSettings(t, h, map[string]any{"pricing_fallback_models": map[string]string{
		"openai": "",
		"cline":  "openai/gpt-6.1-terra",
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	models = pricingFallbackResponseModels(t, rec)
	require.Empty(t, models["openai"], "an explicit empty string disables only that platform")
	require.Equal(t, "openai/gpt-6.1-terra", models["cline"], "aggregators may explicitly reference a global SKU")
	require.Equal(t, "grok-4.6", models["grok"], "unspecified platforms use the server defaults")
	var saved map[string]string
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyPricingFallbackModels]), &saved))
	require.Equal(t, models, saved, "PUT must return the persisted effective map")
}

func TestGetSettingsPricingFallbackModelsReturnsEffectiveDefaultsAndOverrides(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyPricingFallbackModels: `{"command_code":"anthropic/claude-sonnet-5","typesafe":""}`,
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	models := pricingFallbackResponseModels(t, rec)
	require.Equal(t, "anthropic/claude-sonnet-5", models["command_code"])
	require.Empty(t, models["typesafe"])
	require.Equal(t, "gpt-6.1-sol", models["openai"])
	require.Equal(t, "claude-opus-5-5", models["anthropic"])
	require.Equal(t, "deepseek-v4.1-flash", models["deepseek"])
	require.Equal(t, "deepseek-v4.1-flash", models["cline"])
}

func TestUpdateSettingsPricingFallbackModelsRejectsInvalidInputWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mapValue any
	}{
		{"unknown platform", map[string]string{"unknown": "gpt-5.4"}},
		{"composite is not a provider", map[string]string{"composite": "gpt-5.4"}},
		{"wildcard", map[string]string{"openai": "gpt-*"}},
		{"question wildcard", map[string]string{"openai": "gpt-5.?"}},
		{"embedded space", map[string]string{"openai": "gpt 5.4"}},
		{"control character", map[string]string{"openai": "gpt-5.4\n"}},
		{"overlong model", map[string]string{"openai": strings.Repeat("x", 256)}},
		{"nested null", map[string]any{"openai": nil}},
		{"nested number", map[string]any{"openai": 5}},
		{"array", []string{"gpt-5.4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				service.SettingKeySiteName:              "Original Gateway",
				service.SettingKeyPricingFallbackModels: `{"openai":"gpt-6.1-terra"}`,
			})
			rec := doUpdateSettings(t, h, map[string]any{
				"site_name":               "Must not save",
				"pricing_fallback_models": tc.mapValue,
			}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, "Original Gateway", repo.values[service.SettingKeySiteName])
			require.Equal(t, `{"openai":"gpt-6.1-terra"}`, repo.values[service.SettingKeyPricingFallbackModels])
			require.Nil(t, repo.lastUpdates, "invalid pricing configuration cannot partially save other settings")
		})
	}
}

type pricingFallbackWriteFailureRepo struct {
	*settingHandlerRepoStub
	err    error
	writes int
}

func (r *pricingFallbackWriteFailureRepo) SetMultiple(context.Context, map[string]string) error {
	r.writes++
	return r.err
}

func TestUpdateSettingsPricingFallbackModelsStorageFailureDoesNotReportSuccess(t *testing.T) {
	repo := &pricingFallbackWriteFailureRepo{
		settingHandlerRepoStub: &settingHandlerRepoStub{values: map[string]string{
			service.SettingKeyPricingFallbackModels: `{"openai":"gpt-6.1-terra"}`,
		}},
		err: errors.New("settings storage unavailable"),
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
	rec := doUpdateSettings(t, h, map[string]any{"pricing_fallback_models": map[string]string{"openai": "gpt-5.4"}}, nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Equal(t, 1, repo.writes)
	require.Equal(t, `{"openai":"gpt-6.1-terra"}`, repo.values[service.SettingKeyPricingFallbackModels])
}
