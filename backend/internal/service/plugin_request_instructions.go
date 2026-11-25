package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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

// mixedPluginInstructionSection is the host-owned config section in a package
// that also declares a transport. The runtime owns every other field.
const mixedPluginInstructionSection = "request_instructions"

// pluginRequestInstructionConfigFromManifest selects the instruction-owned
// config for a package. Pure declarative packages keep the legacy top-level
// shape; mixed packages use the dedicated section.
func pluginRequestInstructionConfigFromManifest(manifest PluginManifest, raw []byte) (pluginRequestInstructionConfig, error) {
	if !manifest.HasRequestInstructions() {
		return pluginRequestInstructionConfig{}, nil
	}
	if manifest.IsRequestOnly() {
		return validatePluginRequestInstructionConfig(raw)
	}
	section := gjson.GetBytes(raw, mixedPluginInstructionSection)
	if !section.Exists() || section.Type == gjson.Null {
		return pluginRequestInstructionConfig{}, nil
	}
	if !section.IsObject() {
		return pluginRequestInstructionConfig{}, errors.New("request_instructions 必须是 JSON 对象")
	}
	return validatePluginRequestInstructionConfig([]byte(section.Raw))
}

// splitMixedPluginConfig removes the host-owned instruction section before the
// runtime validates or applies its own configuration.
func splitMixedPluginConfig(manifest PluginManifest, raw []byte) (runtimeConfig []byte, instructionConfig []byte, err error) {
	if !manifest.HasRequestInstructions() || manifest.IsRequestOnly() || !manifestHasTransportCapability(manifest) {
		return raw, nil, nil
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return raw, nil, nil
	}
	section := gjson.GetBytes(raw, mixedPluginInstructionSection)
	if !section.Exists() || section.Type == gjson.Null {
		return raw, nil, nil
	}
	if !section.IsObject() {
		return nil, nil, errors.New("request_instructions 必须是 JSON 对象")
	}
	if _, err := validatePluginRequestInstructionConfig([]byte(section.Raw)); err != nil {
		return nil, nil, err
	}
	runtimeConfig, err = sjson.DeleteBytes(raw, mixedPluginInstructionSection)
	if err != nil {
		return nil, nil, err
	}
	return runtimeConfig, []byte(section.Raw), nil
}

func (c pluginRequestInstructionConfig) resolve(platform, accountType string) string {
	platform = strings.TrimSpace(platform)
	accountType = strings.TrimSpace(accountType)
	accountType = pluginAccountType(accountType)
	for _, override := range c.AccountOverrides {
		if override.Platform == platform && pluginAccountType(override.AccountType) == accountType {
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
			config, err := pluginRequestInstructionConfigFromManifest(installation.Manifest, raw)
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
	// Stable plugin ID and binding order make the composed text deterministic
	// across reloads and map iteration.
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].pluginID != rules[j].pluginID {
			return rules[i].pluginID < rules[j].pluginID
		}
		return rules[i].bindingID < rules[j].bindingID
	})
	m.requestRules.Store(rules)
}

// pluginAccountType maps a setup-token credential onto its OAuth binding scope.
func pluginAccountType(value string) string {
	if value == AccountTypeSetupToken {
		return AccountTypeOAuth
	}
	return value
}

