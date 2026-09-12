//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The reader pauses only after the bridge has consumed the previous frame.
type geminiTimingGapReader struct {
	io.Reader
	delay time.Duration
}

func (r *geminiTimingGapReader) Read(p []byte) (int, error) {
	if r.delay > 0 {
		time.Sleep(r.delay)
		r.delay = 0
	}
	return r.Reader.Read(p)
}

func TestGeminiMessagesToolArgumentTiming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const initial = `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":"{\"city\":\""}}]}}]}` + "\n\n"
	for _, tt := range []struct {
		name    string
		next    string
		advance bool
	}{
		{"new arguments", `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":"SF\"}"}}]}}]}` + "\n\n", true},
		{"repeated arguments", initial, false},
		{"missing arguments", `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather"}}]}}]}` + "\n\n", false},
		{"empty arguments", `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{}}}]}}]}` + "\n\n", false},
		{"terminal metadata", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const gap = 80 * time.Millisecond
			body := io.MultiReader(strings.NewReader(initial), &geminiTimingGapReader{
				Reader: strings.NewReader(tt.next + "data: [DONE]\n\n"), delay: gap,
			})
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			result, err := (&GeminiMessagesCompatService{}).handleStreamingResponse(c, resp, time.Now(), "claude")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.firstTokenMs)
			require.NotNil(t, result.lastTokenMs)
			require.Equal(t, "tool", result.firstOutputKind)
			if tt.advance {
				require.GreaterOrEqual(t, *result.lastTokenMs-*result.firstTokenMs, int(gap.Milliseconds()), "later argument deltas must extend the token window")
				require.Contains(t, rec.Body.String(), `SF`)
			} else {
				require.Less(t, *result.lastTokenMs-*result.firstTokenMs, int(gap.Milliseconds()), "metadata and repeated arguments must not extend the token window")
			}
		})
	}
}

func TestGeminiMessagesEmptyToolTiming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, call := range []string{`{}`, `{"args":{}}`, `{"args":""}`} {
		t.Run(call, func(t *testing.T) {
			body := `data: {"candidates":[{"content":{"parts":[{"functionCall":` + call + `}]}}]}` + "\n\ndata: [DONE]\n\n"
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			result, err := (&GeminiMessagesCompatService{}).handleStreamingResponse(c, resp, time.Now(), "claude")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Nil(t, result.firstTokenMs, "synthesized tool names and empty inputs are not upstream tokens")
			require.Nil(t, result.lastTokenMs)
			require.Nil(t, result.firstOutputMs)
		})
	}
}
