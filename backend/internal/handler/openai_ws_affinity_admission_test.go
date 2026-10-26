//go:build unit || !integration

package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type handlerWSAffinityWrite struct {
	groupID int64
	key     string
	owner   int64
}

type handlerWSAffinityRecordingCache struct {
	service.GatewayCache
	mu     sync.Mutex
	values map[string]int64
	writes []handlerWSAffinityWrite
}

func (c *handlerWSAffinityRecordingCache) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owner, ok := c.values[fmt.Sprintf("%d:%s", groupID, key)]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return owner, nil
}

func (c *handlerWSAffinityRecordingCache) SetSessionAccountID(_ context.Context, groupID int64, key string, owner int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[fmt.Sprintf("%d:%s", groupID, key)] = owner
	c.writes = append(c.writes, handlerWSAffinityWrite{groupID: groupID, key: key, owner: owner})
	return nil
}

func (c *handlerWSAffinityRecordingCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *handlerWSAffinityRecordingCache) DeleteSessionAccountID(_ context.Context, groupID int64, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, fmt.Sprintf("%d:%s", groupID, key))
	return nil
}

func TestOpenAIResponsesWebSocket_CacheOnlyClientPolicyRejectionPublishesNoAffinity(t *testing.T) {
	// This runs the real downstream WS handler and upstream WS exchange. The
	// first OAuth account opts into cross-key sharing but rejects the client's
	// profile. Only the admitted API-key account may appear in any cache write.
	cache := testOpenAIWebSocketClientProfileFallback(t, nil)
	cache.mu.Lock()
	writes := append([]handlerWSAffinityWrite(nil), cache.writes...)
	cache.mu.Unlock()
	require.NotEmpty(t, writes, "the admitted route must still receive ordinary affinity")
	for _, write := range writes {
		require.NotEqualValues(t, 9904, write.owner, "a rejected WS candidate cannot publish ordinary or shared affinity")
		require.False(t, strings.HasPrefix(write.key, "openai:oauth-share:"), "the rejected OAuth candidate cannot leave cross-key state")
	}
}
