package service

import (
	"net/http"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	attemptCtx := request.Context()
	var err error
	proxyURL, err = requestCredentialOwnerProxyURL(attemptCtx, account)
	if err != nil {
		return nil, err
	}
	*request = *request.WithContext(WithAccountOutboundIdentity(request.Context(), account))
	response, responseErr := s.projectTokens.RoundTrip(request, account, func(req *http.Request) (*http.Response, error) {
		return s.cnOAuthService.roundTripNativeOAuth(req, account, func(native *http.Request) (*http.Response, error) {
			resp, err := s.doOpenAIUpstreamPrepared(native, proxyURL, account)
			if resp != nil {
				resp.Request = native
			}
			if enabled, _ := native.Context().Value(nativeAnthropicPreflightKey{}).(bool); enabled && err == nil {
				if err = s.prepareNativeAnthropicResponse(native.Context(), resp); err != nil && resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
			}
			return resp, err
		})
	})
	if response != nil {
		finalizeTransmittedRequestCredentialOwner(attemptCtx, response.Request)
	}
	return response, responseErr
}

func (s *OpenAIGatewayService) doOpenAIUpstreamPrepared(request *http.Request, _ string, account *Account) (response *http.Response, responseErr error) {
	credentialAccount, err := requestCredentialAccount(request.Context(), account)
	if err != nil {
		return nil, err
	}
	credentialAccount = snapshotRequestCredentialAccount(credentialAccount)
	// The account's route owns every provider-bound send, including unbound
	// API-key paths and plugins. Explicit transport arguments cannot replace it.
	ownerProxyURL, err := requestCredentialOwnerProxyURL(request.Context(), credentialAccount)
	if err != nil {
		return nil, err
	}
	request = prepareAccountOutboundRequest(request, credentialAccount)
	if err := OutboundPreparationError(request.Context()); err != nil {
		return nil, err
	}
	captureImageResultDownloadOwner(request, credentialAccount)
	ticket, err := applyZhipuOffPeakTicketHeader(request.Context(), credentialAccount, request.Header)
	if err != nil {
		return nil, err
	}
	request = ticket.bindRequest(request)
	defer func() { holdZhipuOffPeakResponse(request.Context(), ticket, response, responseErr) }()
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, ownerProxyURL, credentialAccount)
		if handled {
			return response, err
		}
	}
	return s.httpUpstream.Do(request, ownerProxyURL, account.ID, account.Concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(request *http.Request, proxyURL string, account *Account, useTLSFallback bool) (*http.Response, error) {
	attemptCtx := request.Context()
	var err error
	proxyURL, err = requestCredentialOwnerProxyURL(attemptCtx, account)
	if err != nil {
		return nil, err
	}
	*request = *request.WithContext(WithAccountOutboundIdentity(request.Context(), account))
	response, responseErr := s.projectTokens.RoundTrip(request, account, func(req *http.Request) (*http.Response, error) {
		return s.cnOAuthService.roundTripNativeOAuth(req, account, func(native *http.Request) (*http.Response, error) {
			response, err := s.doOpenAIAccountTestUpstreamPrepared(native, proxyURL, account, useTLSFallback)
			if response != nil {
				response.Request = native
			}
			return response, err
		})
	})
	if response != nil {
		finalizeTransmittedRequestCredentialOwner(attemptCtx, response.Request)
	}
	return response, responseErr
}

func (s *AccountTestService) doOpenAIAccountTestUpstreamPrepared(
	request *http.Request,
	_ string,
	account *Account,
	useTLSFallback bool,
) (response *http.Response, responseErr error) {
	credentialAccount, err := requestCredentialAccount(request.Context(), account)
	if err != nil {
		return nil, err
	}
	credentialAccount = snapshotRequestCredentialAccount(credentialAccount)
	// The account's route owns every provider-bound send, including unbound
	// API-key paths and plugins. Explicit transport arguments cannot replace it.
	ownerProxyURL, err := requestCredentialOwnerProxyURL(request.Context(), credentialAccount)
	if err != nil {
		return nil, err
	}
	request = prepareAccountOutboundRequest(request, credentialAccount)
	if err := OutboundPreparationError(request.Context()); err != nil {
		return nil, err
	}
	ticket, err := applyZhipuOffPeakTicketHeader(request.Context(), credentialAccount, request.Header)
	if err != nil {
		return nil, err
	}
	request = ticket.bindRequest(request)
	defer func() { holdZhipuOffPeakResponse(request.Context(), ticket, response, responseErr) }()
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, ownerProxyURL, credentialAccount)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			ownerProxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(credentialAccount),
		)
	}
	return s.httpUpstream.Do(request, ownerProxyURL, account.ID, account.Concurrency)
}
