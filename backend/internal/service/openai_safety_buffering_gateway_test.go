//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func (r *safetyBufferingSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[key], r.readErr
}

func safetyBufferingGatewayFixture(t *testing.T, kind string, cli bool, verified *codexcli.Status, upstream HTTPUpstream) (*OpenAIGatewayService, *Account, *atomic.Int32) {
	t.Helper()
	svc, repo := safetyBufferingTestService(t)
	repo.values[SettingKeyCodexCLIBridgeEnabled] = strconvBool(cli)
	owner := enabledCodexCLIExecutionAccount(101, "acquired-access", "workspace")
	if kind == AccountTypeAPIKey {
		owner = codexBridgeAPIKeyFixture()
	}
	owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = cli
	owner.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	bridges := &atomic.Int32{}
	status := codexCLIBridgeReadyFixture()
	if verified != nil {
		status = *verified
	}
	svc.codexCLIBridgeRuntime = codexCLIBridgeTransportStub{status: status, bridge: func(ctx context.Context, status codexcli.Status, request *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
		bridges.Add(1)
		if verified != nil {
			return codexcli.Bridge(ctx, status, request, forward)
		}
		return forward(ctx, request)
	}}
	svc.cfg = &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize, OpenAIWS: config.GatewayOpenAIWSConfig{HTTPBridgeEnabled: true, ModeRouterV2Enabled: true, IngressModeDefault: OpenAIWSIngressModeHTTPBridge}}}
	svc.accountRepo = &codexCLIBridgeStreamAccountRepo{openaiTransportAccountRepoStub: &openaiTransportAccountRepoStub{}, owner: owner}
	svc.httpUpstream, svc.cache, svc.toolCorrector = upstream, &stubGatewayCache{}, NewCodexToolCorrector()
	saveSafetyBuffering(t, svc.settingService, true)
	return svc, owner, bridges
}

func TestCodexSafetyBufferingGatewayAccountAndCLIMatrix(t *testing.T) {
	testCodexSafetyBufferingGatewayMatrix(t, nil, false)
}

// The dedicated wire launcher supplies the independently verified pinned
// executable. Manager.Status must create the real integrity proof; setting
// public status booleans cannot authorize execution of this test's CLI hop.
func TestCodexSafetyBufferingOfficialCLIWire(t *testing.T) {
	binary := os.Getenv("SUB2API_CODEX_BRIDGE_BINARY")
	if binary == "" {
		t.Skip("dedicated official wire gate supplies independently verified binary")
	}
	bin := t.TempDir()
	require.NoError(t, os.Symlink(binary, filepath.Join(bin, "codex")))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	status := codexcli.NewManager(t.TempDir()).Status(t.Context())
	require.True(t, status.SourceVerified, status.UnavailableCode)
	require.True(t, status.ExecutionAvailable, status.UnavailableCode)
	testCodexSafetyBufferingGatewayMatrix(t, &status, true)
	testCodexSafetyBufferingWSBridgeMatrix(t, &status, true)
}

func testCodexSafetyBufferingGatewayMatrix(t *testing.T, verified *codexcli.Status, onlyCLI bool) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			if onlyCLI && !cli {
				continue
			}
			t.Run(kind+"/cli="+strconvBool(cli), func(t *testing.T) {
				var sends atomic.Int32
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					sends.Add(1)
					expected := "Bearer acquired-access"
					if kind == AccountTypeAPIKey {
						expected = "Bearer acquired-api-key"
						require.Empty(t, req.Header.Get("Chatgpt-Account-Id"))
					}
					require.Equal(t, expected, req.Header.Get("Authorization"))
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + safetyBufferingNotice("A") + "\n\ndata: " + safetyBufferingNotice("B") + "\n\ndata: " + safetyBufferingNotice("C") + "\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_hot\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n"))}, nil
				}}
				svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, verified, upstream)
				rec := &safetyBufferingSwitchRecorder{ResponseRecorder: httptest.NewRecorder()}
				seen := map[string]bool{}
				rec.afterWrite = func(payload []byte) {
					for _, line := range strings.Split(string(payload), "\n") {
						if !strings.HasPrefix(line, "data:") {
							continue
						}
						p := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
						marker := gjson.GetBytes(p, "marker").String()
						if marker == "" {
							continue
						}
						seen[marker] = true
						require.Equal(t, marker == "B", gjson.GetBytes(p, "metadata.type").Exists(), marker)
						if marker == "A" {
							saveSafetyBuffering(t, svc.settingService, false)
						}
						if marker == "B" {
							saveSafetyBuffering(t, svc.settingService, true)
						}
					}
				}
				c, _ := gin.CreateTestContext(rec)
				body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
				result, err := svc.Forward(t.Context(), c, owner, body)
				require.NoError(t, err, rec.Body.String())
				require.Len(t, seen, 3, rec.Body.String())
				require.Equal(t, 12, result.Usage.InputTokens)
				require.Equal(t, int32(1), sends.Load(), "switching display policy must not replay generation")
				expectedBridges := int32(0)
				if cli {
					expectedBridges = 1
				}
				require.Equal(t, expectedBridges, bridges.Load())
			})
		}
	}
}

