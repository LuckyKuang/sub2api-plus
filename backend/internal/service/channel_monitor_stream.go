package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

var errMonitorGenerationIncomplete = errors.New("stream_generation_incomplete")

var ErrChannelMonitorStreamUnsupported = infraerrors.BadRequest(
	"CHANNEL_MONITOR_STREAM_UNSUPPORTED", "streaming probes require an OpenAI-compatible text protocol and probe or quota_probe mode",
)

type monitorProviderResponse struct {
	Text        string
	RawBody     string
	StatusCode  int
	FirstTextMs *int
	BytesRead   int64
}

func monitorOverrideBody(opts *CheckOptions) map[string]any {
	if opts == nil {
		return nil
	}
	return opts.BodyOverride
}

func validateMonitorStream(provider, checkMode string, stream bool, bodyMode string, body map[string]any) error {
	if stream && (checkMode == MonitorCheckModeQuota || !isOpenAICompatibleChatProvider(provider)) {
		return ErrChannelMonitorStreamUnsupported
	}
	// Native protocols have no streaming reader yet. Do not let an override
	// silently send SSE into the JSON reader, including existing stored configs.
	if !isOpenAICompatibleChatProvider(provider) && defaultBodyMode(bodyMode) != MonitorBodyOverrideModeOff {
		if value, exists := body["stream"]; exists && value != false && value != nil {
			return ErrChannelMonitorStreamUnsupported
		}
	}
	return nil
}

func monitorIsSSE(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "text/event-stream"
}

func consumeMonitorStream(resp *http.Response, apiMode string, start time.Time) (monitorProviderResponse, error) {
	if !monitorIsSSE(resp.Header.Get("Content-Type")) {
		return monitorProviderResponse{StatusCode: resp.StatusCode}, fmt.Errorf("stream_expected: upstream did not return text/event-stream")
	}
	result, err := readMonitorSSE(resp.Body, apiMode, start, time.Now)
	result.StatusCode = resp.StatusCode
	return result, err
}

// readMonitorSSE observes complete events while the response is still arriving.
// All bytes (including comments/unknown frames) share the bounded read budget.
// A complete terminal frame ends the logical response; EOF alone does not.
func readMonitorSSE(reader io.Reader, apiMode string, start time.Time, now func() time.Time) (result monitorProviderResponse, err error) {
	limited := &io.LimitedReader{R: reader, N: monitorResponseMaxBytes + 1}
	defer func() { result.BytesRead = monitorResponseMaxBytes + 1 - limited.N }()
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), monitorResponseMaxBytes+1)
	var frames openAICompatSSEFrameParser
	state := monitorStreamState{apiMode: apiMode, start: start, now: now, parts: make(map[monitorTextPart]string)}
	for scanner.Scan() {
		if limited.N == 0 {
			return state.result(), fmt.Errorf("stream_too_large: response exceeded byte limit")
		}
		frame, ok := frames.AddLine(scanner.Text())
		if !ok {
			continue
		}
		done, err := state.consume(frame)
		if err != nil || done {
			return state.result(), err
		}
	}
	if limited.N == 0 || errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return state.result(), fmt.Errorf("stream_too_large: response exceeded byte limit")
	}
	if err := scanner.Err(); err != nil {
		return state.result(), fmt.Errorf("stream_read_error: %w", err)
	}
	if frame, ok := frames.Finish(); ok {
		done, err := state.consume(frame)
		if err != nil || done {
			return state.result(), err
		}
	}
	return state.result(), fmt.Errorf("stream_incomplete: upstream closed before normal completion")
}

type monitorTextPart struct{ output, content int64 }

type monitorStreamState struct {
	apiMode      string
	start        time.Time
	now          func() time.Time
	firstTextMs  *int
	parts        map[monitorTextPart]string
	terminalText string
	chatFinished bool
}

