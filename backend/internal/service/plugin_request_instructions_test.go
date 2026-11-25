//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"

	pluginv1 "github.com/LuckyKuang/sub2api-plus/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func validRequestManifest() PluginManifest {
	return PluginManifest{
		SchemaVersion: 2,
		ID:            "dev.example.prompt",
		Name:          "Static prompt",
		Version:       "1.0.0",
		Requires: PluginRequirements{
			Sub2API:        ">=0.2.0",
			PluginProtocol: pluginv1.ProtocolVersion,
			TransportAPI:   pluginv1.TransportAPIVersion,
			UIBridge:       pluginv1.UIBridgeVersion,
		},
		Capabilities: []PluginCapability{{
			ID:          PluginCapabilityRequestInstructions,
			Platform:    PlatformStepFun,
			AccountType: AccountTypeOAuth,
		}},
	}
}

func TestPluginManifestV2RequestOnly(t *testing.T) {
	manifest := validRequestManifest()
	require.NoError(t, manifest.Validate())
	require.True(t, manifest.IsRequestOnly())
	require.True(t, manifest.HasRequestInstructions())

	withFile := validRequestManifest()
	withFile.Files = map[string]string{"prompt.txt": "hash"}
	require.ErrorContains(t, withFile.Validate(), "纯请求处理插件")

	withRuntime := validRequestManifest()
	withRuntime.Runtimes = map[string]PluginRuntime{"darwin-arm64": {Path: "bin/plugin"}}
	require.ErrorContains(t, withRuntime.Validate(), "纯请求处理插件")

	schemaOne := validRequestManifest()
	schemaOne.SchemaVersion = 1
	require.ErrorContains(t, schemaOne.Validate(), "版本 2")

	unknownPlatform := validRequestManifest()
	unknownPlatform.Capabilities[0].Platform = "not-a-platform"
	require.ErrorContains(t, unknownPlatform.Validate(), "具体平台")

	transportOtherPlatform := validRequestManifest()
	transportOtherPlatform.Capabilities = []PluginCapability{{
		ID: PluginCapabilityOpenAIOAuthOutbound, Platform: PlatformAnthropic, AccountType: AccountTypeOAuth,
	}}
	transportOtherPlatform.Runtimes = map[string]PluginRuntime{"darwin-arm64": {Path: "bin/plugin"}}
	transportOtherPlatform.UI = &PluginUIManifest{Entrypoint: "ui/index.html"}
	transportOtherPlatform.Files = map[string]string{"bin/plugin": strings.Repeat("a", 64), "ui/index.html": strings.Repeat("b", 64)}
	require.ErrorContains(t, transportOtherPlatform.Validate(), "仅支持 OpenAI OAuth")
}

func TestPluginRequestInstructionConfigPrecedenceAndValidation(t *testing.T) {
	config, err := validatePluginRequestInstructionConfig([]byte(`{
		"default_text":"default",
		"platform_overrides":{"stepfun":"platform"},
		"account_overrides":[{"platform":"stepfun","account_type":"oauth","text":"exact"}]
	}`))
	require.NoError(t, err)
	require.Equal(t, "exact", config.resolve(PlatformStepFun, AccountTypeOAuth))
	require.Equal(t, "platform", config.resolve(PlatformStepFun, AccountTypeAPIKey))
	require.Equal(t, "default", config.resolve(PlatformOpenAI, AccountTypeOAuth))

	_, err = validatePluginRequestInstructionConfig([]byte(`{"platform_overrides":{"unknown":"x"}}`))
	require.Error(t, err)
	_, err = validatePluginRequestInstructionConfig([]byte(`{"account_overrides":[{"platform":"stepfun","account_type":"oauth","text":"a"},{"platform":"stepfun","account_type":"oauth","text":"b"}]}`))
	require.Error(t, err)
	_, err = validatePluginRequestInstructionConfig([]byte(`{"default_text":"` + strings.Repeat("x", 20000) + `"}`))
	require.Error(t, err)
}

func TestPluginRequestInstructionsMatchOwnerAndRollout(t *testing.T) {
	manager := &PluginManager{}
	manager.requestRules.Store([]pluginRequestRule{
		{pluginID: 1, platform: PlatformStepFun, accountType: AccountTypeOAuth, rollout: 100, config: pluginRequestInstructionConfig{DefaultText: "first"}},
		{pluginID: 2, platform: PlatformStepFun, accountType: AccountTypeOAuth, rollout: 100, config: pluginRequestInstructionConfig{DefaultText: "second"}},
		{pluginID: 3, platform: PlatformStepFun, accountType: AccountTypeAPIKey, rollout: 100, config: pluginRequestInstructionConfig{DefaultText: "api key"}},
	})
	account := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth}
	require.Equal(t, "first\n\nsecond", manager.RequestInstructionsForAccount(account))
	require.Empty(t, manager.RequestInstructionsForAccount(&Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))

	manager.requestRules.Store([]pluginRequestRule{{
		pluginID: 1, platform: PlatformStepFun, accountType: AccountTypeOAuth, rollout: 1,
		config: pluginRequestInstructionConfig{DefaultText: "rare"},
	}})
	account.ID = 1
	require.Equal(t, "", manager.RequestInstructionsForAccount(account))
}

func TestApplyPluginRequestInstructions(t *testing.T) {
	chat := []byte(`{"messages":[{"role":"system","content":"existing"},{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`)
	got, changed := ApplyPluginRequestInstructions(chat, APIProtocolChatCompletions, "Prepend")
	require.True(t, changed)
	require.JSONEq(t, `{"messages":[{"role":"system","content":"Prepend\n\nexisting"},{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`, string(got))

	chatNoSystem := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	got, changed = ApplyPluginRequestInstructions(chatNoSystem, APIProtocolChatCompletions, "Prepend")
	require.True(t, changed)
	require.JSONEq(t, `{"messages":[{"role":"system","content":"Prepend"},{"role":"user","content":"hi"}]}`, string(got))

	responses := []byte(`{"input":"hi","instructions":"existing"}`)
	got, changed = ApplyPluginRequestInstructions(responses, APIProtocolResponses, "Prepend")
	require.True(t, changed)
	require.JSONEq(t, `{"input":"hi","instructions":"Prepend\n\nexisting"}`, string(got))

	anthropic := []byte(`{"system":[{"type":"text","text":"existing"}],"messages":[]}`)
	got, changed = ApplyPluginRequestInstructions(anthropic, APIProtocolAnthropic, "Prepend")
	require.True(t, changed)
	var root map[string]any
	require.NoError(t, json.Unmarshal(got, &root))
	system := root["system"].([]any)
	require.Equal(t, "Prepend", system[0].(map[string]any)["text"])
	require.Equal(t, "existing", system[1].(map[string]any)["text"])

	gemini := []byte(`{"contents":[],"systemInstruction":{"parts":[{"text":"existing"}]}}`)
	got, changed = ApplyPluginRequestInstructions(gemini, "gemini", "Prepend")
	require.True(t, changed)
	require.Contains(t, string(got), `"systemInstruction":{"parts":[{"text":"Prepend"},{"text":"existing"}]}`)

	_, changed = ApplyPluginRequestInstructions([]byte(`not json`), APIProtocolChatCompletions, "Prepend")
	require.False(t, changed)
	_, changed = ApplyPluginRequestInstructions(chat, APIProtocolChatCompletions, "")
	require.False(t, changed)
}
