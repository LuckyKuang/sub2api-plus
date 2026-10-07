//go:build unit || !integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// errZhipuStubRejected stands in for a platform rejection so the exchange route is
// exercised without reaching the network.
var errZhipuStubRejected = errors.New("stub platform rejection")

// zhipuHandlerStubClient implements only the link surface the read-only routes
// need; account creation is exercised by the service suite.
type zhipuHandlerStubClient struct{}

func (zhipuHandlerStubClient) StartFlow(context.Context, string, string, string) (*zcode.FlowInit, error) {
	return &zcode.FlowInit{
		FlowID:          "flow-1",
		AuthorizeURL:    "https://bigmodel.cn/login?appId=zcode&state=st-1",
		ExpiresAt:       time.Now().Add(10 * time.Minute),
		PollIntervalSec: 5,
	}, nil
}

func (zhipuHandlerStubClient) PollFlow(context.Context, string, string, string, string) (*zcode.FlowPoll, error) {
	return &zcode.FlowPoll{Status: zcode.FlowStatusPending}, nil
}

func (zhipuHandlerStubClient) ExchangeCode(context.Context, string, string, string, string, string) (*zcode.FlowReady, error) {
	return nil, errZhipuStubRejected
}

func (zhipuHandlerStubClient) ExchangeZaiBusinessToken(context.Context, string, string) (string, error) {
	return "", nil
}

func (zhipuHandlerStubClient) ResolveIndividualCodingPlanKey(context.Context, string, string, string) (*zcode.CodingPlanCredential, error) {
	return &zcode.CodingPlanCredential{APIKey: "ak.sk", OrganizationID: "org-1", ProjectID: "proj-1"}, nil
}

func (zhipuHandlerStubClient) ResolveTeamPlanKey(context.Context, string, string, string, string, string) (*zcode.CodingPlanCredential, error) {
	return &zcode.CodingPlanCredential{APIKey: "tak.tsk", OrganizationID: "org-t", ProjectID: "proj-t"}, nil
}

func zhipuOAuthTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := service.NewZhipuOAuthService(zhipuHandlerStubClient{}, nil)
	t.Cleanup(svc.Stop)
	handler := NewZhipuOAuthHandler(svc, nil)
	router := gin.New()
	router.GET("/oauth/capabilities", handler.GetCapabilities)
	router.POST("/oauth/start", handler.StartLink)
	router.POST("/oauth/poll", handler.PollLink)
	router.POST("/oauth/exchange-code", handler.ExchangeLink)
	router.POST("/oauth/create-from-oauth", handler.CreateAccountFromLink)
	return router
}

// zhipuStringArray reads a JSON string array without gjson's positional metadata,
// which differs between document locations.
func zhipuStringArray(body, path string) []string {
	raw := gjson.Get(body, path).Array()
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		values = append(values, item.String())
	}
	return values
}

func zhipuRequest(t *testing.T, router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestZhipuOAuthHandlerStartAndPoll(t *testing.T) {
	router := zhipuOAuthTestRouter(t)

	capabilities := zhipuRequest(t, router, http.MethodGet, "/oauth/capabilities", "")
	require.Equal(t, http.StatusOK, capabilities.Code)
	require.True(t, gjson.Get(capabilities.Body.String(), "data.enabled").Bool())
	require.Equal(t, "bigmodel", gjson.Get(capabilities.Body.String(), "data.providers.0").String())
	require.Equal(t, "zai", gjson.Get(capabilities.Body.String(), "data.providers.1").String())
	// Every advertised plan kind is implemented.
	require.Equal(t,
		zhipuStringArray(capabilities.Body.String(), "data.plan_kinds"),
		zhipuStringArray(capabilities.Body.String(), "data.supported_plan_kinds"))
	require.Contains(t, zhipuStringArray(capabilities.Body.String(), "data.plan_kinds"), "off-peak")

	start := zhipuRequest(t, router, http.MethodPost, "/oauth/start", `{"provider":"bigmodel"}`)
	require.Equal(t, http.StatusOK, start.Code)
	sessionID := gjson.Get(start.Body.String(), "data.session_id").String()
	require.NotEmpty(t, sessionID)
	require.Equal(t, "bigmodel", gjson.Get(start.Body.String(), "data.provider").String())
	require.Equal(t, "https://bigmodel.cn/login?appId=zcode&state=st-1",
		gjson.Get(start.Body.String(), "data.authorize_url").String())
	require.Equal(t, int64(5), gjson.Get(start.Body.String(), "data.interval_seconds").Int())
	// The poll credential is server-side only.
	require.NotContains(t, start.Body.String(), "poll_token")

	// A request without a provider is rejected rather than silently defaulted.
	noProvider := zhipuRequest(t, router, http.MethodPost, "/oauth/start", `{}`)
	require.Equal(t, http.StatusBadRequest, noProvider.Code)

	poll := zhipuRequest(t, router, http.MethodPost, "/oauth/poll", `{"session_id":"`+sessionID+`"}`)
	require.Equal(t, http.StatusOK, poll.Code)
	require.True(t, gjson.Get(poll.Body.String(), "data.pending").Bool())

	missing := zhipuRequest(t, router, http.MethodPost, "/oauth/poll", `{}`)
	require.Equal(t, http.StatusBadRequest, missing.Code)

	unknown := zhipuRequest(t, router, http.MethodPost, "/oauth/poll", `{"session_id":"nope"}`)
	require.Equal(t, http.StatusBadRequest, unknown.Code)

	unsupported := zhipuRequest(t, router, http.MethodPost, "/oauth/start", `{"provider":"openai"}`)
	require.Equal(t, http.StatusBadRequest, unsupported.Code)
}

func TestZhipuOAuthHandlerExchangeLinkRequiresCode(t *testing.T) {
	router := zhipuOAuthTestRouter(t)

	start := zhipuRequest(t, router, http.MethodPost, "/oauth/start", `{"provider":"zai"}`)
	require.Equal(t, http.StatusOK, start.Code)
	sessionID := gjson.Get(start.Body.String(), "data.session_id").String()

	// The stub returns no credentials, so the code path reports a gateway error
	// rather than succeeding with an empty link.
	exchanged := zhipuRequest(t, router, http.MethodPost, "/oauth/exchange-code",
		`{"session_id":"`+sessionID+`","callback":"https://zcode.z.ai/app/oauth/login?code=c&state=s"}`)
	require.NotEqual(t, http.StatusOK, exchanged.Code)

	missingCallback := zhipuRequest(t, router, http.MethodPost, "/oauth/exchange-code", `{"session_id":"`+sessionID+`"}`)
	require.Equal(t, http.StatusBadRequest, missingCallback.Code)
}

func TestZhipuOAuthHandlerCreateRequiresASession(t *testing.T) {
	router := zhipuOAuthTestRouter(t)
	recorder := zhipuRequest(t, router, http.MethodPost, "/oauth/create-from-oauth",
		`{"session_id":"missing","provider":"bigmodel","plan_kind":"individual-coding-plan","access_token":"t"}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.NotContains(t, recorder.Body.String(), "poll_token")
}

func TestZhipuOAuthCreateRejectsUnavailableProxyBeforeAccountSideEffects(t *testing.T) {
	router := zhipuOAuthTestRouter(t)
	response := zhipuRequest(t, router, http.MethodPost, "/oauth/create-from-oauth", `{"session_id":"session","provider":"bigmodel","access_token":"token","proxy_id":5}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "selected proxy is unavailable")
}
