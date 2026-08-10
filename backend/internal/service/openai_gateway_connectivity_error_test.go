//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleOpenAIUpstreamConnectivityHTTPError_ProviderScopedWithoutAccountPenalty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	account := &Account{ID: 73, Name: "edge-proxy", Platform: PlatformOpenAI, Status: StatusActive}
	resp := &http.Response{
		StatusCode: http.StatusGatewayTimeout,
		Header:     http.Header{"X-Request-ID": []string{"upstream-timeout"}},
	}

	err := (&OpenAIGatewayService{}).handleOpenAIUpstreamConnectivityHTTPError(
		c,
		account,
		resp,
		[]byte("upstream request timeout"),
		"upstream request timeout",
		"upstream request timeout",
		false,
	)

	require.NotNil(t, err)
	require.Equal(t, GatewayFailureScopeProvider, err.Scope)
	require.Equal(t, GatewayFailureReason("openai_gateway_timeout"), err.Reason)
	require.Equal(t, NextAccountRetry, err.NextAccountAction)
	require.True(t, err.RequestScopedTransient)
	require.False(t, err.ShouldReportAccountScheduleFailure())
	require.Equal(t, StatusActive, account.Status)
	require.False(t, c.Writer.Written())

	diagnostics := GetOpsRoutingDiagnostics(c)
	require.NotNil(t, diagnostics)
	require.Equal(t, "edge_gateway_timeout", diagnostics.TimeoutPhase)
	eventsValue, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := eventsValue.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "transport_error", events[0].Kind)
	require.Equal(t, "provider", events[0].Scope)
}

func TestClassifyOpenAIUpstreamConnectivityFailureDoesNotTreatGeneric5xxAsTransport(t *testing.T) {
	require.Nil(t, classifyOpenAIUpstreamConnectivityFailure(http.StatusInternalServerError, "internal provider error", nil))

	classification := classifyOpenAIUpstreamConnectivityFailure(http.StatusServiceUnavailable, "remote connection failure", nil)
	require.NotNil(t, classification)
	require.Equal(t, "connection_reset", classification.TransportFailure)
	require.Equal(t, GatewayFailureReason("openai_transport_connection_reset"), classification.Reason)
}