func (m *PluginManager) RequestInstructionsForAccount(account *Account) string {
	if m == nil || account == nil {
		return ""
	}
	accountType := pluginAccountType(account.Type)
	rules, _ := m.requestRules.Load().([]pluginRequestRule)
	if len(rules) == 0 {
		return ""
	}
	texts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.platform != account.Platform || rule.accountType != accountType {
			continue
		}
		if rule.rollout <= 0 || int(stablePluginBucket(account.ID)) >= rule.rollout {
			continue
		}
		text := rule.config.resolve(account.Platform, account.Type)
		if text == "" {
			continue
		}
		// Every plugin contributes its own block, even when two plugins
		// happen to use identical text.
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

type pluginInstructionBody struct {
	io.Reader
	io.Closer
}

func jsonStringValue(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func prependChatInstruction(body []byte, text string) ([]byte, bool) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		updated, err := sjson.SetRawBytes(body, "messages", []byte(`[{"role":"system","content":`+jsonStringValue(text)+`}]`))
		return updated, err == nil
	}
	if first := messages.Get("0"); first.IsObject() {
		role := first.Get("role").String()
		if role == "system" || role == "developer" {
			content := first.Get("content")
			switch {
			case content.Type == gjson.String:
				updated, err := sjson.SetBytes(body, "messages.0.content", text+"\n\n"+content.String())
				return updated, err == nil
			case content.IsArray():
				parts := make([]string, 0, 1)
				parts = append(parts, `{"type":"text","text":`+jsonStringValue(text)+`}`)
				content.ForEach(func(_, part gjson.Result) bool {
					parts = append(parts, part.Raw)
					return true
				})
				updated, err := sjson.SetRawBytes(body, "messages.0.content", []byte("["+strings.Join(parts, ",")+"]"))
				return updated, err == nil
			default:
				updated, err := sjson.SetBytes(body, "messages.0.content", text)
				return updated, err == nil
			}
		}
	}
	parts := make([]string, 0, 1)
	parts = append(parts, `{"role":"system","content":`+jsonStringValue(text)+`}`)
	messages.ForEach(func(_, message gjson.Result) bool {
		parts = append(parts, message.Raw)
		return true
	})
	updated, err := sjson.SetRawBytes(body, "messages", []byte("["+strings.Join(parts, ",")+"]"))
	return updated, err == nil
}

func prependResponsesInstruction(body []byte, text string) ([]byte, bool) {
	instructions := gjson.GetBytes(body, "instructions")
	value := text
	if instructions.Type == gjson.String {
		if existing := strings.TrimSpace(instructions.String()); existing != "" {
			value = text + "\n\n" + existing
		}
	}
	updated, err := sjson.SetBytes(body, "instructions", value)
	return updated, err == nil
}

func prependAnthropicInstruction(body []byte, text string) ([]byte, bool) {
	system := gjson.GetBytes(body, "system")
	switch {
	case system.Type == gjson.String:
		value := text
		if existing := strings.TrimSpace(system.String()); existing != "" {
			value = text + "\n\n" + existing
		}
		updated, err := sjson.SetBytes(body, "system", value)
		return updated, err == nil
	case system.IsArray():
		parts := make([]string, 0, 1)
		parts = append(parts, `{"type":"text","text":`+jsonStringValue(text)+`}`)
		system.ForEach(func(_, part gjson.Result) bool {
			parts = append(parts, part.Raw)
			return true
		})
		updated, err := sjson.SetRawBytes(body, "system", []byte("["+strings.Join(parts, ",")+"]"))
		return updated, err == nil
	default:
		updated, err := sjson.SetBytes(body, "system", text)
		return updated, err == nil
	}
}

func prependGeminiInstruction(body []byte, text string) ([]byte, bool) {
	parts := gjson.GetBytes(body, "systemInstruction.parts")
	if !parts.IsArray() {
		updated, err := sjson.SetRawBytes(body, "systemInstruction", []byte(`{"parts":[{"text":`+jsonStringValue(text)+`}]}`))
		return updated, err == nil
	}
	values := make([]string, 0, 1)
	values = append(values, `{"text":`+jsonStringValue(text)+`}`)
	parts.ForEach(func(_, part gjson.Result) bool {
		values = append(values, part.Raw)
		return true
	})
	updated, err := sjson.SetRawBytes(body, "systemInstruction.parts", []byte("["+strings.Join(values, ",")+"]"))
	return updated, err == nil
}

// pluginInstructionsAppliedKey marks a body that already passed the
// instruction transform before wire encoding (for example Grok compression).
type pluginInstructionsAppliedKey struct{}

func withPluginInstructionsApplied(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, pluginInstructionsAppliedKey{}, true)
}

