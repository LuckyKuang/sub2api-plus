package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
)

const (
	pluginRequestInstructionMaxTextBytes = 16 * 1024
	pluginRequestInstructionMaxBodyBytes = 32 * 1024 * 1024
)

type pluginRequestInstructionConfig struct {
	DefaultText       string                         `json:"default_text"`
	PlatformOverrides map[string]string              `json:"platform_overrides,omitempty"`
	AccountOverrides  []pluginRequestAccountOverride `json:"account_overrides,omitempty"`
}

type pluginRequestAccountOverride struct {
	Platform    string `json:"platform"`
	AccountType string `json:"account_type"`
	Text        string `json:"text"`
}

type pluginRequestRule struct {
	pluginID    int64
	pluginKey   string
	bindingID   int64
	platform    string
	accountType string
	rollout     int
	config      pluginRequestInstructionConfig
}

func validatePluginRequestInstructionConfig(raw []byte) (pluginRequestInstructionConfig, error) {
	var config pluginRequestInstructionConfig
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("请求处理配置无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return config, errors.New("请求处理配置只能包含一个 JSON 对象")
	}
	if err := validatePluginInstructionText(config.DefaultText, true); err != nil {
		return config, fmt.Errorf("default_text: %w", err)
	}
	for platform, text := range config.PlatformOverrides {
		if !domain.IsConcretePlatform(strings.TrimSpace(platform)) {
			return config, fmt.Errorf("platform_overrides 包含未知平台: %s", platform)
		}
		if err := validatePluginInstructionText(text, true); err != nil {
			return config, fmt.Errorf("platform_overrides.%s: %w", platform, err)
		}
	}
	seen := make(map[string]struct{}, len(config.AccountOverrides))
	for index, override := range config.AccountOverrides {
		platform := strings.TrimSpace(override.Platform)
		if !domain.IsConcretePlatform(platform) {
			return config, fmt.Errorf("account_overrides[%d] 平台无效: %s", index, override.Platform)
		}
		if override.AccountType != AccountTypeOAuth && override.AccountType != AccountTypeAPIKey {
			return config, fmt.Errorf("account_overrides[%d] 账号类型必须是 oauth 或 apikey", index)
		}
		if err := validatePluginInstructionText(override.Text, true); err != nil {
			return config, fmt.Errorf("account_overrides[%d].text: %w", index, err)
		}
		key := platform + "/" + override.AccountType
		if _, exists := seen[key]; exists {
			return config, fmt.Errorf("account_overrides 重复声明: %s", key)
		}
		seen[key] = struct{}{}
	}
	return config, nil
}

func validatePluginInstructionText(text string, allowEmpty bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		if allowEmpty {
			return nil
		}
		return errors.New("文本不能为空")
	}
	if len(text) > pluginRequestInstructionMaxTextBytes || utf8.RuneCountInString(text) > 16384 {
		return fmt.Errorf("文本超过 %d 字节限制", pluginRequestInstructionMaxTextBytes)
	}
	for _, r := range text {
		if r == 0 {
			return errors.New("文本不能包含 NUL")
		}
	}
	return nil
}

func (c pluginRequestInstructionConfig) resolve(platform, accountType string) string {
	platform = strings.TrimSpace(platform)
	accountType = strings.TrimSpace(accountType)
	for _, override := range c.AccountOverrides {
		if override.Platform == platform && override.AccountType == accountType {
			return strings.TrimSpace(override.Text)
		}
	}
	if text := strings.TrimSpace(c.PlatformOverrides[platform]); text != "" {
		return text
	}
	return strings.TrimSpace(c.DefaultText)
}

