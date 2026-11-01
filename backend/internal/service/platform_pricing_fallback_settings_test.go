//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func expectedPlatformPricingFallbackModels() map[string]string {
	return map[string]string{
		"anthropic":    "claude-opus-5-5",
		"openai":       "gpt-6.1-sol",
		"gemini":       "gemini-3.8-flash",
		"antigravity":  "gemini-3.8-flash",
		"grok":         "grok-4.6",
		"kimi":         "kimi-k3",
		"zhipu":        "glm-5.3",
		"deepseek":     "deepseek-v4.1-flash",
		"minimax":      "minimax-m3",
		"stepfun":      "step-5-preview",
		"opencode_go":  "glm-5.3",
		"typesafe":     "jev-latest",
		"command_code": "deepseek-v4.1-flash",
		"cline":        "deepseek-v4.1-flash",
	}
}

type platformPricingFallbackRepoStub struct {
	settingUpdateRepoStub
	values       map[string]string
	getValueErr  error
	getAllErr    error
	writeCalls   int
	getValueKeys []string
}

func (r *platformPricingFallbackRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.getValueKeys = append(r.getValueKeys, key)
	if r.getValueErr != nil {
		return "", r.getValueErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *platformPricingFallbackRepoStub) GetAll(context.Context) (map[string]string, error) {
	if r.getAllErr != nil {
		return nil, r.getAllErr
	}
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *platformPricingFallbackRepoStub) SetMultiple(ctx context.Context, updates map[string]string) error {
	r.writeCalls++
	if err := r.settingUpdateRepoStub.SetMultiple(ctx, updates); err != nil {
		return err
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range updates {
		r.values[key] = value
	}
	return nil
}

func TestDefaultPricingFallbackModels_CoversEveryConcretePlatformAndReturnsCopies(t *testing.T) {
	want := expectedPlatformPricingFallbackModels()
	got := DefaultPricingFallbackModels()
	require.Equal(t, want, got)
	require.Len(t, got, len(domain.ConcretePlatformIDs()))
	for _, platform := range domain.ConcretePlatformIDs() {
		require.Contains(t, got, platform)
	}
	require.NotContains(t, got, "composite")

	got["openai"] = "changed-by-caller"
	delete(got, "cline")
	require.Equal(t, want, DefaultPricingFallbackModels())
}

func TestNormalizePricingFallbackModels_DefaultsPartialMapsAndExplicitDisable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input map[string]string
	}{
		{name: "nil map", input: nil},
		{name: "empty object", input: map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizePricingFallbackModels(tc.input)
			require.NoError(t, err)
			require.Equal(t, expectedPlatformPricingFallbackModels(), got)
		})
	}

	for platform := range expectedPlatformPricingFallbackModels() {
		t.Run(platform, func(t *testing.T) {
			// Aggregators may explicitly refer to a different vendor's global SKU.
			input := map[string]string{platform: "openai/custom-price-card"}
			want := expectedPlatformPricingFallbackModels()
			want[platform] = "openai/custom-price-card"
			got, err := normalizePricingFallbackModels(input)
			require.NoError(t, err)
			require.Equal(t, want, got)
			got[platform] = "modified-result"
			require.Equal(t, "openai/custom-price-card", input[platform])

			input[platform] = ""
			want[platform] = ""
			got, err = normalizePricingFallbackModels(input)
			require.NoError(t, err)
			require.Equal(t, want, got, "empty string disables only the selected platform")
			input[platform] = "modified-input"
			require.Empty(t, got[platform])
		})
	}
}

func TestNormalizePricingFallbackModels_LiteralNamesAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "outer ordinary spaces", input: "  vendor/model:version_1.2-3  ", want: "vendor/model:version_1.2-3"},
		{name: "spaces disable", input: "   ", want: ""},
		{name: "255 ASCII bytes", input: strings.Repeat("a", 255), want: strings.Repeat("a", 255)},
		{name: "255 UTF-8 bytes", input: strings.Repeat("型", 85), want: strings.Repeat("型", 85)},
		{name: "unpriced literal accepted by settings", input: "future-provider/unknown-model", want: "future-provider/unknown-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizePricingFallbackModels(map[string]string{"openai": tc.input})
			require.NoError(t, err)
			require.Equal(t, tc.want, got["openai"])
		})
	}
}

