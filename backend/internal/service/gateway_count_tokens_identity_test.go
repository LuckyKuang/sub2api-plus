//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardCountTokensRetryRetainsIdentitySnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGatewayForwardingSettingsCacheForTest(t)
	settings, ctx := outboundIdentityTestSettings(t, OutboundIdentitySettings{Profiles: map[string]OutboundIdentitySelection{"claude": {Preset: "claude", Version: "3.9.1"}}})
	var headers []http.Header
	var bodies []string
	upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		headers = append(headers, req.Header.Clone())
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.NoError(t, req.Body.Close())
		bodies = append(bodies, string(body))
		status, response := http.StatusOK, `{"input_tokens":1}`
		if len(headers) == 1 {
			require.NoError(t, settings.SetOutboundIdentitySettings(ctx, OutboundIdentitySettings{Profiles: map[string]OutboundIdentitySelection{"claude": {Preset: "claude", Version: "3.9.2"}}}))
			status, response = http.StatusBadRequest, `{"error":{"message":"Invalid signature in thinking block"}}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	}}
	cfg := &config.Config{}
	svc := &GatewayService{cfg: cfg, settingService: settings, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/3.9.1 (external, cli)")
	body := []byte(`{"model":"claude-haiku-4-5","system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.81; cc_entrypoint=cli;"}],"messages":[{"role":"user","content":"hello"}]}`)
	parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-haiku-4-5"}
	require.NoError(t, svc.ForwardCountTokens(ctx, c, account, parsed))
	require.Len(t, headers, 2, "exercise the real signature retry path")
	for index, header := range headers {
		require.Equal(t, "claude-cli/3.9.1 (external, cli)", header.Get("User-Agent"))
		require.Contains(t, bodies[index], "cc_version=3.9.1")
		require.Equal(t, "Bearer test-token", getHeaderRaw(header, "authorization"))
	}
	// The next independent request must observe the new global configuration.
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/3.9.2 (external, cli)")
	parsed = &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-haiku-4-5"}
	require.NoError(t, svc.ForwardCountTokens(ctx, c, account, parsed))
	require.Len(t, headers, 3)
	require.Contains(t, headers[2].Get("User-Agent"), "/3.9.2")
	require.Contains(t, bodies[2], "cc_version=3.9.2")
}
