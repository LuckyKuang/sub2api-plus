package httputil

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
)

// RequestBodyReadError carries progress, never partial input. Its public text
// is bounded server vocabulary; Unwrap preserves cancellation/MaxBytes checks.
type RequestBodyReadError struct {
	stage, reason  string
	read, declared int64
	cause          error
}

func (e *RequestBodyReadError) Error() string {
	if e.reason == "unsupported_encoding" {
		return "unsupported Content-Encoding"
	}
	return "request body read failed: " + e.reason
}
func (e *RequestBodyReadError) Unwrap() error { return e.cause }
func (e *RequestBodyReadError) RequestBodyReadDiagnostic() (string, string, int64, int64) {
	return e.stage, e.reason, e.read, e.declared
}

func requestBodyReadReason(err error) string {
	var max *http.MaxBytesError
	var timeout net.Error
	switch {
	case errors.As(err, &max):
		return "body_too_large"
	case errors.Is(err, context.Canceled):
		return "client_cancelled"
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
