//go:build unit

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokSSOProductionClientProxyFailuresNeverDialDirect(t *testing.T) {
	for _, failure := range []string{"timeout", "refused", "407", "malformed"} {
		t.Run(failure, func(t *testing.T) {
			var attempts, directDials atomic.Int32
			release := make(chan struct{})
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if r.Method != http.MethodConnect || r.Host != "accounts.x.ai:443" {
					t.Errorf("unexpected CONNECT: %s %s", r.Method, r.Host)
				}
				if failure == "timeout" {
					<-release
					return
				}
				w.WriteHeader(http.StatusProxyAuthRequired)
			}))
			t.Cleanup(proxy.Close)
			t.Cleanup(func() { close(release) })
			proxyURL, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			proxyURL.User = url.UserPassword("owner", "private-proxy-password")
			selected := proxyURL.String()
			if failure == "refused" {
				proxy.Close()
			}
			if failure == "malformed" {
				selected = "http://owner:private-proxy-password@%zz"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{ConnectStart: func(_, address string) {
				if address != proxyURL.Host {
					directDials.Add(1)
					cancel()
				}
			}})
			token, err := NewGrokOAuthClient().ConvertSSOToBuild(ctx, "private-web-cookie", selected)
			require.Error(t, err)
			require.Nil(t, token)
			require.Zero(t, directDials.Load())
			if failure == "timeout" || failure == "407" {
				require.Positive(t, attempts.Load())
			}
			for _, secret := range []string{"private-web-cookie", "private-proxy-password"} {
				require.NotContains(t, err.Error(), secret)
			}
		})
	}
}
