package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var codexSafetyBufferingHeaders = [...]string{
	"x-codex-safety-buffering-enabled",
	"x-codex-safety-buffering-faster-model",
}

// This is a downstream-only projection. Callers retain the original payload
// for accounting, policy errors, route state, and transport lifecycle observers.
func stripCodexSafetyBuffering(payload []byte) ([]byte, error) {
	if !bytes.Contains(payload, []byte("safety_buffering")) && !bytes.Contains(payload, []byte(`"headers"`)) && !bytes.Contains(payload, []byte(`\u`)) {
		return payload, nil
	}
	if !json.Valid(payload) {
		return payload, errors.New("invalid output JSON")
	}
	root := gjson.ParseBytes(payload)
	if !root.IsObject() {
		return payload, nil
	}
	paths := make([]string, 0, 7)
	if root.Get("safety_buffering").Exists() {
		paths = append(paths, "safety_buffering")
	}
	if root.Get("type").String() == "response.metadata" && root.Get("metadata.type").String() == "safety_buffering" {
		paths = append(paths, "metadata.type", "metadata.use_cases", "metadata.reasons", "metadata.retry_model")
	}
	// Only the protocol's header carrier is inspected, never arbitrary nested
	// user content or tool arguments containing similarly named keys.
	root.Get("headers").ForEach(func(key, _ gjson.Result) bool {
		for _, name := range codexSafetyBufferingHeaders {
			if strings.EqualFold(key.String(), name) {
				paths = append(paths, "headers."+key.String())
			}
		}
		return true
	})
	result := payload
	for _, path := range paths {
		var err error
		result, err = sjson.DeleteBytes(result, path)
		if err != nil {
			return payload, err
		}
	}
	// A pure notification has no remaining business content. Do not manufacture
	// an empty metadata event: both the App and CLI consume these envelopes.
	if len(paths) > 0 && root.Get("type").String() == "response.metadata" {
		meaningful := false
		gjson.ParseBytes(result).ForEach(func(key, value gjson.Result) bool {
			switch key.String() {
			case "type":
				return true
			case "sequence_number":
				if value.Type == gjson.Number {
					return true
				}
			case "metadata", "headers":
				if value.IsObject() && len(value.Map()) == 0 {
					return true
				}
			}
			meaningful = true
			return false
		})
		if !meaningful {
			return nil, nil
		}
	}
	return result, nil
}

func (s *OpenAIGatewayService) codexSafetyBufferingOutputEnabled(ctx context.Context, account *Account) bool {
	return s != nil && account != nil && account.Platform == PlatformOpenAI && s.settingService.CodexSafetyBufferingSuppressed(ctx)
}

func (s *OpenAIGatewayService) projectCodexSafetyBufferingOutput(ctx context.Context, c *gin.Context, account *Account, payload []byte, protocol string) []byte {
	if !s.codexSafetyBufferingOutputEnabled(ctx, account) {
		return payload
	}
	result, err := stripCodexSafetyBuffering(payload)
	if err != nil {
		logCodexSafetyBufferingOutputFailure(ctx, c, protocol, "codex_safety_buffering_projection_failed", len(payload))
		return payload
	}
	return result
}

func logCodexSafetyBufferingOutputFailure(ctx context.Context, c *gin.Context, protocol, code string, size int) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestID, endpoint := "", ""
	requestID, _ = ctx.Value(ctxkey.RequestID).(string)
	if c != nil {
		endpoint = c.FullPath()
	}
	slog.WarnContext(ctx, "codex safety buffering output projection failed", "request_id", requestID, "endpoint", endpoint,
		"protocol", protocol, "stage", "downstream_output", "code", code, "input_bytes", size)
}

// Installing at the HTTP writer keeps first-output staging and bridge replay
// untouched until their bytes are actually released to the client. WS has the
// same projection at its final client writer, after upstream observers.
func (s *OpenAIGatewayService) beginCodexSafetyBufferingHTTPOutput(ctx context.Context, c *gin.Context, account *Account) func() {
	if s == nil || s.settingService == nil || c == nil || c.Writer == nil || account == nil || account.Platform != PlatformOpenAI {
		return func() {}
	}
	if active, _ := c.Get("codex_safety_buffering_output_writer"); active != nil {
		return func() {}
	}
	// Initialize before any provider write; subsequent output reads are atomic.
	s.settingService.CodexSafetyBufferingSuppressed(ctx)
	w := &codexSafetyBufferingHTTPWriter{ResponseWriter: c.Writer, ctx: ctx, c: c, service: s, account: account}
	c.Writer = w
	c.Set("codex_safety_buffering_output_writer", w)
	return func() {
		defer func() {
			c.Writer = w.ResponseWriter
			c.Set("codex_safety_buffering_output_writer", nil)
		}()
		if err := w.finish(); err != nil {
			logCodexSafetyBufferingOutputFailure(ctx, c, "http", "codex_safety_buffering_output_write_failed", 0)
		}
	}
}

