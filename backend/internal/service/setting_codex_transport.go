package service

import (
	"context"
	"strings"
	"time"
)

const (
	SettingKeyOpenAICodexWSEnabled     = "openai_codex_ws_enabled"
	SettingKeyOpenAICodexHTTPWSEnabled = "openai_codex_http_ws_enabled"
	codexTransportSettingsTTL          = 5 * time.Second
)

// CodexTransportSettings only grants permission; deployment and account
// restrictions remain authoritative. HTTP-to-WS is a separate, explicit opt-in.
type CodexTransportSettings struct {
	WSEnabled     bool
	HTTPWSEnabled bool
}

type cachedCodexTransportSettings struct {
	value     CodexTransportSettings
	expiresAt time.Time
}

func parseCodexTransportBool(raw string, fallback bool) bool {
	switch strings.TrimSpace(raw) {
	case "true":
		return true
	case "false":
		return false
	case "":
		return fallback
	default:
		return false
	}
}

func (s *SettingService) GetCodexTransportSettings(ctx context.Context) CodexTransportSettings {
	defaults := CodexTransportSettings{WSEnabled: true}
	if s == nil || s.settingRepo == nil {
		return defaults
	}
	s.openAICodexTransportMu.Lock()
	defer s.openAICodexTransportMu.Unlock()
	if cached := s.openAICodexTransportCache; cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.value
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenAICodexWSEnabled, SettingKeyOpenAICodexHTTPWSEnabled})
	value := CodexTransportSettings{}
	if err == nil {
		value.WSEnabled = parseCodexTransportBool(values[SettingKeyOpenAICodexWSEnabled], true)
		value.HTTPWSEnabled = parseCodexTransportBool(values[SettingKeyOpenAICodexHTTPWSEnabled], false)
	}
	// A read failure disables new WS work instead of enabling a transport that
	// may have just been administratively disabled on another instance.
	s.openAICodexTransportCache = &cachedCodexTransportSettings{value: value, expiresAt: time.Now().Add(codexTransportSettingsTTL)}
	return value
}

func (s *SettingService) storeCodexTransportSettings(value CodexTransportSettings) {
	s.openAICodexTransportMu.Lock()
	defer s.openAICodexTransportMu.Unlock()
	s.openAICodexTransportCache = &cachedCodexTransportSettings{value: value, expiresAt: time.Now().Add(codexTransportSettingsTTL)}
}
