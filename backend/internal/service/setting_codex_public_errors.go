package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const codexPublicErrorMessageMaxRunes = 500

var ErrCodexPublicErrorRevisionConflict = errors.New("codex public error message revision conflict")

var ErrCodexPublicErrorInvalid = errors.New("invalid codex public error message setting")

type CodexPublicErrorItem struct {
	Key              string  `json:"key"`
	Group            string  `json:"group"`
	PublicCode       *string `json:"public_code"`
	DefaultMessage   string  `json:"default_message"`
	CustomMessage    *string `json:"custom_message"`
	EffectiveMessage string  `json:"effective_message"`
}

type CodexPublicErrorSnapshot struct {
	Revision      string                 `json:"revision"`
	CustomEnabled bool                   `json:"custom_enabled"`
	Items         []CodexPublicErrorItem `json:"items"`
}

type codexPublicErrorSettings struct {
	customEnabled bool
	overrides     map[string]string
	revision      string
}

func parseCodexPublicErrorOverrides(raw string) (map[string]string, error) {
	overrides := make(map[string]string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return overrides, nil
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("parse codex public error overrides: %w", err)
	}
	for key, value := range stored {
		var message string
		if err := json.Unmarshal(value, &message); err != nil {
			return nil, fmt.Errorf("parse codex public error override %q: %w", key, err)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if _, ok := codexPublicErrorByKey(key); !ok {
			// Historical or unknown keys are ignored rather than rendered.
			continue
		}
		message, err := validateCodexPublicErrorMessage(message)
		if err != nil {
			return nil, fmt.Errorf("invalid codex public error override %q: %w", key, err)
		}
		overrides[key] = message
	}
	return overrides, nil
}

func validateCodexPublicErrorMessage(message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("message is blank")
	}
	if utf8.RuneCountInString(message) > codexPublicErrorMessageMaxRunes {
		return "", fmt.Errorf("message exceeds %d Unicode code points", codexPublicErrorMessageMaxRunes)
	}
	for _, r := range message {
		if unicode.IsControl(r) {
			return "", errors.New("message must be single-line plain text")
		}
	}
	return message, nil
}

func codexPublicErrorRevision(customEnabled bool, overrides map[string]string) string {
	if overrides == nil {
		overrides = map[string]string{}
	}
	payload, _ := json.Marshal(struct {
		CustomEnabled bool              `json:"custom_enabled"`
		Overrides     map[string]string `json:"overrides"`
	}{customEnabled, overrides})
	sum := sha256.Sum256(payload)
	return "r" + hex.EncodeToString(sum[:8])
}

func codexPublicErrorSettingsFromValues(values map[string]string) (codexPublicErrorSettings, error) {
	overrides, err := parseCodexPublicErrorOverrides(values[SettingKeyOpenAIErrorMessageOverrides])
	if err != nil {
		return codexPublicErrorSettings{}, err
	}
	enabled := strings.TrimSpace(values[SettingKeyOpenAIErrorMessagesNormalized]) == "true"
	return codexPublicErrorSettings{
		customEnabled: enabled,
		overrides:     overrides,
		revision:      codexPublicErrorRevision(enabled, overrides),
	}, nil
}

func (s *SettingService) codexPublicErrorSettings(ctx context.Context) codexPublicErrorSettings {
	if s == nil {
		return defaultCodexPublicErrorSettings()
	}
	if !s.codexSafetyBuffering.initialized.Load() {
		s.refreshCodexSafetyBuffering(ctx)
	}
	settings, _ := s.codexSafetyBuffering.publicErrors.Load().(codexPublicErrorSettings)
	if settings.overrides == nil {
		return defaultCodexPublicErrorSettings()
	}
	settings.overrides = cloneCodexPublicErrorOverrides(settings.overrides)
	return settings
}

// storeCodexPublicErrorSnapshotLocked persists the latest committed database
// state into the runtime after a write. Re-reading under the same mutex used by
// refreshes prevents an earlier commit that publishes late from overwriting a
// newer revision.
func (s *SettingService) storeCodexPublicErrorSnapshotLocked(ctx context.Context) bool {
	result, ok := s.readCodexPublicErrorSettingsLocked(ctx)
	if !ok {
		return false
	}
	if raw := result.values[SettingKeyOpenAICodexSafetyBufferingSuppressed]; raw == "" || raw == "true" || raw == "false" {
		s.codexSafetyBuffering.enabled.Store(raw == "true")
	}
	s.codexSafetyBuffering.publicErrors.Store(result.settings)
	return true
}

func cloneCodexPublicErrorOverrides(overrides map[string]string) map[string]string {
	cloned := make(map[string]string, len(overrides))
	for key, value := range overrides {
		cloned[key] = value
	}
	return cloned
}

func (s *SettingService) CodexPublicErrorCustomEnabled(ctx context.Context) bool {
	return s.codexPublicErrorSettings(ctx).customEnabled
}

func (s *SettingService) CodexPublicErrorMessage(ctx context.Context, key string) string {
	definition, ok := codexPublicErrorByKey(key)
	if !ok {
		definition, ok = codexPublicErrorByKey("server_error")
		if !ok {
			return ""
		}
	}
	settings := s.codexPublicErrorSettings(ctx)
	if settings.customEnabled {
		if message := strings.TrimSpace(settings.overrides[definition.Key]); message != "" {
			return message
		}
	}
	return definition.DefaultMessage
}

func (s *SettingService) CodexPublicErrorSnapshot(ctx context.Context) *CodexPublicErrorSnapshot {
	settings := s.codexPublicErrorSettings(ctx)
	return buildCodexPublicErrorSnapshot(settings)
}

