//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Official Codex 6a6e2fb1f0e9122464a75929c58356cb7af9e0cc classifies
// only this exact code as policy capacity and retains overload Retry-After.
// Plus keeps capacity request-scoped: no account quarantine, retry or pool scan.
func TestOpenAIPolicyCapacityContract(t *testing.T) {
	for _, envelope := range []string{
		`{"error":{"code":"policy_capacity_unavailable","headers":{"retry-after":"3"}}}`,
		`{"type":"response.failed","response":{"error":{"code":"policy_capacity_unavailable","headers":{"retry-after":"3"}}}}`,
	} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(accountType+envelope, func(t *testing.T) {
				payload := []byte(envelope)
				account := &Account{ID: 42, Platform: PlatformOpenAI, Type: accountType}
				repo := &capacityShedAccountRepoStub{}
				svc := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
				require.True(t, IsOpenAIPolicyCapacityUnavailable(payload))
				require.Equal(t, http.StatusServiceUnavailable, openAIStreamFailureStatus(payload, ""))
				require.Equal(t, http.StatusServiceUnavailable, openAIWSPayloadTransientStatus(payload))
				require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, nil, payload))
				require.Zero(t, repo.tempUnschedCalls)
				failure := newOpenAIUpstreamFailoverError(http.StatusServiceUnavailable, nil, payload, "", true)
				require.Equal(t, GatewayFailureScopeRequest, failure.Scope)
				require.True(t, failure.RequestScopedTransient)
				require.False(t, failure.RetryableOnSameAccount)
				require.False(t, failure.ShouldRetryNextAccount())
				require.Equal(t, 3*time.Second, failure.SameAccountRetryDelay)
				require.Equal(t, "3", failure.ResponseHeaders.Get("Retry-After"))
				require.Equal(t, "Policy-restricted capacity is unavailable. Please try again later.", failure.ClientMessage)
				client, changed := sanitizeOpenAICapacityShedErrorCodeForClient(payload)
				require.False(t, changed)
				require.Equal(t, payload, client)
			})
		}
	}
	for _, payload := range []string{
		`{"error":{"code":"server_is_overloaded","type":"policy_error"}}`,
		`{"error":{"code":"POLICY_CAPACITY_UNAVAILABLE"}}`,
		`{"error":{"code":"policy_capacity_unavailable ","type":"policy_error"}}`,
		`{"error":{"type":"policy_capacity_unavailable","message":"policy_capacity_unavailable"}}`,
	} {
		require.False(t, IsOpenAIPolicyCapacityUnavailable([]byte(payload)), payload)
	}
}

func TestOpenAIPolicyCapacityPrecedesConnectivityMessage(t *testing.T) {
	for _, message := range []string{"connection refused", "connection termination", "reset before headers"} {
		body := []byte(`{"error":{"code":"policy_capacity_unavailable","headers":{"retry-after":"3"},"message":"` + message + `"}}`)
		failure := newOpenAIUpstreamFailoverError(http.StatusServiceUnavailable, nil, body, message, true)
		require.Equal(t, GatewayFailureScopeRequest, failure.Scope, message)
		require.Equal(t, NextAccountStop, failure.NextAccountAction, message)
		require.True(t, failure.RequestScopedTransient, message)
		require.False(t, failure.RetryableOnSameAccount, message)
		require.False(t, failure.ShouldRetryNextAccount(), message)
		require.Equal(t, 3*time.Second, failure.SameAccountRetryDelay, message)
		require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode, message)
	}
}

func TestOpenAIPolicyCapacityHTTPStatusBoundary(t *testing.T) {
	// api_bridge.rs recognizes this HTTP code only with 503. A real 401/403
	// still describes the rejected credential/access, even with this code or
	// a misleading ordinary-overload message.
	body := []byte(`{"error":{"code":"policy_capacity_unavailable","message":"Selected model is at capacity"}}`)
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		require.False(t, isOpenAIHTTPCapacityShed(status, "Selected model is at capacity", body))
		require.False(t, isOpenAITransientProcessingError(status, "Selected model is at capacity", body))
		failure := newOpenAIUpstreamFailoverError(status, nil, body, "Selected model is at capacity", false)
		require.False(t, failure.IsOpenAICapacityShed())
		require.False(t, failure.RequestScopedTransient)
		require.Equal(t, status, failure.StatusCode)
	}
	for _, event := range []string{
		`{"type":"response.failed","response":{"error":{"code":"policy_capacity_unavailable","status_code":401}}}`,
		`{"type":"error","error":{"code":"policy_capacity_unavailable","status_code":403}}`,
	} {
		require.Equal(t, http.StatusServiceUnavailable, openAIStreamFailureStatus([]byte(event), ""))
		require.Equal(t, http.StatusServiceUnavailable, openAIWSPayloadTransientStatus([]byte(event)))
	}
}

func TestOpenAIPolicyCapacityHTTPToWebSocketBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, turn := range []int{1, 2} {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     http.Header{"Retry-After": {"3"}, "Set-Cookie": {"must-not-forward"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"policy_capacity_unavailable","type":"service_unavailable_error"}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1}
			payload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)
			var writes [][]byte
			_, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture-token", payload, len(payload), "gpt-5.1", "", "", "", "", turn, func(message []byte) error {
				writes = append(writes, append([]byte(nil), message...))
				return nil
			})
			require.NoError(t, err)
			require.Len(t, writes, 1)
			require.Contains(t, string(writes[0]), `"code":"policy_capacity_unavailable"`)
			require.Contains(t, string(writes[0]), `"status":503`)
			require.Contains(t, string(writes[0]), `"Retry-After":"3"`)
			require.NotContains(t, string(writes[0]), "must-not-forward")
			require.NotContains(t, string(writes[0]), `"code":"server_error"`)
		}
	}
}

// Exercise actual native and passthrough SSE forwarding on both sides of the
// semantic-output boundary. A policy-capacity terminal keeps its own code and
// retry advice; partial output never authorizes a second account attempt.
func TestOpenAIPolicyCapacitySSEForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			stream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_policy\"}}\n\n"
			if partial {
				stream += "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\n"
			}
			stream += "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_policy\",\"status\":\"failed\",\"error\":{\"code\":\"policy_capacity_unavailable\",\"headers\":{\"retry-after\":\"3\"}}}}\n\n"
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), response, c, account, time.Now(), "gpt-5.1", "gpt-5.1")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), response, c, account, time.Now(), "gpt-5.1", "gpt-5.1")
			}
			require.Error(t, err)
			var failure *UpstreamFailoverError
			if partial {
				require.False(t, errors.As(err, &failure))
				require.Contains(t, rec.Body.String(), "partial answer")
				require.Contains(t, rec.Body.String(), `"code":"policy_capacity_unavailable"`)
				require.Contains(t, rec.Body.String(), `"retry-after":"3"`)
				require.NotContains(t, rec.Body.String(), `"code":"server_error"`)
			} else {
				require.ErrorAs(t, err, &failure)
				require.False(t, failure.ShouldRetryNextAccount())
				require.Equal(t, 3*time.Second, failure.SameAccountRetryDelay)
				require.Empty(t, rec.Body.String())
			}
		}
	}
}
