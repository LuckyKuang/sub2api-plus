//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// The known price is $2 for one output token and priority costs three times
// Standard. These independent $2/$6 expectations describe the selling contract;
// neither the estimator nor the body-policy implementation computes them.
// Admission precedes account selection, so a mixed account pool must reserve
// the highest charge that its eligible account policies can actually produce.
func TestInflightOpenAIFastPolicyReserveMatchesForwardingAndSettlement(t *testing.T) {
	priorityFilter := func(scope string) *OpenAIFastPolicySettings {
		return &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter, Scope: scope}}}
	}
	forceMissing := func(scope string) *OpenAIFastPolicySettings {
		return &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: scope}}}
	}
	for _, test := range []struct {
		name          string
		body          string
		force         bool
		free          bool
		groupPlatform string
		target        string
		settings      *OpenAIFastPolicySettings
		accountTypes  []string
		wantTiers     []string
		wantCharges   []float64
		wantHold      float64
	}{
		{name: "group force injects priority", body: `{"model":"gpt-5.6-sol"}`, force: true,
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "group force replaces default", body: `{"model":"gpt-5.6-sol","service_tier":"default"}`, force: true,
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "free priority charges Standard", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, free: true,
			wantTiers: []string{"priority"}, wantCharges: []float64{2}, wantHold: 2},
		{name: "forced free priority charges Standard", body: `{"model":"gpt-5.6-sol"}`, force: true, free: true,
			wantTiers: []string{"priority"}, wantCharges: []float64{2}, wantHold: 2},
		{name: "filter explicit priority charges Standard", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, settings: priorityFilter(BetaPolicyScopeAll),
			wantTiers: []string{""}, wantCharges: []float64{2}, wantHold: 2},
		{name: "global filter overrides group force", body: `{"model":"gpt-5.6-sol"}`, force: true, settings: priorityFilter(BetaPolicyScopeAll),
			wantTiers: []string{""}, wantCharges: []float64{2}, wantHold: 2},
		{name: "global missing tier force injects priority", body: `{"model":"gpt-5.6-sol"}`, settings: forceMissing(BetaPolicyScopeAll),
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "global missing tier force honors free Standard", body: `{"model":"gpt-5.6-sol"}`, free: true, settings: forceMissing(BetaPolicyScopeAll),
			wantTiers: []string{"priority"}, wantCharges: []float64{2}, wantHold: 2},
		{name: "OAuth missing tier force applies to OAuth pool", body: `{"model":"gpt-5.6-sol"}`, settings: forceMissing(BetaPolicyScopeOAuth), accountTypes: []string{AccountTypeOAuth},
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "OAuth missing tier force skips API key pool", body: `{"model":"gpt-5.6-sol"}`, settings: forceMissing(BetaPolicyScopeOAuth),
			wantTiers: []string{""}, wantCharges: []float64{2}, wantHold: 2},
		{name: "mixed account scopes reserve eligible priority maximum", body: `{"model":"gpt-5.6-sol"}`, settings: forceMissing(BetaPolicyScopeOAuth), accountTypes: []string{AccountTypeOAuth, AccountTypeAPIKey},
			wantTiers: []string{"priority", ""}, wantCharges: []float64{6, 2}, wantHold: 6},
		{name: "OAuth filter removes priority for OAuth only pool", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, settings: priorityFilter(BetaPolicyScopeOAuth), accountTypes: []string{AccountTypeOAuth},
			wantTiers: []string{""}, wantCharges: []float64{2}, wantHold: 2},
		{name: "OAuth filter preserves API key priority", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, settings: priorityFilter(BetaPolicyScopeOAuth),
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "mixed filter retains highest eligible charge", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, settings: priorityFilter(BetaPolicyScopeOAuth), accountTypes: []string{AccountTypeOAuth, AccountTypeAPIKey},
			wantTiers: []string{"", "priority"}, wantCharges: []float64{2, 6}, wantHold: 6},
		{name: "Composite OpenAI keeps group force", body: `{"model":"gpt-5.6-sol"}`, force: true, groupPlatform: PlatformComposite,
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
		{name: "Composite Grok ignores group and missing tier force", body: `{"model":"gpt-5.6-sol"}`, force: true, free: true, groupPlatform: PlatformComposite, target: PlatformGrok, settings: forceMissing(BetaPolicyScopeAll),
			wantTiers: []string{""}, wantCharges: []float64{2}, wantHold: 2},
		{name: "Composite Grok cannot inherit free OpenAI priority", body: `{"model":"gpt-5.6-sol","service_tier":"priority"}`, free: true, groupPlatform: PlatformComposite, target: PlatformGrok,
			wantTiers: []string{"priority"}, wantCharges: []float64{6}, wantHold: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			platform := test.target
			if platform == "" {
				platform = PlatformOpenAI
			}
			groupPlatform := test.groupPlatform
			if groupPlatform == "" {
				groupPlatform = PlatformOpenAI
			}
			accountTypes := test.accountTypes
			if len(accountTypes) == 0 {
				accountTypes = []string{AccountTypeAPIKey}
			}
			inputPrice, outputPrice, priorityFactor := 0.0, 2.0, 3.0
			group := &Group{ID: 77, Platform: groupPlatform, Status: StatusActive, Hydrated: true, RateMultiplier: 1,
				ForceOpenAIFast: test.force, FreeOpenAIFast: test.free,
				ModelPricing: []ChannelModelPricing{{Platform: platform, Models: []string{"gpt-5.6-sol"}, BillingMode: BillingModeToken,
					InputPrice: &inputPrice, OutputPrice: &outputPrice, FastMultiplier: &priorityFactor}},
			}
			user := &User{ID: 42, Balance: 100}
			key := &APIKey{ID: 11, UserID: user.ID, User: user, GroupID: &group.ID, Group: group}
			ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}}
			service := newOpenAIRecordUsageServiceWithBillingRepoForTest(&openAIRecordUsageLogRepoStub{inserted: true}, ledger,
				&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			service.cfg.Default.RateMultiplier = 1
			service.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1, MaxInputTokens: 1, MaxOutputTokens: 1}
			service.resolver = NewModelPricingResolver(nil, service.billingService)
			service.settingService = newOpenAIGatewayServiceWithSettings(t, test.settings).settingService
			cache := &inflightFastPolicyAccountSnapshot{}
			for i, accountType := range accountTypes {
				cache.accounts = append(cache.accounts, &Account{ID: int64(100 + i), Platform: platform, Type: accountType, Status: StatusActive, Schedulable: true})
			}
			service.schedulerSnapshot = NewSchedulerSnapshotService(cache, nil, nil, nil, service.cfg)
			ctx := WithResolvedTargetPlatform(context.Background(), platform)
			ctx = context.WithValue(ctx, ctxkey.UserID, user.ID)
			ctx = context.WithValue(ctx, ctxkey.Group, group)
			ctx = service.WithBillingPricingSnapshot(ctx, key, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), "gpt-5.6-sol")
			require.NoError(t, ValidateBillingPricingSnapshot(ctx))

			// Estimate before selecting any particular account or rewriting the
			// outbound body, exactly as request admission does in production.
			req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata([]byte(test.body))
			hold, priced := service.EstimateInflightReservation(ctx, key, req)
			for i, account := range cache.accounts {
				outbound, err := service.applyOpenAIFastPolicyToBody(ctx, account, "gpt-5.6-sol", []byte(test.body))
				require.NoError(t, err)
				require.Equal(t, test.wantTiers[i], gjson.GetBytes(outbound, "service_tier").String(), "the actual forwarded tier must honor the configured policy")
				tier := extractOpenAIServiceTierFromBody(outbound)
				err = service.RecordUsage(ctx, &OpenAIRecordUsageInput{
					PricingSnapshot: BillingPricingSnapshotFromContext(ctx),
					Result: &OpenAIForwardResult{RequestID: "fast-policy-reservation", Model: "gpt-5.6-sol", ServiceTier: tier,
						Usage: OpenAIUsage{OutputTokens: 1}, Duration: time.Second},
					APIKey: key, User: user, Account: account,
				})
				require.NoError(t, err)
				require.NotNil(t, ledger.lastCmd)
				require.InDelta(t, test.wantCharges[i], ledger.lastCmd.BalanceCost, 1e-12, "the real settlement must follow the independently specified $2/$6 contract")
			}
			require.True(t, priced)
			require.InDelta(t, test.wantHold, hold, 1e-12, "pre-account admission must reserve the highest eligible customer charge, including late fast-policy rewrites")
		})
	}
}

