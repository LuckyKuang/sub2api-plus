//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Business acceptance: remove the complete target notification and its UI,
// including empty placeholders, while keeping independent events and usage.
// Fixtures follow the reviewed Responses carriers linked in the protocol docs.
const pureSafetyBufferingNotice = `{"type":"response.metadata","sequence_number":2,"metadata":{"type":"safety_buffering","use_cases":["cyber"],"reasons":["user_risk"],"retry_model":"gpt-6.1-sol"}}`

func TestCodexSafetyBufferingBusinessPureAndMixedBoundaries(t *testing.T) {
	for _, input := range []string{
		pureSafetyBufferingNotice,
		`{"type":"response.metadata","safety_buffering":{"use_cases":["cyber"],"reasons":["user_risk"]}}`,
		`{"type":"response.metadata","headers":{"X-Codex-Safety-Buffering-Enabled":"true","X-Codex-Safety-Buffering-Faster-Model":"fast"}}`,
		`{"type":"response.metadata","sequence_number":0,"safety_buffering":{},"metadata":{"type":"safety_buffering","use_cases":[],"reasons":[]},"headers":{}}`,
	} {
		got, err := stripCodexSafetyBuffering([]byte(input))
		require.NoError(t, err)
		require.Nil(t, got, "pure notices must produce no event")
	}
	// Presence of any unknown sibling, including null or an empty object,
	// prevents dropping the event. Lifecycle events are never pure UI notices.
	for _, sibling := range []string{`"future":null`, `"future":{}`, `"sequence_number":"opaque"`, `"metadata":{"guardian":{"requires_review":true}}`, `"headers":{"x-request-id":"retain"}`, `"metadata":null`} {
		input := `{"type":"response.metadata","safety_buffering":{},` + sibling + `}`
		got, err := stripCodexSafetyBuffering([]byte(input))
		require.NoError(t, err)
		require.JSONEq(t, `{"type":"response.metadata",`+sibling+`}`, string(got))
	}
	for _, kind := range []string{"response.created", "response.in_progress", "response.completed", "response.failed", "error"} {
		got, err := stripCodexSafetyBuffering([]byte(`{"type":"` + kind + `","safety_buffering":{}}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"type":"`+kind+`"}`, string(got))
	}
}

// A received response must never be released as raw fallback, or retain its
// anonymous spool, if temporary storage cannot be read at final delivery.
func TestCodexSafetyBufferingBusinessUnreadableStageClosesWithoutFallback(t *testing.T) {
	svc, _ := safetyBufferingTestService(t)
	saveSafetyBuffering(t, svc.settingService, true)
	c, rec := newSafetyBufferingContext(t)
	finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
	defer finish()
	c.Header("Content-Type", "application/json")
	_, err := c.Writer.WriteString(`{"safety_buffering":{},"opaque":"`)
	require.NoError(t, err)
	writer := c.Writer.(*codexSafetyBufferingHTTPWriter)
	stage := writer.pending
	var spool *os.File
	stage.createTemp = func() (*os.File, error) {
		file, err := os.CreateTemp(t.TempDir(), "unreadable-stage-*")
		if err != nil {
			return nil, err
		}
		name := file.Name()
		if err := file.Close(); err != nil {
			return nil, err
		}
		spool, err = os.OpenFile(name, os.O_WRONLY, 0600)
		return spool, err
	}
	_, err = c.Writer.WriteString(strings.Repeat("x", 128*1024) + `"}`)
	require.Error(t, err, "an unreadable accepted response must fail delivery")
	require.NotNil(t, spool)
	_, err = spool.WriteString("probe")
	require.ErrorIs(t, err, os.ErrClosed, "release temporary resources when delivery fails")
	require.Empty(t, rec.Body.String(), "no original or partial response may escape")
}

func TestCodexSafetyBufferingBusinessPureNoticeHotSwitchAfterStaging(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			t.Run(kind+"/passthrough="+strconvBool(passthrough), func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				saveSafetyBuffering(t, svc.settingService, true)
				rec := &safetyBufferingSwitchRecorder{ResponseRecorder: httptest.NewRecorder()}
				rec.afterWrite = func(p []byte) {
					if bytes.Contains(p, []byte(`"checkpoint":"disable"`)) {
						saveSafetyBuffering(t, svc.settingService, false)
					}
					if bytes.Contains(p, []byte(`"checkpoint":"enable"`)) {
						saveSafetyBuffering(t, svc.settingService, true)
					}
				}
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				events := []string{pureSafetyBufferingNotice, `{"type":"response.metadata","checkpoint":"disable"}`, pureSafetyBufferingNotice, `{"type":"response.metadata","checkpoint":"enable"}`, pureSafetyBufferingNotice, `{"type":"response.output_text.delta","delta":"answer"}`, `{"type":"response.completed","response":{"id":"resp_switch","status":"completed","usage":{"input_tokens":12,"output_tokens":3}}}`}
				wire := "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind}
				if passthrough {
					result, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					require.NoError(t, err)
					require.Equal(t, 12, result.usage.InputTokens)
				} else {
					result, err := svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					require.NoError(t, err)
					require.Equal(t, 12, result.usage.InputTokens)
				}
				output := rec.Body.String()
				require.Equal(t, 1, strings.Count(output, pureSafetyBufferingNotice), "only the notice released while OFF survives")
				require.Equal(t, 3, strings.Count(output, `"type":"response.metadata"`), "two independent checkpoints and the OFF notice; no empty envelopes")
				require.Contains(t, output, `"delta":"answer"`)
				require.Contains(t, output, `"type":"response.completed"`)
			})
		}
	}
}

