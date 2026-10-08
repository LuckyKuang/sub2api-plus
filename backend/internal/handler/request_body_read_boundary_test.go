//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type auditInterruptedUpload struct{ cause error }

func (r auditInterruptedUpload) Read(p []byte) (int, error) {
	return copy(p, `{"model":"gpt-5.1","input":"partial"}`), r.cause
}
func (auditInterruptedUpload) Close() error { return nil }

// A read error is basic validation, before audit, routing, admission and any
// provider writes. Even a syntactically valid prefix cannot become a request.
func TestResponsesBodyReadFailureCannotForwardPartialRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		status int
	}{
		{"truncated", io.ErrUnexpectedEOF, 400}, {"cancelled", context.Canceled, 400}, {"oversized", &http.MaxBytesError{Limit: 4}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := newAstraProCapturedUpstream(astra200())
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			c, w := newOpenAIResponsesFailoverTestContext(t, context.Background())
			c.Request.Body = auditInterruptedUpload{cause: tc.cause}
			h.Responses(c)
			require.Equal(t, tc.status, w.Code)
			require.NotContains(t, w.Body.String(), "partial")
			_, owners, bodies := upstream.snapshot()
			require.Empty(t, owners)
			require.Empty(t, bodies)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
			_, audited := c.Get(securityAuditCompletedContextKey)
			require.False(t, audited)
			require.Nil(t, service.GetOpsRoutingDiagnostics(c))
		})
	}
}
