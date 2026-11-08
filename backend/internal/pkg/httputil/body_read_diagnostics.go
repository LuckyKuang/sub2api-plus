package httputil

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

func (e *RequestBodyReadError) RequestBodyReadDiagnostic() (string, string, int64, int64) {
	stage, reason := e.stage, e.reason
	if stage == "" {
		stage = "read"
	}
	if reason == "" {
		reason = requestBodyReadReason(e.Err)
	}
	return stage, reason, e.BytesRead, e.declared
}

func annotateRequestBodyReadFailure(req *http.Request, err error) error {
	var detail *RequestBodyReadError
	if !errors.As(err, &detail) {
		detail = &RequestBodyReadError{Err: err, BytesRead: 0}
	}
	detail.declared = req.ContentLength
	logRequestBodyReadFailure(req, detail)
	return detail
}

func requestBodyReadReason(err error) string {
	var max *http.MaxBytesError
	var timeout net.Error
	switch {
	case errors.As(err, &max):
		return "body_too_large"
	case errors.Is(err, context.Canceled):
		// A reader can cancel independently of the accepted HTTP request.
		// logRequestBodyReadFailure identifies a cancelled client context.
		return "read_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "read_timeout"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.As(err, &timeout) && timeout.Timeout():
		return "read_timeout"
	default:
		return "read_error"
	}
}

func logRequestBodyReadFailure(req *http.Request, err error) {
	if req == nil {
		return
	}
	var failure *RequestBodyReadError
	if !errors.As(err, &failure) {
		return
	}
	if errors.Is(req.Context().Err(), context.Canceled) && !IsRequestBodyReadTimeout(failure.Err) {
		failure.reason = "client_cancelled"
	}
	endpoint, protocol := "unknown", "unknown"
	if req.URL != nil {
		path := req.URL.Path
		for _, prefix := range []string{"/openai", "/anthropic", "/gemini", "/antigravity"} {
			path = strings.TrimPrefix(path, prefix)
		}
		switch path {
		case "/v1/responses", "/v1/responses/compact":
			endpoint, protocol = path, "responses"
		case "/v1/chat/completions":
			endpoint, protocol = path, "chat_completions"
		case "/v1/messages", "/v1/messages/count_tokens":
			endpoint, protocol = path, "anthropic"
		case "/v1/embeddings", "/v1/alpha/search", "/v1/images/generations", "/v1/images/edits":
			endpoint, protocol = path, "openai"
		default:
			if strings.HasPrefix(path, "/v1beta/models/") {
				if strings.HasSuffix(path, ":generateContent") {
					endpoint, protocol = "/v1beta/models/:model:generateContent", "gemini"
				}
				if strings.HasSuffix(path, ":streamGenerateContent") {
					endpoint, protocol = "/v1beta/models/:model:streamGenerateContent", "gemini"
				}
			}
		}
	}
	stage, reason, _, _ := failure.RequestBodyReadDiagnostic()
	requestID, _ := req.Context().Value(ctxkey.RequestID).(string)
	logger.FromContext(req.Context()).Warn("gateway.request_body_read_failed",
		zap.String("request_id", requestID), zap.String("endpoint", endpoint), zap.String("protocol", protocol),
		zap.String("stage", stage), zap.String("error_code", "request_body_read_failed"),
		zap.String("reason", reason), zap.Int64("body_bytes", failure.BytesRead), zap.Int64("declared_body_bytes", failure.declared))
}
