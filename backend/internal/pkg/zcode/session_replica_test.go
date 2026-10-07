//go:build unit

package zcode

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreSingleUseAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	a, b := NewRedisSessionStore(client), NewRedisSessionStore(client)
	defer a.Stop()
	defer b.Stop()
	require.NoError(t, a.Set("shared", &OAuthSession{Provider: ProviderBigModel, ExpiresAt: time.Now().Add(time.Minute)}))
	_, ok := a.Get("shared")
	require.True(t, ok)
	_, ok = b.Get("shared")
	require.True(t, ok)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store := a
			if n%2 == 1 {
				store = b
			}
			if store.TryConsume("shared") {
				accepted.Add(1)
			}
		}(n)
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.False(t, a.TryConsume("shared"))
	require.False(t, b.TryConsume("shared"))
}

func TestSessionStoreRedisFailureCannotUseCachedAuthorization(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	store := NewRedisSessionStore(client)
	defer store.Stop()
	require.NoError(t, store.Set("shared", &OAuthSession{ExpiresAt: time.Now().Add(time.Minute)}))
	_, ok := store.Get("shared")
	require.True(t, ok)
	require.NoError(t, client.Close())
	_, ok = store.Get("shared")
	require.False(t, ok)
	require.False(t, store.TryConsume("shared"))
	require.Error(t, store.Set("new", &OAuthSession{ExpiresAt: time.Now().Add(time.Minute)}))
	_, ok = store.Get("new")
	require.False(t, ok)
}