type inflightFastPolicyAccountSnapshot struct {
	SchedulerCache
	accounts []*Account
}

func (c *inflightFastPolicyAccountSnapshot) GetSnapshot(_ context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	var accounts []*Account
	for _, account := range c.accounts {
		if account.Platform == bucket.Platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, true, nil
}

func TestInflightOpenAIFastPolicyPinsSettingsAndGroupThroughForwardAndSettlement(t *testing.T) {
	svc, key, ledger, account := newInflightFastPolicySnapshotFixture(t)
	key.Group.ForceOpenAIFast = true
	repo := &openAIFastPolicyRepoStub{values: map[string]string{SettingKeyOpenAIFastPolicySettings: `{"rules":[]}`}}
	svc.settingService = NewSettingService(repo, svc.cfg)
	base := context.WithValue(WithResolvedTargetPlatform(context.Background(), PlatformOpenAI), ctxkey.Group, key.Group)
	ctx := svc.WithBillingPricingSnapshot(base, key, time.Now(), "gpt-5.6-sol")
	require.NoError(t, ValidateBillingPricingSnapshot(ctx))

	// An administrator edits both knobs after admission. The accepted request
	// retains priority and the $6 agreed charge; the next request sees the edit.
	key.Group.ForceOpenAIFast, key.Group.FreeOpenAIFast = false, true
	repo.values[SettingKeyOpenAIFastPolicySettings] = `{"rules":[{"scope":"all","service_tier":"priority","action":"filter"}]}`
	req := (InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}).WithRequestPricingMetadata([]byte(`{"model":"gpt-5.6-sol"}`))
	hold, priced := svc.EstimateInflightReservation(ctx, key, req)
	require.True(t, priced)
	require.InDelta(t, 6, hold, 1e-12)
	// Copying into detached billing/retry work must preserve the exact rule
	// generation as well as the price generation.
	copied := CopyBillingPricingSnapshot(ctx, base)
	out, err := svc.applyOpenAIFastPolicyToBody(copied, account, req.Model, []byte(`{"model":"gpt-5.6-sol"}`))
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(out, "service_tier").String())
	err = svc.RecordUsage(copied, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(copied), APIKey: key, User: key.User, Account: account,
		Result: &OpenAIForwardResult{RequestID: "before-policy-edit", Model: req.Model, ServiceTier: extractOpenAIServiceTierFromBody(out), Usage: OpenAIUsage{OutputTokens: 1}}})
	require.NoError(t, err)
	require.InDelta(t, 6, ledger.lastCmd.BalanceCost, 1e-12)

	next := svc.WithBillingPricingSnapshot(base, key, time.Now(), req.Model)
	hold, priced = svc.EstimateInflightReservation(next, key, req)
	require.True(t, priced)
	require.InDelta(t, 2, hold, 1e-12)
	out, err = svc.applyOpenAIFastPolicyToBody(next, account, req.Model, []byte(`{"model":"gpt-5.6-sol","service_tier":"priority"}`))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "service_tier").Exists())
	err = svc.RecordUsage(next, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(next), APIKey: key, User: key.User, Account: account,
		Result: &OpenAIForwardResult{RequestID: "after-policy-edit", Model: req.Model, ServiceTier: extractOpenAIServiceTierFromBody(out), Usage: OpenAIUsage{OutputTokens: 1}}})
	require.NoError(t, err)
	require.InDelta(t, 2, ledger.lastCmd.BalanceCost, 1e-12)
}

