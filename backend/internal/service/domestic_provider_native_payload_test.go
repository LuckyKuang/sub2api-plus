//go:build unit

package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDomesticProviderNativeMessagesPreserveOfficialMediaAndToolChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, platform, payload, beta string }{
		{
			name: "DeepSeek file image and deferred tools", platform: PlatformDeepseek,
			payload: `"tools":[{"name":"lookup","description":"lookup metadata","input_schema":{"type":"object"},"defer_loading":true}],"messages":[{"role":"user","content":"previous request"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"lookup"}},{"type":"tool_removal","tool":{"type":"tool_reference","name":"old_tool"}}]},{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file-test"}},{"type":"text","text":"inspect current attachment"}]}]`,
			beta:    "files-api-2025-04-14,mid-conversation-tool-changes-2026-07-01",
		},
		{
			name: "DeepSeek inline fallback", platform: PlatformDeepseek,
			payload: `"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"text","text":"inspect current attachment"}]}]`,
		},
		{
			name: "MiniMax inline image and video", platform: PlatformMiniMax,
			payload: `"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"video","source":{"type":"base64","media_type":"video/mp4","data":"BBBB"}},{"type":"text","text":"inspect current attachment"}]}]`,
		},
		{
			name: "MiniMax video MIME fallback", platform: PlatformMiniMax,
			payload: `"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"video/mp4","data":"BBBB"}},{"type":"text","text":"inspect current attachment"}]}]`,
		},
		{
			name: "MiniMax uploaded media references", platform: PlatformMiniMax,
			payload: `"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"mm_file://image-test"}},{"type":"video","source":{"type":"url","url":"mm_file://video-test"}},{"type":"text","text":"inspect current attachment"}]}]`,
		},
	} {
		for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", tc.name, accountType, stream), func(t *testing.T) {
					// Literal source shapes and beta tokens come from the pinned
					// official clients, independently of gateway normalizers.
					body := []byte(fmt.Sprintf(`{"model":"native-model","max_tokens":64,"stream":%t,%s}`, stream, tc.payload))
					account := &Account{ID: 71, Platform: tc.platform, Type: accountType, Status: StatusActive,
						Concurrency: 1, Credentials: map[string]any{"api_key": "provider-api-key", "api_protocol": "anthropic"}}
					if accountType == AccountTypeOAuth {
						account.Credentials = cnOAuthCredentials(&cnoauth.Flow{Platform: tc.platform, Region: "cn"},
							&cnoauth.Grant{AccessToken: "native-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(`{"id":"msg_native","type":"message","role":"assistant","model":"native-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)),
					}}
					if stream {
						upstream.resp.Header.Set("Content-Type", "text/event-stream")
						upstream.resp.Body = io.NopCloser(strings.NewReader(miniAnthropicSSEStream()))
					}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream,
						cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request.Header.Set("Anthropic-Beta", tc.beta)
					c.Request.Header.Set("Authorization", "Bearer inbound-token")
					c.Request.Header.Set("X-Api-Key", "inbound-key")
					c.Request.Header.Set("Cookie", "session=inbound-cookie")
					c.Request.Header.Set("User-Agent", "untrusted-client/999")
					result, err := svc.forwardAnthropicViaNativeAnthropicEndpoint(context.Background(), c, account, body, "")
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requests, 1)
					require.JSONEq(t, string(body), string(upstream.lastBody), "native Messages must preserve file/video references and tool changes")
					// Serialize and parse the HTTP boundary: the gateway deliberately
					// retains official lowercase wire casing in its header map.
					var wire bytes.Buffer
					require.NoError(t, upstream.lastReq.Write(&wire))
					wireReq, err := http.ReadRequest(bufio.NewReader(&wire))
					require.NoError(t, err)
					t.Cleanup(func() { _ = wireReq.Body.Close() })
					wantBeta := tc.beta
					if tc.platform == PlatformMiniMax {
						wantBeta = "interleaved-thinking-2025-05-14"
					}
					require.Equal(t, wantBeta, wireReq.Header.Get("Anthropic-Beta"))
					require.Equal(t, "2023-06-01", wireReq.Header.Get("Anthropic-Version"))
					require.Empty(t, upstream.lastReq.Header.Get("Cookie"))
					wantUA := "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)"
					if tc.platform == PlatformMiniMax {
						wantUA = "MiniMaxAgent"
						if accountType == AccountTypeAPIKey {
							wantUA = "Anthropic/JS 0.91.1"
						}
					}
					require.Equal(t, wantUA, upstream.lastReq.UserAgent())
					if accountType == AccountTypeAPIKey {
						require.Equal(t, "provider-api-key", wireReq.Header.Get("X-Api-Key"))
						require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
					} else {
						require.Empty(t, upstream.lastReq.Header.Get("X-Api-Key"))
						if tc.platform == PlatformDeepseek {
							require.Equal(t, "native-token", upstream.lastReq.Header.Get("X-Dsh-Auth-Token"))
							require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
						} else {
							require.Equal(t, "Bearer native-token", upstream.lastReq.Header.Get("Authorization"))
						}
					}
					require.Equal(t, http.StatusOK, recorder.Code)
				})
			}
		}
	}
}
