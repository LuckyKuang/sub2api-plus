package service

import (
	"context"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
)

// selectedBillingModelCandidates mirrors settlement's source precedence once
// the credential owner is known. Public selling multipliers remain bound to
// the original request; only the price lookup follows the selected wire model.
func (d inflightEstimateDeps) selectedBillingModelCandidates(ctx context.Context, key *APIKey, public string) (primary, fallbacks []string, input string) {
	input = public
	if routed, ok := ResolvedUpstreamModelFromContext(ctx); ok && routed != "" {
		input = routed
	}
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.routingModel != "" {
		input = snapshot.routingModel
	}
	var mapping ChannelMappingResult
	if d.resolveMapping != nil && key.GroupID != nil {
		mapping = d.resolveMapping(ctx, *key.GroupID, input)
	}
	channelInput := input
	if mapping.MappedModel != "" {
		input = mapping.MappedModel
	}
	upstream := input
	if d.selectedUpstreamModel != nil {
		if resolved := strings.TrimSpace(d.selectedUpstreamModel(input)); resolved != "" {
			upstream = resolved
		}
	}
	chosen := upstream
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		chosen = public
	case "", BillingModelSourceChannelMapped:
		if !d.selectedOpenAI || (mapping.Mapped && input != channelInput) {
			chosen = input
		}
	}
	// Composite aliases without a public card settle against the concrete SKU.
	// The caller checks authoritative public cards before using this chain.
	if key.Group != nil && key.Group.Platform == PlatformComposite {
		chosen = upstream
	}
	return []string{chosen}, []string{upstream, input, public}, input
}

func (d inflightEstimateDeps) forAccount(ctx context.Context, account *Account, req InflightEstimateRequest, openAI bool) inflightEstimateDeps {
	d.selectedAccount = account
	d.selectedOpenAI = openAI || account.Platform == PlatformOpenAI
	d.selectedUpstreamModel = func(model string) string {
		if d.selectedOpenAI {
			if req.ResponsesWebSocket && responsesWSInflightPassthrough(ctx, account) {
				return model
			}
			return resolveOpenAIAccountUpstreamModelForRequest(account, model, req.RequireCompact)
		}
		if account.IsBedrock() {
			if resolved, ok := ResolveBedrockModelID(account, model); ok {
				return resolved
			}
		}
		if account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey {
			if account.Type == AccountTypeServiceAccount {
				if mapped, matched := account.ResolveMappedModel(model); matched {
					return mapped
				}
				return normalizeVertexAnthropicModelID(claude.NormalizeModelID(model))
			}
			return claude.NormalizeModelID(model)
		}
		return resolveAccountUpstreamModel(account, model)
	}
	// Effort and policy conversion may inspect only this credential owner.
	d.accountEffortAccounts = func(context.Context, *APIKey, string) []Account { return []Account{*account} }
	d.accountMappedModels = func(_ context.Context, _ *APIKey, model string) []string {
		return []string{d.selectedUpstreamModel(model)}
	}
	return d
}

// EstimateInflightReservationForAccount reconciles the admission estimate with
// the selected owner's actual mapping, without querying a scheduler account pool.
func (s *GatewayService) EstimateInflightReservationForAccount(ctx context.Context, key *APIKey, req InflightEstimateRequest, account *Account) (float64, bool) {
	if s == nil || account == nil {
		return 0, false
	}
	return estimateInflightWithFastPolicy(ctx, key, req, s.inflightEstimateDeps().forAccount(ctx, account, req, false), nil)
}

// EstimateInflightReservationForAccount uses the same frozen retail contract as
// settlement and OpenAI's protocol-specific account mapping rules.
func (s *OpenAIGatewayService) EstimateInflightReservationForAccount(ctx context.Context, key *APIKey, req InflightEstimateRequest, account *Account) (float64, bool) {
	if s == nil || key == nil || key.User == nil || account == nil {
		return 0, false
	}
	ctx = freezeInflightFastPolicy(withOpenAIDefaultBillingPlatform(ctx, key), s.settingService)
	return estimateInflightWithFastPolicy(ctx, key, req, s.inflightEstimateDeps().forAccount(ctx, account, req, true), nil)
}

// WithoutInflightReservation starts a distinct billing owner (for example the
// next WebSocket turn) without inheriting the preceding turn's hold.
func WithoutInflightReservation(ctx context.Context) context.Context {
	return context.WithValue(ctx, inflightReservationCtxKey{}, (*InflightReservation)(nil))
}
