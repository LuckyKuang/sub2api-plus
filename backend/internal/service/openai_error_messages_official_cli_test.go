//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Actual official CLI consumer + production Forward + real CLI outbound hop.
// OFF is a positive control; ON must render the requested wording and fail
// normally with one upstream generation. No production account is involved.
func TestOpenAIErrorMessagesOfficialCLI(t *testing.T) {
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
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, cli := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, code := range []string{"upstream_response_interrupted", "upstream_unavailable", "session_account_lost"} {
					t.Run(kind+"/cli="+strconvBool(cli)+"/enabled="+strconvBool(enabled)+"/"+code, func(t *testing.T) {
						var sends, requests atomic.Int32
						upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							sends.Add(1)
							expected := "Bearer acquired-access"
							if kind == AccountTypeAPIKey {
								expected = "Bearer acquired-api-key"
							}
							if req.Header.Get("Authorization") != expected {
								return nil, fmt.Errorf("wrong fixture credential owner")
							}
							wire := `data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_cli_error","object":"response","created_at":1700000000,"status":"in_progress","model":"gpt-6.1-sol","output":[]}}` + "\n\n" +
								`data: {"type":"response.output_text.delta","sequence_number":1,"delta":"partial answer"}` + "\n\n" +
								`data: {"type":"response.failed","sequence_number":2,"response":{"id":"resp_cli_error","object":"response","created_at":1700000000,"model":"gpt-6.1-sol","status":"failed","output":[],"error":{"code":"` + code + `","type":"upstream_error","message":"original diagnostic"},"usage":{"input_tokens":12,"output_tokens":3}}}` + "\n\n"
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
						}}
						svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, &status, upstream)
						saveErrorMessages(t, svc.settingService, enabled)
						completed := make(chan error, 1)
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if r.Method != http.MethodPost || r.URL.Path != "/backend-api/codex/responses" {
								http.NotFound(w, r)
								return
							}
							if requests.Add(1) != 1 {
								http.Error(w, "unexpected replay", 409)
								return
							}
							body, err := io.ReadAll(r.Body)
							if err != nil {
								completed <- err
								return
							}
							c, _ := gin.CreateTestContext(w)
							c.Request = r.WithContext(context.WithValue(r.Context(), ctxkey.Platform, PlatformOpenAI))
							OpenAIErrorMessageOutput(svc.settingService, func(c *gin.Context) {
								result, forwardErr := svc.Forward(c.Request.Context(), c, owner, body)
								if forwardErr == nil || result == nil || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3 {
									completed <- fmt.Errorf("failure/usage contract changed")
									return
								}
								completed <- nil
							})(c)
						}))
						defer server.Close()
						ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
						defer cancel()
						command := exec.CommandContext(ctx, "python3", "../../../tools/test_codex_error_messages.py", "--binary", binary, "--url", server.URL, "--code", code, "--enabled", strconvBool(enabled))
						output, err := command.CombinedOutput()
						require.NoError(t, err, string(output))
						require.Contains(t, string(output), "PASS: official CLI")
						select {
						case err := <-completed:
							require.NoError(t, err)
						case <-ctx.Done():
							t.Fatal("gateway did not complete")
						}
						require.Equal(t, int32(1), sends.Load())
						require.Equal(t, int32(1), requests.Load())
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
	// The official outbound path also needs its WS-to-HTTP final writer.
	t.Run("ws-http", func(t *testing.T) { testOpenAIErrorMessagesWSHTTPBridgeMatrix(t, &status) })
}