func TestCodexSafetyBufferingBusinessDropKeepsOnlyIndependentComments(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		svc, _ := safetyBufferingTestService(t)
		saveSafetyBuffering(t, svc.settingService, true)
		c, rec := newSafetyBufferingContext(t)
		finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString(": keepalive" + newline + "id: hidden" + newline + "event: response.metadata" + newline + "data: " + pureSafetyBufferingNotice + newline + newline)
		require.NoError(t, err)
		finish()
		require.Equal(t, ": keepalive"+newline+newline, rec.Body.String())
	}
}

func TestCodexSafetyBufferingBusinessStageFailureCannotReleaseRawNotice(t *testing.T) {
	for _, contentType := range []string{"text/event-stream", "application/json"} {
		svc, _ := safetyBufferingTestService(t)
		saveSafetyBuffering(t, svc.settingService, true)
		c, rec := newSafetyBufferingContext(t)
		finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
		c.Header("Content-Type", contentType)
		prefix := `{"safety_buffering":{},"opaque":"`
		if contentType == "text/event-stream" {
			prefix = "data: " + prefix
		}
		_, err := c.Writer.WriteString(prefix)
		require.NoError(t, err)
		writer := c.Writer.(*codexSafetyBufferingHTTPWriter)
		writer.pending.createTemp = func() (*os.File, error) { return nil, errors.New("temporary storage unavailable") }
		_, err = c.Writer.WriteString(strings.Repeat("x", 128*1024))
		require.Error(t, err, "transport must receive staging failure")
		finish()
		require.Empty(t, rec.Body.String(), "failure must not release staged plaintext as a fallback")
	}
}

func TestCodexSafetyBufferingBusinessPureNoticeHasNoPlaceholder(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				t.Run(kind+"/passthrough="+strconvBool(passthrough)+"/enabled="+strconvBool(enabled), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveSafetyBuffering(t, svc.settingService, enabled)
					c, rec := newSafetyBufferingContext(t)
					wire := "event: response.metadata\ndata: " + pureSafetyBufferingNotice + "\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_business\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n"
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
					account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind}
					if passthrough {
						result, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NoError(t, err)
						require.Equal(t, 12, result.usage.InputTokens)
					} else {
						result, err := svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NoError(t, err)
						require.Equal(t, 12, result.usage.InputTokens)
					}
					if enabled {
						require.NotContains(t, rec.Body.String(), "response.metadata", "a hidden pure notification must not leave an empty event")
						require.NotContains(t, rec.Body.String(), "safety_buffering")
					} else {
						require.Contains(t, rec.Body.String(), pureSafetyBufferingNotice)
					}
					require.Contains(t, rec.Body.String(), `"delta":"answer"`)
					require.Contains(t, rec.Body.String(), `"type":"response.completed"`)
				})
			}
		}
	}
}

func TestCodexSafetyBufferingBusinessLargeAcceptedEventCannotLeak(t *testing.T) {
	// This valid 5 MiB event is within the gateway's existing accepted limits.
	// Its unrelated value must survive; an added 4 MiB display-filter threshold
	// must not silently leak the target notification.
	opaque := strings.Repeat("x", 5*1024*1024)
	for _, enabled := range []bool{false, true} {
		for _, contentType := range []string{"text/event-stream", "application/json"} {
			t.Run(contentType+"/enabled="+strconvBool(enabled), func(t *testing.T) {
				svc, _ := safetyBufferingTestService(t)
				saveSafetyBuffering(t, svc.settingService, enabled)
				c, rec := newSafetyBufferingContext(t)
				finish := svc.beginCodexSafetyBufferingHTTPOutput(t.Context(), c, &Account{Platform: PlatformOpenAI})
				c.Header("Content-Type", contentType)
				payload := `{"type":"response.metadata","safety_buffering":{},"metadata":{"type":"safety_buffering","reasons":["user_risk"]},"opaque":"` + opaque + `"}`
				wire := payload
				if contentType == "text/event-stream" {
					wire = "data: " + payload + "\n\n"
				}
				for start := 0; start < len(wire); start += 32768 {
					_, err := c.Writer.WriteString(wire[start:min(start+32768, len(wire))])
					require.NoError(t, err)
				}
				finish()
				if !enabled {
					require.Equal(t, wire, rec.Body.String(), "OFF must preserve even large fragmented events byte for byte")
					return
				}
				body := strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "data: "))
				require.False(t, gjson.Get(body, "safety_buffering").Exists())
				require.False(t, gjson.Get(body, "metadata.type").Exists())
				require.Equal(t, opaque, gjson.Get(body, "opaque").String())
			})
		}
	}
}

func TestCodexSafetyBufferingBusinessWSPureNoticeHasNoEmptyFrame(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
			svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
			_, repo := safetyBufferingTestService(t)
			svc.settingService = NewSettingService(repo, svc.cfg)
			saveSafetyBuffering(t, svc.settingService, true)
			account := passthroughLifecycleAccount()
			account.Type = kind
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
			if kind == AccountTypeOAuth {
				account.Credentials = map[string]any{"access_token": "sk-test", "chatgpt_account_id": "fixture-workspace"}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			server, finished := startPassthroughLifecycleServer(t, ctx, svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			upstream.Send(pureSafetyBufferingNotice)
			upstream.frames <- stagedPassthroughFrame{messageType: coderws.MessageBinary, payload: []byte(pureSafetyBufferingNotice)}
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_hidden","usage":{"input_tokens":12,"output_tokens":3}}}`)
			readCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			_, payload, err := client.Read(readCtx)
			require.NoError(t, err)
			require.Equal(t, "response.completed", gjson.GetBytes(payload, "type").String(), "the first delivered frame must be the real terminal, with no placeholder")
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("the completed session was not released")
			}
		})
	}
}
