//go:build unit || !integration

package service

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// Every model the reviewed Codex release ships must be served from the pinned
// catalog, not from a synthesized descriptor: the catalog owns the numbers the
// client reads, and Plus owns only catalog policy.
func TestConfiguredCodexDescriptorsFollowOfficialCatalog(t *testing.T) {
	slugs := openai.CodexModelCatalogSlugs()
	require.NotEmpty(t, slugs)
	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			official, ok := openai.CodexModelCatalogMetadata(slug)
			require.True(t, ok)

			var want struct {
				Slug           string `json:"slug"`
				DisplayName    string `json:"display_name"`
				ContextWindow  int64  `json:"context_window"`
				MaxContext     int64  `json:"max_context_window"`
				Priority       int    `json:"priority"`
				DefaultLevel   string `json:"default_reasoning_level"`
				ShellType      string `json:"shell_type"`
				ToolMode       any    `json:"tool_mode"`
				MinimalClient  string `json:"minimal_client_version"`
				SupportsSearch bool   `json:"supports_search_tool"`
			}
			require.NoError(t, json.Unmarshal(official, &want))

			descriptor := newConfiguredCodexModelDescriptor(slug)

			require.Equal(t, slug, descriptor.Slug)
			require.Equal(t, want.ContextWindow, descriptor.ContextWindow)
			require.Equal(t, want.MaxContext, descriptor.MaxContextWindow)
			require.Equal(t, want.Priority, descriptor.Priority)
			require.Equal(t, want.DisplayName, descriptor.DisplayName)
			require.Equal(t, want.MinimalClient, stringOfAny(descriptor.MinimalClientVersion))
			require.Equal(t, want.ToolMode, descriptor.ToolMode)
			require.Equal(t, want.DefaultLevel, stringOfPointer(descriptor.DefaultReasoningLevel))
			require.Equal(t, official, []byte(descriptor.officialMetadata))
			// shell_type and input_modalities are Plus-owned policy, covered by
			// their own tests below.
			// An administrator-configured model stays selectable even when the
			// official catalog hides it.
			require.Equal(t, "list", descriptor.Visibility)
		})
	}
}

// The reviewed descriptor must win over the historical Plus override: sol moved
// from 272k to 500k upstream, so a stale local constant would silently downgrade
// the window the client negotiates.
func TestConfiguredCodexGPT61WindowsFollowOfficialCatalog(t *testing.T) {
	sol := newConfiguredCodexModelDescriptor("gpt-6.1-sol")
	require.Equal(t, int64(500_000), sol.ContextWindow)
	require.Equal(t, int64(872_000), sol.MaxContextWindow)

	terra := newConfiguredCodexModelDescriptor("gpt-6.1-terra")
	require.Equal(t, int64(500_000), terra.ContextWindow)
	require.Equal(t, int64(872_000), terra.MaxContextWindow)

	astra := newConfiguredCodexModelDescriptor("gpt-6.2-astra")
	require.Equal(t, int64(272_000), astra.ContextWindow)
	require.Equal(t, int64(872_000), astra.MaxContextWindow)
	// Image input stays gated on Plus evidence: the official snapshot declares it
	// for every model, but a false claim makes the client send images an upstream
	// may reject. Astra alone carries the verified default.
	require.Equal(t, []string{"text"}, astra.InputModalities)
	require.Equal(t, []string{"text", "image"}, newConfiguredCodexModelDescriptor("gpt-6-astra").InputModalities)
}

func TestConfiguredCodexAliasesUseOfficialModelDescriptor(t *testing.T) {
	for _, test := range []struct {
		slug             string
		contextWindow    int64
		maxContextWindow int64
	}{
		{"gpt-6.1-sol-high", 500_000, 872_000},
		{"gpt-6.1-terra-openai-compact", 500_000, 872_000},
		{"gpt-6.2-astra-high", 272_000, 872_000},
		{"gpt-6.2-sol-openai-compact", 500_000, 872_000},
	} {
		t.Run(test.slug, func(t *testing.T) {
			descriptor := newConfiguredCodexModelDescriptor(test.slug)
			require.Equal(t, test.slug, descriptor.Slug)
			require.Equal(t, test.contextWindow, descriptor.ContextWindow)
			require.Equal(t, test.maxContextWindow, descriptor.MaxContextWindow)
			require.NotEmpty(t, descriptor.officialMetadata)
		})
	}
	_, ok := openai.CodexModelCatalogMetadata("gpt-6.2-sol-custom")
	require.False(t, ok)
}