func (m *PluginManager) publishRequestRules(installations []*PluginInstallation) {
	if m == nil {
		return
	}
	rules := make([]pluginRequestRule, 0)
	for _, installation := range installations {
		if installation == nil {
			continue
		}
		for _, binding := range installation.Bindings {
			if !binding.Enabled || binding.Capability != PluginCapabilityRequestInstructions || binding.RolloutPercent < 1 {
				continue
			}
			raw, err := m.decryptConfig(installation)
			if err != nil {
				slog.Warn("plugin_request_config_decrypt_failed", "plugin_id", installation.ID, "error", err)
				continue
			}
			config, err := validatePluginRequestInstructionConfig(raw)
			if err != nil {
				slog.Warn("plugin_request_config_invalid", "plugin_id", installation.ID, "error", err)
				continue
			}
			rules = append(rules, pluginRequestRule{
				pluginID:    installation.ID,
				pluginKey:   installation.PluginKey,
				bindingID:   binding.ID,
				platform:    binding.Platform,
				accountType: binding.AccountType,
				rollout:     binding.RolloutPercent,
				config:      config,
			})
		}
	}
	m.requestRules.Store(rules)
}

// RequestInstructionsForAccount returns the ordered, non-empty instruction
// blocks that apply to this credential. One text is selected per plugin, using
// exact account override, platform override, then default precedence.
func (m *PluginManager) RequestInstructionsForAccount(account *Account) string {
	if m == nil || account == nil {
		return ""
	}
	rules, _ := m.requestRules.Load().([]pluginRequestRule)
	if len(rules) == 0 {
		return ""
	}
	texts := make([]string, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if rule.platform != account.Platform || rule.accountType != account.Type {
			continue
		}
		if rule.rollout <= 0 || int(stablePluginBucket(account.ID)) >= rule.rollout {
			continue
		}
		text := rule.config.resolve(account.Platform, account.Type)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n\n")
}

// ApplyPluginRequestInstructions prepends text at the protocol's instruction
// slot while preserving existing roles, blocks, tools and media.
func ApplyPluginRequestInstructions(body []byte, protocol, text string) ([]byte, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(body) == 0 || !json.Valid(body) {
		return body, false
	}
	switch protocol {
	case APIProtocolChatCompletions:
		return prependChatInstruction(body, text)
	case APIProtocolResponses:
		return prependResponsesInstruction(body, text)
	case APIProtocolAnthropic:
		return prependAnthropicInstruction(body, text)
	case "gemini":
		return prependGeminiInstruction(body, text)
	default:
		return body, false
	}
}

func prependChatInstruction(body []byte, text string) ([]byte, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	messages, _ := root["messages"].([]any)
	if len(messages) > 0 {
		if first, ok := messages[0].(map[string]any); ok {
			role, _ := first["role"].(string)
			if role == "system" || role == "developer" {
				switch content := first["content"].(type) {
				case string:
					first["content"] = text + "\n\n" + content
				case []any:
					first["content"] = append([]any{map[string]any{"type": "text", "text": text}}, content...)
				default:
					first["content"] = text
				}
				return mustMarshal(root), true
			}
		}
	}
	root["messages"] = append([]any{map[string]any{"role": "system", "content": text}}, messages...)
	return mustMarshal(root), true
}

func prependResponsesInstruction(body []byte, text string) ([]byte, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	existing, _ := root["instructions"].(string)
	if strings.TrimSpace(existing) == "" {
		root["instructions"] = text
	} else {
		root["instructions"] = text + "\n\n" + existing
	}
	return mustMarshal(root), true
}

func prependAnthropicInstruction(body []byte, text string) ([]byte, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	switch system := root["system"].(type) {
	case string:
		if strings.TrimSpace(system) == "" {
			root["system"] = text
		} else {
			root["system"] = text + "\n\n" + system
		}
	case []any:
		root["system"] = append([]any{map[string]any{"type": "text", "text": text}}, system...)
	default:
		root["system"] = text
	}
	return mustMarshal(root), true
}

func prependGeminiInstruction(body []byte, text string) ([]byte, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	instruction, _ := root["systemInstruction"].(map[string]any)
	if instruction == nil {
		instruction = map[string]any{}
	}
	parts, _ := instruction["parts"].([]any)
	instruction["parts"] = append([]any{map[string]any{"text": text}}, parts...)
	root["systemInstruction"] = instruction
	return mustMarshal(root), true
}

func mustMarshal(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

// applyPluginRequestInstructions is the OpenAI-service choke point used by
// native Chat, Responses, Anthropic-compatible Messages and OpenAI-compatible
// providers. The final account identity and protocol are already selected.
func (s *OpenAIGatewayService) applyPluginRequestInstructions(request *http.Request, account *Account) (*http.Request, error) {
	if s == nil {
		return request, nil
	}
	return applyPluginRequestInstructionsWithManager(s.pluginManager, request, account)
}

func (s *AccountTestService) applyPluginRequestInstructions(request *http.Request, account *Account) (*http.Request, error) {
	if s == nil {
		return request, nil
	}
	return applyPluginRequestInstructionsWithManager(s.pluginManager, request, account)
}

func applyPluginRequestInstructionsWithManager(manager *PluginManager, request *http.Request, account *Account) (*http.Request, error) {
	if manager == nil || request == nil || account == nil || request.Body == nil {
		return request, nil
	}
	text := manager.RequestInstructionsForAccount(account)
	if text == "" {
		return request, nil
	}
	if request.ContentLength > pluginRequestInstructionMaxBodyBytes {
		return request, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, pluginRequestInstructionMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	if len(body) > pluginRequestInstructionMaxBodyBytes {
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		return request, nil
	}
	protocol := pluginRequestProtocol(request, body)
	modified, changed := ApplyPluginRequestInstructions(body, protocol, text)
	if !changed {
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		return request, nil
	}
	request.Body = io.NopCloser(strings.NewReader(string(modified)))
	request.ContentLength = int64(len(modified))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(modified))), nil
	}
	return request, nil
}

