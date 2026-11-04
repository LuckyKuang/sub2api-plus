//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexSafetyBufferingConsumerWriter struct {
	http.ResponseWriter
	ready *atomic.Bool
}

func (w *codexSafetyBufferingConsumerWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err == nil && bytes.Contains(p[:n], []byte(`"delta":"Reviewing fixture"`)) {
		w.ready.Store(true)
	}
	return n, err
}

func (w *codexSafetyBufferingConsumerWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

// Acceptance is observed in the real official interactive consumer, through
// the production gateway. OFF must display the real menu, ON must hide it;
// both must finish the answer on the original generation and keep the CLI live.
// The independent reviewed UI contract is linked in the Python PTY driver.
func TestCodexSafetyBufferingOfficialCLIInteractive(t *testing.T) {
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
			for _, suppressed := range []bool{false, true} {
				t.Run(kind+"/cli="+strconvBool(cli)+"/suppressed="+strconvBool(suppressed), func(t *testing.T) {
					finish := make(chan struct{})
					var finishOnce sync.Once
					release := func() { finishOnce.Do(func() { close(finish) }) }
					defer release()
					var sends atomic.Int32
					var requests atomic.Int32
					var titleRequests atomic.Int32
					var ready atomic.Bool
					upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						sends.Add(1)
						expected := "Bearer acquired-access"
						if kind == AccountTypeAPIKey {
							expected = "Bearer acquired-api-key"
						}
						if req.Header.Get("Authorization") != expected {
							return nil, fmt.Errorf("interactive fixture received wrong credential owner")
						}
						reader, writer := io.Pipe()
						go func() {
							defer writer.Close()
							// Reasoning releases first-output staging without starting an
							// assistant message, which would dismiss the official menu.
							prefix := []string{
								`{"type":"response.created","response":{"id":"resp_interactive","model":"gpt-6.1-sol"}}`,
								`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_fixture","type":"reasoning","summary":[]}}`,
								`{"type":"response.reasoning_summary_part.added","item_id":"rs_fixture","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}`,
								`{"type":"response.reasoning_summary_text.delta","item_id":"rs_fixture","output_index":0,"summary_index":0,"delta":"Reviewing fixture"}`,
								pureSafetyBufferingNotice,
							}
							for _, event := range prefix {
								if _, err := fmt.Fprintf(writer, "data: %s\n\n", event); err != nil {
									return
								}
							}
							// Release the official proxy's small-chunk buffering.
							if _, err := fmt.Fprintf(writer, ":%s\n\n", strings.Repeat(" ", 64*1024)); err != nil {
								return
							}
							select {
							case <-finish:
							case <-req.Context().Done():
								return
							}
							for _, event := range []string{
								`{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_fixture","type":"reasoning","summary":[{"type":"summary_text","text":"Reviewing fixture"}]}}`,
								`{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_fixture","type":"message","role":"assistant","phase":"final_answer","status":"in_progress","content":[]}}`,
								`{"type":"response.content_part.added","item_id":"msg_fixture","output_index":1,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`,
								`{"type":"response.output_text.delta","item_id":"msg_fixture","output_index":1,"content_index":0,"delta":"safety-wire-ok"}`,
								`{"type":"response.output_text.done","item_id":"msg_fixture","output_index":1,"content_index":0,"text":"safety-wire-ok"}`,
								`{"type":"response.content_part.done","item_id":"msg_fixture","output_index":1,"content_index":0,"part":{"type":"output_text","text":"safety-wire-ok","annotations":[]}}`,
								`{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_fixture","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"safety-wire-ok","annotations":[]}]}}`,
								`{"type":"response.completed","response":{"id":"resp_interactive","object":"response","model":"gpt-6.1-sol","status":"completed","end_turn":true,"output":[{"id":"msg_fixture","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"safety-wire-ok","annotations":[]}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
							} {
								if _, err := fmt.Fprintf(writer, "data: %s\n\n", event); err != nil {
									return
								}
							}
						}()
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
					}}
					svc, owner, bridges := safetyBufferingGatewayFixture(t, kind, cli, &status, upstream)
					saveSafetyBuffering(t, svc.settingService, suppressed)
					completed := make(chan error, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/ready" {
							if !ready.Load() {
								w.WriteHeader(http.StatusAccepted)
							}
							return
						}
						if r.URL.Path == "/finish" {
							release()
							w.WriteHeader(http.StatusOK)
							return
						}
						if r.Method != http.MethodPost || r.URL.Path != "/backend-api/codex/responses" {
							http.NotFound(w, r)
							return
						}
						body, err := io.ReadAll(r.Body)
						if err != nil {
							completed <- err
							return
						}
						// The pinned official TUI starts an independent structured
						// title request (app/thread_title.rs). It is not a replay of
						// the user's generation. Serve this auxiliary fixture apart
						// from the production gateway under test, with a strict
						// schema and source-defined prompt discriminator.
						items := gjson.GetBytes(body, "input").Array()
						if len(items) > 0 && strings.HasPrefix(items[len(items)-1].Get("content.0.text").String(), "Generate a concise, single-line task title") && gjson.GetBytes(body, "text.format.schema.properties.title.type").String() == "string" {
							titleRequests.Add(1)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"msg_title\",\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"type\":\"output_text\",\"text\":\"{\\\"title\\\":\\\"Check fixture response\\\"}\"}]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_title\",\"status\":\"completed\",\"end_turn\":true}}\n\n")
							return
						}
						if requests.Add(1) != 1 {
							http.Error(w, "unexpected fixture generation replay", http.StatusConflict)
							return
						}
						c, _ := gin.CreateTestContext(&codexSafetyBufferingConsumerWriter{ResponseWriter: w, ready: &ready})
						c.Request = r
						result, err := svc.Forward(r.Context(), c, owner, body)
						if err == nil && (result == nil || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 3) {
							err = fmt.Errorf("interactive generation lost terminal usage")
						}
						completed <- err
					}))
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 65*time.Second)
					defer cancel()
					command := exec.CommandContext(ctx, "python3", "../../../tools/test_codex_safety_buffering_tui.py", "--binary", binary, "--url", server.URL, "--suppressed", strconvBool(suppressed))
					output, err := command.CombinedOutput()
					// Always release before a failing assertion/server cleanup.
					release()
					require.NoError(t, err, string(output))
					require.Contains(t, string(output), "PASS: official interactive CLI")
					select {
					case err := <-completed:
						require.NoError(t, err)
					case <-ctx.Done():
						t.Fatal("interactive generation did not finish")
					}
					require.Equal(t, int32(1), sends.Load(), "no generation replay or extra tool request")
					require.Equal(t, int32(1), requests.Load(), "the original interactive generation must not be replayed")
					require.Equal(t, int32(1), titleRequests.Load(), "the official auxiliary title request is distinct from the user generation")
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
