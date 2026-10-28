package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// Retain only canonical pricing metadata, never the request body. An alias may
// normalize max differently from a model selected later by channel/account
// mapping; absent effort can also acquire a mapped model's suffix/default.
type inflightEffortMetadata struct {
	captured     bool
	explicit     bool
	raw          string
	chatRaw      string
	nestedRaw    string
	thinking     bool
	thinkingType string
}

func (req InflightEstimateRequest) captureInflightEffortMetadata(body []byte) InflightEstimateRequest {
	metadata := inflightEffortMetadata{}
	metadata.thinkingType = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "thinking.type").String()))
	switch metadata.thinkingType {
	case "enabled", "adaptive", "disabled", "between_tools", "off", "none":
	default:
		metadata.thinkingType = ""
	}
	var raw string
	switch req.Protocol {
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolOpenAIResponses:
		metadata.captured = true
		raw = strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
		if raw == "" {
			raw = strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String())
		}
		metadata.thinking = OpenAIBodyHasThinkingEnabled(body)
		metadata.chatRaw = inflightCanonicalEffort(gjson.GetBytes(body, "reasoning_effort").String())
		metadata.nestedRaw = inflightCanonicalEffort(gjson.GetBytes(body, "reasoning.effort").String())
	case ContentModerationProtocolAnthropicMessages:
		metadata.captured = true
		raw = strings.TrimSpace(gjson.GetBytes(body, "output_config.effort").String())
		metadata.thinking = OpenAIBodyHasThinkingEnabled(body)
	}
	metadata.explicit = raw != ""
	metadata.raw = inflightCanonicalEffort(raw)
	req.effortMetadata = metadata
	return req
}

func inflightCanonicalEffort(raw string) string {
	if normalized := NormalizeMaxReasoningEffort(raw); normalized != "" {
		return normalized
	}
	switch normalizeEffortToken(raw) {
	case "none", "ultra", "ultracode":
		return normalizeEffortToken(raw)
	default:
		return ""
	}
}

func (req InflightEstimateRequest) needsMappedEffortCandidates() bool {
	metadata := req.effortMetadata
	if !metadata.captured {
		return false
	}
	switch req.Protocol {
	case ContentModerationProtocolOpenAIChat, ContentModerationProtocolOpenAIResponses, ContentModerationProtocolAnthropicMessages:
		// Native and converted protocols can change every explicit scale, and
		// Messages conversions also introduce a default when effort is absent.
		return metadata.raw != "" || !metadata.explicit || metadata.thinking
	default:
		return false
	}
}

// Estimate the same retail card under every reachable effort interpretation.
// Mapped models contribute effort metadata only; they cannot replace an
// authoritative public unit price or its explicit zero with another SKU.
func (d inflightEstimateDeps) inflightEffortCandidates(ctx context.Context, key *APIKey, req InflightEstimateRequest, models []string, upstreamInput string) []string {
	levels := []string{req.ReasoningEffort}
	if !req.needsMappedEffortCandidates() {
		return levels
	}
	var configuredEfforts map[string]float64
	if key != nil && key.Group != nil {
		preferred := preferredConfiguredPublicPricingModel(ctx, d.resolver, key.Group, publicBillingModel(ctx, "", req.Model), "")
		if preferred != "" && d.resolver != nil {
			resolved := d.resolver.Resolve(ctx, PricingInput{Model: preferred, GroupID: key.GroupID, Group: key.Group})
			if resolved != nil && resolved.channelPricing != nil {
				configuredEfforts = resolved.channelPricing.ReasoningEffortMultipliers
			}
			if len(configuredEfforts) == 0 && resolved != nil && resolved.BasePricing != nil {
				configuredEfforts = resolved.BasePricing.ReasoningEffortMultipliers
			}
			if resolved != nil && (resolved.channelPricing == nil || len(resolved.channelPricing.ReasoningEffortMultipliers) == 0) &&
				(resolved.BasePricing == nil || len(resolved.BasePricing.ReasoningEffortMultipliers) == 0) {
				// The public card stays authoritative across mappings. With no
				// effort factors its quote cannot change with the final scale.
				return levels
			}
		}
	}
	models = append(models, req.Model)
	routeModel, _ := ResolvedUpstreamModelFromContext(ctx)
	if snapshot := BillingPricingSnapshotFromContext(ctx); snapshot != nil && snapshot.routingModel != "" {
		routeModel = snapshot.routingModel
	}
	if routeModel != "" {
		models = append(models, routeModel)
		upstreamInput = routeModel
		if d.resolveMapping != nil && key != nil && key.GroupID != nil {
			if mapped := d.resolveMapping(ctx, *key.GroupID, routeModel).MappedModel; mapped != "" {
				models = append(models, mapped)
				upstreamInput = mapped
			}
		}
	}
	var accounts []Account
	if d.accountEffortAccounts != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		accounts = d.accountEffortAccounts(lookupCtx, key, upstreamInput)
		cancel()
		models = append(models, accountMappedModelsFrom(accounts, upstreamInput)...)
	} else if d.accountMappedModels != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		models = append(models, d.accountMappedModels(lookupCtx, key, upstreamInput)...)
		cancel()
	}
	seen := map[string]bool{req.ReasoningEffort: true}
	add := func(level string) {
		if !seen[level] {
			seen[level] = true
			levels = append(levels, level)
		}
	}
	if len(accounts) == 0 && len(configuredEfforts) > 0 {
		// A temporarily unavailable snapshot is not proof that a later
		// scheduler attempt cannot select an account with a different scale.
		// Bound this uncertainty by the admitted public card's own factors.
		add("")
		for effort := range configuredEfforts {
			add(effort)
		}
	}
	metadata := req.effortMetadata
	for _, model := range models {
		var effort string
		switch req.Protocol {
		case ContentModerationProtocolOpenAIChat, ContentModerationProtocolOpenAIResponses:
			if metadata.explicit {
				effort = normalizeOpenAIReasoningEffortForModel(metadata.raw, model)
			} else {
				effort = deriveOpenAIReasoningEffortFromModelCandidates([]string{model, req.Model})
			}
		case ContentModerationProtocolAnthropicMessages:
			// Messages may be converted for an OpenAI-compatible supplier. Keep
			// the native level already in levels, plus that supplier's scale.
			effort = normalizeOpenAIReasoningEffortForModel(metadata.raw, model)
		}
		if effort == "" && metadata.thinking {
			if fallback := DefaultEffortForThinkingEnabled(model); fallback != nil {
				effort = *fallback
			}
		}
		add(effort)
	}
	for i := range accounts {
		for _, effort := range d.inflightConvertedEfforts(ctx, &accounts[i], req, upstreamInput) {
			add(effort)
		}
	}
	if len(accounts) == 0 && key != nil && key.Group != nil && requestBillingPlatform(ctx, key.Group, "") == PlatformAnthropic {
		// This gateway always forwards Anthropic groups as Messages, including
		// deployments without the optional scheduler snapshot dependency.
		for _, model := range models {
			for _, effort := range d.inflightConvertedEfforts(ctx, &Account{Platform: PlatformAnthropic}, req, model) {
				add(effort)
			}
		}
	}
	return levels
}

