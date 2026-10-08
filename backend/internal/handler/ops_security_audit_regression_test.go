//go:build unit || !integration

package handler

import (
	"context"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Approved error-request audit: a local policy refusal is a request rejection,
// not an internal malfunction. Expectations come from that distinction, not
// the current error normalizer. Capture the actual middleware queue boundary.
func TestOpsLocalSecurityAuditDenialCategory(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		legacy     bool
	}{
		{"content", "content_policy_violation", true},
		{"session", "session_blocked_by_content_policy", true},
		{"prompt", "prompt_guard_blocked", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				d := &securityaudit.Decision{Kind: securityaudit.DecisionBlock, HTTPStatus: 403, ErrorCode: tc.code, ClientMessage: "policy refusal"}
				if tc.legacy {
					d.Legacy = &securityaudit.LegacyDecision{Blocked: true, ErrorCode: tc.code, Message: "policy refusal", StatusCode: 403}
				}
				(&OpenAIGatewayHandler{}).openAISecurityAuditError(c, d)
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Equal(t, 403, w.Code)
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			entry := (<-opsErrorLogQueue).entry
			require.Equal(t, "request", entry.ErrorPhase)
			require.Equal(t, tc.code, entry.ErrorType)
			require.Equal(t, "client", entry.ErrorOwner)
			require.Equal(t, "client_request", entry.ErrorSource)
			require.Equal(t, "security_audit", service.MapUserErrorCategory(entry.ErrorPhase, entry.ErrorType))
			require.Nil(t, entry.AccountID)
			require.False(t, entry.IsBusinessLimited, "classification correction preserves the established metric flag")
		})
	}
}

func TestOpsSecurityAuditDependencyFailureIsStillInternal(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		(&OpenAIGatewayHandler{}).openAISecurityAuditError(c, promptGuardDecision(securityaudit.DecisionUnavailable))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, 503, w.Code)
	entry := (<-opsErrorLogQueue).entry
	require.Equal(t, "internal", entry.ErrorPhase)
	require.Equal(t, "platform", entry.ErrorOwner)
	require.NotEqual(t, "security_audit", service.MapUserErrorCategory(entry.ErrorPhase, entry.ErrorType))
}

func TestOpsProviderPolicyCodeDoesNotBecomeLocalAudit(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		service.SetOpsUpstreamError(c, 403, "provider refusal", "")
		c.JSON(403, gin.H{"error": gin.H{"type": "permission_error", "code": "prompt_guard_blocked", "message": "provider refusal"}})
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	entry := (<-opsErrorLogQueue).entry
	require.Equal(t, "upstream", entry.ErrorPhase)
	require.Equal(t, "provider", entry.ErrorOwner)
	require.Equal(t, "upstream", service.MapUserErrorCategory(entry.ErrorPhase, entry.ErrorType))
}

func TestOpsLocalAuditWebSocketTurnDoesNotRewriteEarlierProviderFailure(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 4)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
	service.BeginOpsStreamTurn(c, 1)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.AccountID, int64(29)))
	c.Set(opsAccountIDKey, int64(29))
	service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, 17)
	service.SetOpsUpstreamError(c, 502, "provider failed", "")
	service.MarkOpsStreamFailure(c, "upstream_error", "remote", "first turn", 502)
	service.BeginOpsStreamTurn(c, 2)
	markOpsSecurityAuditDecision(c, promptGuardDecision(securityaudit.DecisionBlock))
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	logOpsStreamError(c, ops, 101)
	require.EqualValues(t, 2, OpsErrorLogQueueLength())
	first := (<-opsErrorLogQueue).entry
	second := (<-opsErrorLogQueue).entry
	require.Equal(t, "upstream", first.ErrorPhase)
	require.Equal(t, "provider", first.ErrorOwner)
	require.EqualValues(t, 29, *first.AccountID)
	require.EqualValues(t, 17, *first.TimeToFirstTokenMs)
	require.Equal(t, "request", second.ErrorPhase)
	require.Equal(t, "prompt_guard_blocked", second.ErrorType)
	require.Equal(t, "client", second.ErrorOwner)
	require.Equal(t, 403, second.StatusCode)
	require.Nil(t, second.AccountID)
	require.Nil(t, second.UpstreamStatusCode)
	require.Nil(t, second.TimeToFirstTokenMs)
	require.Empty(t, second.UpstreamModel)
	require.False(t, second.IsBusinessLimited)
}
