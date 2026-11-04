//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Independent wire fixtures follow the client's two documented carriers:
// https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/codex-api/src/sse/responses.rs
// https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/codex-api/src/safety_buffering.rs
// sibling policy, verification and opaque fields are not display controls.
func TestCodexSafetyBufferingProjectionContract(t *testing.T) {
	for _, tc := range []struct{ name, input, expected string }{
		{"top-level", `{"type":"response.in_progress","safety_buffering":{"use_cases":["cyber"],"reasons":["user_risk"],"retry_model":"fast"},"response":{"id":"resp_1"}}`, `{"type":"response.in_progress","response":{"id":"resp_1"}}`},
		{"mixed-metadata", `{"type":"response.metadata","metadata":{"type":"safety_buffering","use_cases":["cyber"],"reasons":["user_risk"],"retry_model":"fast","openai_verification_recommendation":["trusted_access_for_cyber"],"openai_chatgpt_moderation_metadata":{"presentation":"inline"},"guardian":{"requires_review":true},"future":{"keep":true}}}`, `{"type":"response.metadata","metadata":{"openai_verification_recommendation":["trusted_access_for_cyber"],"openai_chatgpt_moderation_metadata":{"presentation":"inline"},"guardian":{"requires_review":true},"future":{"keep":true}}}`},
		{"both-carriers-and-header-case", `{"type":"response.metadata","safety_buffering":{},"metadata":{"type":"safety_buffering","use_cases":[],"reasons":[]},"headers":{"X-Codex-Safety-Buffering-Enabled":"true","x-codex-safety-buffering-faster-model":["fast"],"x-request-id":"retain"}}`, `{"type":"response.metadata","metadata":{},"headers":{"x-request-id":"retain"}}`},
		{"policy-failure", `{"type":"response.failed","safety_buffering":{},"response":{"status":"failed","error":{"code":"cyber_policy","message":"blocked"},"usage":{"input_tokens":12,"output_tokens":3}}}`, `{"type":"response.failed","response":{"status":"failed","error":{"code":"cyber_policy","message":"blocked"},"usage":{"input_tokens":12,"output_tokens":3}}}`},
		{"escaped-key", `{"type":"response.in_progress","safety\u005fbuffering":{},"unknown":true}`, `{"type":"response.in_progress","unknown":true}`},
		{"uppercase-header-only", `{"type":"response.metadata","headers":{"X-CODEX-SAFETY-BUFFERING-ENABLED":"true","X-CODEX-SAFETY-BUFFERING-FASTER-MODEL":["fast"],"x-request-id":"retain"}}`, `{"type":"response.metadata","headers":{"x-request-id":"retain"}}`},
		{"user-content", `{"type":"response.output_item.done","item":{"type":"function_call","arguments":"{\"safety_buffering\":true}","safety_buffering":{"keep":true}},"metadata":{"type":"another_type","retry_model":"retain"}}`, `{"type":"response.output_item.done","item":{"type":"function_call","arguments":"{\"safety_buffering\":true}","safety_buffering":{"keep":true}},"metadata":{"type":"another_type","retry_model":"retain"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stripCodexSafetyBuffering([]byte(tc.input))
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, string(got))
		})
	}
	for _, input := range []string{`{"type":"future","unknown":true}`, `[DONE]`, `{"type":"error","code":"misalignment_policy_violation"}`} {
		got, err := stripCodexSafetyBuffering([]byte(input))
		require.NoError(t, err)
		require.Equal(t, input, string(got))
	}
	invalid := []byte(`{"safety_buffering":`)
	got, err := stripCodexSafetyBuffering(invalid)
	require.Error(t, err)
	require.Equal(t, invalid, got)
}

func safetyBufferingTestService(t *testing.T) (*OpenAIGatewayService, *safetyBufferingSettingsRepo) {
	t.Helper()
	repo := &safetyBufferingSettingsRepo{values: map[string]string{}}
	return &OpenAIGatewayService{cfg: &config.Config{}, settingService: NewSettingService(repo, &config.Config{})}, repo
}

func saveSafetyBuffering(t *testing.T, settings *SettingService, enabled bool) {
	t.Helper()
	raw := "false"
	if enabled {
		raw = "true"
	}
	require.NoError(t, settings.persistSystemSettingsWithCodexSafetyBuffering(t.Context(), &SystemSettings{}, map[string]string{SettingKeyOpenAICodexSafetyBufferingSuppressed: raw}))
}

func safetyBufferingNotice(marker string) string {
	return `{"type":"response.metadata","marker":"` + marker + `","metadata":{"type":"safety_buffering","use_cases":["cyber"],"reasons":["user_risk"],"retry_model":"fast","guardian":{"requires_review":true}}}`
}

type safetyBufferingSwitchRecorder struct {
	*httptest.ResponseRecorder
	afterWrite func([]byte)
}

func (r *safetyBufferingSwitchRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseRecorder.Write(p)
	if err == nil && r.afterWrite != nil {
		r.afterWrite(p)
	}
	return n, err
}

// This drives the production parsers and their real first-output staging.
// The setting changes after each client write, including within one batched
// CommitTo call, so request/turn or parse-time snapshots fail this regression.
func TestCodexSafetyBufferingHTTPHotSwitchAfterStaging(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			t.Run(kind+"/passthrough="+strconvBool(passthrough), func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				saveSafetyBuffering(t, svc.settingService, true)
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind}
				rec := &safetyBufferingSwitchRecorder{ResponseRecorder: httptest.NewRecorder()}
				rec.afterWrite = func(p []byte) {
					if bytes.Contains(p, []byte(`"marker":"A"`)) {
						saveSafetyBuffering(t, svc.settingService, false)
					}
					if bytes.Contains(p, []byte(`"marker":"B"`)) {
						saveSafetyBuffering(t, svc.settingService, true)
					}
				}
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				events := []string{`{"type":"response.created","response":{"id":"resp_hot","model":"fixture"}}`, safetyBufferingNotice("A"), safetyBufferingNotice("B"), safetyBufferingNotice("C"), `{"type":"response.output_text.delta","delta":"answer"}`, `{"type":"response.completed","response":{"id":"resp_hot","model":"fixture","status":"completed","usage":{"input_tokens":12,"output_tokens":3}}}`}
				var upstream strings.Builder
				for _, event := range events {
					upstream.WriteString("data: " + event + "\n\n")
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(upstream.String()))}
				if passthrough {
					result, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					require.NoError(t, err)
					require.Equal(t, 12, result.usage.InputTokens)
				} else {
					result, err := svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					require.NoError(t, err)
					require.Equal(t, 12, result.usage.InputTokens)
				}
				seen := map[string]bool{}
				for _, line := range strings.Split(rec.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					payload := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
					marker := gjson.GetBytes(payload, "marker").String()
					if marker == "" {
						continue
					}
					seen[marker] = true
					require.Equal(t, marker == "B", gjson.GetBytes(payload, "metadata.type").Exists(), marker)
					require.True(t, gjson.GetBytes(payload, "metadata.guardian.requires_review").Bool())
				}
				require.Len(t, seen, 3, rec.Body.String())
				require.Contains(t, rec.Body.String(), `"delta":"answer"`)
				require.Contains(t, rec.Body.String(), `"type":"response.completed"`)
			})
		}
	}
}

func strconvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestCodexSafetyBufferingSSEFragmentationAndToggleBeforeCommit(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		svc, _ := safetyBufferingTestService(t)
		c, rec := newSafetyBufferingContext(t)
		finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
		c.Header("Content-Type", "text/event-stream")
		if newline == "\r\n" {
			c.Header("Content-Type", "Text/Event-Stream; charset=utf-8")
		}
		// A multi-line event already buffered while OFF must be projected if the
		// setting becomes ON before the event's final empty line is written.
		prefix := "id: retain" + newline + "event: response.metadata" + newline + "data: {\"type\":\"response.metadata\"," + newline + "data: \"metadata\":{\"type\":\"safety_buffering\",\"use_cases\":[],\"reasons\":[]},\"future\":true}" + newline
		for _, b := range []byte(prefix) {
			_, err := c.Writer.Write([]byte{b})
			require.NoError(t, err)
		}
		require.Empty(t, rec.Body.String())
		saveSafetyBuffering(t, svc.settingService, true)
		_, err := c.Writer.WriteString(newline)
		require.NoError(t, err)
		finish()
		require.NotContains(t, rec.Body.String(), "safety_buffering")
		require.Contains(t, rec.Body.String(), "id: retain"+newline)
		require.Contains(t, rec.Body.String(), `"future":true`)
	}
}

func newSafetyBufferingContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func TestCodexSafetyBufferingDisabledAndOtherPlatformsPreserveBytes(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic} {
		svc, _ := safetyBufferingTestService(t)
		if platform != PlatformOpenAI {
			saveSafetyBuffering(t, svc.settingService, true)
		}
		c, rec := newSafetyBufferingContext(t)
		finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: platform})
		c.Header("Content-Type", "text/event-stream")
		wire := "event: response.metadata\r\ndata: " + safetyBufferingNotice("raw") + "\r\n\r\n: keepalive\n\n"
		for _, b := range []byte(wire) {
			_, err := c.Writer.Write([]byte{b})
			require.NoError(t, err)
		}
		finish()
		require.Equal(t, wire, rec.Body.String())
	}
}

func TestCodexSafetyBufferingHeadersErrorsAndEOFArePreserved(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	saveSafetyBuffering(t, svc.settingService, true)
	c, rec := newSafetyBufferingContext(t)
	finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
	c.Header("Content-Type", "text/event-stream")
	c.Header("X-Codex-Safety-Buffering-Enabled", "true")
	c.Header("X-Codex-Safety-Buffering-Faster-Model", "fast")
	c.Header("X-Request-Id", "retain")
	_, err := c.Writer.WriteString("data: " + safetyBufferingNotice("eof"))
	require.NoError(t, err)
	finish()
	require.NotContains(t, rec.Body.String(), "safety_buffering")
	require.Empty(t, rec.Result().Header.Get("X-Codex-Safety-Buffering-Enabled"))
	require.Empty(t, rec.Result().Header.Get("X-Codex-Safety-Buffering-Faster-Model"))
	require.Equal(t, "retain", rec.Result().Header.Get("X-Request-Id"))
	// Invalid/unrecognized data must not turn into a new policy failure.
	_, err = c.Writer.WriteString("\n\ndata: {\"safety_buffering\":invalid}\n\n")
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `{"safety_buffering":invalid}`)
}

func TestCodexSafetyBufferingJSONFragmentsUseSettingAtCommit(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconvBool(enabled), func(t *testing.T) {
			svc, _ := safetyBufferingTestService(t)
			saveSafetyBuffering(t, svc.settingService, !enabled)
			c, rec := newSafetyBufferingContext(t)
			original := c.Writer
			finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
			c.Header("Content-Type", "application/json")
			originalSize := strconv.Itoa(len(`{"safety_buffering":{"reasons":["user_risk"]},"status":"completed"}`))
			c.Header("Content-Length", originalSize)
			_, err := c.Writer.WriteString(`{"safety_buffering":{"reasons":["user_risk"]},`)
			require.NoError(t, err)
			require.Empty(t, rec.Body.String())
			saveSafetyBuffering(t, svc.settingService, enabled)
			_, err = c.Writer.WriteString(`"status":"completed"}`)
			require.NoError(t, err)
			finish()
			require.Same(t, original, c.Writer)
			if enabled {
				require.JSONEq(t, `{"status":"completed"}`, rec.Body.String())
				require.Empty(t, rec.Result().Header.Get("Content-Length"))
			} else {
				require.JSONEq(t, `{"safety_buffering":{"reasons":["user_risk"]},"status":"completed"}`, rec.Body.String())
				require.Equal(t, originalSize, rec.Result().Header.Get("Content-Length"))
			}
		})
	}
}

func TestCodexSafetyBufferingHTTPWriterDoesNotLeakAcrossAttempts(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	saveSafetyBuffering(t, svc.settingService, true)
	c, rec := newSafetyBufferingContext(t)
	finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
	finish() // An attempt returning before output must not own a later account.
	finish = svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformAnthropic})
	c.Header("Content-Type", "application/json")
	payload := safetyBufferingNotice("other-provider")
	_, err := c.Writer.WriteString(payload)
	require.NoError(t, err)
	finish()
	require.Equal(t, payload, rec.Body.String())
}

func TestCodexSafetyBufferingLargeSSEPreservesContentAndFollowingEvents(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	saveSafetyBuffering(t, svc.settingService, true)
	c, rec := newSafetyBufferingContext(t)
	finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
	c.Header("Content-Type", "text/event-stream")
	// Business requirement: accepted events above 4 MiB still hide the notice,
	// preserve unrelated content, and do not disrupt following events/keepalives.
	// This replaces the superseded expectation that large notices pass raw.
	large := "data: {\"safety_buffering\":{},\"opaque\":\"" + strings.Repeat("x", 4*1024*1024) + "\"}\r\n\r\n"
	for start := 0; start < len(large); start += 65536 {
		_, err := c.Writer.WriteString(large[start:min(start+65536, len(large))])
		require.NoError(t, err)
	}
	_, err := c.Writer.WriteString("data: " + safetyBufferingNotice("after-limit") + "\n\n: keepalive\n\n")
	require.NoError(t, err)
	finish()
	output := rec.Body.String()
	require.NotContains(t, output, "safety_buffering")
	first := strings.TrimPrefix(strings.Split(output, "\r\n\r\n")[0], "data: ")
	require.Equal(t, strings.Repeat("x", 4*1024*1024), gjson.Get(first, "opaque").String())
	require.Contains(t, output, `"marker":"after-limit"`)
	require.Contains(t, output, ": keepalive\n\n")
}

func TestCodexSafetyBufferingHeadersUseSettingAtFirstCommit(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		svc, _ := safetyBufferingTestService(t)
		saveSafetyBuffering(t, svc.settingService, !enabled)
		c, rec := newSafetyBufferingContext(t)
		finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
		c.Header("X-Codex-Safety-Buffering-Enabled", "true")
		saveSafetyBuffering(t, svc.settingService, enabled)
		c.Writer.WriteHeaderNow()
		saveSafetyBuffering(t, svc.settingService, !enabled)
		c.Writer.Flush()
		finish()
		require.Equal(t, !enabled, rec.Result().Header.Get("X-Codex-Safety-Buffering-Enabled") != "", "committed headers cannot be recalled or restored")
	}
}

func TestCodexSafetyBufferingFailureDiagnosticDoesNotExposePayload(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	saveSafetyBuffering(t, svc.settingService, true)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		defer svc.beginCodexSafetyBufferingHTTPOutput(c.Request.Context(), c, &Account{Platform: PlatformOpenAI})()
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString("data: {\"safety_buffering\":invalid,\"private-secret\":true}\n\n")
		require.NoError(t, err)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set("X-Request-ID", "forged-request")
	req = req.WithContext(context.WithValue(t.Context(), ctxkey.RequestID, "trusted-request"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Contains(t, rec.Body.String(), "private-secret", "projection failure must pass through without a new policy response")
	record := logs.String()
	require.Equal(t, "trusted-request", gjson.Get(record, "request_id").String())
	require.Equal(t, "/v1/responses", gjson.Get(record, "endpoint").String())
	require.Equal(t, "sse", gjson.Get(record, "protocol").String())
	require.Equal(t, "downstream_output", gjson.Get(record, "stage").String())
	require.Equal(t, "codex_safety_buffering_projection_failed", gjson.Get(record, "code").String())
	require.Positive(t, gjson.Get(record, "input_bytes").Int())
	require.NotContains(t, record, "private-secret")
	require.NotContains(t, record, "forged-request")
}

// The original streaming contracts require EOF residuals to reach the existing
// flush, and downstream write failures to drain upstream usage. Exercise a real
// settings service in both states so the no-settings bypass cannot prove this.
func TestCodexSafetyBufferingEOFFlushAndDisconnectRetainUsage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, disconnected := range []bool{false, true} {
				t.Run("passthrough="+strconvBool(passthrough)+"/enabled="+strconvBool(enabled)+"/disconnect="+strconvBool(disconnected), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveSafetyBuffering(t, svc.settingService, enabled)
					rec := newOpenAIResponseFlushRecorder()
					if disconnected {
						rec.failAfterWrites = 0
					}
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					wire := "data: " + safetyBufferingNotice("eof") + "\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_eof\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n"
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
					account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
					var err error
					if passthrough {
						var result *openaiStreamingResultPassthrough
						result, err = svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NotNil(t, result)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					} else {
						var result *openaiStreamingResult
						result, err = svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NotNil(t, result)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					}
					body, flushes := rec.snapshot()
					if disconnected {
						require.ErrorContains(t, err, "client disconnected")
						require.Empty(t, body)
						require.Empty(t, flushes)
					} else {
						require.NoError(t, err)
						require.NotEmpty(t, flushes)
						require.Equal(t, body, flushes[len(flushes)-1], "the final flush must include the EOF residual")
						require.Contains(t, body, `"type":"response.completed"`)
						require.Equal(t, !enabled, strings.Contains(body, "safety_buffering"))
					}
				})
			}
		}
	}
}

type safetyBufferingTerminalWriteFailureRecorder struct {
	*httptest.ResponseRecorder
}

func (r *safetyBufferingTerminalWriteFailureRecorder) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"type":"response.completed"`)) {
		return 0, io.ErrClosedPipe
	}
	return r.ResponseRecorder.Write(p)
}

// A completed event received at EOF without a final empty line still has to
// reach the client before the gateway can report successful delivery. A failure
// only on that final write must retain usage and report a client disconnect.
func TestCodexSafetyBufferingEOFWriteFailureIsNotSuccess(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(kind+"/passthrough="+strconvBool(passthrough)+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveSafetyBuffering(t, svc.settingService, enabled)
					rec := &safetyBufferingTerminalWriteFailureRecorder{httptest.NewRecorder()}
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"safety_buffering\":{},\"response\":{\"id\":\"resp_eof\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n"
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
					account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind}
					if passthrough {
						result, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.ErrorContains(t, err, "client disconnected")
						require.NotNil(t, result)
						require.True(t, result.clientDisconnect)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					} else {
						result, err := svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.ErrorContains(t, err, "client disconnected")
						require.NotNil(t, result)
						require.True(t, result.clientDisconnect)
						require.Equal(t, 12, result.usage.InputTokens)
						require.Equal(t, 3, result.usage.OutputTokens)
					}
					require.Contains(t, rec.Body.String(), `"delta":"answer"`)
					require.NotContains(t, rec.Body.String(), `"type":"response.completed"`)
				})
			}
		}
	}
}
