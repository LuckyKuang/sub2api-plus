//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeImageAccessUsesExistingGatewayCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, platform, credentialType, public, mapped, endpoint string
		image                                                    bool
	}{
		{"gpt_api_key", PlatformOpenAI, AccountTypeAPIKey, "gpt-image-1", "gpt-image-1", "/v1/images/generations", true},
		{"gpt_oauth", PlatformOpenAI, AccountTypeOAuth, "gpt-image-2", "gpt-image-2", "/v1/images/generations", true},
		{"compatible_gemini", PlatformOpenAI, AccountTypeAPIKey, "gemini-3.1-flash-image", "gemini-3.1-flash-image", "/v1/images/generations", true},
		{"compatible_banana", PlatformOpenAI, AccountTypeAPIKey, "nano-banana-2", "nano-banana-2", "/v1/images/generations", true},
		{"no_oauth_compatible_bridge", PlatformOpenAI, AccountTypeOAuth, "gemini-3.1-flash-image", "gemini-3.1-flash-image", "", true},
		{"unsupported_credentials", PlatformOpenAI, "unsupported", "gpt-image-1", "gpt-image-1", "", true},
		{"invalid_public_alias", PlatformOpenAI, AccountTypeAPIKey, "art-alias", "gpt-image-1", "", true},
		{"composite_rewritten_alias", PlatformOpenAI, AccountTypeAPIKey, "gpt-image-1", "gpt-image-1", "/v1/images/generations", true},
		{"grok", PlatformGrok, AccountTypeAPIKey, "grok-imagine-image", "grok-imagine-image", "/v1/images/generations", true},
		{"grok_alias", PlatformGrok, AccountTypeAPIKey, "art-alias", "grok-imagine-image", "/v1/images/generations", true},
		{"edit_requires_source", PlatformGrok, AccountTypeAPIKey, "grok-imagine-edit", "grok-imagine-edit", "", true},
		{"normalized_edit_requires_source", PlatformGrok, AccountTypeAPIKey, "grok-imagine-edit", " Grok-Imagine-Edit ", "", true},
		{"gemini", PlatformGemini, AccountTypeAPIKey, "gemini-2.5-flash-image", "gemini-2.5-flash-image", "/v1beta", true},
		{"gemini_alias", PlatformAntigravity, AccountTypeOAuth, "art-alias", "gemini-3-pro-image-preview", "/v1beta", true},
		{"no_cross_platform_guess", PlatformAnthropic, AccountTypeAPIKey, "gpt-image-1", "gpt-image-1", "", false},
		{"text_model", PlatformOpenAI, AccountTypeAPIKey, "gpt-5.4", "gpt-5.4", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, image := NativeImageAccess(&Account{Platform: tc.platform, Type: tc.credentialType}, tc.public, tc.mapped)
			require.Equal(t, tc.endpoint, endpoint)
			require.Equal(t, tc.image, image)
		})
	}
}

func TestAggregateNativeImageOffersRespectPermissionAndAliases(t *testing.T) {
	svc, _, groups := aggregateTestService()
	accounts := svc.aggregateAccounts.(*aggregateTestAccounts)
	for _, id := range []int64{1, 2} {
		accounts.byGroup[id][0].Credentials["model_mapping"] = map[string]any{
			"gpt-image-1": "gpt-image-1", "private-alias": "gpt-image-1", "text": "gpt-5.4",
		}
	}
	groups.groups[0].AllowImageGeneration = true // group 2; group 1 denies images.
	key, err := svc.AggregateScope(t.Context(), &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	offers, err := svc.AggregateNativeModels(t.Context(), key)
	require.NoError(t, err)
	byName := map[string]AggregateNativeModel{}
	for _, offer := range offers {
		byName[offer.Name] = offer
	}
	require.Equal(t, []string{"/v1/images/generations"}, byName["gpt-image-1"].Endpoints)
	require.Equal(t, []int64{2}, byName["gpt-image-1"].GroupIDs)
	require.Equal(t, []int64{2}, byName["gpt-image-1"].ImageGroupIDs)
	require.True(t, byName["gpt-image-1"].IsImage)
	require.Empty(t, byName["gpt-image-1"].ConversionEndpoints)
	require.NotContains(t, byName, "private-alias", "endpoint basic validation cannot admit arbitrary image aliases")
	require.ElementsMatch(t, []string{"/v1/responses", "/v1/chat/completions"}, byName["text"].Endpoints)
	require.False(t, byName["text"].IsImage)
	bound, err := svc.BindAggregateGroup(t.Context(), key, "gpt-image-1", "", 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, *bound.GroupID)
	_, err = svc.BindAggregateGroup(t.Context(), key, "gpt-image-1", "", 99)
	require.Error(t, err, "the image hint cannot escape the authorized group scope")
}

func TestAggregateNativeImageOffersKeepImageGroupOwnership(t *testing.T) {
	svc, _, groups := aggregateTestService()
	accounts := svc.aggregateAccounts.(*aggregateTestAccounts)
	accounts.byGroup[1][0].Credentials["model_mapping"] = map[string]any{"gpt-image-1": "gpt-5.4"}
	accounts.byGroup[2][0].Credentials["model_mapping"] = map[string]any{"gpt-image-1": "gpt-image-1"}
	groups.groups[0].AllowImageGeneration = true
	key, err := svc.AggregateScope(t.Context(), &APIKey{UserID: 9, GroupBindingMode: GroupBindingAll})
	require.NoError(t, err)
	offers, err := svc.AggregateNativeModels(t.Context(), key)
	require.NoError(t, err)
	require.Len(t, offers, 1)
	require.Equal(t, []int64{1, 2}, offers[0].GroupIDs)
	require.Equal(t, []int64{2}, offers[0].ImageGroupIDs)
	require.ElementsMatch(t, []string{"/v1/responses", "/v1/chat/completions", "/v1/images/generations"}, offers[0].Endpoints)
	require.True(t, offers[0].IsImage)
	bound, err := svc.BindAggregateGroup(t.Context(), key, "gpt-image-1", "", offers[0].ImageGroupIDs[0])
	require.NoError(t, err)
	require.EqualValues(t, 2, *bound.GroupID)
}
