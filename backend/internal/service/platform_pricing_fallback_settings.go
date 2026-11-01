package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

// DefaultPricingFallbackModels names existing price cards, never replacement
// upstream models. Each concrete platform owns its fallback independently,
// including providers that expose models from several vendors.
func DefaultPricingFallbackModels() map[string]string {
	return map[string]string{
		PlatformAnthropic:   "claude-opus-5-5",
		PlatformOpenAI:      "gpt-6.1-sol",
		PlatformGemini:      "gemini-3.8-flash",
		PlatformAntigravity: "gemini-3.8-flash",
		PlatformGrok:        "grok-4.6",
		PlatformKimi:        "kimi-k3",
		PlatformZhipu:       "glm-5.3",
		PlatformDeepseek:    "deepseek-v4.1-flash",
		PlatformMiniMax:     "minimax-m3",
		PlatformStepFun:     "step-5-preview",
		PlatformOpenCodeGo:  "glm-5.3",
		PlatformTypeSafe:    "jev-latest",
		PlatformCommandCode: "deepseek-v4.1-flash",
		PlatformCline:       "deepseek-v4.1-flash",
	}
}

func invalidPricingFallbackModels(message string) error {
	return infraerrors.BadRequest("INVALID_PRICING_FALLBACK_MODELS", message)
}

func normalizePricingFallbackModels(input map[string]string) (map[string]string, error) {
	normalized := DefaultPricingFallbackModels()
	for platform, value := range input {
		if !domain.IsConcretePlatform(platform) {
			return nil, invalidPricingFallbackModels("pricing fallback keys must name registered concrete platforms")
		}
		model := strings.TrimSpace(value)
		if len(model) > 255 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.ContainsAny(model, "*?") || strings.IndexFunc(model, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		}) >= 0 {
			return nil, invalidPricingFallbackModels("pricing fallback models must be literal model names of at most 255 bytes without whitespace, control characters or wildcards")
		}
		// Empty is an explicit opt-out, distinct from an omitted platform key.
		normalized[platform] = model
	}
	return normalized, nil
}

// ParsePricingFallbackModelsJSON validates a supplied object before its values
// lose their JSON types. In particular, a null model must not disable a platform
// as if the administrator had explicitly supplied an empty string.
func ParsePricingFallbackModelsJSON(data []byte) (map[string]string, error) {
	var fields map[string]json.RawMessage
	if !utf8.Valid(data) {
		return nil, invalidPricingFallbackModels("pricing_fallback_models must contain valid UTF-8")
	}
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return nil, invalidPricingFallbackModels("pricing_fallback_models must be a JSON object of platform names and string model names")
	}
	models := make(map[string]string, len(fields))
	for platform, raw := range fields {
		if len(raw) == 0 || raw[0] != '"' {
			return nil, invalidPricingFallbackModels("every pricing fallback model must be a string")
		}
		var model string
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, invalidPricingFallbackModels("every pricing fallback model must be a string")
		}
		models[platform] = model
	}
	return normalizePricingFallbackModels(models)
}

func parsePricingFallbackModelsSetting(raw string) (map[string]string, error) {
	return ParsePricingFallbackModelsJSON([]byte(raw))
}

// GetPricingFallbackModels reads one settings generation for a new pricing
// snapshot. Only an absent setting uses defaults; invalid stored JSON and
// dependency errors must remain visible to admission rather than change prices.
func (s *SettingService) GetPricingFallbackModels(ctx context.Context) (map[string]string, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("pricing fallback settings repository is unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyPricingFallbackModels)
	if errors.Is(err, ErrSettingNotFound) {
		return DefaultPricingFallbackModels(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pricing fallback models: %w", err)
	}
	models, err := parsePricingFallbackModelsSetting(raw)
	if err != nil {
		return nil, fmt.Errorf("parse stored pricing fallback models: %w", err)
	}
	return models, nil
}
