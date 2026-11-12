package service

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

type compositeNativeProtocolKey struct{}

// WithCompositeNativeProtocol restricts Composite admission and every retry to
// the ingress protocol. A preferred upstream protocol is not a native capability.
func WithCompositeNativeProtocol(ctx context.Context, protocol string) context.Context {
	return context.WithValue(ctx, compositeNativeProtocolKey{}, protocol)
}

func CompositeNativeProtocol(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(compositeNativeProtocolKey{}).(string)
	return value
}

func CompositeNativeProtocolForPath(path string) string {
	switch {
	case strings.Contains(path, "/responses"), strings.HasSuffix(path, "/live"), strings.Contains(path, "/realtime/calls"), strings.Contains(path, "/alpha/search"):
		return APIProtocolResponses
	case strings.HasSuffix(path, "/chat/completions"):
		return APIProtocolChatCompletions
	case strings.Contains(path, "/messages"):
		return APIProtocolAnthropic
	case strings.HasPrefix(path, "/v1beta/"):
		return "gemini"
	case strings.HasSuffix(path, "/systemone"):
		return "systemone"
	default:
		return ""
	}
}

// NativeProtocols is independent of transient load, balance and rate limits.
// It describes only protocols the credential can send without a bridge.
func (a *Account) NativeProtocols(model string) []string {
	if a == nil {
		return nil
	}
	switch a.Platform {
	case PlatformAnthropic:
		return []string{APIProtocolAnthropic}
	case PlatformGemini, PlatformAntigravity:
		return []string{"gemini"}
	case PlatformTypeSafe:
		return []string{"systemone"}
	case PlatformOpenAI:
		if a.IsOpenAIOAuthLike() {
			return []string{APIProtocolResponses}
		}
		// GetAccessToken does not accept other OpenAI credential types. Do not
		// advertise a protocol that this credential cannot actually send.
		if a.Type != AccountTypeAPIKey {
			return nil
		}
		protocols := []string{APIProtocolChatCompletions}
		if !shouldForwardOpenAIResponsesViaRawChatCompletions(a) {
			protocols = append(protocols, APIProtocolResponses)
		}
		return protocols
	case PlatformGrok:
		return []string{APIProtocolResponses, APIProtocolChatCompletions}
	}
	if !a.IsMultiProtocolAPIKey() && !IsMultiProtocolAPIKeyProvider(a.Platform) {
		return nil
	}
	if a.Platform == PlatformZhipu && a.Type == AccountTypeOAuth {
		switch a.GetCredential("plan_kind") {
		case ZhipuPlanStartPlan, ZhipuPlanOffPeak:
			return []string{APIProtocolAnthropic}
		}
	}
	if !a.IsAdaptiveAPIProtocol() {
		protocol := a.GetAPIProtocol()
		if a.providerSupportsProtocol(protocol) {
			return []string{protocol}
		}
		return nil
	}
	if a.routesByModel() {
		return a.supportedCatalogProtocols(a.modelProtocolSet(model, cachedNativeModelProtocols(a, model)))
	}
	var protocols []string
	for _, protocol := range []string{APIProtocolResponses, APIProtocolChatCompletions, APIProtocolAnthropic} {
		if a.providerSupportsProtocol(protocol) {
			protocols = append(protocols, protocol)
		}
	}
	return protocols
}

func compositeNativeAccountAllowed(ctx context.Context, account *Account, model string) bool {
	// A native constraint also applies to unified Responses continuations:
	// conversion consent cannot make a provider-owned response ID portable.
	if protocol := CompositeNativeProtocol(ctx); protocol != "" {
		return account != nil && slices.Contains(account.NativeProtocols(account.GetMappedModel(model)), protocol)
	}
	if endpoint := CompositeUnifiedEndpoint(ctx); endpoint != "" {
		return compositeAccountConversionSupported(account, model, CompositeNativeProtocolForPath(endpoint))
	}
	return true
}

// CheckCompositeNativeForward is the terminal defense for direct senders and
// handler retries. It never derives outbound identity from the selected client.
func CheckCompositeNativeForward(ctx context.Context, c *gin.Context, account *Account, protocol, model string) error {
	if c == nil {
		return nil
	}
	keyValue, _ := c.Get("api_key")
	key, _ := keyValue.(*APIKey)
	if key == nil || key.Group == nil || (key.Group.Platform != PlatformComposite && !key.IsAggregate()) {
		return nil
	}
	if key.ConversionEnabled && key.DefaultEndpoint != "" && CompositeUnifiedEndpoint(ctx) == key.DefaultEndpoint && CompositeNativeProtocolForPath(key.DefaultEndpoint) == protocol && compositeAccountConversionSupported(account, model, protocol) {
		return nil
	}
	if !key.ConversionEnabled && account != nil && CompositeNativeProtocol(ctx) == protocol && slices.Contains(account.NativeProtocols(account.GetMappedModel(model)), protocol) {
		return nil
	}
	err := fmt.Errorf("model does not support this native endpoint with the selected account; use one of its advertised native endpoints")
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "native_endpoint_unsupported", "message": err.Error()}})
	return err
}
