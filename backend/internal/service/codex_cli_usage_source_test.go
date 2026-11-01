//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/stretchr/testify/require"
)

func TestCodexCLITaskUsageSourceAndAtomicBillingCommand(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		reported, incomplete bool
		tokens               int64
		source               string
		cost                 float64
	}{
		{"successful measured", true, false, 1_000_000, UsageSourceUpstreamExact, 2.2},
		{"failed after measurement", true, true, 1_000_000, UsageSourceUpstreamExact, 2.2},
		{"known partial", false, true, 1_000_000, UsageSourcePartial, 2.2},
		{"measured zero", true, false, 0, UsageSourceUpstreamExact, 0},
		{"failed before measurement", false, true, 0, UsageSourceUnknown, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := codexCLITaskForwardResult(codexcli.TaskResult{UsageReported: tc.reported, UsageIncomplete: tc.incomplete, Usage: codexcli.TaskUsage{InputTokens: tc.tokens}}, CodexCLITaskRequest{Model: "gpt-6.1-sol"}, "gpt-6.1-sol", "resp_task_usage", time.Second)
			result.Outbound = &OutboundMetadata{Transport: OutboundCodexCLITask, Evidence: "cli_process", CLIVersion: "0.165.0"}
			require.Equal(t, tc.source, result.UsageSource)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}))
			require.NotNil(t, billing.lastCmd, "even unknown zero usage must have a persistent, idempotent record")
			require.NotNil(t, billing.lastCmd.UsageLog, "usage stays in the monetary transaction")
			log := billing.lastCmd.UsageLog
			require.Equal(t, tc.source, log.UsageSource)
			require.Equal(t, !tc.incomplete, *log.IsComplete)
			require.Equal(t, int(tc.tokens), log.InputTokens)
			require.InDelta(t, tc.cost, billing.lastCmd.BalanceCost, 1e-12)
			require.Equal(t, result.Outbound, log.Outbound)
			require.NotSame(t, result.Outbound, log.Outbound)
			require.Zero(t, logs.calls, "successful atomic persistence must not write a second log")
		})
	}
}

func TestRecordUsageUsesDispatchedTierAndPreservesBillingContracts(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(kind, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			stale := "default"
			metadata := &OutboundMetadata{Transport: OutboundCodexCLIBridge, Evidence: "gateway_send", CLIVersion: "0.165.0", TierKnown: true, ServiceTier: "priority"}
			result := &OpenAIForwardResult{Model: "gpt-6.1-sol", RequestID: "resp_tier", ServiceTier: &stale, UpstreamResponseServiceTier: "default", ResponseServiceTier: "default", Outbound: metadata, Usage: OpenAIUsage{InputTokens: 1_000_000}, Duration: time.Second}
			require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3, Platform: PlatformOpenAI, Type: kind}}))
			require.Equal(t, "priority", *logs.lastLog.RequestedServiceTier)
			require.Equal(t, "default", *logs.lastLog.ResponseServiceTier)
			if kind == AccountTypeAPIKey {
				require.Equal(t, "default", *logs.lastLog.ServiceTier)
				require.InDelta(t, 2.2, logs.lastLog.ActualCost, 1e-12)
			} else {
				require.Equal(t, "priority", *logs.lastLog.ServiceTier)
				require.InDelta(t, 4.4, logs.lastLog.ActualCost, 1e-12)
			}
		})
	}
}
