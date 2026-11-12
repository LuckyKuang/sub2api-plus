//go:build unit || !integration

package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFilterSchedulerCredentialsKeepsSubscriptionPlanType(t *testing.T) {
	filtered := filterSchedulerCredentials(map[string]any{
		"plan_type":     "plus",
		"access_token":  "secret-access-token",
		"refresh_token": "secret-refresh-token",
	})

	require.Equal(t, "plus", filtered["plan_type"])
	require.NotContains(t, filtered, "access_token")
	require.NotContains(t, filtered, "refresh_token")
}

func TestFilterSchedulerCredentialsKeepsGeminiExplicitCacheKeys(t *testing.T) {
	filtered := filterSchedulerCredentials(map[string]any{
		"api_key":              "k",
		"base_url":             "https://gw.example.com",
		"auth_mode":            "apikey",
		"service_account_json": "secret",
		service.GeminiExplicitCacheUpstreamCredentialKey:       true,
		service.GeminiExplicitCacheUpstreamMaxTTLCredentialKey: 3600,
	})

	require.Equal(t, "https://gw.example.com", filtered["base_url"])
	require.Equal(t, "apikey", filtered["auth_mode"])
	require.Equal(t, true, filtered[service.GeminiExplicitCacheUpstreamCredentialKey])
	require.Equal(t, 3600, filtered[service.GeminiExplicitCacheUpstreamMaxTTLCredentialKey])
	require.NotContains(t, filtered, "service_account_json")
}

func TestSchedulerMetadataAccountKeepsOpenAISubscriptionIdentity(t *testing.T) {
	account := service.Account{
		ID:       24,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"plan_type":    "plus",
			"access_token": "secret-access-token",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)

	require.True(t, metadata.IsOpenAIChatGPTSubscription())
	require.Empty(t, metadata.GetCredential("access_token"))
}

func TestSchedulerMetadataAccountDropsRetiredUpstreamBillingProbe(t *testing.T) {
	metadata := buildSchedulerMetadataAccount(service.Account{
		Extra: map[string]any{
			"upstream_billing_probe": map[string]any{"status": "ok"},
			"unused_large_field":     "drop-me",
		},
	})
	require.NotContains(t, metadata.Extra, "upstream_billing_probe")
	require.NotContains(t, metadata.Extra, "unused_large_field")
}

func TestSchedulerMetadataAccountKeepsProArgonQuotaIsolation(t *testing.T) {
	account := service.Account{
		ID: 25, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"plan_type": "Pro", "access_token": "secret-access-token", "refresh_token": "secret-refresh-token",
		},
		Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"antigravity:gemini:argon": map[string]any{
					"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				},
			},
		},
	}
	_, payload, err := marshalSchedulerCacheAccount(account)
	require.NoError(t, err)
	var metadata service.Account
	require.NoError(t, json.Unmarshal([]byte(payload), &metadata))
	require.Equal(t, "Pro", metadata.GetCredential("plan_type"))
	require.Empty(t, metadata.GetCredential("access_token"))
	require.Empty(t, metadata.GetCredential("refresh_token"))
	require.False(t, metadata.IsSchedulableForModel("gemini-4-argon-low"))
	require.Positive(t, metadata.GetRateLimitRemainingTime("gemini-4-argon-low"))
	require.True(t, metadata.IsSchedulableForModel("gemini-3.8-flash"))
	require.Zero(t, metadata.GetRateLimitRemainingTime("gemini-3.8-flash"))
}
