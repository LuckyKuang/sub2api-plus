//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// docs/CHANNEL_MONITOR.md: V1 body data is independent from full JSON and
// challenge validation. A fast response body cannot turn a failure into success.
func TestChannelMonitorFirstByteIndependentFromTotalAndSuccess(t *testing.T) {
	swapMonitorHTTPClient(t)
	for _, tc := range []struct {
		name   string
		code   int
		tail   string
		status string
	}{
		{"success", 200, `"choices":[{"message":{"content":"ok"}}]}`, MonitorStatusOperational},
		{"http error", 503, `"error":{"message":"unavailable"}}`, MonitorStatusError},
		{"empty text", 200, `"choices":[{"message":{"content":""}}]}`, MonitorStatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte("{"))
				w.(http.Flusher).Flush()
				time.Sleep(80 * time.Millisecond)
				_, _ = w.Write([]byte(tc.tail))
			}))
			defer server.Close()
			result := runCheckForModel(context.Background(), MonitorProviderOpenAI, server.URL, "test", "model", &CheckOptions{BodyOverrideMode: MonitorBodyOverrideModeReplace, BodyOverride: map[string]any{"model": "model", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}})
			require.Equal(t, tc.status, result.Status)
			require.NotNil(t, result.FirstByteMs)
			require.NotNil(t, result.LatencyMs)
			require.GreaterOrEqual(t, *result.LatencyMs-*result.FirstByteMs, 40)
			require.Nil(t, result.PingLatencyMs)
		})
	}
}

func TestChannelMonitorFirstByteEmptyBodyAndSlowTotal(t *testing.T) {
	swapMonitorHTTPClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	result := runCheckForModel(context.Background(), MonitorProviderOpenAI, server.URL, "test", "model", nil)
	require.Nil(t, result.FirstByteMs)
	require.NotEqual(t, MonitorStatusOperational, result.Status)
	first := 1
	result = finalizeOperationalOrDegraded(&CheckResult{FirstByteMs: &first}, 6*time.Second, 6000)
	require.Equal(t, MonitorStatusDegraded, result.Status)
	require.Equal(t, 1, *result.FirstByteMs)
}
