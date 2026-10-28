//go:build unit || !integration

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Codex 6a6e2fb1f0e9122464a75929c58356cb7af9e0cc distinguishes policy
// capacity from ordinary overload. The gateway must retain its exact public
// code and Retry-After after pre-output forwarding returns a capacity failure.
func TestOpenAIPolicyCapacityClientResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		failure := &service.UpstreamFailoverError{
			StatusCode:             http.StatusServiceUnavailable,
			ResponseBody:           []byte(`{"error":{"code":"policy_capacity_unavailable"}}`),
			ResponseHeaders:        http.Header{"Retry-After": {"3"}},
			RequestScopedTransient: true,
			ClientStatusCode:       http.StatusServiceUnavailable,
			ClientMessage:          "Policy-restricted capacity is unavailable. Please try again later.",
		}
		(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, failure, stream)
		require.Equal(t, "3", rec.Header().Get("Retry-After"))
		body := rec.Body.String()
		if stream {
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, body, "event: response.failed")
			require.Equal(t, 1, strings.Count(body, `"code":"policy_capacity_unavailable"`))
			require.Contains(t, body, `"Retry-After":"3"`)
		} else {
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Equal(t, "policy_capacity_unavailable", gjson.Get(body, "error.code").String())
		}
		require.NotContains(t, body, `"code":"server_error"`)
	}
}
