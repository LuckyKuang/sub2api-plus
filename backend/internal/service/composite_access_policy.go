package service

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type compositeUnifiedEndpointKey struct{}

func CompositeUnifiedEndpoint(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(compositeUnifiedEndpointKey{}).(string)
	return value
}

// BindCompositeAccessPolicy runs after authentication and before routing/audit.
// It validates the requested interface without selecting an account or editing
// the original protocol payload consumed by the security audit engines.
// compositeEndpointAdmits reports whether a canonical path is inference at the
// configured unified endpoint, or a non-inference auxiliary call that belongs to
// it. Anthropic clients size a prompt with count_tokens before sending inference,
// so a unified Messages key must admit its own count_tokens sub-path; the other
// unified endpoints have no equivalent auxiliary call.
func compositeEndpointAdmits(canonical, defaultEndpoint string) bool {
	if canonical == defaultEndpoint {
		return true
	}
	return defaultEndpoint == "/v1/messages" && canonical == "/v1/messages/count_tokens"
}

func BindCompositeAccessPolicy(c *gin.Context, key *APIKey) bool {
	if key == nil || key.Group == nil || key.Group.Platform != PlatformComposite {
		return true
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	if !key.ConversionEnabled {
		c.Request = c.Request.WithContext(WithCompositeNativeProtocol(c.Request.Context(), CompositeNativeProtocolForPath(path)))
		return true
	}
	// Model discovery is shared by all client protocols and is not inference.
	if c.Request.Method == http.MethodGet && (path == "/v1/models" || path == "/models" || strings.HasPrefix(path, "/v1/models/")) {
		return true
	}
	canonical := "/v1/" + strings.TrimPrefix(strings.TrimPrefix(path, "/v1/"), "/")
	if c.Request.Method != http.MethodPost || !slices.Contains(CompositeConversionEndpoints(), key.DefaultEndpoint) || !compositeEndpointAdmits(canonical, key.DefaultEndpoint) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "composite_endpoint_required", "message": "This Composite API key only accepts inference at its configured endpoint: " + key.DefaultEndpoint}})
		return false
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), compositeUnifiedEndpointKey{}, key.DefaultEndpoint))
	return true
}

func CompositeConversionEndpoints() []string {
	return []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"}
}

// Dedicated non-chat protocols cannot be represented by chat adapters. Return
// only real converters, never a fabricated successful capability.
func CompositePlatformConversionSupported(platform, protocol string) bool {
	if protocol != APIProtocolResponses && protocol != APIProtocolChatCompletions && protocol != APIProtocolAnthropic {
		return false
	}
	return platform == PlatformAnthropic || platform == PlatformOpenAI || platform == PlatformGrok || platform == PlatformGemini || platform == PlatformAntigravity || IsMultiProtocolAPIKeyProvider(platform)
}

func compositeAccountConversionSupported(account *Account, model, protocol string) bool {
	return account != nil && len(account.NativeProtocols(account.GetMappedModel(model))) > 0 && CompositePlatformConversionSupported(account.Platform, protocol)
}

// A provider-owned response ID cannot be reconstructed by a stateless adapter.
// Require native Responses instead of silently dropping conversation state.
func CheckCompositeResponsesContinuation(ctx context.Context, c *gin.Context, account *Account, body []byte) error {
	if CompositeUnifiedEndpoint(ctx) != "/v1/responses" || gjson.GetBytes(body, "previous_response_id").String() == "" {
		return nil
	}
	model := gjson.GetBytes(body, "model").String()
	if account != nil && slices.Contains(account.NativeProtocols(account.GetMappedModel(model)), APIProtocolResponses) {
		return nil
	}
	err := errors.New("previous_response_id requires native Responses support; send the full conversation history for protocol conversion")
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "composite_native_continuation_required", "message": err.Error()}})
	return err
}
