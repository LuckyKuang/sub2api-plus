//go:build unit || !integration

package httputil

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type interruptedAuditBody struct {
	done  bool
	cause error
}

func (r *interruptedAuditBody) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.cause
	}
	r.done = true
	return copy(p, "partial"), r.cause
}
func (r *interruptedAuditBody) Close() error { return nil }

// Approved audit: operators need actual read progress without retaining the
// partial request or printing attacker-controlled underlying error text.
func TestRequestBodyReadFailureHasSafeProgress(t *testing.T) {
	for _, tc := range []struct {
		name, encoding, stage, reason string
		body                          io.ReadCloser
		cause                         error
		bytes                         int64
	}{
		{"truncated", "", "read", "unexpected_eof", &interruptedAuditBody{cause: io.ErrUnexpectedEOF}, io.ErrUnexpectedEOF, 7},
		{"cancelled", "", "read", "client_cancelled", &interruptedAuditBody{cause: context.Canceled}, context.Canceled, 7},
		{"deadline", "", "read", "read_timeout", &interruptedAuditBody{cause: context.DeadlineExceeded}, context.DeadlineExceeded, 7},
		{"unknown", "", "read", "read_error", &interruptedAuditBody{cause: errors.New("secret-input sk-private")}, nil, 7},
		{"compression", "gzip", "decode", "invalid_compression", io.NopCloser(strings.NewReader("not-gzip")), nil, 8},
		{"oversize", "", "read", "body_too_large", &interruptedAuditBody{cause: &http.MaxBytesError{Limit: 4}}, nil, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/responses", nil)
			req.Body = tc.body
			req.ContentLength = 20
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			body, err := ReadRequestBodyWithPrealloc(req)
			require.Error(t, err)
			require.Nil(t, body, "partial input cannot be forwarded or audited as a full request")
			var diagnostic interface {
				RequestBodyReadDiagnostic() (string, string, int64, int64)
			}
			require.True(t, errors.As(err, &diagnostic), "read failure must retain a sanitized progress diagnostic")
			stage, reason, read, declared := diagnostic.RequestBodyReadDiagnostic()
			require.Equal(t, tc.stage, stage)
			require.Equal(t, tc.reason, reason)
			require.Equal(t, tc.bytes, read)
			require.Equal(t, int64(20), declared)
			require.NotContains(t, err.Error(), "secret-input")
			require.NotContains(t, err.Error(), "sk-private")
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			}
			if tc.name == "oversize" {
				var max *http.MaxBytesError
				require.ErrorAs(t, err, &max)
				require.Equal(t, int64(4), max.Limit)
			}
		})
	}
}

func TestRequestBodyReadFailureLogPrivacyAndProgress(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.WithValue(context.Background(), ctxkey.RequestID, "read-request-1"), zap.New(core))
	req := httptest.NewRequest("POST", "/v1beta/models/private-model:generateContent?key=sk-private", nil).WithContext(ctx)
	req.Body = &interruptedAuditBody{cause: errors.New("secret-payload sk-private")}
	req.ContentLength = 20
	body, err := ReadRequestBodyWithPrealloc(req)
	require.Error(t, err)
	require.Nil(t, body)
	entries := logs.All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "gateway.request_body_read_failed", entries[0].Message)
	require.Equal(t, "read-request-1", fields["request_id"])
	require.Equal(t, "/v1beta/models/:model:generateContent", fields["endpoint"])
	require.Equal(t, "gemini", fields["protocol"])
	require.Equal(t, "read", fields["stage"])
	require.Equal(t, "request_body_read_failed", fields["error_code"])
	require.Equal(t, "read_error", fields["reason"])
	require.EqualValues(t, 7, fields["body_bytes"])
	require.EqualValues(t, 20, fields["declared_body_bytes"])
	for _, private := range []string{"secret-payload", "sk-private", "private-model", "partial"} {
		require.NotContains(t, entries[0].Message+fmt.Sprint(fields), private)
	}
}

func TestSuccessfulCompressedReadAndNormalizeLimit(t *testing.T) {
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, err := writer.Write([]byte("{\"input\":\"ok\"}"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(packed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	body, err := ReadRequestBodyWithPrealloc(req)
	require.NoError(t, err)
	require.JSONEq(t, `{"input":"ok"}`, string(body))
	require.Empty(t, req.Header.Get("Content-Encoding"))
	req = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"too large"}`))
	body, err = ReadLenientJSONRequestBodyWithPrealloc(req, 4)
	require.Nil(t, body)
	var limit *http.MaxBytesError
	require.ErrorAs(t, err, &limit)
	var diagnostic *RequestBodyReadError
	require.ErrorAs(t, err, &diagnostic)
	stage, reason, read, declared := diagnostic.RequestBodyReadDiagnostic()
	require.Equal(t, "normalize", stage)
	require.Equal(t, "body_too_large", reason)
	require.EqualValues(t, 21, read)
	require.EqualValues(t, 21, declared)
}

type auditZeroBody struct{}

func (auditZeroBody) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// Size is an admission limit, not permission to silently truncate an upload.
func TestCompressedRequestLimitDoesNotForwardTruncatedBody(t *testing.T) {
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, err := io.CopyN(writer, auditZeroBody{}, (64<<20)+1)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(packed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	body, err := ReadRequestBodyWithPrealloc(req)
	require.Nil(t, body)
	var limit *http.MaxBytesError
	require.ErrorAs(t, err, &limit)
	require.EqualValues(t, 64<<20, limit.Limit)
	var diagnostic *RequestBodyReadError
	require.ErrorAs(t, err, &diagnostic)
	stage, reason, read, declared := diagnostic.RequestBodyReadDiagnostic()
	require.Equal(t, "decode", stage)
	require.Equal(t, "body_too_large", reason)
	require.EqualValues(t, packed.Len(), read)
	require.EqualValues(t, packed.Len(), declared)
}
