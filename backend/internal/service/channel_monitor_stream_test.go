//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonitorStreamProtocolContract(t *testing.T) {
	const chatText = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"42\"}}]}\n\n"
	const chatEnd = "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	const responseText = "event: response.output_text.delta\ndata: {\"output_index\":0,\"content_index\":0,\"delta\":\"42\"}\n\n"
	const responseEnd = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	for _, tc := range []struct {
		name, mode, wire, text, err string
		timed                       bool
	}{
		{"chat", "chat_completions", ": heartbeat\r\n\r\ndata:  \n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"thinking\"}}]}\n\n" + chatText + chatEnd, "42", "", true},
		{"responses", "responses", "data: {\"type\":\"response.created\"}\n\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n\n" + responseText + responseEnd, "42", "", true},
		{"completion without disconnect", "responses", responseText + responseEnd + "not JSON", "42", "", true},
		{"aggregate has no delta clock", "responses", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"42\"}]}]}}\n\n", "42", "", false},
		{"tools no text", "chat_completions", "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{}]}}]}\n\n" + chatEnd, "", "", false},
		{"whitespace no clock", "chat_completions", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" \\n\"}}]}\n\n" + chatEnd, " \n", "", false},
		{"other choices ignored", "chat_completions", "data: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"wrong\"}}]}\n\n" + chatText + chatEnd, "42", "", true},
		{"correct answer disconnected", "chat_completions", chatText, "42", "stream_incomplete", true},
		{"DONE alone insufficient", "chat_completions", chatText + "data: [DONE]\n\n", "42", "stream_incomplete", true},
		{"responses DONE insufficient", "responses", responseText + "data: [DONE]\n\n", "42", "stream_incomplete", true},
		{"length limited", "chat_completions", chatText + "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"length\"}]}\n\n", "42", "stream_generation_incomplete", true},
		{"response incomplete", "responses", responseText + "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n", "42", "stream_generation_incomplete", true},
		{"response failed", "responses", responseText + "data: {\"type\":\"response.failed\"}\n\n", "42", "stream_upstream_error", true},
		{"invalid delta", "responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":{}}\n\n", "", "stream_parse_error", false},
		{"invalid JSON", "responses", "data: {broken}\n\n", "", "stream_parse_error", false},
		{"unknown events", "responses", "data: {\"type\":\"future.event\",\"delta\":\"not text\"}\n\n" + responseText + responseEnd, "42", "", true},
		{"byte cap including comments", "responses", ":" + strings.Repeat("x", monitorResponseMaxBytes) + "\n\n" + responseEnd, "", "stream_too_large", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Unix(100, 0)
			calls := 0
			got, err := readMonitorSSE(iotest.OneByteReader(strings.NewReader(tc.wire)), tc.mode, start, func() time.Time { calls++; return start.Add(37 * time.Millisecond) })
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
			require.Equal(t, tc.text, got.Text)
			if tc.timed {
				require.NotNil(t, got.FirstTextMs)
				require.Equal(t, 37, *got.FirstTextMs)
				require.Equal(t, 1, calls)
			} else {
				require.Nil(t, got.FirstTextMs)
				require.Zero(t, calls)
			}
		})
	}
}

func TestMonitorStreamOrderingAndZero(t *testing.T) {
	wire := "data: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"content_index\":0,\"delta\":\"2\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"4\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"42\"}}\n\n"
	start := time.Now()
	got, err := readMonitorSSE(strings.NewReader(wire), MonitorAPIModeResponses, start, func() time.Time { return start })
	require.NoError(t, err)
	require.Equal(t, "42", got.Text)
	require.NotNil(t, got.FirstTextMs)
	require.Zero(t, *got.FirstTextMs)
}

