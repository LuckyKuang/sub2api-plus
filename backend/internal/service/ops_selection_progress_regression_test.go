//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/usagestats"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Approved audit: a known empty candidate pool is zero, not an omitted/unknown
// value, and compact exclusions must carry the same actual selection evidence.
func TestAuditSelectionKnownZeroAndCompactDiagnostics(t *testing.T) {
	for _, compact := range []bool{false, true} {
		err := noAvailableOpenAISelectionErrorWithStats("test-model", compact, openAISelectionFilterStats{pool: 0}, "")
		d := OpsRoutingDiagnosticsFromSelectionError(fmt.Errorf("selection failed: %w", err))
		require.NotNil(t, d, "wrapped no-account errors retain their diagnosis")
		c, _ := newOpenAITransportErrTestContext()
		SetOpsRoutingDiagnostics(c, d)
		SetOpsRoutingDiagnostics(c, &OpsRoutingDiagnostics{OutboundIdentitySource: "account"})
		payload, marshalErr := json.Marshal(GetOpsRoutingDiagnostics(c))
		require.NoError(t, marshalErr)
		var saved map[string]any
		require.NoError(t, json.Unmarshal(payload, &saved))
		require.Equal(t, float64(0), saved["candidate_pool"], "operators can distinguish an empty pool from unavailable telemetry")
		if compact {
			require.ErrorIs(t, err, ErrNoAvailableCompactAccounts)
		} else {
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
		}
	}
	c, _ := newOpenAITransportErrTestContext()
	SetOpsRoutingDiagnostics(c, &OpsRoutingDiagnostics{SelectionDecision: "no_available_account"})
	payload, err := json.Marshal(GetOpsRoutingDiagnostics(c))
	require.NoError(t, err)
	require.NotContains(t, string(payload), "candidate_pool", "unknown cannot be invented as zero")
	require.Nil(t, OpsRoutingDiagnosticsFromSelectionError(errors.New("pool=99 token=private")))
}

func TestAuditPricingRestrictionHasUnknownPoolWithoutListingAccounts(t *testing.T) {
	ch := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10}, RestrictModels: true, BillingModelSource: BillingModelSourceRequested,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}}}}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{10: PlatformOpenAI}))
	svc := &OpenAIGatewayService{channelService: channelSvc}
	gid := int64(10)
	// A nil account repository deliberately proves the pricing gate never lists.
	selection, _, err := svc.selectAccountForModelWithExclusionsStickyHit(context.Background(), &gid, PlatformOpenAI, "", "gpt-5.5", nil, false, 0, "", false)
	require.Nil(t, selection)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	d := OpsRoutingDiagnosticsFromSelectionError(err)
	require.NotNil(t, d)
	require.Equal(t, "channel_pricing", d.SelectionLayer)
	require.Equal(t, "channel_pricing_restricted", d.SelectionReason)
	require.Nil(t, d.CandidatePool)
	require.Empty(t, d.FilteredCandidates)
}

func TestAuditActualCompactExclusionKeepsSelectionEvidence(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Extra: map[string]any{"openai_compact_supported": false}}}}, cfg: &config.Config{}}
	scheduler := newDefaultOpenAIAccountScheduler(svc, newOpenAIAccountRuntimeStats()).(*defaultOpenAIAccountScheduler)
	_, _, _, _, err := scheduler.selectByLoadBalance(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-5.5", RequireCompact: true})
	require.ErrorIs(t, err, ErrNoAvailableCompactAccounts)
	d := OpsRoutingDiagnosticsFromSelectionError(err)
	require.NotNil(t, d)
	require.Equal(t, 1, *d.CandidatePool)
	require.Equal(t, map[string]int{"compact_unsupported": 1}, d.FilteredCandidates)
	entry := &OpsInsertErrorLogInput{RoutingDiagnostics: d}
	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	require.NotNil(t, entry.RoutingDiagnosticsJSON)
	require.Contains(t, *entry.RoutingDiagnosticsJSON, `"compact_unsupported":1`)
}

func TestAuditGrokQuotaGateDoesNotReportInventedEmptyPool(t *testing.T) {
	openaiGrokFreeQuotaGateCache = sync.Map{}
	account := Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"subscription_tier": "FREE"}}
	usage := &grokFreeQuotaUsageRepoStub{stats: map[int64]*usagestats.AccountStats{1: {Tokens: 475_000}}}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, cfg: grokFreeQuotaTestConfig(), usageLogRepo: usage}
	scheduler := newDefaultOpenAIAccountScheduler(svc, newOpenAIAccountRuntimeStats()).(*defaultOpenAIAccountScheduler)
	scheduler.filterGrokFreeQuotaAccounts(context.Background(), []Account{account})
	require.Eventually(t, func() bool {
		return len(scheduler.filterGrokFreeQuotaAccounts(context.Background(), []Account{account})) == 0
	}, time.Second, 10*time.Millisecond)
	_, _, _, _, err := scheduler.selectByLoadBalance(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformGrok, RequestedModel: "grok-4"})
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	d := OpsRoutingDiagnosticsFromSelectionError(err)
	require.NotNil(t, d)
	require.NotNil(t, d.CandidatePool)
	require.Equal(t, 1, *d.CandidatePool, "one observed account was rejected by quota; the pool was not empty")
	require.Equal(t, "grok_free_quota_soft_gate", d.SelectionReason)
	require.Equal(t, map[string]int{"grok_free_quota_soft_gate": 1}, d.FilteredCandidates)
}

func TestAuditNewUnknownSelectionCannotBorrowEarlierPool(t *testing.T) {
	c, _ := newOpenAITransportErrTestContext()
	SetOpsRoutingDiagnostics(c, &OpsRoutingDiagnostics{SelectionDecision: "no_available_account", SelectionLayer: "load_balance", CandidatePool: opsKnownCandidatePool(4), FilteredCandidates: map[string]int{"excluded": 4}, OutboundIdentitySource: "account"})
	SetOpsRoutingDiagnostics(c, &OpsRoutingDiagnostics{SelectionDecision: "no_available_account", SelectionLayer: "channel_pricing", SelectionReason: "channel_pricing_restricted"})
	d := GetOpsRoutingDiagnostics(c)
	require.Nil(t, d.CandidatePool)
	require.Empty(t, d.FilteredCandidates)
	require.Equal(t, "channel_pricing", d.SelectionLayer)
	require.Equal(t, "account", d.OutboundIdentitySource)
}
