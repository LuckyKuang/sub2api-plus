//go:build unit || !integration

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBillingServiceTier(t *testing.T) {
	tests := []struct {
		name       string
		requested  string
		observed   string
		billing    string
		downgraded bool
	}{
		{name: "openai priority served as default", requested: "priority", observed: "default", billing: "default", downgraded: true},
		{name: "anthropic fast served as standard", requested: "fast", observed: "standard", billing: "standard", downgraded: true},
		{name: "ultrafast served as priority", requested: "ultrafast", observed: "priority", billing: "priority", downgraded: true},
		{name: "ultrafast served as default", requested: "ultrafast", observed: "default", billing: "default", downgraded: true},
		{name: "ultrafast honoured", requested: "ultrafast", observed: "ultrafast", billing: "ultrafast"},
		{name: "ultrafast missing declaration", requested: "ultrafast", billing: "ultrafast"},
		{name: "response cannot upgrade to ultrafast", requested: "priority", observed: "ultrafast", billing: "priority"},
		{name: "priority honoured", requested: "priority", observed: "priority", billing: "priority"},
		{name: "no declaration keeps request", requested: "priority", observed: "", billing: "priority"},
		{name: "no request no declaration", requested: "", observed: "", billing: ""},
		{name: "response never raises the tier", requested: "", observed: "priority", billing: ""},
		{name: "flex never raised to default", requested: "flex", observed: "default", billing: "flex"},
		{name: "default echoed for untiered request", requested: "", observed: "default", billing: ""},
		{name: "unknown response tier ignored", requested: "priority", observed: "turbo", billing: "priority"},
		{name: "case and whitespace normalised", requested: " Priority ", observed: "DEFAULT", billing: "default", downgraded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingServiceTier(tt.requested, tt.observed)
			require.Equal(t, tt.billing, got.Billing)
			require.Equal(t, tt.downgraded, got.Downgraded)
		})
	}
}

func TestResolveOpenAIServiceTierBilling(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		requested  string
		observed   string
		billing    string
		downgraded bool
	}{
		{name: "api key fast default echo", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "priority", observed: "default", billing: "priority"},
		{name: "api key fast alias default echo", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "fast", observed: "default", billing: "fast"},
		{name: "oauth fast default echo", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, requested: "priority", observed: "default", billing: "priority"},
		{name: "setup token fast default echo", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken}, requested: "priority", observed: "default", billing: "priority"},
		{name: "oauth ultrafast default echo keeps existing behaviour", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, requested: "ultrafast", observed: "default", billing: "ultrafast"},
		{name: "api key ultrafast explicit default still downgrades", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "ultrafast", observed: "default", billing: "default", downgraded: true},
		{name: "api key explicit flex downgrade", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "priority", observed: "flex", billing: "flex", downgraded: true},
		{name: "filtered fast is settled at the sent tier", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "", observed: "default", billing: ""},
		{name: "missing response keeps the sent tier", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "priority", billing: "priority"},
		{name: "response cannot raise an untiered request", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "", observed: "priority", billing: ""},
		{name: "unknown response leaves the sent tier", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, requested: "priority", observed: "turbo", billing: "priority"},
		{name: "non-openai fast default echo keeps the generic contract", account: &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}, requested: "priority", observed: "default", billing: "default", downgraded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveOpenAIServiceTierBilling(tt.account, tt.requested, tt.observed)
			require.Equal(t, tt.billing, got.Billing)
			require.Equal(t, tt.downgraded, got.Downgraded)
			require.Equal(t, normalizeBillingServiceTier(tt.requested), got.Requested)
			require.Equal(t, normalizeBillingServiceTier(tt.observed), got.Observed)
		})
	}
}

