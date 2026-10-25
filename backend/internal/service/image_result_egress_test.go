//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageOffloadUpstream struct {
	HTTPUpstream
	proxy   string
	request *http.Request
	mutate  func()
}

func (s *imageOffloadUpstream) Do(request *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	s.proxy, s.request = proxy, request
	if s.mutate != nil {
		s.mutate()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"created":123,"data":[{"url":"http://cdn.example.test/result.png"}]}`)), Request: request}, nil
}

func imageOffloadProxy(t *testing.T, endpoint string, id int64) *Proxy {
	t.Helper()
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Proxy{ID: id, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive, Username: "owner-route", Password: "fake-password"}
}

func forwardURLOnlyImageTask(t *testing.T, ctx context.Context, account *Account, mutate func()) []byte {
	t.Helper()
	upstream := &imageOffloadUpstream{mutate: mutate}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	body := []byte(`{"model":"gpt-image-2","prompt":"a cat","response_format":"url"}`)
	if account.Platform == PlatformGrok {
		body = []byte(`{"model":"grok-imagine-image","prompt":"a cat","response_format":"url"}`)
	}
	c, rec := newOpenAIImagesTestContext(t, body)
	c.Request = c.Request.WithContext(ctx)
	if account.IsGrokOAuth() {
		svc.grokTokenProvider = NewGrokTokenProvider(&auxiliaryOwnerRepo{account: account}, nil)
	}
	var err error
	if account.Platform == PlatformGrok {
		_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json")
	} else {
		parsed, parseErr := svc.ParseOpenAIImagesRequest(c, body)
		require.NoError(t, parseErr)
		_, err = svc.ForwardImages(ctx, c, account, body, parsed, "")
	}
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, upstream.proxy, "the source generation used its configured proxy")
	require.NotContains(t, rec.Body.String(), "b64_json", "exercise URL-only offloading, with no gateway backfill")
	return append([]byte(nil), rec.Body.Bytes()...)
}

// This enters the real generation gateway and ImageTaskService.Complete, then
// performs real HTTP downloads through a failing/reachable proxy. A reachable
// direct destination counts TCP connections, so dropping the captured route
// cannot silently pass as another provider failure.
func TestAsyncURLOnlyImageOffloadRetainsActualAccountProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"openai-apikey", "grok-apikey", "grok-oauth"} {
		for _, mode := range []string{"success", "redirect", "timeout", "refused", "authentication-required", "private-redirect"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				var directConnections atomic.Int32
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(pngBytes)
				}))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directConnections.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				var mu sync.Mutex
				var received []http.Header
				release := make(chan struct{})
				proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					received = append(received, r.Header.Clone())
					mu.Unlock()
					switch mode {
					case "timeout":
						<-release
						return
					case "authentication-required":
						w.WriteHeader(http.StatusProxyAuthRequired)
						return
					case "redirect":
						if r.URL.Path == "/result.png" {
							w.Header().Set("Location", "http://second-cdn.example.test/final.png")
							w.WriteHeader(http.StatusFound)
							return
						}
					case "private-redirect":
						w.Header().Set("Location", "http://169.254.169.254/metadata")
						w.WriteHeader(http.StatusFound)
						return
					}
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(pngBytes)
				}))
				t.Cleanup(proxyServer.Close)
				t.Cleanup(func() { close(release) })
				proxy := imageOffloadProxy(t, proxyServer.URL, 7)
				account := &Account{ID: 883021, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, ProxyID: &proxy.ID, Proxy: proxy, Credentials: map[string]any{"api_key": "fake-generation-key", "user_agent": auxiliaryOwnerUA, "base_url": "https://relay.example.test/v1"}}
				if kind != "openai-apikey" {
					account.Platform = PlatformGrok
					account.Credentials[outboundIdentityCredential] = map[string]any{"preset": "grok", "user_agent": "grok-shell/3.9.1 (linux; x86_64)", "version": "3.9.1"}
					if kind == "grok-oauth" {
						account.Type = AccountTypeOAuth
						account.Credentials["access_token"] = "fake-generation-token"
						account.Credentials["refresh_token"] = "fake-refresh"
						account.Credentials["expires_at"] = "2099-01-01T00:00:00Z"
					}
				}
				ctx := WithImageTaskResultDownloadScope(context.Background())
				result := forwardURLOnlyImageTask(t, ctx, account, func() {
					// Editing/unbinding the source account after its generation
					// send cannot change CDN networking for that already-paid image.
					account.ProxyID, account.Proxy = nil, nil
					account.Credentials["user_agent"] = auxiliaryReplacementUA
				})
				client, _, _, err := imageResultDownloadClient(ctx)
				require.NoError(t, err)
				client.Timeout = 100 * time.Millisecond
				transport := client.Transport.(*imageDownloadTransport)
				transport.resolvesPrivate = func(context.Context, string) (bool, error) { return false, nil }
				// Rewrite only the final direct socket target in the test. The
				// explicit proxy itself is a real loopback HTTP proxy connection.
				dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, destination.Listener.Addr().String())
				}
				transport.direct = &http.Transport{DialContext: dial}
				fallback := &http.Client{Transport: &http.Transport{DialContext: dial}}
				t.Cleanup(fallback.CloseIdleConnections)
				if mode == "refused" {
					proxyServer.Close()
				}
				storage := &fakeImageStorage{}
				tasks := NewImageTaskServiceWithUploader(&imageTaskMemoryStore{}, NewImageResultUploader(storage, "images", false, 0, fallback), time.Hour, time.Minute)
				owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
				task, err := tasks.Create(context.Background(), owner)
				require.NoError(t, err)
				require.NoError(t, tasks.Complete(context.WithoutCancel(ctx), task.ID, http.StatusOK, json.RawMessage(result)))
				completed, err := tasks.Get(context.Background(), owner, task.ID)
				require.NoError(t, err)
				if mode == "success" || mode == "redirect" {
					require.Equal(t, ImageTaskStatusCompleted, completed.Status)
					require.Len(t, storage.saved, 1)
					require.Equal(t, pngBytes, storage.saved[0].data)
					require.NotContains(t, string(completed.Result), "cdn.example.test")
					require.NotContains(t, string(completed.Result), "fake-generation")
				} else {
					require.Equal(t, ImageTaskStatusFailed, completed.Status)
					require.Empty(t, storage.saved)
					require.Empty(t, completed.Result)
				}
				require.Zero(t, directConnections.Load(), "proxy failure or account edit must not create any direct CDN connection")
				mu.Lock()
				headers := append([]http.Header(nil), received...)
				mu.Unlock()
				if mode != "refused" {
					require.NotEmpty(t, headers)
				}
				if mode == "redirect" {
					require.Len(t, headers, 2, "every redirect hop retains the proxy")
				}
				if mode == "private-redirect" {
					require.Len(t, headers, 1, "private redirect is rejected before another send")
				}
				for _, header := range headers {
					require.Empty(t, header.Get("Authorization"))
					require.Empty(t, header.Get("Cookie"))
					require.NotEmpty(t, header.Get("Proxy-Authorization"))
					if kind == "openai-apikey" {
						require.Equal(t, auxiliaryOwnerUA, header.Get("User-Agent"))
						require.Equal(t, "0.164.0", header.Get("Version"))
					} else {
						require.Equal(t, "grok-shell/3.9.1 (linux; x86_64)", header.Get("User-Agent"))
						require.Equal(t, "3.9.1", header.Get("x-grok-client-version"))
					}
				}
			})
		}
	}
}

func TestAsyncImageOffloadMissingOwnerOrBoundProxyCannotUseUploaderDefault(t *testing.T) {
	for _, missing := range []string{"owner", "bound-proxy"} {
		t.Run(missing, func(t *testing.T) {
			ctx := WithImageTaskResultDownloadScope(context.Background())
			if missing == "bound-proxy" {
				id := int64(7)
				account := &Account{ID: 883022, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ProxyID: &id, Credentials: map[string]any{"api_key": "fake"}}
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/images/generations", nil)
				captureImageResultDownloadOwner(request, account)
			}
			direct := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				direct++
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(pngBytes))}, nil
			})}
			storage := &fakeImageStorage{}
			tasks := NewImageTaskServiceWithUploader(&imageTaskMemoryStore{}, NewImageResultUploader(storage, "images", false, 0, client), time.Hour, time.Minute)
			owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
			task, err := tasks.Create(context.Background(), owner)
			require.NoError(t, err)
			require.NoError(t, tasks.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"url":"http://cdn.example.test/result.png"}]}`)))
			completed, err := tasks.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, ImageTaskStatusFailed, completed.Status)
			require.Zero(t, direct)
			require.Empty(t, storage.saved)
		})
	}
}