func pluginRequestProtocol(request *http.Request, body []byte) string {
	path := strings.ToLower(request.URL.Path)
	switch {
	case strings.Contains(path, "/messages"):
		return APIProtocolAnthropic
	case strings.Contains(path, "/responses"):
		return APIProtocolResponses
	case strings.Contains(path, "/chat/completions"):
		return APIProtocolChatCompletions
	}
	var root map[string]any
	if json.Unmarshal(body, &root) == nil {
		if _, ok := root["input"]; ok {
			return APIProtocolResponses
		}
		if _, ok := root["messages"]; ok {
			return APIProtocolChatCompletions
		}
		if _, ok := root["system"]; ok {
			return APIProtocolAnthropic
		}
	}
	return ""
}

func manifestHasTransportCapability(manifest PluginManifest) bool {
	for _, capability := range manifest.Capabilities {
		if capability.ID == PluginCapabilityOpenAIOAuthOutbound {
			return true
		}
	}
	return false
}

func (m *PluginManager) refreshRequestRules(ctx context.Context) {
	if m == nil || m.repo == nil {
		return
	}
	installations, err := m.repo.List(ctx)
	if err != nil {
		slog.Warn("plugin_request_rules_refresh_failed", "error", err)
		return
	}
	m.publishRequestRules(installations)
}

func (m *PluginManager) enableRequestOnlyPlugin(ctx context.Context, installation *PluginInstallation, compatibility PluginCompatibility, rolloutPercent int) (*PluginInstallation, error) {
	if installation == nil {
		return nil, errors.New("插件安装记录为空")
	}
	raw, err := m.decryptConfig(installation)
	if err != nil {
		return nil, err
	}
	if _, err := validatePluginRequestInstructionConfig(raw); err != nil {
		return nil, err
	}
	bindings := append([]PluginBinding(nil), installation.Bindings...)
	for index := range bindings {
		if bindings[index].Capability != PluginCapabilityRequestInstructions {
			continue
		}
		bindings[index].Enabled = true
		bindings[index].RolloutPercent = rolloutPercent
	}
	now := time.Now()
	if err := m.repo.UpdateBindingsAndState(ctx, installation.ID, bindings, PluginStateEnabled, "", &now, installation.State, installation.BinarySHA256); err != nil {
		return nil, err
	}
	m.refreshRequestRules(ctx)
	result, err := m.repo.GetByID(ctx, installation.ID)
	if err != nil {
		return nil, err
	}
	result.Compatibility = compatibility
	result.RuntimeHealthy = true
	result.RuntimeMessage = "声明式规则已启用"
	return result, nil
}
