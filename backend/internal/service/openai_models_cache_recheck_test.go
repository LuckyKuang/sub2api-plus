//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A caller that observed a cache miss can arrive after another refresh has
// finished. The catalog must not issue another provider request for a fresh
// owner-scoped result, while stale entries must still be revalidated.
func TestOpenAIModelsRefreshRechecksFreshOwnerScopedCache(t *testing.T) {
	svc := &OpenAIGatewayService{}
	fresh := &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"gpt-6.1-terra"}]}`), ETag: "fresh", upstreamETag: "provider-etag"}
	svc.openAIModelsCache.set("owner-a", fresh, time.Now())
	calls := 0
	fetch := func(context.Context, string) (*OpenAIModelsResponse, error) {
		calls++
		return &OpenAIModelsResponse{Body: []byte(`{"models":[]}`)}, nil
	}
	result := <-svc.refreshCachedOpenAIModels("owner-a", openAIModelsRequest{}, fetch)
	require.NoError(t, result.Err)
	require.Same(t, fresh, result.Val)
	require.Zero(t, calls, "a just-completed refresh must satisfy a late caller")

	result = <-svc.refreshCachedOpenAIModels("owner-b", openAIModelsRequest{}, fetch)
	require.NoError(t, result.Err)
	require.Equal(t, 1, calls, "another credential owner must fetch its own catalog")
}

func TestOpenAIModelsRefreshRevalidatesStaleCache(t *testing.T) {
	svc := &OpenAIGatewayService{}
	stale := &OpenAIModelsResponse{Body: []byte(`{"models":[]}`), upstreamETag: "stale-provider-etag"}
	svc.openAIModelsCache.set("owner-a", stale, time.Now().Add(-openAIModelsCacheTTL-time.Second))
	var sentETag string
	result := <-svc.refreshCachedOpenAIModels("owner-a", openAIModelsRequest{}, func(_ context.Context, etag string) (*OpenAIModelsResponse, error) {
		sentETag = etag
		return &OpenAIModelsResponse{NotModified: true}, nil
	})
	require.NoError(t, result.Err)
	require.Equal(t, "stale-provider-etag", sentETag)
	require.Same(t, stale, result.Val)
	_, state := svc.openAIModelsCache.get("owner-a", time.Now())
	require.Equal(t, openAIModelsCacheFresh, state)
}
