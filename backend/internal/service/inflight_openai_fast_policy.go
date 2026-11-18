package service

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

const inflightFastPolicyReadTimeout = 2 * time.Second

// The OpenAI gateway routes ungrouped requests to OpenAI by default. Bind that
// existing routing contract for pricing without changing another concrete
// target, a force-platform override, or the generic gateway's default.
func withOpenAIDefaultBillingPlatform(ctx context.Context, key *APIKey) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == nil || key.User == nil || key.Group != nil {
		return ctx
	}
	if requestBillingPlatform(ctx, nil, "") != "" {
		return ctx
	}
	return WithResolvedTargetPlatform(ctx, PlatformOpenAI)
}

func cloneInflightFastPolicy(settings *OpenAIFastPolicySettings) *OpenAIFastPolicySettings {
	copy := &OpenAIFastPolicySettings{}
	if settings == nil {
		return copy
	}
	copy.Rules = append([]OpenAIFastPolicyRule(nil), settings.Rules...)
	for i := range copy.Rules {
		copy.Rules[i].UserIDs = append([]int64(nil), copy.Rules[i].UserIDs...)
		copy.Rules[i].ModelWhitelist = append([]string(nil), copy.Rules[i].ModelWhitelist...)
	}
	return copy
}

func captureInflightFastPolicy(ctx context.Context, service *SettingService) *OpenAIFastPolicySettings {
	if settings := openAIFastPolicySettingsFromContext(ctx); settings != nil {
		return cloneInflightFastPolicy(settings)
	}
	if service == nil {
		return &OpenAIFastPolicySettings{}
	}
	readCtx, cancel := context.WithTimeout(ctx, inflightFastPolicyReadTimeout)
	defer cancel()
	settings, err := service.GetOpenAIFastPolicySettings(readCtx)
	if err != nil {
		// Match the forwarding contract: unavailable global policy passes the
		// request. Freeze that decision so admission and forwarding cannot drift.
		logger.FromContext(ctx).Warn("billing.fast_policy_snapshot_unavailable", zap.String("stage", "billing_admission"), zap.String("code", "FAST_POLICY_UNAVAILABLE"), zap.Error(err))
		return &OpenAIFastPolicySettings{}
	}
	return cloneInflightFastPolicy(settings)
}

func freezeInflightFastPolicy(ctx context.Context, service *SettingService) context.Context {
	snapshot := BillingPricingSnapshotFromContext(ctx)
	if snapshot == nil {
		// Ungrouped keys have no retail group snapshot, but global Fast rules
		// still rewrite their upstream tier. Pin those rules for admission and
		// forwarding once the request's OpenAI target is known.
		if requestBillingPlatform(ctx, nil, "") == PlatformOpenAI {
			return withOpenAIFastPolicyContext(ctx, captureInflightFastPolicy(ctx, service))
		}
		return ctx
	}
	if snapshot.platform != PlatformOpenAI {
		return ctx
	}
	if snapshot.fastPolicySettings != nil {
		return pricingSnapshotContext(ctx, snapshot)
	}
	copy := *snapshot
	copy.fastPolicySettings = captureInflightFastPolicy(ctx, service)
	if group, _ := ctx.Value(ctxkey.Group).(*Group); group != nil {
		frozen := *group
		copy.fastExecutionGroup = &frozen
	}
	return pricingSnapshotContext(ctx, &copy)
}

func openAIFastExecutionGroup(ctx context.Context) *Group {
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.fastExecutionGroup != nil &&
		group != nil && group.ID == snapshot.fastExecutionGroup.ID {
		return snapshot.fastExecutionGroup
	}
	// A different routing group retains its existing execution policy even
	// when the request's retail pricing snapshot belongs to its original group.
	return group
}