func (s *monitorStreamState) appendText(key monitorTextPart, delta string) {
	if s.firstTextMs == nil && strings.TrimSpace(delta) != "" {
		ms := max(0, int(s.now().Sub(s.start).Milliseconds()))
		s.firstTextMs = &ms
	}
	s.parts[key] += delta
}

func (s *monitorStreamState) result() monitorProviderResponse {
	keys := make([]monitorTextPart, 0, len(s.parts))
	for key := range s.parts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].output == keys[j].output {
			return keys[i].content < keys[j].content
		}
		return keys[i].output < keys[j].output
	})
	var text strings.Builder
	for _, key := range keys {
		_, _ = text.WriteString(s.parts[key])
	}
	value := text.String()
	if len(keys) == 0 {
		value = s.terminalText
	}
	return monitorProviderResponse{Text: value, FirstTextMs: s.firstTextMs}
}

func (s *monitorStreamState) consume(frame openAICompatSSEFrame) (bool, error) {
	data := strings.TrimSpace(frame.Data)
	if data == "" {
		return false, nil
	}
	if data == "[DONE]" {
		if s.apiMode == MonitorAPIModeResponses || !s.chatFinished {
			return false, fmt.Errorf("stream_incomplete: terminal success missing before DONE")
		}
		return true, nil
	}
	if !gjson.Valid(data) || !gjson.Parse(data).IsObject() {
		return false, fmt.Errorf("stream_parse_error: invalid event JSON")
	}
	value := gjson.Parse(data)
	if value.Get("error").Exists() && value.Get("error").Type != gjson.Null {
		return false, fmt.Errorf("stream_upstream_error: upstream error event")
	}
	if s.apiMode == MonitorAPIModeResponses {
		return s.consumeResponses(frame, value)
	}
	return s.consumeChat(value)
}

func (s *monitorStreamState) consumeChat(value gjson.Result) (bool, error) {
	for _, choice := range value.Get("choices").Array() {
		if choice.Get("index").Int() != 0 {
			continue
		}
		content := choice.Get("delta.content")
		if content.Type == gjson.String {
			s.appendText(monitorTextPart{}, content.String())
		} else if content.Exists() && content.Type != gjson.Null {
			return false, fmt.Errorf("stream_parse_error: invalid Chat text delta")
		}
		if reason := choice.Get("finish_reason").String(); reason != "" {
			if reason != "stop" {
				return false, errMonitorGenerationIncomplete
			}
			s.chatFinished = true
		}
	}
	return false, nil
}

func (s *monitorStreamState) consumeResponses(frame openAICompatSSEFrame, value gjson.Result) (bool, error) {
	eventType := effectiveOpenAISSEEventType([]byte(frame.Data), frame.EventType)
	switch eventType {
	case "response.output_text.delta":
		delta := value.Get("delta")
		if delta.Type != gjson.String {
			return false, fmt.Errorf("stream_parse_error: invalid Responses text delta")
		}
		s.appendText(monitorTextPart{value.Get("output_index").Int(), value.Get("content_index").Int()}, delta.String())
	case "response.completed":
		if value.Get("response.status").String() != "completed" {
			return false, fmt.Errorf("stream_incomplete: completed event missing successful response status")
		}
		s.terminalText = extractOpenAIResponsesText([]byte(value.Get("response").Raw))
		return true, nil
	case "response.incomplete", "response.cancelled", "response.canceled":
		return false, errMonitorGenerationIncomplete
	case "response.failed", "error":
		return false, fmt.Errorf("stream_upstream_error: upstream failed response")
	}
	return false, nil
}

// Only locally defined reason codes may enter logs, never an upstream error body.
func monitorStreamErrorReason(err error) string {
	for _, code := range []string{"stream_generation_incomplete", "stream_expected", "stream_too_large", "stream_read_error", "stream_incomplete", "stream_parse_error", "stream_upstream_error"} {
		if strings.HasPrefix(err.Error(), code) {
			return code
		}
	}
	return "stream_error"
}
