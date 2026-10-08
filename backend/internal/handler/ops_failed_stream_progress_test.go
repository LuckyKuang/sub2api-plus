//go:build unit

package handler

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type auditStreamReadFailure struct{}

func (auditStreamReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer")
}

// Approved audit: actual token observation survives a failed partial response
// at the handler -> error-log boundary, while metadata-only attempts stay null.
func TestOpsFailedPartialStreamKeepsObservedTimingAndSuppressesReplay(t *testing.T) {
	for _, tc := range []struct {
		name, events        string
		hasToken, committed bool
	}{
		{"text", `data: {"type":"response.output_text.delta","delta":"delivered"}` + "\n\n", true, true},
		{"tool", `data: {"type":"response.function_call_arguments.delta","delta":"{}"}` + "\n\n", true, true},
		{"media", `data: {"type":"response.image_generation_call.partial_image","partial_image_b64":"aW1hZ2U="}` + "\n\n", false, true},
		{"metadata", `data: {"type":"response.created","response":{"id":"resp_metadata"}}` + "\n\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 8)
			response := func() *http.Response {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(io.MultiReader(strings.NewReader(tc.events), auditStreamReadFailure{}))}
			}
			upstream := newAstraProCapturedUpstream(response(), response())
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			c, w := newOpenAIResponsesFailoverTestContext(t, context.Background())
			c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
			c.Request.ContentLength = -1
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			keys := c.Keys
			router.POST("/v1/responses", func(actual *gin.Context) {
				for k, v := range keys {
					actual.Set(k, v)
				}
				h.Responses(actual)
			})
			w = httptest.NewRecorder()
			router.ServeHTTP(w, c.Request)
			require.GreaterOrEqual(t, OpsErrorLogQueueLength(), int64(1), "visible stream failure must enter error persistence")
			entry := (<-opsErrorLogQueue).entry
			if tc.hasToken {
				require.NotNil(t, entry.TimeToFirstTokenMs)
				require.GreaterOrEqual(t, *entry.TimeToFirstTokenMs, int64(0))
			} else {
				require.Nil(t, entry.TimeToFirstTokenMs)
			}
			_, owners, _ := upstream.snapshot()
			if tc.committed {
				require.Equal(t, []int64{1}, owners, "a delivered answer/tool action must not be replayed")
				require.NotNil(t, entry.UpstreamErrorsJSON)
				attempts, parseErr := service.ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
				require.NoError(t, parseErr)
				require.Len(t, attempts, 1)
				require.NotNil(t, attempts[0].SemanticOutputCommitted)
				require.True(t, *attempts[0].SemanticOutputCommitted)
				require.Equal(t, "semantic_output_committed", attempts[0].ReplaySuppressedReason)
				if tc.hasToken {
					require.NotNil(t, attempts[0].TimeToFirstTokenMs)
				} else {
					require.Nil(t, attempts[0].TimeToFirstTokenMs)
				}
				require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.failed\n")+strings.Count(w.Body.String(), "event: error\n"), "one failure terminal")
			}
		})
	}
}
