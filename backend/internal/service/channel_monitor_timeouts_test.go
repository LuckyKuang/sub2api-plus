//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/firstbyte"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorProbeTimeoutsRuntimeReadsCurrentSettings(t *testing.T) {
	const headerKey = "channel_monitor_probe_response_header_timeout_seconds"
	const totalKey = "channel_monitor_probe_total_timeout_seconds"
	repo := &settingPublicRepoStub{values: map[string]string{}}
	settings := NewSettingService(repo, &config.Config{})
	ctx := context.Background()
	require.Equal(t, channelMonitorProbeTimeouts{30, 45}, settings.GetChannelMonitorRuntime(ctx).probeTimeouts)

	repo.values[headerKey] = "60"
	repo.values[totalKey] = "90"
	runtime := settings.GetChannelMonitorRuntime(ctx)
	require.Equal(t, channelMonitorProbeTimeouts{60, 90}, runtime.probeTimeouts)
	require.Equal(t, 108*time.Second, runtime.probeTimeouts.checkTimeout(MonitorCheckModeProbe))
	require.Equal(t, 153*time.Second, runtime.probeTimeouts.checkTimeout(MonitorCheckModeQuotaProbe))
	require.Equal(t, 63*time.Second, runtime.probeTimeouts.checkTimeout(MonitorCheckModeQuota))

	repo.values[totalKey] = "30"
	require.Equal(t, channelMonitorProbeTimeouts{30, 45}, settings.GetChannelMonitorRuntime(ctx).probeTimeouts)
}

func TestChannelMonitorProbeTimeoutsBoundHeadersAndBody(t *testing.T) {
	for _, tc := range []struct {
		name           string
		headerTimeout  time.Duration
		totalTimeout   time.Duration
		flushFirstByte bool
		wantError      string
	}{
		{"waiting for headers", 40 * time.Millisecond, 2 * time.Second, false, "timeout awaiting response headers"},
		{"reading the body", 150 * time.Millisecond, 250 * time.Millisecond, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if tc.flushFirstByte {
					_, _ = w.Write([]byte(`{"choices":[`))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			transport := newMonitorHTTPTransport(tc.headerTimeout)
			transport.DialContext = nil // Use the local test server; production uses safeDialContext.
			defer transport.CloseIdleConnections()
			client := &http.Client{Timeout: tc.totalTimeout, Transport: firstbyte.WrapTransport(transport)}
			started := time.Now()
			result := runCheckForModel(context.Background(), MonitorProviderOpenAI, server.URL, "test-key", "test-model", &CheckOptions{httpClient: client})
			require.Equal(t, MonitorStatusError, result.Status)
			// CHANNEL_MONITOR requires separate header/body deadlines. The standard
			// library has multiple cancellation wrappers. Error status, observed
			// deadline and first-byte presence prove the required failure stage.
			require.NotEmpty(t, result.Message)
			if tc.wantError != "" {
				require.Contains(t, result.Message, tc.wantError)
			}
			elapsed := time.Since(started)
			deadline := tc.headerTimeout
			if tc.flushFirstByte {
				deadline = tc.totalTimeout
			}
			require.GreaterOrEqual(t, elapsed, deadline*8/10)
			require.Less(t, elapsed, deadline+time.Second)
			if tc.flushFirstByte {
				require.NotNil(t, result.FirstByteMs)
			} else {
				require.Nil(t, result.FirstByteMs)
			}
		})
	}
}