func TestNormalizePricingFallbackModels_RejectsInvalidKeysAndModels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		model    string
	}{
		{name: "unknown platform", platform: "unknown", model: "gpt-5.4"},
		{name: "composite is not a concrete platform", platform: "composite", model: "gpt-5.4"},
		{name: "empty platform", platform: "", model: "gpt-5.4"},
		{name: "platform key is literal", platform: " openai ", model: "gpt-5.4"},
		{name: "platform key is case sensitive", platform: "OpenAI", model: "gpt-5.4"},
		{name: "internal space", platform: "openai", model: "gpt 5.4"},
		{name: "internal tab", platform: "openai", model: "gpt\t5.4"},
		{name: "leading tab", platform: "openai", model: "\tgpt-5.4"},
		{name: "trailing newline", platform: "openai", model: "gpt-5.4\n"},
		{name: "control-only value", platform: "openai", model: "\t\n"},
		{name: "NUL", platform: "openai", model: "gpt\x005.4"},
		{name: "DEL", platform: "openai", model: "gpt\x7f5.4"},
		{name: "unicode whitespace", platform: "openai", model: "gpt\u00a05.4"},
		{name: "asterisk wildcard", platform: "openai", model: "gpt-*"},
		{name: "question mark wildcard", platform: "openai", model: "gpt-5.?"},
		{name: "256 ASCII bytes", platform: "openai", model: strings.Repeat("a", 256)},
		{name: "UTF-8 byte limit", platform: "openai", model: strings.Repeat("型", 86)},
		{name: "invalid UTF-8", platform: "openai", model: "gpt-\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizePricingFallbackModels(map[string]string{tc.platform: tc.model})
			require.Error(t, err)
			require.Nil(t, got)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Equal(t, "INVALID_PRICING_FALLBACK_MODELS", infraerrors.Reason(err))
		})
	}
}

func TestParsePricingFallbackModelsJSON_PreservesStringTypesAndRejectsInvalidStructures(t *testing.T) {
	got, err := ParsePricingFallbackModelsJSON([]byte(`{"openai":"","cline":"vendor/new-sku"}`))
	require.NoError(t, err)
	want := expectedPlatformPricingFallbackModels()
	want["openai"] = ""
	want["cline"] = "vendor/new-sku"
	require.Equal(t, want, got)

	for _, raw := range []string{
		``, `null`, `[]`, `"gpt-5.4"`, `42`, `true`, `{`,
		`{"openai":null}`, `{"openai":0}`, `{"openai":false}`,
		`{"openai":[]}`, `{"openai":{}}`, `{"unknown":"gpt-5.4"}`,
		"{\"openai\":\"gpt-\xff\"}",
	} {
		t.Run(raw, func(t *testing.T) {
			models, err := ParsePricingFallbackModelsJSON([]byte(raw))
			require.Error(t, err)
			require.Nil(t, models)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
		})
	}
}

func TestGetPricingFallbackModels_MissingPartialDisabledAndFreshReads(t *testing.T) {
	ctx := context.Background()
	repo := &platformPricingFallbackRepoStub{values: make(map[string]string)}
	svc := NewSettingService(repo, &config.Config{})

	got, err := svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedPlatformPricingFallbackModels(), got)
	require.Equal(t, []string{SettingKeyPricingFallbackModels}, repo.getValueKeys)
	settings, err := svc.GetAllSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedPlatformPricingFallbackModels(), settings.PricingFallbackModels)

	repo.values[SettingKeyPricingFallbackModels] = `{"openai":"","cline":"custom/global-sku"}`
	want := expectedPlatformPricingFallbackModels()
	want["openai"] = ""
	want["cline"] = "custom/global-sku"
	got, err = svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)
	got["cline"] = "changed-by-caller"

	again, err := svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Equal(t, want, again)
	repo.values[SettingKeyPricingFallbackModels] = `{}`
	again, err = svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedPlatformPricingFallbackModels(), again, "a new snapshot must see the current persisted configuration")
}

func TestGetPricingFallbackModels_PropagatesDependencyAndStoredValueErrors(t *testing.T) {
	ctx := context.Background()
	readErr := errors.New("settings database unavailable")
	repo := &platformPricingFallbackRepoStub{getValueErr: readErr}
	svc := NewSettingService(repo, &config.Config{})
	got, err := svc.GetPricingFallbackModels(ctx)
	require.ErrorIs(t, err, readErr)
	require.Nil(t, got)

	for _, raw := range []string{"", "null", "{", `{"openai":null}`, `{"composite":"gpt-5.4"}`} {
		t.Run(raw, func(t *testing.T) {
			repo := &platformPricingFallbackRepoStub{values: map[string]string{SettingKeyPricingFallbackModels: raw}}
			svc := NewSettingService(repo, &config.Config{})
			got, err := svc.GetPricingFallbackModels(ctx)
			require.Error(t, err)
			require.Nil(t, got, "invalid persisted configuration must not silently select defaults")
		})
	}

	for _, svc := range []*SettingService{nil, {}} {
		got, err := svc.GetPricingFallbackModels(ctx)
		require.Error(t, err)
		require.Nil(t, got)
	}
}