func TestMonitorStreamSwitchOwnsBody(t *testing.T) {
	for _, mode := range []string{"off", "merge", "replace"} {
		for _, stream := range []bool{false, true} {
			opts := &CheckOptions{StreamProbe: stream, BodyOverrideMode: mode, BodyOverride: map[string]any{"model": "test", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": !stream, "stream_options": map[string]any{"include_usage": true}}}
			body, err := buildRequestBody(providerOpenAIChatAdapter, MonitorProviderOpenAI, MonitorAPIModeChatCompletions, "test", "hi", opts)
			require.NoError(t, err)
			var value map[string]any
			require.NoError(t, json.Unmarshal(body, &value))
			require.Equal(t, stream, value["stream"])
			require.Equal(t, !stream, opts.BodyOverride["stream"], "must not mutate shared template snapshot")
			if !stream {
				require.NotContains(t, value, "stream_options")
			}
		}
	}
	for _, provider := range []string{MonitorProviderAnthropic, MonitorProviderGemini, MonitorProviderAntigravity} {
		require.ErrorIs(t, validateMonitorStream(provider, "probe", true, "off", nil), ErrChannelMonitorStreamUnsupported)
	}
	require.ErrorIs(t, validateMonitorStream("openai", "quota", true, "off", nil), ErrChannelMonitorStreamUnsupported)
}

func TestChannelMonitorStreamIncrementalAndCompletion(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, complete := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if json.Unmarshal(body, &payload) != nil || payload["stream"] != true || r.Header.Get("Accept") != "text/event-stream" {
				t.Error("stream request contract missing")
			}
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			_, _ = io.WriteString(w, ": ping\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(80 * time.Millisecond)
			if complete {
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}
		}))
		result := runCheckForModel(context.Background(), "openai", server.URL, "test", "test", &CheckOptions{StreamProbe: true, BodyOverrideMode: "replace", BodyOverride: map[string]any{"model": "test", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
		server.Close()
		if complete {
			require.Equal(t, MonitorStatusOperational, result.Status)
		} else {
			require.Equal(t, MonitorStatusError, result.Status)
		}
		require.NotNil(t, result.FirstByteMs)
		require.NotNil(t, result.FirstTextMs)
		require.NotNil(t, result.LatencyMs)
		require.GreaterOrEqual(t, *result.FirstTextMs-*result.FirstByteMs, 30)
		require.GreaterOrEqual(t, *result.LatencyMs-*result.FirstTextMs, 40)
		require.NotNil(t, result.StreamProbe)
		require.True(t, *result.StreamProbe)
	}
}

func TestMonitorStreamReadFailureAndContentType(t *testing.T) {
	_, err := readMonitorSSE(iotest.ErrReader(errors.New("broken")), "responses", time.Now(), time.Now)
	require.ErrorContains(t, err, "stream_read_error")
	_, err = consumeMonitorStream(&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"output_text":"42"}`))}, "responses", time.Now())
	require.ErrorContains(t, err, "stream_expected")
}

func TestChannelMonitorStreamValidatesWholeChallengeAnswer(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, mode := range []string{"chat_completions", "responses"} {
		for _, bodyMode := range []string{"off", "merge"} {
			for _, correct := range []bool{true, false} {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					prompt, _ := body["input"].(string)
					if mode == "chat_completions" {
						prompt = body["messages"].([]any)[0].(map[string]any)["content"].(string)
					}
					var a, b int
					var op string
					if _, err := fmt.Sscanf(prompt[strings.LastIndex(prompt, "Q:"):], "Q: %d %s %d = ?", &a, &op, &b); err != nil {
						t.Error(err)
						return
					}
					answer := a + b
					if op == "-" {
						answer = a - b
					}
					if !correct {
						answer = 1000000
					}
					w.Header().Set("Content-Type", "text/event-stream")
					for _, digit := range strconv.Itoa(answer) {
						if mode == "responses" {
							fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":%q,\"output_index\":0,\"content_index\":0}\n\n", string(digit))
						} else {
							fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", string(digit))
						}
						w.(http.Flusher).Flush()
					}
					if mode == "responses" {
						io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
					} else {
						io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					}
				}))
				result := runCheckForModel(context.Background(), "openai", server.URL, "test", "test", &CheckOptions{APIMode: mode, BodyOverrideMode: bodyMode, StreamProbe: true})
				server.Close()
				require.NotNil(t, result.FirstTextMs)
				if correct {
					require.Equal(t, MonitorStatusOperational, result.Status)
				} else {
					require.Equal(t, MonitorStatusFailed, result.Status)
				}
			}
		}
	}
}