// The shell tool surface is Plus-owned: adopting the 0.170 shell_command
// declaration changes the tools the client announces and needs its own
// security-audit coverage revision.
func TestConfiguredCodexDescriptorsKeepAuditedShellType(t *testing.T) {
	for _, slug := range openai.CodexModelCatalogSlugs() {
		descriptor := newConfiguredCodexModelDescriptor(slug)
		require.Equal(t, configuredCodexShellType, descriptor.ShellType, slug)
	}
}

// Ultrafast exists only as a Plus declaration; the official catalog must never
// gain it, and the tier must survive the official overlay.
func TestConfiguredCodexUltrafastStaysPlusOnly(t *testing.T) {
	official, ok := openai.CodexModelCatalogMetadata("gpt-5.6-sol")
	require.True(t, ok)
	require.NotContains(t, string(official), OpenAIFastTierUltrafast,
		"the official catalog must not declare the Plus-only ultrafast tier")

	descriptor := newConfiguredCodexModelDescriptor("gpt-5.6-sol")
	var ids []string
	for _, tier := range descriptor.ServiceTiers {
		ids = append(ids, tier.ID)
	}
	require.Contains(t, ids, OpenAIFastTierPriority)
	require.Contains(t, ids, OpenAIFastTierUltrafast)
}

// A fail-closed conflict clears the reasoning slider. The official snapshot must
// not refill it during marshalling, or an ambiguous alias would advertise the
// wrong levels to the client.
func TestOfficialCatalogDoesNotResurrectClearedDescriptorFields(t *testing.T) {
	descriptor := newConfiguredCodexModelDescriptor("gpt-6-astra")
	require.NotEmpty(t, descriptor.SupportedReasoningLevels)

	applyUpstreamModelMetadataToCodexDescriptor(&descriptor, codexModelMetadataOverride{reasoningConflict: true})
	require.Nil(t, descriptor.DefaultReasoningLevel)
	require.Empty(t, descriptor.SupportedReasoningLevels)

	encoded, err := json.Marshal(descriptor)
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.NotContains(t, decoded, "default_reasoning_level")
	require.JSONEq(t, `[]`, string(decoded["supported_reasoning_levels"]))
	// Fields the descriptor does not model still pass through from the snapshot.
	require.Contains(t, decoded, "guardian")
	require.Contains(t, decoded, "available_in_plans")
}

// The official catalog declares the ChatGPT-only Responses Lite wire. API-key
// routes must not inherit it without their own snapshot evidence.
func TestAPIKeyRoutesDoNotInheritOfficialResponsesLite(t *testing.T) {
	account := Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "https://relay.example/v1",
	}}

	capabilities := accountCodexToolCapabilities(&account, "gpt-6-astra")
	require.JSONEq(t, `false`, string(capabilities["use_responses_lite"]))
	require.JSONEq(t, `null`, string(capabilities["tool_mode"]))

	// An explicit snapshot remains authoritative for a model the gateway has no
	// standing "no Lite" rule for.
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-5.5": {CodexToolCapabilities: map[string]json.RawMessage{
			"use_responses_lite": json.RawMessage("true"),
			"tool_mode":          json.RawMessage(`"code_mode_only"`),
		}},
	}})
	declared := accountCodexToolCapabilities(&account, "gpt-5.5")
	require.JSONEq(t, `true`, string(declared["use_responses_lite"]))
	require.JSONEq(t, `"code_mode_only"`, string(declared["tool_mode"]))
}

func stringOfPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringOfAny(value any) string {
	text, _ := value.(string)
	return text
}
