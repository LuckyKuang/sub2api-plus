//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Batch 3.1: a Codex-protocol account whose upstream reported model differs from
// the baseline billing model must bill the server-reported model (identified
// pricing only), keeping the audit chain intact. This is the half of row 3.1
// that is not covered by the response-model observation test.
func TestOpenAIGatewayServiceRecordUsage_CodexServerModelCorrectsBilling(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}
	baseline, server, _, serverCost := orderedResponseBillingModels(t, svc.billingService, tokens, openAICheapFixtureModel, openAIPriceyFixtureModel)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:             "codex_server_model_billing_correction",
			Model:                 baseline,
			UpstreamModel:         baseline,
			UpstreamResponseModel: server,
			Usage:                 OpenAIUsage{InputTokens: 20, OutputTokens: 10},
			Duration:              time.Second,
		},
		APIKey:  &APIKey{ID: 10},
		User:    &User{ID: 20},
		Account: &Account{ID: 31, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		ChannelUsageFields: ChannelUsageFields{
			ChannelID:          9,
			OriginalModel:      baseline,
			ChannelMappedModel: baseline,
			BillingModelSource: BillingModelSourceResponse,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, serverCost.ActualCost, usageRepo.lastLog.ActualCost, 1e-12,
		"an identified server-reported model must correct the billing record")
	require.InDelta(t, serverCost.ActualCost, userRepo.lastAmount, 1e-12)
	// Audit chain still reports the requested baseline model.
	require.Equal(t, baseline, usageRepo.lastLog.Model)
	require.NotNil(t, usageRepo.lastLog.UpstreamResponseModel)
	require.Equal(t, server, *usageRepo.lastLog.UpstreamResponseModel)
}

// The correction is Codex-protocol only and refuses unidentified, conflicting,
// and media-surcharged observations. Each guard case bills the request twice —
// once with a diverging server-reported model and once without it — and requires
// the recorded cost to be identical, which proves the server model was ignored
// without hard-coding any pricing value.
func TestOpenAIGatewayServiceRecordUsage_CodexServerModelCorrectionGuards(t *testing.T) {
	tokens := UsageTokens{InputTokens: 20, OutputTokens: 10}

	record := func(t *testing.T, account *Account, result *OpenAIForwardResult) float64 {
		t.Helper()
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		userRepo := &openAIRecordUsageUserRepoStub{}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
		baseline, _, _, _ := orderedResponseBillingModels(t, svc.billingService, tokens, openAICheapFixtureModel, openAIPriceyFixtureModel)
		result.Model = baseline
		result.UpstreamModel = baseline
		result.Usage = OpenAIUsage{InputTokens: 20, OutputTokens: 10}
		result.Duration = time.Second
		result.RequestID = "codex_server_model_guard_" + account.Type
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result:  result,
			APIKey:  &APIKey{ID: 10},
			User:    &User{ID: 20},
			Account: account,
			ChannelUsageFields: ChannelUsageFields{
				ChannelID:          9,
				OriginalModel:      baseline,
				ChannelMappedModel: baseline,
				// The channel opt-in response-model billing path is deliberately
				// disabled so only the new Codex branch is exercised.
				BillingModelSource: BillingModelSourceRequested,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, usageRepo.lastLog)
		require.Equal(t, baseline, usageRepo.lastLog.Model, "the audit chain keeps the baseline model")
		return usageRepo.lastLog.ActualCost
	}

	guardWithServerModel := func(t *testing.T, name string, account *Account, mutate func(*OpenAIForwardResult), serverModel string) {
		t.Helper()
		baselineRun := &OpenAIForwardResult{}
		mutate(baselineRun)
		control := record(t, account, baselineRun)

		divergingRun := &OpenAIForwardResult{UpstreamResponseModel: serverModel}
		mutate(divergingRun)
		diverging := record(t, account, divergingRun)

		require.InDelta(t, control, diverging, 1e-12,
			"%s: the server-reported model must not change the recorded cost", name)
	}

	guard := func(t *testing.T, name string, account *Account, mutate func(*OpenAIForwardResult)) {
		t.Helper()
		guardWithServerModel(t, name+" (identified server model)", account, mutate, openAIPriceyFixtureModel)
	}

	// Non-Codex (API-key) accounts never take the Codex branch.
	guard(t, "api-key account", &Account{ID: 40, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, func(*OpenAIForwardResult) {})

	// A conflicting observation is never used to bill.
	guard(t, "conflicting observation", &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, func(r *OpenAIForwardResult) {
		r.UpstreamResponseModelConflict = true
	})

	// Image-surcharged requests keep the baseline model.
	guard(t, "image surcharge", &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, func(r *OpenAIForwardResult) {
		r.ImageCount = 1
	})

	// Web-search surcharges keep the baseline model too.
	guard(t, "web search surcharge", &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, func(r *OpenAIForwardResult) {
		r.WebSearchCalls = 2
	})

	// Audio usage keeps the baseline model.
	guard(t, "audio usage", &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, func(r *OpenAIForwardResult) {
		r.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: 1}
	})

	// Unidentified server models keep the baseline model.
	guardWithServerModel(t, "unidentified server model",
		&Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		func(*OpenAIForwardResult) {}, "gpt-9.9-codex-unpriced")
}
