// Package concurrencycache owns the production Redis-backed cache fixture and
// explicit owner-loss controls used by gateway business regressions.
package concurrencycache

import (
	"context"
	"strconv"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// UserOwners exposes only the capacity evidence and loss operations required by
// acceptance tests. Gateway tests do not depend on the persistence client.
type UserOwners struct{ client *redis.Client }

func New(t *testing.T) (service.ConcurrencyCache, *UserOwners) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return repository.NewConcurrencyCache(client, 15, 900), &UserOwners{client: client}
}

func (o *UserOwners) List(ctx context.Context, userID int64) ([]string, error) {
	return o.client.ZRange(ctx, userOwnerKey(userID), 0, -1).Result()
}

func (o *UserOwners) Remove(ctx context.Context, userID int64, owner string) error {
	return o.client.ZRem(ctx, userOwnerKey(userID), owner).Err()
}

func (o *UserOwners) RemoveAll(ctx context.Context, userID int64) error {
	return o.client.Del(ctx, userOwnerKey(userID)).Err()
}

func userOwnerKey(userID int64) string { return "concurrency:user:" + strconv.FormatInt(userID, 10) }