func estimateInflightWithFastPolicy(ctx context.Context, key *APIKey, req InflightEstimateRequest, deps inflightEstimateDeps, scheduler *SchedulerSnapshotService) (float64, bool) {
	if key == nil || key.User == nil {
		return 0, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	group := key.Group
	platform := requestBillingPlatform(ctx, group, "")
	settings := openAIFastPolicySettingsFromContext(ctx)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil {
		group, platform = snapshot.group, snapshot.platform
		if snapshot.fastPolicySettings != nil {
			settings = snapshot.fastPolicySettings
		}
		if snapshot.channels == nil {
			deps.resolveMapping = nil
		} else {
			deps.resolveMapping = snapshot.channels.ResolveChannelMapping
		}
	}
	if platform != PlatformOpenAI ||
		(req.Protocol != "" && req.Protocol != ContentModerationProtocolOpenAIChat && req.Protocol != ContentModerationProtocolOpenAIResponses && req.Protocol != ContentModerationProtocolAnthropicMessages) {
		return deps.estimate(ctx, key, req)
	}
	if req.Protocol == ContentModerationProtocolAnthropicMessages && req.AnthropicFast {
		// The native Messages header becomes priority only on the OpenAI
		// conversion path; it is not a foreign body-level pricing declaration.
		req.ServiceTier = OpenAIFastTierPriority
	}
	force := openAIGroupForcesFast(ctx, &Account{Platform: PlatformOpenAI})
	executionGroup := openAIFastExecutionGroup(ctx)
	forceUltrafast := IsGroupContextValid(executionGroup) && groupSupportsOpenAIFast(executionGroup.Platform) && executionGroup.ForceOpenAIUltrafast
	free := group != nil && groupSupportsOpenAIFast(group.Platform) && group.FreeOpenAIFast
	tier := normalizedOpenAIServiceTierValue(req.ServiceTier)
	if tier == "" && req.ServiceTier != "" && !force && !forceUltrafast {
		return deps.estimate(ctx, key, req) // Unknown explicit tiers are never "missing".
	}
	if force {
		tier = OpenAIFastTierPriority
	}
	userID := openAIFastPolicyUserID(ctx)
	policyRelevant := false
	if settings != nil {
		for _, rule := range settings.Rules {
			if !openAIFastPolicyUserMatches(rule.UserIDs, userID) {
				continue
			}
			matchTier := strings.ToLower(strings.TrimSpace(rule.ServiceTier))
			if (tier == "" && matchTier == OpenAIFastTierMissing) || (tier != "" && (matchTier == "" || matchTier == OpenAIFastTierAny || matchTier == tier)) ||
				(forceUltrafast && (matchTier == "" || matchTier == OpenAIFastTierAny || matchTier == OpenAIFastTierUltrafast)) {
				if rule.Action != BetaPolicyActionPass || (rule.FallbackAction != "" && rule.FallbackAction != BetaPolicyActionPass) {
					policyRelevant = true
				}
			}
		}
	}
	if !force && !forceUltrafast && !free && !policyRelevant {
		return deps.estimate(ctx, key, req)
	}
	routingModel := req.Model
	if routed, ok := ResolvedUpstreamModelFromContext(ctx); ok && strings.TrimSpace(routed) != "" {
		routingModel = routed
	}
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.routingModel != "" {
		routingModel = snapshot.routingModel
	}
	_, _, upstreamModel := inflightBillingModelCandidates(ctx, deps, key, routingModel)
	tiers := make(map[string]struct{})
	add := func(tier string) {
		if free && normalizeBillingServiceTier(tier) == "priority" {
			tier = ""
		}
		tiers[tier] = struct{}{}
	}
	forAccount := func(account *Account, model string) {
		final := tier
		if forcedTier := openAIGroupForcedServiceTier(ctx, account, model); forcedTier != "" {
			final = forcedTier
		} else if !force && normalizedOpenAIServiceTierValue(req.ServiceTier) == "" && req.ServiceTier != "" {
			// Unsupported models preserve an unknown explicit client tier;
			// it must not become a missing-tier policy match.
			add(req.ServiceTier)
			return
		}
		matchTier := final
		if matchTier == "" {
			matchTier = OpenAIFastTierMissing
		}
		action, _ := evaluateOpenAIFastPolicyWithSettings(settings, userID, account, model, matchTier)
		if final == "" {
			if action == OpenAIFastPolicyActionForcePriority {
				final = OpenAIFastTierPriority
			}
		} else {
			switch action {
			case BetaPolicyActionBlock:
				return
			case BetaPolicyActionFilter:
				final = ""
			case OpenAIFastPolicyActionForcePriority:
				final = OpenAIFastTierPriority
			}
		}
		add(final)
	}
	// Fast/free alone does not depend on the account mapping. Ultrafast does:
	// the selected wire model controls whether it overrides Fast or the client.
	if !policyRelevant && !forceUltrafast {
		add(tier)
	} else {
		var accounts []Account
		if deps.selectedAccount != nil {
			accounts = []Account{*deps.selectedAccount}
		} else if scheduler != nil {
			var err error
			groupID := key.GroupID
			if executionGroup := openAIFastExecutionGroup(ctx); executionGroup != nil && executionGroup.ID > 0 {
				groupID = &executionGroup.ID
			}
			accounts, err = listInflightSnapshotAccounts(ctx, scheduler, groupID, PlatformOpenAI, false)
			if err != nil {
				accounts = nil
			}
		}
		for i := range accounts {
			account := &accounts[i]
			if account.Platform != PlatformOpenAI || !account.IsSchedulable() {
				continue
			}
			resolveModel := deps.selectedUpstreamModel
			if resolveModel == nil {
				resolveModel = deps.forAccount(ctx, account, req, true).selectedUpstreamModel
			}
			model := resolveModel(upstreamModel)
			forAccount(account, model)
		}
		if len(accounts) == 0 {
			// Without an authoritative pool, mappings and scopes are unknown.
			// Preserve the requested tier and every possible policy price outcome;
			// missing snapshot evidence must never prove a lower customer charge.
			add(tier)
			add("")
			add(OpenAIFastTierPriority)
			if forceUltrafast {
				// The unavailable pool cannot prove that every wire model lacks
				// Ultrafast. Retain its possible charge until selection reconciles.
				add(OpenAIFastTierUltrafast)
			}
		}
	}
	if len(tiers) == 0 {
		return 0, true // Every known eligible account blocks before upstream work.
	}
	best, priced := 0.0, false
	for tier := range tiers {
		candidate := req
		candidate.ServiceTier = tier
		amount, ok := deps.estimate(ctx, key, candidate)
		if !ok {
			return 0, false
		}
		priced = true
		best = math.Max(best, amount)
	}
	return best, priced
}
