package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var errCodexUpstreamWSDisabled = errors.New("codex upstream WebSocket is disabled; reconnect using HTTP with full context")

// Retain transport eligibility and session seeds before account transformations.
// In particular, legacy normalization must not make an opaque continuation
// appear to be a full-history request eligible for an upgrade.
type codexHTTPWSRequestKey struct{}
type codexHTTPWSRequest struct {
	eligible       bool
	cacheKey       string
	executionScope string
}

func codexHTTPWSBodyEligible(body []byte) bool {
	input := gjson.GetBytes(body, "input")
	return gjson.GetBytes(body, "stream").Bool() && input.Exists() &&
		(input.IsArray() || input.Type == gjson.String) &&
		!gjson.GetBytes(body, "background").Bool() &&
		gjson.GetBytes(body, "previous_response_id").String() == "" &&
		!gjson.GetBytes(body, "stream_id").Exists()
}

type runtimeCodexWSResolver struct {
	base     OpenAIWSProtocolResolver
	settings *SettingService
}

func (r runtimeCodexWSResolver) Resolve(account *Account) OpenAIWSProtocolDecision {
	decision := r.base.Resolve(account)
	if !r.settings.GetCodexTransportSettings(context.Background()).WSEnabled {
		return openAIWSHTTPDecision("outbound_ws_disabled")
	}
	return decision
}

// This gate also covers direct WS paths and background pool work. Account
// Responses mode selection remains the protocol resolver's responsibility.
func (s *OpenAIGatewayService) codexUpstreamWSAllowed(ctx context.Context, account *Account) bool {
	if s == nil {
		return false
	}
	if !s.settingService.GetCodexTransportSettings(ctx).WSEnabled {
		return false
	}
	if s.cfg != nil {
		cfg := s.cfg.Gateway.OpenAIWS
		if !cfg.Enabled || cfg.ForceHTTP {
			return false
		}
		if account != nil && ((account.IsOpenAIOAuthLike() && !cfg.OAuthEnabled) || (account.IsOpenAIApiKey() && !cfg.APIKeyEnabled)) {
			return false
		}
	}
	return account == nil || !account.IsOpenAIWSForceHTTPEnabled()
}

func (s *OpenAIGatewayService) requireCodexUpstreamWS(ctx context.Context, account *Account) error {
	if s.codexUpstreamWSAllowed(ctx, account) {
		return nil
	}
	return NewOpenAIWSClientCloseError(coderws.StatusServiceRestart, errCodexUpstreamWSDisabled.Error(), errCodexUpstreamWSDisabled)
}

func (s *OpenAIGatewayService) codexHTTPWSDecision(ctx context.Context, c *gin.Context, account *Account, body []byte) OpenAIWSProtocolDecision {
	if GetOpenAIClientTransport(c) != OpenAIClientTransportHTTP || isOpenAIResponsesCompactPath(c) {
		return openAIWSHTTPDecision("client_protocol_http")
	}
	settings := s.settingService.GetCodexTransportSettings(ctx)
	if !settings.WSEnabled || !settings.HTTPWSEnabled {
		return openAIWSHTTPDecision("http_ws_disabled")
	}
	// A transport upgrade must not strand an HTTP continuation on a socket
	// which does not own its response ID. Complete histories work on either
	// transport; opaque continuation IDs keep their original HTTP semantics.
	original, retained := ctx.Value(codexHTTPWSRequestKey{}).(codexHTTPWSRequest)
	if (retained && !original.eligible) || !codexHTTPWSBodyEligible(body) {
		return openAIWSHTTPDecision("http_request_requires_http")
	}
	if isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader)) {
		metadata := gjson.GetBytes(body, "client_metadata")
		if metadata.Exists() && metadata.Type != gjson.Null && !metadata.IsObject() {
			// An unknown metadata shape cannot be augmented without losing data.
			return openAIWSHTTPDecision("http_request_requires_http")
		}
	}
	if account == nil || s.isOpenAIWSFallbackCooling(account.ID) {
		return openAIWSHTTPDecision("ws_cooling")
	}
	return s.getOpenAIWSProtocolResolver().Resolve(account)
}

// tryCodexHTTPWebSocket attempts exactly one WS generation. Only failures
// before submitting a request may fall back. A silent or broken connection
// after a write is ambiguous even when no text has reached the client.
func (s *OpenAIGatewayService) tryCodexHTTPWebSocket(ctx context.Context, c *gin.Context, account *Account, body []byte, token, originalModel, model, cacheKey string, start time.Time) (*OpenAIForwardResult, bool, error) {
	decision := s.codexHTTPWSDecision(ctx, c, account, body)
	if decision.Transport != OpenAIUpstreamTransportResponsesWebsocketV2 {
		return nil, false, nil
	}
	c.Set("openai_ws_transport_decision", string(decision.Transport))
	c.Set("openai_ws_transport_reason", "http_ws_preferred")
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, true, err
	}
	// Official client.rs::build_ws_client_metadata carries the HTTP Lite
	// declaration in response.create, not in the WebSocket handshake.
	if isOpenAIResponsesLiteHeader(c.GetHeader(responsesLiteHeader)) {
		metadata, _ := payload["client_metadata"].(map[string]any)
		if metadata == nil {
			metadata = make(map[string]any)
		}
		metadata[responsesLiteWSMetadataKey] = "true"
		payload["client_metadata"] = metadata
	}
	executionScope := ""
	if original, ok := ctx.Value(codexHTTPWSRequestKey{}).(codexHTTPWSRequest); ok {
		cacheKey, executionScope = original.cacheKey, original.executionScope
	}
	recovered := false
	result, err := s.forwardOpenAIWSV2(ctx, c, account, payload, cacheKey, executionScope, token, decision, false, true, originalModel, model, start, 1, "", &recovered)
	if err == nil {
		return result, true, nil
	}
	var failure *openAIWSFallbackError
	if ctx.Err() == nil && !c.Writer.Written() && errors.As(err, &failure) && !failure.RequestSubmitted {
		var dialError *openAIWSDialError
		if errors.As(err, &dialError) || errors.Is(err, errCodexUpstreamWSDisabled) {
			s.markOpenAIWSFallbackCooling(account.ID, failure.Reason)
			c.Set("openai_ws_transport_decision", string(OpenAIUpstreamTransportHTTPSSE))
			c.Set("openai_ws_transport_reason", "http_ws_pre_send_fallback")
			logOpenAIWSModeInfo("http_ws_fallback account_id=%d reason=%s", account.ID, normalizeOpenAIWSLogValue(failure.Reason))
			return nil, false, nil
		}
	}
	s.writeOpenAIWSFallbackErrorResponse(c, account, err)
	return result, true, err
}