func inflightEffortInboundProtocol(protocol string) string {
	switch protocol {
	case ContentModerationProtocolOpenAIChat:
		return APIProtocolChatCompletions
	case ContentModerationProtocolOpenAIResponses:
		return APIProtocolResponses
	case ContentModerationProtocolAnthropicMessages:
		return APIProtocolAnthropic
	default:
		return ""
	}
}

// Protocol prediction is local and never triggers provider discovery. A model
// catalog can change a model-routed account's next protocol; when no persisted
// rule pins it, include only protocols this same account actually supports.
func inflightEffortUpstreamProtocols(account *Account, inbound, model string) []string {
	if account.Platform == PlatformAnthropic {
		return []string{APIProtocolAnthropic}
	}
	if account.Platform == PlatformGrok && inbound != APIProtocolAnthropic {
		return []string{inbound}
	}
	protocol := resolveUpstreamProtocol(account, inbound, model, nil)
	protocols := []string{protocol}
	if !account.routesByModel() || account.GetAPIProtocol() != APIProtocolAdaptive {
		return protocols
	}
	if rules, _ := account.configuredProtocolRules(); len(rules) > 0 {
		if _, matched := matchProtocolRuleSet(model, rules); matched {
			return protocols
		}
	}
	for _, candidate := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		if candidate != protocol && account.providerSupportsProtocol(candidate) {
			protocols = append(protocols, candidate)
		}
	}
	return protocols
}

// Reconstruct only canonical pricing metadata, then use the production protocol
// converters. This preserves model-specific defaults (including Claude 5.5)
// without retaining prompts or duplicating provider effort mapping tables.
func inflightEffortMetadataBody(req InflightEstimateRequest) []byte {
	body := map[string]any{"model": req.Model}
	metadata := req.effortMetadata
	switch req.Protocol {
	case ContentModerationProtocolAnthropicMessages:
		body["messages"] = []map[string]string{{"role": "user", "content": "metadata"}}
		body["max_tokens"] = 16
		if metadata.raw != "" {
			body["output_config"] = map[string]string{"effort": metadata.raw}
		}
	case ContentModerationProtocolOpenAIChat:
		body["messages"] = []map[string]string{{"role": "user", "content": "metadata"}}
		body["max_tokens"] = 16
		if metadata.chatRaw != "" {
			body["reasoning_effort"] = metadata.chatRaw
		}
		if metadata.nestedRaw != "" {
			body["reasoning"] = map[string]string{"effort": metadata.nestedRaw}
		}
	case ContentModerationProtocolOpenAIResponses:
		body["input"] = "metadata"
		body["max_output_tokens"] = 16
		if metadata.nestedRaw != "" {
			body["reasoning"] = map[string]string{"effort": metadata.nestedRaw}
		}
		if metadata.chatRaw != "" {
			body["reasoning_effort"] = metadata.chatRaw
		}
	}
	if metadata.thinkingType != "" {
		body["thinking"] = map[string]string{"type": metadata.thinkingType}
	}
	encoded, _ := json.Marshal(body)
	return encoded
}

