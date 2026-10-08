//go:build unit

package cnoauth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionStoresSerializeAndExpire(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{true: "redis", false: "memory"}[remote], func(t *testing.T) {
			var client *redis.Client
			var server *miniredis.Miniredis
			if remote {
				server = miniredis.RunT(t)
				client = redis.NewClient(&redis.Options{Addr: server.Addr()})
				t.Cleanup(func() { _ = client.Close() })
			}
			s := NewStore(client)
			other := s
			if remote {
				other = NewStore(client)
			}
			ctx := context.Background()
			id, err := s.Create(ctx, &Session{OwnerID: 1, Flow: &Flow{Platform: "kimi", ExpiresAt: time.Now().Add(time.Minute)}})
			require.NoError(t, err)
			var wg sync.WaitGroup
			var writes atomic.Int32
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = other.Update(ctx, id, func(ctx context.Context, v *Session) error {
						_, ok := ctx.Deadline()
						if !ok {
							return errors.New("missing bounded context")
						}
						if v.Committing {
							return ErrBusy
						}
						v.Committing = true
						writes.Add(1)
						return nil
					})
				}()
			}
			wg.Wait()
			require.Equal(t, int32(1), writes.Load())
			require.NoError(t, s.Update(ctx, id, func(_ context.Context, v *Session) error { require.True(t, v.Committing); return nil }))
			require.Error(t, s.Update(ctx, id, func(_ context.Context, v *Session) error { v.OwnerID = 999; return errors.New("abort") }))
			require.NoError(t, other.Update(ctx, id, func(_ context.Context, v *Session) error { require.EqualValues(t, 1, v.OwnerID); return nil }))
			if remote {
				server.FastForward(2 * time.Minute)
			} else {
				require.NoError(t, s.Update(ctx, id, func(_ context.Context, v *Session) error { v.Flow.ExpiresAt = time.Now().Add(-time.Second); return nil }))
			}
			require.ErrorIs(t, s.Update(ctx, id, func(context.Context, *Session) error { t.Fatal("expired callback executed"); return nil }), ErrSession)
		})
	}
}
func TestRedisFailureNeverFallsBackToLocal(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	defer func() { _ = client.Close() }()
	store := NewStore(client)
	id, err := store.Create(context.Background(), &Session{Flow: &Flow{ExpiresAt: time.Now().Add(time.Minute)}})
	require.NoError(t, err)
	server.Close()
	err = store.Update(context.Background(), id, func(context.Context, *Session) error { t.Fatal("must fail closed"); return nil })
	require.Error(t, err)
}
