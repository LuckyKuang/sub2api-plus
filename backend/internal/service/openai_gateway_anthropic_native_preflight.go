package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
)

type nativeAnthropicReplayBody struct {
	io.Reader
	io.Closer
}

type nativeAnthropicPreflightKey struct{}

type nativeAnthropicRead struct {
	line []byte
	err  error
}

// A deadline can release the preflight while one read is still blocked. The
// downstream reader adopts that exact read before using the buffered reader,
// preserving byte order and avoiding concurrent reads or discarded prefetch.
type nativeAnthropicPendingRead struct {
	result <-chan nativeAnthropicRead
	buffer *bytes.Reader
	err    error
}

func (r *nativeAnthropicPendingRead) Read(p []byte) (int, error) {
	if r.buffer == nil {
		result := <-r.result
		r.buffer, r.err = bytes.NewReader(result.line), result.err
		if errors.Is(r.err, bufio.ErrBufferFull) {
			r.err = nil
		}
	}
	n, err := r.buffer.Read(p)
	if errors.Is(err, io.EOF) && r.err != nil {
		return n, r.err
	}
	return n, err
}

func withNativeAnthropicPreflight(ctx context.Context) context.Context {
	return context.WithValue(ctx, nativeAnthropicPreflightKey{}, true)
}

// Hold structural SSE frames until the first meaningful output. A provider may
// return HTTP 200 followed by an authentication/quota error; exposing its initial
// message_start would otherwise prevent safe account recovery. Once output,
// unknown content or the bounded prefix is reached, the original bytes resume
// unchanged and no subsequent error can authorize replay.
func (s *OpenAIGatewayService) prepareNativeAnthropicResponse(ctx context.Context, resp *http.Response) error {
	if resp == nil || resp.Body == nil || resp.StatusCode >= 400 ||
		!strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return nil
	}
	original := resp.Body
	reader := bufio.NewReaderSize(original, 4096)
	var prefix bytes.Buffer
	var data []string
	eventType := ""
	const maxPrefix = 64 * 1024

	interval := anthropicNativeStreamInterval(s.cfg)
	var timer *time.Timer
	var idle <-chan time.Time
	if interval > 0 {
		timer = time.NewTimer(interval)
		idle = timer.C
		defer timer.Stop()
	}
	// Do not hide healthy long-thinking streams and their keepalives. After
	// five seconds (or the configured shorter keepalive interval), expose the
	// original prefix and relinquish permission to switch accounts.
	window := 5 * time.Second
	if s.cfg != nil && s.cfg.Gateway.StreamKeepaliveInterval > 0 {
		if configured := time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second; configured < window {
			window = configured
		}
	}
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	replay := func() {
		resp.Body = &nativeAnthropicReplayBody{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader), Closer: original}
	}
	inspect := func() bool {
		if len(data) == 0 {
			return false
		}
		payload := []byte(strings.Join(data, "\n"))
		var event apicompat.AnthropicStreamEvent
		if json.Unmarshal(payload, &event) != nil {
			return true
		}
		if event.Type == "" {
			event.Type = eventType
		}
		if event.Type == "error" {
			var envelope struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if json.Unmarshal(payload, &envelope) != nil {
				return true
			}
			status := nativeAnthropicErrorStatus(envelope.Error.Type)
			if status == 0 {
				return true
			}
			_ = original.Close()
			resp.StatusCode = status
			resp.Status = http.StatusText(status)
			resp.Header.Set("Content-Type", "application/json")
			resp.Header.Del("Content-Length")
			resp.ContentLength = int64(len(payload))
			resp.Body = io.NopCloser(bytes.NewReader(payload))
			return true
		}
		if apicompat.ObserveAnthropicOutput(&event).MeaningfulOutput {
			return true
		}
		return !nativeAnthropicStructuralFrame(&event, payload)
	}
	for prefix.Len() < maxPrefix {
		result := make(chan nativeAnthropicRead, 1)
		go func() {
			line, err := reader.ReadSlice('\n')
			result <- nativeAnthropicRead{line: line, err: err}
		}()
		var line []byte
		var err error
		select {
		case read := <-result:
			line, err = read.line, read.err
		case <-ctx.Done():
			_ = original.Close()
			return ctx.Err()
		case <-idle:
			_ = original.Close()
			return errAnthropicNativeStreamIdle
		case <-deadline.C:
			resp.Body = &nativeAnthropicReplayBody{
				Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), &nativeAnthropicPendingRead{result: result}, reader), Closer: original,
			}
			return nil
		}
		_, _ = prefix.Write(line) // bytes.Buffer.Write always succeeds.
		if timer != nil {
			timer.Stop()
			timer.Reset(interval)
		}
		if errors.Is(err, bufio.ErrBufferFull) || prefix.Len() >= maxPrefix {
			replay()
			return nil
		}
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if strings.HasPrefix(text, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(text, "event:"))
		} else if strings.HasPrefix(text, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(text, "data:"), " "))
		}
		if text == "" || errors.Is(err, io.EOF) {
			if inspect() {
				if resp.StatusCode < 400 {
					replay()
				}
				return nil
			}
			data = nil
			eventType = ""
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				replay()
				return nil
			}
			return err
		}
	}
	replay()
	return nil
}

// Unknown structures may contain output that an older converter cannot
// recognize. Only positively identified empty structural frames permit replay.
func nativeAnthropicStructuralFrame(event *apicompat.AnthropicStreamEvent, payload []byte) bool {
	knownKeys := func(raw []byte, names ...string) bool {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		for key := range fields {
			found := false
			for _, name := range names {
				found = found || key == name
			}
			if !found {
				return false
			}
		}
		return true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return false
	}
	switch event.Type {
	case "ping":
		return knownKeys(payload, "type")
	case "message_start":
		return knownKeys(payload, "type", "message") && event.Message != nil && len(event.Message.Content) == 0 &&
			knownKeys(fields["message"], "id", "type", "role", "content", "model", "stop_reason", "stop_sequence", "usage")
	case "content_block_start":
		if !knownKeys(payload, "type", "index", "content_block") || event.ContentBlock == nil {
			return false
		}
		switch event.ContentBlock.Type {
		case "text":
			return knownKeys(fields["content_block"], "type", "text") && event.ContentBlock.Text == ""
		case "thinking":
			return knownKeys(fields["content_block"], "type", "thinking", "signature") && event.ContentBlock.Thinking == "" && event.ContentBlock.Signature == ""
		default:
			return false
		}
	case "content_block_stop":
		return knownKeys(payload, "type", "index")
	case "message_delta":
		return knownKeys(payload, "type", "delta", "usage") && knownKeys(fields["delta"], "stop_reason", "stop_sequence")
	default:
		return false
	}
}

func nativeAnthropicErrorStatus(kind string) int {
	switch kind {
	case "authentication_error":
		return http.StatusUnauthorized
	case "permission_error":
		return http.StatusForbidden
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "api_error":
		return http.StatusInternalServerError
	case "invalid_request_error":
		return http.StatusBadRequest
	default:
		return 0
	}
}
