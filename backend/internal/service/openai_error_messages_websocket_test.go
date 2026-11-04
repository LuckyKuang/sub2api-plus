//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIErrorMessagesWSPassthroughHotSwitch(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, enabled := range []bool{false, true} {
			for _, frameType := range []coderws.MessageType{coderws.MessageText, coderws.MessageBinary} {
				t.Run(fmt.Sprintf("%s/enabled=%t/frame=%d", kind, enabled, frameType), func(t *testing.T) {
					upstream := newStagedPassthroughConn()
					svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
					svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
					_, repo := safetyBufferingTestService(t)
					svc.settingService = NewSettingService(repo, svc.cfg)
					account := passthroughLifecycleAccount()
					account.Type = kind
					account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
					if kind == AccountTypeOAuth {
						account.Credentials = map[string]any{"access_token": "sk-test", "chatgpt_account_id": "fixture-workspace"}
					}
					saveErrorMessages(t, svc.settingService, !enabled)
					ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
					defer cancel()
					server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
					defer server.Close()
					client := dialPassthroughLifecycleClient(t, server)
					defer client.CloseNow()
					requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
					upstream.Send(`{"type":"response.output_text.delta","delta":"partial answer"}`)
					_, delta, err := client.Read(ctx)
					require.NoError(t, err)
					require.Contains(t, string(delta), "partial answer")
					saveErrorMessages(t, svc.settingService, enabled)
					upstream.frames <- stagedPassthroughFrame{messageType: frameType, payload: []byte(`{"type":"response.failed","response":{"id":"resp_ws","status":"failed","error":{"code":"session_account_lost","type":"invalid_request_error","message":"original session diagnostic"},"usage":{"input_tokens":12,"output_tokens":3}}}`)}
					gotType, payload, err := client.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, frameType, gotType)
					want := "original session diagnostic"
					if enabled {
						want = "当前会话可能无法恢复，请新开会话继续。"
					}
					require.Equal(t, want, gjson.GetBytes(payload, "response.error.message").String())
					require.Equal(t, "session_account_lost", gjson.GetBytes(payload, "response.error.code").String())
					require.Equal(t, "failed", gjson.GetBytes(payload, "response.status").String())
					require.Empty(t, upstream.writes, "display changes must not replay a turn")
					require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
					select {
					case <-serverErr:
					case <-ctx.Done():
						t.Fatal("session did not release")
					}
				})
			}
		}
	}
}

func TestOpenAIErrorMessagesWSHTTPBridgeMatrix(t *testing.T) {
	testOpenAIErrorMessagesWSHTTPBridgeMatrix(t, nil)
}

func testOpenAIErrorMessagesWSHTTPBridgeMatrix(t *testing.T, verified *codexcli.Status) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(kind+"/cli="+strconvBool(cli)+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					advance := make(chan struct{})
					var sends atomic.Int32
					upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						sends.Add(1)
						reader, writer := io.Pipe()
						go func() {
							defer writer.Close()
							if _, err := io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\n:"+strings.Repeat(" ", 64*1024)+"\n\n"); err != nil {
								return
							}
							select {
							case <-advance:
							case <-req.Context().Done():
								return
							}
							_, _ = io.WriteString(writer, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_ws\",\"status\":\"failed\",\"error\":{\"code\":\"upstream_response_interrupted\",\"type\":\"upstream_error\",\"message\":\"original diagnostic\"},\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n")
						}()
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
					}}
					svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, verified, upstream)
					saveErrorMessages(t, svc.settingService, !enabled)
					errCh := make(chan error, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							errCh <- err
							return
						}
						defer conn.CloseNow()
						_, first, err := conn.Read(r.Context())
						if err != nil {
							errCh <- err
							return
						}
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = r
						token := "acquired-access"
						if kind == AccountTypeAPIKey {
							token = "acquired-api-key"
						}
						errCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, owner, token, first, nil)
					}))
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
					defer cancel()
					conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
					require.NoError(t, err)
					defer conn.CloseNow()
					require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)))
					_, delta, err := conn.Read(ctx)
					require.NoError(t, err)
					require.Contains(t, string(delta), "partial answer")
					saveErrorMessages(t, svc.settingService, enabled)
					close(advance)
					_, payload, err := conn.Read(ctx)
					require.NoError(t, err)
					want := "original diagnostic"
					if enabled {
						want = "响应已中断，请输入「继续」重新提交。"
					}
					require.Equal(t, want, gjson.GetBytes(payload, "response.error.message").String())
					require.Equal(t, "upstream_response_interrupted", gjson.GetBytes(payload, "response.error.code").String())
					require.Equal(t, "failed", gjson.GetBytes(payload, "response.status").String())
					_ = conn.Close(coderws.StatusNormalClosure, "done")
					select {
					case <-errCh:
					case <-ctx.Done():
						t.Fatal("bridge did not release")
					}
					require.Equal(t, int32(1), sends.Load())
					wantBridges := int32(0)
					if cli {
						wantBridges = 1
					}
					require.Equal(t, wantBridges, bridges.Load())
				})
			}
		}
	}
}
