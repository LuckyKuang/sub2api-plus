package service

import "context"

type responsesWSInflightTransportKey struct{}

// Keep only routing decisions for one credential owner, never frame contents.
type responsesWSInflightTransport struct {
	ownerID     int64
	forceHTTP   bool
	passthrough bool
}

// WithResponsesWebSocketInflightTransport captures the actual ingress routing
// decision before reconciling a selected owner's hold. Every attempt replaces
// inherited decisions, including a retry against the same account.
func (s *OpenAIGatewayService) WithResponsesWebSocketInflightTransport(ctx context.Context, account *Account, firstPayload []byte) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	decision := responsesWSInflightTransport{forceHTTP: s.responsesWSUncachedForceHTTPBridge(ctx, account)}
	if account != nil {
		decision.ownerID = account.ID
		if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled && !decision.forceHTTP {
			decision.passthrough = account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault) == OpenAIWSIngressModePassthrough &&
				!s.shouldBridgeOpenAIWSPassthroughFirstMessage(account, firstPayload)
		}
	}
	return context.WithValue(ctx, responsesWSInflightTransportKey{}, decision)
}

// Only an authoritative decision for this exact owner can exempt its mapping.
func responsesWSInflightPassthrough(ctx context.Context, account *Account) bool {
	if ctx == nil || account == nil {
		return false
	}
	decision, ok := ctx.Value(responsesWSInflightTransportKey{}).(responsesWSInflightTransport)
	return ok && decision.ownerID == account.ID && decision.passthrough
}

// Proxy and reservation share the same attempt's dynamic force-HTTP decision.
// Direct service callers without a snapshot retain the existing routing rules.
func (s *OpenAIGatewayService) responsesWSForceHTTPBridge(ctx context.Context, account *Account) bool {
	if ctx != nil && account != nil {
		if decision, ok := ctx.Value(responsesWSInflightTransportKey{}).(responsesWSInflightTransport); ok && decision.ownerID == account.ID {
			return decision.forceHTTP
		}
	}
	return s.responsesWSUncachedForceHTTPBridge(ctx, account)
}

func (s *OpenAIGatewayService) responsesWSUncachedForceHTTPBridge(ctx context.Context, account *Account) bool {
	return s == nil || !s.codexUpstreamWSAllowed(ctx, account) || (account != nil && account.Platform == PlatformGrok) ||
		(s.pluginManager != nil && s.pluginManager.ShouldRouteOpenAIOAuth(account))
}
