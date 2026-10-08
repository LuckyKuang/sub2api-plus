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
