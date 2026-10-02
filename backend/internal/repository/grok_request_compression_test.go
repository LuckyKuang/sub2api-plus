//go:build unit || !integration

package repository

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func grokCompressionEncodedRequest(t *testing.T, plain []byte, withPlainCtx bool) *http.Request {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	require.NoError(t, err)
	encoded := encoder.EncodeAll(plain, nil)

	ctx := context.Background()
	if withPlainCtx {
		ctx = service.WithGrokPlainJSONBody(ctx, plain)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", bytes.NewReader(encoded))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer oauth-token")
	req.Header.Set("X-XAI-Token-Auth", xai.CLITokenAuth)
	req.Header.Set("x-authenticateresponse", xai.CLIAuthenticateResponse)
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("Content-Type", "application/json")
	// http.NewRequestWithContext already recorded a GetBody for the compressed
	// bytes; the compression layer keeps that same-target replay contract.
	req.ContentLength = int64(len(encoded))
	require.True(t, service.GrokRequestCompressionOwned(req))
	return req
}

func TestGrokOfficialAPIFallbackRebuildsPlainCompressedBody(t *testing.T) {
	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, true)

	fallback, err := newGrokOfficialAPIFallbackRequest(req)
	require.NoError(t, err)
	require.Equal(t, grokOfficialAPIHost, fallback.URL.Host)
	require.Empty(t, fallback.Header.Get("Content-Encoding"))
	require.Empty(t, fallback.Header.Get("X-XAI-Token-Auth"))
	require.Empty(t, fallback.Header.Get("x-authenticateresponse"))
	require.Equal(t, int64(len(plain)), fallback.ContentLength)
	body, err := io.ReadAll(fallback.Body)
	require.NoError(t, err)
	require.Equal(t, plain, body)
	replay, err := fallback.GetBody()
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, plain, replayed)
}

func TestGrokOfficialAPIFallbackRejectsUnrebuildableCompressedBody(t *testing.T) {
	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, false)

	fallback, err := newGrokOfficialAPIFallbackRequest(req)
	require.Error(t, err)
	require.Nil(t, fallback)
	require.Contains(t, err.Error(), "plain_body_unavailable")
}

func TestGrokAccessDeniedFallbackSkipsUnadvertisedAPIForUnrebuildableBody(t *testing.T) {
	var hosts []string
	transport := &grokAccessDeniedFallbackTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			hosts = append(hosts, req.URL.Hostname())
			if req.URL.Hostname() == grokCLIProxyHost {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body: io.NopCloser(bytes.NewReader([]byte(
						`{"code":"permission_denied","error":"Access to the chat endpoint is denied. Please ensure you're using the correct credentials. If you believe this is a mistake, please contact support."}`,
					))),
					Request: req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"response-ok"}`))),
				Request:    req,
			}, nil
		}),
	}

	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, false)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []string{grokCLIProxyHost}, hosts, "an unrebuildable encoded body must never reach api.x.ai")
}

func TestGrokAccessDeniedFallbackSendsPlainBodyToApiXAI(t *testing.T) {
	var hosts []string
	var fallbackBody []byte
	var fallbackEncoding string
	transport := &grokAccessDeniedFallbackTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			hosts = append(hosts, req.URL.Hostname())
			if req.URL.Hostname() == grokCLIProxyHost {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     make(http.Header),
					Body: io.NopCloser(bytes.NewReader([]byte(
						`{"code":"permission_denied","error":"Access to the chat endpoint is denied. Please ensure you're using the correct credentials. If you believe this is a mistake, please contact support."}`,
					))),
					Request: req,
				}, nil
			}
			body, _ := io.ReadAll(req.Body)
			fallbackBody = body
			fallbackEncoding = req.Header.Get("Content-Encoding")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"response-ok"}`))),
				Request:    req,
			}, nil
		}),
	}

	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, true)
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []string{grokCLIProxyHost, grokOfficialAPIHost}, hosts)
	require.Empty(t, fallbackEncoding)
	require.Equal(t, plain, fallbackBody)
}

func TestHTTPUpstreamCrossOriginRedirectRebuildsPlainCompressedBody(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	base := &http.Client{}

	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, true)
	client := upstream.httpClientForUpstreamRequest(base, req)
	require.NotSame(t, base, client)
	require.NotNil(t, client.CheckRedirect)
	require.Nil(t, base.CheckRedirect, "the cached client must stay untouched")

	sameOrigin, err := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses/retry", bytes.NewReader([]byte("encoded")))
	require.NoError(t, err)
	sameOrigin.Header.Set("Content-Encoding", "zstd")
	require.NoError(t, client.CheckRedirect(sameOrigin, []*http.Request{req}))
	require.Equal(t, "zstd", sameOrigin.Header.Get("Content-Encoding"), "a same-origin hop replays the encoded body")

	crossOrigin, err := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://api.x.ai/v1/responses", bytes.NewReader([]byte("encoded")))
	require.NoError(t, err)
	crossOrigin.Header.Set("Content-Encoding", "zstd")
	crossOrigin.ContentLength = int64(len("encoded"))
	require.NoError(t, client.CheckRedirect(crossOrigin, []*http.Request{req}))
	require.Empty(t, crossOrigin.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(plain)), crossOrigin.ContentLength)
	body, err := io.ReadAll(crossOrigin.Body)
	require.NoError(t, err)
	require.Equal(t, plain, body)
}

func TestHTTPUpstreamCrossOriginRedirectFailsWithoutPlainBody(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)

	plain := []byte(`{"model":"grok-4.5","input":"hello"}`)
	req := grokCompressionEncodedRequest(t, plain, false)
	client := upstream.httpClientForUpstreamRequest(&http.Client{}, req)

	crossOrigin, err := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://api.x.ai/v1/responses", bytes.NewReader([]byte("encoded")))
	require.NoError(t, err)
	crossOrigin.Header.Set("Content-Encoding", "zstd")
	err = client.CheckRedirect(crossOrigin, []*http.Request{req})
	require.Error(t, err)
	require.Contains(t, err.Error(), "plain_body_unavailable")
}

// A request that does not carry gateway request compression keeps the existing
// redirect behavior untouched.
func TestHTTPUpstreamRedirectCheckerUnchangedWithoutCompression(t *testing.T) {
	upstream, ok := NewHTTPUpstream(nil).(*httpUpstreamService)
	require.True(t, ok)
	base := &http.Client{}

	req, err := http.NewRequest(http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", bytes.NewReader([]byte(`{"model":"grok-4.5"}`)))
	require.NoError(t, err)
	require.Same(t, base, upstream.httpClientForUpstreamRequest(base, req))

	plain, err := http.NewRequest(http.MethodGet, "https://cdn.example.com/a.png", nil)
	require.NoError(t, err)
	require.Same(t, base, upstream.httpClientForUpstreamRequest(base, plain))
}