func TestCodexSafetyBufferingWSBridgeAccountAndCLIMatrix(t *testing.T) {
	testCodexSafetyBufferingWSBridgeMatrix(t, nil, false)
}

// The real downstream socket and production passthrough relay must apply the
// live setting to text and binary frames, across turns on the same connection.
func TestCodexSafetyBufferingWSPassthroughHotSwitch(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
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
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
			for turn := 0; turn < 2; turn++ {
				for i, enabled := range []bool{false, true, false, true} {
					saveSafetyBuffering(t, svc.settingService, enabled)
					msgType := coderws.MessageText
					if i%2 == 1 {
						msgType = coderws.MessageBinary
					}
					upstream.frames <- stagedPassthroughFrame{messageType: msgType, payload: []byte(safetyBufferingNotice("native"))}
					gotType, payload, err := client.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, msgType, gotType)
					require.Equal(t, !enabled, gjson.GetBytes(payload, "metadata.type").Exists())
					require.True(t, gjson.GetBytes(payload, "metadata.guardian.requires_review").Bool())
				}
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_native","usage":{"input_tokens":12,"output_tokens":3}}}`)
				_, completed, err := client.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
				require.Equal(t, int64(12), gjson.GetBytes(completed, "response.usage.input_tokens").Int())
				if turn == 0 {
					require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"next"}`)))
					requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
				}
			}
			require.Empty(t, upstream.writes, "changing display policy must not replay a turn")
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case <-serverErr:
			case <-ctx.Done():
				t.Fatal("passthrough did not release its session")
			}
		})
	}
}

func testCodexSafetyBufferingWSBridgeMatrix(t *testing.T, verified *codexcli.Status, onlyCLI bool) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			if onlyCLI && !cli {
				continue
			}
			t.Run("ws/"+kind+"/cli="+strconvBool(cli), func(t *testing.T) {
				advance := make(chan struct{})
				padding := ""
				if verified != nil {
					// The pinned official proxy's HTTP writer buffers small body
					// chunks. Opaque SSE comments release each fixture checkpoint
					// without adding client events or changing their assertions.
					padding = ":" + strings.Repeat(" ", 64*1024) + "\n\n"
				}
				var sends atomic.Int32
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					sends.Add(1)
					reader, writer := io.Pipe()
					go func() {
						defer writer.Close()
						if _, err := io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n"+padding); err != nil {
							return
						}
						for _, marker := range []string{"A", "B", "C"} {
							// A/C are delivered while suppression is ON. A pure
							// notice must not create an extra or empty WS frame
							// before the independent mixed event below.
							if marker != "B" {
								if _, err := fmt.Fprintf(writer, "data: %s\n\n", pureSafetyBufferingNotice); err != nil {
									return
								}
							}
							if _, err := fmt.Fprintf(writer, "data: %s\n\n%s", safetyBufferingNotice(marker), padding); err != nil {
								return
							}
							select {
							case <-advance:
							case <-req.Context().Done():
								return
							}
						}
						_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ws\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n")
					}()
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
				}}
				svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, verified, upstream)
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
					c.Request = r.Clone(r.Context())
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
				_, _, err = conn.Read(ctx)
				require.NoError(t, err) // initial answer delta
				for _, marker := range []string{"A", "B", "C"} {
					_, payload, readErr := conn.Read(ctx)
					require.NoError(t, readErr)
					require.Equal(t, marker, gjson.GetBytes(payload, "marker").String())
					require.Equal(t, marker == "B", gjson.GetBytes(payload, "metadata.type").Exists(), marker)
					require.True(t, gjson.GetBytes(payload, "metadata.guardian.requires_review").Bool())
					if marker == "A" {
						saveSafetyBuffering(t, svc.settingService, false)
					}
					if marker == "B" {
						saveSafetyBuffering(t, svc.settingService, true)
					}
					select {
					case advance <- struct{}{}:
					case <-ctx.Done():
						t.Fatal("upstream did not advance")
					}
				}
				_, completed, err := conn.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
				require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
				require.NoError(t, <-errCh)
				require.Equal(t, int32(1), sends.Load())
				expectedBridges := int32(0)
				if cli {
					expectedBridges = 1
				}
				require.Equal(t, expectedBridges, bridges.Load())
			})
		}
	}
}