func TestApplyServiceTierBillingResolutionOnlyRewritesDowngrades(t *testing.T) {
	t.Run("default echo is the only non-authoritative response tier", func(t *testing.T) {
		require.True(t, openAIResponseDefaultTierIsNonAuthoritative("default"))
		require.False(t, openAIResponseDefaultTierIsNonAuthoritative("standard"))
		require.False(t, openAIResponseDefaultTierIsNonAuthoritative("flex"))
	})

	t.Run("openai api key default echo keeps fast tier", func(t *testing.T) {
		requested := "priority"
		result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "default"}
		resolution := ApplyOpenAIServiceTierBillingResolution(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, result)
		require.False(t, resolution.Downgraded)
		require.Equal(t, "priority", resolution.Billing)
		require.Same(t, &requested, result.ServiceTier)
	})

	t.Run("openai api key explicit flex downgrade rewrites tier", func(t *testing.T) {
		requested := "priority"
		result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "flex"}
		resolution := ApplyOpenAIServiceTierBillingResolution(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, result)
		require.True(t, resolution.Downgraded)
		require.NotNil(t, result.ServiceTier)
		require.Equal(t, "flex", *result.ServiceTier)
	})

	t.Run("openai honoured tier keeps pointer", func(t *testing.T) {
		requested := "priority"
		result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "priority"}
		require.False(t, ApplyOpenAIServiceTierBillingResolution(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, result).Downgraded)
		require.Same(t, &requested, result.ServiceTier)
	})

	t.Run("openai untiered request stays nil", func(t *testing.T) {
		result := &OpenAIForwardResult{UpstreamResponseServiceTier: "priority"}
		require.False(t, ApplyOpenAIServiceTierBillingResolution(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, result).Downgraded)
		require.Nil(t, result.ServiceTier)
	})

	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run("codex "+accountType+" keeps outbound priority despite default echo", func(t *testing.T) {
			requested := "priority"
			result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "default"}
			resolution := ApplyOpenAIServiceTierBillingResolution(
				&Account{Platform: PlatformOpenAI, Type: accountType},
				result,
			)
			require.False(t, resolution.Downgraded)
			require.Equal(t, "priority", resolution.Requested)
			require.Equal(t, "default", resolution.Observed)
			require.Equal(t, "priority", resolution.Billing)
			require.Same(t, &requested, result.ServiceTier)
		})

		t.Run("codex "+accountType+" still accepts an explicit flex downgrade", func(t *testing.T) {
			requested := "priority"
			result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "flex"}
			resolution := ApplyOpenAIServiceTierBillingResolution(
				&Account{Platform: PlatformOpenAI, Type: accountType},
				result,
			)
			require.True(t, resolution.Downgraded)
			require.Equal(t, "flex", resolution.Billing)
			require.Equal(t, "flex", *result.ServiceTier)
		})

		t.Run("codex "+accountType+" response never promotes an untiered request", func(t *testing.T) {
			result := &OpenAIForwardResult{UpstreamResponseServiceTier: "priority"}
			resolution := ApplyOpenAIServiceTierBillingResolution(
				&Account{Platform: PlatformOpenAI, Type: accountType},
				result,
			)
			require.False(t, resolution.Downgraded)
			require.Empty(t, resolution.Billing)
			require.Nil(t, result.ServiceTier)
		})
	}

	t.Run("non-openai oauth still uses the generic response contract", func(t *testing.T) {
		requested := "priority"
		result := &OpenAIForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "default"}
		resolution := ApplyOpenAIServiceTierBillingResolution(
			&Account{Platform: PlatformGrok, Type: AccountTypeOAuth},
			result,
		)
		require.True(t, resolution.Downgraded)
		require.Equal(t, "default", resolution.Billing)
		require.Equal(t, "default", *result.ServiceTier)
	})

	t.Run("anthropic standard speed rewrites fast", func(t *testing.T) {
		requested := "fast"
		result := &ForwardResult{ServiceTier: &requested, UpstreamResponseServiceTier: "standard"}
		require.True(t, ApplyForwardServiceTierBillingResolution(result).Downgraded)
		require.Equal(t, "standard", *result.ServiceTier)
	})

	t.Run("nil results are ignored", func(t *testing.T) {
		require.False(t, ApplyOpenAIServiceTierBillingResolution(nil, nil).Downgraded)
		require.False(t, ApplyForwardServiceTierBillingResolution(nil).Downgraded)
	})
}