func TestPricingFallbackModels_SettingsPersistenceAndUpstreamFallbackSeparation(t *testing.T) {
	ctx := context.Background()
	repo := &platformPricingFallbackRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	input := map[string]string{"openai": "", "typesafe": "other-vendor/global-sku"}
	err := svc.UpdateSettings(ctx, &SystemSettings{
		PricingFallbackModels: input,
		EnableModelFallback:   true,
		FallbackModelOpenAI:   "upstream-retry-model",
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.writeCalls)
	var stored map[string]string
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyPricingFallbackModels]), &stored))
	want := expectedPlatformPricingFallbackModels()
	want["openai"] = ""
	want["typesafe"] = "other-vendor/global-sku"
	require.Equal(t, want, stored)
	require.Equal(t, map[string]string{"openai": "", "typesafe": "other-vendor/global-sku"}, input)

	settings, err := svc.GetAllSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, want, settings.PricingFallbackModels)
	require.True(t, settings.EnableModelFallback)
	require.Equal(t, "upstream-retry-model", svc.GetFallbackModel(ctx, "openai"))
	pricing, err := svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Empty(t, pricing["openai"], "retry-model replacement must not enable pricing fallback")

	input["typesafe"] = "changed-after-save"
	require.Equal(t, "other-vendor/global-sku", settings.PricingFallbackModels["typesafe"])
}

func TestPricingFallbackModels_OmittedAndNullPreserveStoredMapAndEmptyObjectRestoresDefaults(t *testing.T) {
	ctx := context.Background()
	const existing = `{"openai":"","cline":"custom/saved-sku"}`
	for _, raw := range []string{`{}`, `{"pricing_fallback_models":null}`} {
		t.Run(raw, func(t *testing.T) {
			repo := &platformPricingFallbackRepoStub{values: map[string]string{SettingKeyPricingFallbackModels: existing}}
			svc := NewSettingService(repo, &config.Config{})
			var settings SystemSettings
			require.NoError(t, json.Unmarshal([]byte(raw), &settings))
			require.NoError(t, svc.UpdateSettings(ctx, &settings))
			require.NotContains(t, repo.updates, SettingKeyPricingFallbackModels)
			require.Equal(t, existing, repo.values[SettingKeyPricingFallbackModels])
		})
	}

	repo := &platformPricingFallbackRepoStub{values: map[string]string{SettingKeyPricingFallbackModels: existing}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.UpdateSettings(ctx, &SystemSettings{PricingFallbackModels: map[string]string{}}))
	got, err := svc.GetPricingFallbackModels(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedPlatformPricingFallbackModels(), got)
}

func TestPricingFallbackModels_InvalidUpdateAndDatabaseErrorsDoNotChangeStoredConfiguration(t *testing.T) {
	ctx := context.Background()
	const existing = `{"openai":"custom/existing"}`
	repo := &platformPricingFallbackRepoStub{values: map[string]string{SettingKeyPricingFallbackModels: existing}}
	svc := NewSettingService(repo, &config.Config{})
	err := svc.UpdateSettings(ctx, &SystemSettings{PricingFallbackModels: map[string]string{"composite": "gpt-5.4"}})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Zero(t, repo.writeCalls, "an invalid map must reject the whole settings write")
	require.Equal(t, existing, repo.values[SettingKeyPricingFallbackModels])

	writeErr := errors.New("settings write failed")
	repo.setMultipleErr = writeErr
	err = svc.UpdateSettings(ctx, &SystemSettings{PricingFallbackModels: map[string]string{"openai": "custom/new"}})
	require.ErrorIs(t, err, writeErr)
	require.Equal(t, existing, repo.values[SettingKeyPricingFallbackModels])

	readErr := errors.New("settings read failed")
	repo.getAllErr = readErr
	settings, err := svc.GetAllSettings(ctx)
	require.ErrorIs(t, err, readErr)
	require.Nil(t, settings)

	repo.getAllErr = nil
	repo.values[SettingKeyPricingFallbackModels] = "null"
	settings, err = svc.GetAllSettings(ctx)
	require.Error(t, err, "admin settings reads must surface corrupted persisted pricing configuration")
	require.Nil(t, settings)
}