func (d inflightEstimateDeps) inflightConvertedEfforts(ctx context.Context, account *Account, req InflightEstimateRequest, inputModel string) []string {
	billingModel := resolveOpenAIForwardModel(account, inputModel, "")
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	inbound := inflightEffortInboundProtocol(req.Protocol)
	body := inflightEffortMetadataBody(req)
	var levels []string
	for _, protocol := range inflightEffortUpstreamProtocols(account, inbound, upstreamModel) {
		forwarded, err := d.inflightConvertEffortBody(ctx, account, req, body, upstreamModel, protocol)
		if err != nil {
			// A rejected conversion cannot create a billable upstream request.
			continue
		}
		var effort *string
		if protocol == APIProtocolAnthropic {
			effort = NormalizeClaudeOutputEffort(gjson.GetBytes(forwarded, "output_config.effort").String())
		} else {
			effort = extractOpenAIReasoningEffortFromBody(forwarded, upstreamModel, billingModel, inputModel)
		}
		effort = ApplyThinkingEnabledFallback(effort, forwarded, billingModel)
		levels = append(levels, optionalStringValue(effort))
	}
	return levels
}

func (d inflightEstimateDeps) inflightConvertEffortBody(ctx context.Context, account *Account, req InflightEstimateRequest, body []byte, model, protocol string) ([]byte, error) {
	inbound := inflightEffortInboundProtocol(req.Protocol)
	forwarded := body
	if protocol == APIProtocolAnthropic {
		if inbound == APIProtocolAnthropic {
			if account.Platform != PlatformAnthropic {
				forwarded, _ = NormalizeGLM53AnthropicThinking(body, model)
			}
			return forwarded, nil
		}
		responses, err := d.inflightEffortResponsesRequest(req, body)
		if err != nil {
			return nil, err
		}
		responses.Model = model
		anthropic, err := apicompat.ResponsesToAnthropicRequest(responses)
		if err != nil {
			return nil, err
		}
		return json.Marshal(anthropic)
	}
	if inbound == APIProtocolAnthropic {
		var anthropic apicompat.AnthropicRequest
		if err := json.Unmarshal(body, &anthropic); err != nil {
			return nil, err
		}
		if protocol == APIProtocolChatCompletions {
			chat, err := apicompat.AnthropicToChatCompletionsRequestWithMinOutputTokens(&anthropic, configuredMinOutputTokens(d.cfg))
			if err != nil {
				return nil, err
			}
			chat.Model = model
			chat.ReasoningEffort = openAICompatAnthropicReasoningEffort(&anthropic, model, chat.ReasoningEffort)
			forwarded, _ = json.Marshal(chat)
			forwarded, _ = NormalizeGLMOpenAIReasoningEffort(forwarded, model)
		} else {
			anthropic.Model = model
			responses, err := anthropicToResponses(d.cfg, &anthropic)
			if err != nil {
				return nil, err
			}
			responses.Model = model
			if responses.Reasoning != nil {
				responses.Reasoning.Effort = openAICompatAnthropicReasoningEffort(&anthropic, model, responses.Reasoning.Effort)
			}
			forwarded, _ = json.Marshal(responses)
		}
	} else if inbound == APIProtocolChatCompletions && protocol == APIProtocolChatCompletions {
		forwarded, _ = NormalizeGLMOpenAIReasoningEffort(forwarded, model)
	} else if inbound == APIProtocolChatCompletions && protocol == APIProtocolResponses {
		responses, err := d.inflightEffortResponsesRequest(req, body)
		if err != nil {
			return nil, err
		}
		responses.Model = model
		forwarded, _ = json.Marshal(responses)
	}
	if account.Platform == PlatformGrok && inbound != APIProtocolAnthropic {
		if protocol == APIProtocolChatCompletions {
			return normalizeGrokChatReasoningEffort(forwarded, model)
		}
		return normalizeGrokResponsesReasoningEffort(forwarded, model)
	}
	if account.Platform == PlatformOpenAI {
		forwarded = ReplaceModelInBody(forwarded, model)
		if protocol == APIProtocolResponses {
			var err error
			forwarded, err = filterOpenAIResponsesNoneReasoningEffortForAccount(account, forwarded)
			if err != nil {
				return nil, err
			}
		}
		if inbound == APIProtocolAnthropic {
			policyBody, _, err := ApplyOpenAIReasoningEffortPolicyFromContext(ctx, forwarded)
			return policyBody, err
		}
	}
	return forwarded, nil
}

func (d inflightEstimateDeps) inflightEffortResponsesRequest(req InflightEstimateRequest, body []byte) (*apicompat.ResponsesRequest, error) {
	if req.Protocol == ContentModerationProtocolOpenAIChat {
		var chat apicompat.ChatCompletionsRequest
		if err := json.Unmarshal(body, &chat); err != nil {
			return nil, err
		}
		return chatCompletionsToResponses(d.cfg, &chat)
	}
	var responses apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responses); err != nil {
		return nil, err
	}
	return &responses, nil
}
