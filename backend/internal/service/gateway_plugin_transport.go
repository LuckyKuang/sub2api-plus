package service

import (
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
)

// SetPluginManager gives the legacy Anthropic gateway access to installed
// outbound transports. The OpenAI gateway and account tests receive it through
// their constructors.
func (s *GatewayService) SetPluginManager(manager *PluginManager) {
	if s == nil {
		return
	}
	s.pluginManager = manager
}

// SetPluginManager enables request instructions for the native Gemini
// production path. Transport dispatch remains the native sender here; native
// Gemini/Antigravity transport is separately verified.
func (s *GeminiMessagesCompatService) SetPluginManager(manager *PluginManager) {
	if s == nil {
		return
	}
	s.pluginManager = manager
}

// sendGatewayUpstream runs the prepared request through a selected transport
// when one matches; otherwise it keeps the native TLS-fingerprinted sender.
func (s *GatewayService) sendGatewayUpstream(request *http.Request, proxyURL string, account *Account, profile *tlsfingerprint.Profile) (*http.Response, bool, error) {
	prepared := prepareAccountOutboundRequest(request, account)
	if s != nil && s.pluginManager != nil {
		if response, handled, err := s.pluginManager.RoundTripTransport(prepared.Context(), prepared, proxyURL, account); handled {
			return response, true, err
		}
	}
	response, err := s.httpUpstream.DoWithTLS(prepared, proxyURL, account.ID, account.Concurrency, profile)
	return response, false, err
}

func (s *GatewayService) applyPluginInstructionsToBody(body []byte, account *Account, protocol string) []byte {
	if s == nil || s.pluginManager == nil || account == nil || len(body) == 0 {
		return body
	}
	text := s.pluginManager.RequestInstructionsForAccount(account)
	if text == "" || len(body) > pluginRequestInstructionMaxBodyBytes {
		return body
	}
	modified, changed := ApplyPluginRequestInstructions(body, protocol, text)
	if !changed {
		return body
	}
	return modified
}

func (s *GeminiMessagesCompatService) applyPluginInstructionsToBody(body []byte, account *Account, protocol string) []byte {
	if s == nil || s.pluginManager == nil || account == nil || len(body) == 0 {
		return body
	}
	text := s.pluginManager.RequestInstructionsForAccount(account)
	if text == "" || len(body) > pluginRequestInstructionMaxBodyBytes {
		return body
	}
	modified, changed := ApplyPluginRequestInstructions(body, protocol, text)
	if !changed {
		return body
	}
	return modified
}
