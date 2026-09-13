package geminicli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type driveTestTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt driveTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme, cloned.URL.Host = rt.target.Scheme, rt.target.Host
	return rt.base.RoundTrip(cloned)
}

func TestDriveQuotaIdentityOnWireAndRetries(t *testing.T) {
	for _, source := range []string{"compiled", "global", "account"} {
		t.Run(source, func(t *testing.T) {
			var calls, resolutions atomic.Int64
			captured := make(chan *http.Request, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Clone(context.Background())
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`{"storageQuota":{"limit":"100","usage":"25"}}`))
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			client, ok := NewDriveClient().(*driveClient)
			require.True(t, ok)
			client.clientFactory = func(httpclient.Options) (*http.Client, error) {
				return &http.Client{Transport: brandidentity.WrapRoundTripper(driveTestTransport{target: target, base: http.DefaultTransport})}, nil
			}
			ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity {
				resolutions.Add(1)
				if source == "compiled" {
					return outboundidentity.Identity{}
				}
				version := "3.9.1"
				if calls.Load() > 0 {
					version = "3.9.2"
				}
				return outboundidentity.Identity{Preset: "gemini", UserAgent: "GeminiCLI/" + version, Version: version}
			})
			expected := "GeminiCLI/3.9.1"
			if source == "compiled" {
				expected = GeminiCLIUserAgent
			}
			if source == "account" {
				expected = "GeminiCLI/3.9.3"
				ctx = outboundidentity.WithIdentity(ctx, outboundidentity.Identity{AccountID: 7, Preset: "gemini", UserAgent: expected, Version: "3.9.3"})
			}
			quota, err := client.GetStorageQuota(ctx, "drive-test-token", "")
			require.NoError(t, err)
			require.Equal(t, &DriveStorageInfo{Limit: 100, Usage: 25}, quota)
			for range 2 {
				req := <-captured
				require.Equal(t, "www.googleapis.com", req.Host)
				require.Equal(t, "/drive/v3/about", req.URL.Path)
				require.Equal(t, "storageQuota", req.URL.Query().Get("fields"))
				require.Equal(t, "Bearer drive-test-token", req.Header.Get("Authorization"))
				require.Equal(t, expected, req.Header.Get("User-Agent"))
				require.Empty(t, req.Header.Get("Originator"))
				require.Empty(t, req.Header.Get("Version"))
			}
			if source == "account" {
				require.Zero(t, resolutions.Load())
			} else {
				require.Equal(t, int64(1), resolutions.Load())
			}
		})
	}
}
