package service

// OutboundIdentityAccountPolicy is shared by management, previews and sending.
// A fixed client family still permits valid account-level declaration overrides.
type OutboundIdentityAccountPolicy struct {
	Key                 string   `json:"key"`
	NativePreset        string   `json:"native_preset"`
	AllowedPresets      []string `json:"allowed_presets"`
	AllowDefaultMapping bool     `json:"allow_default_mapping"`
}

// Only compatible suppliers and relay account types may change client families.
// Keep the persistence migration and explicit contract tests in sync with this list.
var compatibleOutboundDefaultKeys = []string{
	"openai:apikey", "openai:upstream",
	"anthropic:apikey", "anthropic:upstream",
	"gemini:apikey", "gemini:upstream",
	"grok:apikey", "grok:upstream",
	"antigravity:upstream", "typesafe:apikey", "opencode_go:apikey",
}

func allowsOutboundDefaultMapping(key string) bool {
	for _, allowed := range compatibleOutboundDefaultKeys {
		if key == allowed {
			return true
		}
	}
	return false
}

func outboundAccountPolicy(platform, accountType string) OutboundIdentityAccountPolicy {
	preset := nativeAccountOutboundPreset(platform, accountType)
	policy := OutboundIdentityAccountPolicy{
		Key: platform + ":" + accountType, NativePreset: preset, AllowedPresets: []string{preset},
	}
	policy.AllowDefaultMapping = allowsOutboundDefaultMapping(policy.Key)
	if policy.AllowDefaultMapping {
		policy.AllowedPresets = append([]string(nil), outboundPresetNames...)
	}
	return policy
}

func outboundAccountPolicies() []OutboundIdentityAccountPolicy {
	var policies []OutboundIdentityAccountPolicy
	// Describe every account key the existing account API accepts. Only the
	// explicit compatible keys appear in the advanced default-mapping editor.
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok, PlatformAntigravity, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformStepFun, PlatformOpenCodeGo, PlatformTypeSafe} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey, AccountTypeUpstream, AccountTypeBedrock, AccountTypeServiceAccount} {
			policies = append(policies, outboundAccountPolicy(platform, accountType))
		}
	}
	return policies
}
