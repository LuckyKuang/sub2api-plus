//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPublicModels(t *testing.T) {
	cases := []struct {
		name     string
		account  Account
		platform string
		want     []string
	}{
		{
			name: "configured mapping is authoritative",
			account: Account{Platform: PlatformOpenAI, Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-image-2": "gpt-image-2", "nano-banana-pro": "upstream"},
			}},
			want: []string{"gpt-image-2", "nano-banana-pro"},
		},
		{
			name:    "passthrough account exposes the platform catalog",
			account: Account{Platform: PlatformOpenAI, Extra: map[string]any{"openai_passthrough": true}, Credentials: map[string]any{"model_mapping": map[string]any{"stale": "stale"}}},
			want:    PlatformDefaultModelIDs(PlatformOpenAI),
		},
		{
			name:    "unmapped OAuth account contributes nothing on its own",
			account: Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AccountPublicModels(&tc.account, tc.platform)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestAccountPublicModelsMixedListingKeepsOnlyListingPlatform(t *testing.T) {
	// Antigravity accounts carry implicit default mappings; a gemini listing must
	// expose only their gemini entries.
	account := &Account{Platform: PlatformAntigravity, Credentials: map[string]any{
		"model_mapping": map[string]any{"claude-sonnet-5": "claude-sonnet-5", "gemini-3-pro-image": "gemini-3-pro-image"},
	}}
	got := AccountPublicModels(account, PlatformGemini)
	require.Contains(t, got, "gemini-3-pro-image")
	require.NotContains(t, got, "claude-sonnet-5")
	for _, model := range got {
		require.Contains(t, model, "gemini", model)
	}
}

func TestPlatformDefaultModelIDsSkipCNVendors(t *testing.T) {
	// CN vendors have no maintained static catalog: a group-wide default would
	// invent models the upstream may not serve.
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu, PlatformStepFun} {
		require.False(t, platformHasStaticDefaultModels(platform), platform)
	}
	require.True(t, platformHasStaticDefaultModels(PlatformOpenAI))
	require.True(t, platformHasStaticDefaultModels(PlatformAnthropic))
}