// Existing parsers own semantic flush boundaries, including EOF/read-error
// residuals without a final blank line. Release projection before that flush so
// their write-error and usage-draining rules retain ownership of failures.
func flushCodexSafetyBufferingHTTPOutput(c *gin.Context) error {
	if c == nil {
		return nil
	}
	value, _ := c.Get("codex_safety_buffering_output_writer")
	if writer, ok := value.(*codexSafetyBufferingHTTPWriter); ok && writer != nil {
		if err := writer.finish(); err != nil {
			return err
		}
	}
	value, _ = c.Get("openai_error_message_output_writer")
	if writer, ok := value.(*codexSafetyBufferingHTTPWriter); ok && writer != nil {
		return writer.finish()
	}
	return nil
}

type codexSafetyBufferingHTTPWriter struct {
	gin.ResponseWriter
	mu                sync.Mutex
	ctx               context.Context
	c                 *gin.Context
	service           *OpenAIGatewayService
	account           *Account
	projectionEnabled func() bool
	projectPayload    func([]byte, string) []byte
	bypassProjection  func() bool
	pending           *openAIFirstOutputStage
	lineSize          int
	lineHead          byte
}

// Reuse the anonymous spool used by first-output staging. The existing gateway
// parsers own accepted input limits; a display filter must not add a smaller
// fail-open threshold. Only one event is retained and it is closed on all paths.
func (w *codexSafetyBufferingHTTPWriter) enabled() bool {
	if w.projectionEnabled != nil {
		return w.projectionEnabled()
	}
	return w.service.codexSafetyBufferingOutputEnabled(w.ctx, w.account)
}

func (w *codexSafetyBufferingHTTPWriter) project(payload []byte, protocol string) []byte {
	if w.projectPayload != nil {
		return w.projectPayload(payload, protocol)
	}
	return w.service.projectCodexSafetyBufferingOutput(w.ctx, w.c, w.account, payload, protocol)
}

func (w *codexSafetyBufferingHTTPWriter) appendPending(p []byte) error {
	if w.pending == nil {
		w.pending = newOpenAIFirstOutputStage(math.MaxInt64)
	}
	if _, err := w.pending.Write(p); err != nil {
		logCodexSafetyBufferingOutputFailure(w.ctx, w.c, "http", "codex_safety_buffering_stage_failed", len(p))
		w.closeStage(w.pending)
		w.pending = nil
		return err
	}
	return nil
}

func codexSafetyBufferingStageBytes(stage *openAIFirstOutputStage) ([]byte, error) {
	if stage.tempFile == nil {
		return stage.memory.Bytes(), nil
	}
	return io.ReadAll(io.NewSectionReader(stage.tempFile, 0, stage.Buffered()))
}

func (w *codexSafetyBufferingHTTPWriter) closeStage(stage *openAIFirstOutputStage) {
	size := stage.Buffered()
	if err := stage.Close(); err != nil {
		logCodexSafetyBufferingOutputFailure(w.ctx, w.c, "http", "codex_safety_buffering_stage_cleanup_failed", int(size))
	}
}

func (w *codexSafetyBufferingHTTPWriter) prepareHeaders() {
	if w.service != nil && !w.Written() && w.service.codexSafetyBufferingOutputEnabled(w.ctx, w.account) {
		for key := range w.Header() {
			for _, name := range codexSafetyBufferingHeaders {
				if strings.EqualFold(key, name) {
					delete(w.Header(), key)
				}
			}
		}
	}
}

func (w *codexSafetyBufferingHTTPWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prepareHeaders()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *codexSafetyBufferingHTTPWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *codexSafetyBufferingHTTPWriter) writeRaw(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	w.prepareHeaders()
	_, err := w.ResponseWriter.Write(p)
	return err
}

func (w *codexSafetyBufferingHTTPWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Other providers and non-protocol bodies keep their original write/flush
	// boundaries. Only an eligible OpenAI response needs event framing.
	if w.pending == nil && w.bypassProjection != nil && w.bypassProjection() {
		return w.ResponseWriter.Write(p)
	}
	if !isEventStreamResponse(w.Header()) {
		inputSize := len(p)
		if strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "application/json") && (w.pending != nil || !json.Valid(p)) {
			if err := w.appendPending(p); err != nil {
				return 0, err
			}
			// Responses JSON is an object. Avoid rereading a large spool for each
			// fragment that cannot yet finish the document.
			end := bytes.TrimSpace(p)
			if len(end) == 0 || (end[len(end)-1] != '}' && end[len(end)-1] != ']') {
				return inputSize, nil
			}
			complete, err := codexSafetyBufferingStageBytes(w.pending)
			if err != nil {
				w.closeStage(w.pending)
				w.pending = nil
				return 0, err
			}
			if !json.Valid(complete) {
				return inputSize, nil
			}
			stage := w.pending
			w.pending = nil
			defer w.closeStage(stage)
			p = complete
		}
		projected := w.project(p, "http")
		if !bytes.Equal(projected, p) && !w.Written() {
			w.Header().Del("Content-Length")
		}
		if err := w.writeRaw(projected); err != nil {
			return 0, err
		}
		return inputSize, nil
	}
	// Buffer one SSE event, not the response. Keep parsing even while
	// disabled so enabling in the middle of a fragmented event cannot bypass it.
	size := len(p)
	for len(p) > 0 {
		n := bytes.IndexByte(p, '\n')
		if n < 0 {
			n = len(p)
		} else {
			n++
		}
		part := p[:n]
		p = p[n:]
		if w.lineSize == 0 {
			w.lineHead = part[0]
		}
		w.lineSize += len(part)
		if err := w.appendPending(part); err != nil {
			return 0, err
		}
		if part[len(part)-1] == '\n' {
			blank := w.lineSize == 1 || (w.lineSize == 2 && w.lineHead == '\r')
			w.lineSize = 0
			if blank {
				if err := w.writeSSEFrame(); err != nil {
					return 0, err
				}
			}
		}
	}
	return size, nil
}

func (w *codexSafetyBufferingHTTPWriter) writeSSEFrame() error {
	stage := w.pending
	w.pending = nil
	defer w.closeStage(stage)
	if !w.enabled() {
		w.prepareHeaders()
		return stage.CommitTo(w.ResponseWriter)
	}
	frame, err := codexSafetyBufferingStageBytes(stage)
	if err != nil {
		return err
	}
	lines := bytes.SplitAfter(frame, []byte{'\n'})
	var data []byte
	for _, line := range lines {
		line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte{'\n'}), []byte{'\r'})
		if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line[5:], []byte{' '})
			if data != nil {
				data = append(data, '\n')
			}
			data = append(data, value...)
		}
	}
	projected := w.project(data, "sse")
	if bytes.Equal(projected, data) {
		return w.writeRaw(frame)
	}
	if !w.Written() {
		w.Header().Del("Content-Length")
	}
	if len(projected) == 0 && len(data) > 0 {
		// Remove id/event/data together; emitting an empty named SSE event is
		// still an interaction. Independent comment keepalives may survive.
		var comments []byte
		for _, line := range lines {
			if bytes.HasPrefix(line, []byte{':'}) {
				comments = append(comments, line...)
			}
		}
		if len(comments) > 0 {
			if bytes.HasSuffix(comments, []byte("\r\n")) {
				comments = append(comments, '\r', '\n')
			} else {
				comments = append(comments, '\n')
			}
		}
		return w.writeRaw(comments)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, projected); err != nil {
		logCodexSafetyBufferingOutputFailure(w.ctx, w.c, "sse", "codex_safety_buffering_projection_failed", len(data))
		return w.writeRaw(frame)
	}
	output := make([]byte, 0, len(frame))
	emitted := false
	for _, line := range lines {
		if !bytes.HasPrefix(line, []byte("data:")) {
			output = append(output, line...)
			continue
		}
		if !emitted {
			output = append(output, "data: "...)
			output = append(output, compact.Bytes()...)
			if bytes.HasSuffix(line, []byte("\r\n")) {
				output = append(output, '\r', '\n')
			} else if bytes.HasSuffix(line, []byte{'\n'}) {
				output = append(output, '\n')
			}
			emitted = true
		}
	}
	return w.writeRaw(output)
}

func (w *codexSafetyBufferingHTTPWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prepareHeaders()
	w.ResponseWriter.Flush()
}

func (w *codexSafetyBufferingHTTPWriter) finish() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil {
		return nil
	}
	if !isEventStreamResponse(w.Header()) {
		stage := w.pending
		w.pending = nil
		defer w.closeStage(stage)
		original, err := codexSafetyBufferingStageBytes(stage)
		if err != nil {
			return err
		}
		payload := w.project(original, "http")
		if !bytes.Equal(payload, original) && !w.Written() {
			w.Header().Del("Content-Length")
		}
		return w.writeRaw(payload)
	}
	// EOF can terminate a valid SSE event without a trailing empty line.
	return w.writeSSEFrame()
}

var _ http.Flusher = (*codexSafetyBufferingHTTPWriter)(nil)