func pluginInstructionsApplied(ctx context.Context) bool {
	applied, _ := ctx.Value(pluginInstructionsAppliedKey{}).(bool)
	return applied
}

// applyPluginInstructionsToBody applies the configured blocks to a protocol
// body. Callers that encode or compress the body before the transport hook must
// use this first and mark the request context.
func (s *OpenAIGatewayService) applyPluginInstructionsToBody(body []byte, account *Account, protocol string) []byte {
	if s == nil || s.pluginManager == nil || account == nil || len(body) == 0 {
		return body
	}
	text := s.pluginManager.RequestInstructionsForAccount(account)
	if text == "" || len(body) > pluginRequestInstructionMaxBodyBytes {
		return body
	}
	modified, changed := ApplyPluginRequestInstructions(body, protocol, text)
	if !changed {
		return body
	}
	return modified
}

// applyPluginRequestInstructions is the final pre-signing choke point. It runs
// after the credential owner is resolved and uses that owner's binding scope.
func (s *OpenAIGatewayService) applyPluginRequestInstructions(request *http.Request, account *Account) (*http.Request, error) {
	if s == nil || request == nil || pluginInstructionsApplied(request.Context()) {
		return request, nil
	}
	return applyPluginRequestInstructionsWithManager(s.pluginManager, request, account)
}

func (s *AccountTestService) applyPluginRequestInstructions(request *http.Request, account *Account) (*http.Request, error) {
	if s == nil || request == nil || pluginInstructionsApplied(request.Context()) {
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
	original := request.Body
	body, err := io.ReadAll(io.LimitReader(original, pluginRequestInstructionMaxBodyBytes+1))
	if err != nil {
		_ = original.Close()
		return nil, err
	}
	if len(body) > pluginRequestInstructionMaxBodyBytes {
		// An unknown-length stream can exceed the transform limit. Preserve
		// every byte: the already-read prefix stays in front of the unread
		// remainder of the original stream.
		request.Body = pluginInstructionBody{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
		return request, nil
	}
	_ = original.Close()
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

// pluginRequestOperationKey declares the final wire operation when the path
// alone cannot identify it (for example a compatible base path containing
// another protocol's name).
type pluginRequestOperationKey struct{}

func WithPluginRequestOperation(ctx context.Context, operation string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, pluginRequestOperationKey{}, strings.ToLower(strings.TrimSpace(operation)))
}

func pluginRequestProtocol(request *http.Request, body []byte) string {
	path := strings.ToLower(request.URL.Path)
	if operation, _ := request.Context().Value(pluginRequestOperationKey{}).(string); operation != "" {
		switch operation {
		case "embeddings", "images", "models", "count_tokens", "moderations":
			return ""
		case APIProtocolAnthropic, APIProtocolResponses, APIProtocolChatCompletions, "gemini":
			return operation
		}
	}
	// Endpoints without an instruction slot must never be inferred from a
	// body field such as embeddings' "input".
	for _, unsupported := range []string{"/embeddings", "/images", "/models", "/count_tokens", "/moderations"} {
		if strings.Contains(path, unsupported) {
			return ""
		}
	}
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

func manifestHasOpenAIOAuthCapability(manifest PluginManifest) bool {
	for _, capability := range manifest.Capabilities {
		if capability.ID == PluginCapabilityOpenAIOAuthOutbound {
			return true
		}
	}
	return false
}

func manifestHasTransportCapability(manifest PluginManifest) bool {
	for _, capability := range manifest.Capabilities {
		switch capability.ID {
		case PluginCapabilityOpenAIOAuthOutbound, PluginCapabilityOutboundTransport:
			return true
		}
	}
	return false
}

func manifestHasMultiTransportCapability(manifest PluginManifest) bool {
	for _, capability := range manifest.Capabilities {
		if capability.ID == PluginCapabilityOutboundTransport {
			return true
		}
	}
	return false
}

func hasEnabledMultiTransportBinding(bindings []PluginBinding) bool {
	for _, binding := range bindings {
		if binding.Enabled && binding.Capability == PluginCapabilityOutboundTransport {
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