func TestInflightOpenAIFastPolicyUnavailableSettingsFreezePassWithBoundedRead(t *testing.T) {
	for _, name := range []string{"dependency error", "shorter parent deadline", "own two second budget"} {
		t.Run(name, func(t *testing.T) {
			svc, key, ledger, account := newInflightFastPolicySnapshotFixture(t)
			calls := 0
			var observedDeadline time.Time
			var observedErr error
			recovered := false
			repo := &inflightFastPolicySettingsDependency{read: func(ctx context.Context) (string, error) {
				calls++
				var ok bool
				observedDeadline, ok = ctx.Deadline()
				require.True(t, ok, "settings reads need a finite dependency budget")
				require.LessOrEqual(t, time.Until(observedDeadline), 2*time.Second)
				if recovered {
					return `{"rules":[{"scope":"all","service_tier":"missing","action":"force_priority"}]}`, nil
				}
				if name == "own two second budget" {
					return `{"rules":[]}`, nil
				}
				if name == "shorter parent deadline" {
					<-ctx.Done()
					observedErr = ctx.Err()
					return "", observedErr
				}
				return "", errors.New("policy dependency unavailable")
			}}
			svc.settingService = NewSettingService(repo, svc.cfg)
			observedLogs, entries := observer.New(zap.WarnLevel)
			base := logger.IntoContext(context.Background(), zap.New(observedLogs))
			var parentDeadline time.Time
			if name == "shorter parent deadline" {
				var cancel context.CancelFunc
				base, cancel = context.WithTimeout(base, 30*time.Millisecond)
				defer cancel()
				parentDeadline, _ = base.Deadline()
			}
			started := time.Now()
			ctx := svc.WithBillingPricingSnapshot(WithResolvedTargetPlatform(base, PlatformOpenAI), key, time.Now(), "gpt-5.6-sol")
			require.Less(t, time.Since(started), time.Second, "the dependency must honor the shorter request deadline")
			require.NoError(t, ValidateBillingPricingSnapshot(ctx), "policy failure preserves the existing pass contract")
			require.Equal(t, 1, calls)
			if name == "shorter parent deadline" {
				require.Equal(t, parentDeadline, observedDeadline)
				require.ErrorIs(t, observedErr, context.DeadlineExceeded)
			}
			if name != "own two second budget" {
				require.Equal(t, 1, entries.FilterMessage("billing.fast_policy_snapshot_unavailable").Len())
				fields := entries.All()[0].ContextMap()
				require.Equal(t, "FAST_POLICY_UNAVAILABLE", fields["code"])
				require.Equal(t, "billing_admission", fields["stage"])
			}
			// Dependency recovery after capture cannot change this request to
			// a late priority charge. A fresh independent request may refresh it.
			recovered = true
			copied := CopyBillingPricingSnapshot(ctx, context.Background())
			req := InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}
			hold, priced := svc.EstimateInflightReservation(copied, key, req)
			require.True(t, priced)
			require.InDelta(t, 2, hold, 1e-12)
			out, err := svc.applyOpenAIFastPolicyToBody(copied, account, req.Model, []byte(`{"model":"gpt-5.6-sol"}`))
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(out, "service_tier").Exists())
			err = svc.RecordUsage(copied, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(copied), APIKey: key, User: key.User, Account: account,
				Result: &OpenAIForwardResult{RequestID: "policy-read-failure", Model: req.Model, Usage: OpenAIUsage{OutputTokens: 1}}})
			require.NoError(t, err)
			require.InDelta(t, 2, ledger.lastCmd.BalanceCost, 1e-12)
			require.Equal(t, 1, calls, "forward/retry/settlement must reuse the captured policy result")
		})
	}
}