func buildCodexPublicErrorSnapshot(settings codexPublicErrorSettings) *CodexPublicErrorSnapshot {
	items := make([]CodexPublicErrorItem, 0, len(codexPublicErrorCatalog))
	for _, definition := range codexPublicErrorCatalog {
		var publicCode *string
		if definition.PublicCode != "" {
			code := definition.PublicCode
			publicCode = &code
		}
		var custom *string
		if message, ok := settings.overrides[definition.Key]; ok {
			value := message
			custom = &value
		}
		effective := definition.DefaultMessage
		if settings.customEnabled && custom != nil {
			effective = *custom
		}
		items = append(items, CodexPublicErrorItem{
			Key:              definition.Key,
			Group:            definition.Group,
			PublicCode:       publicCode,
			DefaultMessage:   definition.DefaultMessage,
			CustomMessage:    custom,
			EffectiveMessage: effective,
		})
	}
	return &CodexPublicErrorSnapshot{
		Revision:      settings.revision,
		CustomEnabled: settings.customEnabled,
		Items:         items,
	}
}

// SaveCodexPublicErrorSettings applies a revision-checked draft. A nil
// customEnabled keeps the current mode; a nil change value removes the override.
func (s *SettingService) SaveCodexPublicErrorSettings(ctx context.Context, expectedRevision string, customEnabled *bool, changes map[string]*string) (*CodexPublicErrorSnapshot, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("setting repository unavailable")
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOpenAIErrorMessagesNormalized, SettingKeyOpenAIErrorMessageOverrides})
	if err != nil {
		return nil, err
	}
	current, err := codexPublicErrorSettingsFromValues(values)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(expectedRevision) == "" || expectedRevision != current.revision {
		return nil, ErrCodexPublicErrorRevisionConflict
	}
	next := codexPublicErrorSettings{
		customEnabled: current.customEnabled,
		overrides:     cloneCodexPublicErrorOverrides(current.overrides),
	}
	if customEnabled != nil {
		next.customEnabled = *customEnabled
	}
	for rawKey, rawMessage := range changes {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		definition, ok := codexPublicErrorByKey(key)
		if !ok {
			return nil, fmt.Errorf("%w: unknown key %s", ErrCodexPublicErrorInvalid, rawKey)
		}
		if rawMessage == nil {
			delete(next.overrides, definition.Key)
			continue
		}
		message, validateErr := validateCodexPublicErrorMessage(*rawMessage)
		if validateErr != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrCodexPublicErrorInvalid, definition.Key, validateErr)
		}
		next.overrides[definition.Key] = message
	}
	next.revision = codexPublicErrorRevision(next.customEnabled, next.overrides)
	overridesJSON, err := json.Marshal(next.overrides)
	if err != nil {
		return nil, err
	}
	updates := map[string]string{
		SettingKeyOpenAIErrorMessagesNormalized: fmt.Sprintf("%t", next.customEnabled),
		SettingKeyOpenAIErrorMessageOverrides:   string(overridesJSON),
	}
	expected := map[string]string{
		SettingKeyOpenAIErrorMessagesNormalized: values[SettingKeyOpenAIErrorMessagesNormalized],
		SettingKeyOpenAIErrorMessageOverrides:   values[SettingKeyOpenAIErrorMessageOverrides],
	}
	applied, err := s.compareAndSetCodexPublicErrors(ctx, expected, updates)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, ErrCodexPublicErrorRevisionConflict
	}

	runtime := &s.codexSafetyBuffering
	runtime.mu.Lock()
	// Another instance may have committed a newer revision while this save was
	// finishing. Re-read the committed pair so this late publish cannot regress
	// the runtime to an older snapshot.
	s.storeCodexPublicErrorSnapshotLocked(ctx)
	bus := runtime.bus
	runtime.mu.Unlock()
	if bus != nil {
		publishCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if publishErr := bus.Publish(publishCtx, SettingKeyOpenAICodexSafetyBufferingSuppressed); publishErr != nil {
			slog.Warn("codex public error setting broadcast failed", "stage", "settings_publish", "code", "codex_public_error_setting_publish_failed")
		}
	}
	return buildCodexPublicErrorSnapshot(next), nil
}

type settingCompareAndSetRepository interface {
	CompareAndSetSettings(ctx context.Context, expected, updates map[string]string) (bool, error)
}

func (s *SettingService) compareAndSetCodexPublicErrors(ctx context.Context, expected, updates map[string]string) (bool, error) {
	if repo, ok := s.settingRepo.(settingCompareAndSetRepository); ok {
		return repo.CompareAndSetSettings(ctx, expected, updates)
	}
	// Test doubles and older repositories fall back to a re-read. Production
	// settingsRepository implements the atomic compare-and-set path.
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOpenAIErrorMessagesNormalized, SettingKeyOpenAIErrorMessageOverrides})
	if err != nil {
		return false, err
	}
	current, err := codexPublicErrorSettingsFromValues(values)
	if err != nil {
		return false, err
	}
	if current.revision != codexPublicErrorRevision(expected[SettingKeyOpenAIErrorMessagesNormalized] == "true", mustParseCodexPublicErrorOverrides(expected[SettingKeyOpenAIErrorMessageOverrides])) {
		return false, nil
	}
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return false, err
	}
	return true, nil
}

func mustParseCodexPublicErrorOverrides(raw string) map[string]string {
	overrides, err := parseCodexPublicErrorOverrides(raw)
	if err != nil {
		return map[string]string{}
	}
	return overrides
}