func TestInflightOpenAIFastPolicyMissingKeyOwnerFailsWithoutPoolAccess(t *testing.T) {
	svc, key, _, _ := newInflightFastPolicySnapshotFixture(t)
	key.Group.ForceOpenAIFast = true
	svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeAll}}}).settingService
	ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-5.6-sol")
	other := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver, schedulerSnapshot: svc.schedulerSnapshot}
	req := InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", MaxTokens: 1}
	for _, missingOwner := range []*APIKey{nil, {ID: key.ID, GroupID: key.GroupID, Group: key.Group}} {
		for _, estimator := range []func(context.Context, *APIKey, InflightEstimateRequest) (float64, bool){svc.EstimateInflightReservation, other.EstimateInflightReservation} {
			amount, priced := estimator(ctx, missingOwner, req)
			require.False(t, priced)
			require.Zero(t, amount)
		}
	}
}

func TestInflightOpenAIFastFallbackUsesExecutionForceAndRetailFreePolicy(t *testing.T) {
	for _, test := range []struct {
		name                          string
		originalForce, executionForce bool
		originalFree, executionFree   bool
		wantTier                      string
		wantCost                      float64
	}{
		{name: "original force does not override target", originalForce: true, wantCost: 2},
		{name: "target force applies with original retail price", executionForce: true, wantTier: "priority", wantCost: 6},
		{name: "original free priority remains retail contract", executionForce: true, originalFree: true, wantTier: "priority", wantCost: 2},
		{name: "target free does not change original retail contract", executionForce: true, executionFree: true, wantTier: "priority", wantCost: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, key, ledger, account := newInflightFastPolicySnapshotFixture(t)
			key.Group.ForceOpenAIFast, key.Group.FreeOpenAIFast = test.originalForce, test.originalFree
			ctx := svc.WithBillingPricingSnapshot(context.WithValue(context.Background(), ctxkey.Group, key.Group), key, time.Now(), "gpt-5.6-sol")
			target := &Group{ID: key.Group.ID + 1, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
				ForceOpenAIFast: test.executionForce, FreeOpenAIFast: test.executionFree}
			ctx = context.WithValue(ctx, ctxkey.Group, target)
			req := InflightEstimateRequest{Protocol: ContentModerationProtocolOpenAIResponses, Model: "gpt-5.6-sol", Kind: InflightEstimateToken, MaxTokens: 1}
			hold, priced := svc.EstimateInflightReservation(ctx, key, req)
			require.True(t, priced)
			require.InDelta(t, test.wantCost, hold, 1e-12)
			out, err := svc.applyOpenAIFastPolicyToBody(ctx, account, req.Model, []byte(`{"model":"gpt-5.6-sol"}`))
			require.NoError(t, err)
			require.Equal(t, test.wantTier, gjson.GetBytes(out, "service_tier").String())
			err = svc.RecordUsage(ctx, &OpenAIRecordUsageInput{PricingSnapshot: BillingPricingSnapshotFromContext(ctx), APIKey: key, User: key.User, Account: account,
				Result: &OpenAIForwardResult{RequestID: "fallback-group-fast-policy", Model: req.Model, ServiceTier: extractOpenAIServiceTierFromBody(out), Usage: OpenAIUsage{OutputTokens: 1}}})
			require.NoError(t, err)
			require.InDelta(t, test.wantCost, ledger.lastCmd.BalanceCost, 1e-12)
			require.Equal(t, key.Group.ID, BillingPricingSnapshotFromContext(ctx).group.ID)
		})
	}
}

func newInflightFastPolicySnapshotFixture(t *testing.T) (*OpenAIGatewayService, *APIKey, *openAIRecordUsageBillingRepoStub, *Account) {
	t.Helper()
	ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true, UsageLogPersisted: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(&openAIRecordUsageLogRepoStub{inserted: true}, ledger,
		&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.cfg.Default.RateMultiplier = 1
	svc.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1, MaxInputTokens: 1, MaxOutputTokens: 1}
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	group := &Group{ID: 77, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-5.6-sol"}, BillingMode: BillingModeToken,
			InputPrice: billingFloat(0), OutputPrice: billingFloat(2), FastMultiplier: billingFloat(3)}},
	}
	user := &User{ID: 42, Balance: 100}
	key := &APIKey{ID: 11, UserID: user.ID, User: user, GroupID: &group.ID, Group: group}
	account := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	svc.schedulerSnapshot = NewSchedulerSnapshotService(&inflightFastPolicyAccountSnapshot{accounts: []*Account{account}}, nil, nil, nil, svc.cfg)
	return svc, key, ledger, account
}

type inflightFastPolicySettingsDependency struct {
	SettingRepository
	read func(context.Context) (string, error)
}

func (r *inflightFastPolicySettingsDependency) GetValue(ctx context.Context, _ string) (string, error) {
	return r.read(ctx)
}
