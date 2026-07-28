package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type redisInvalidationBus struct {
	rdb *redis.Client
}

func NewRedisInvalidationBus(rdb *redis.Client) service.InvalidationBus {
	if rdb == nil {
		return nil
	}
	return &redisInvalidationBus{rdb: rdb}
}

func (b *redisInvalidationBus) Publish(ctx context.Context, channel string) error {
	if b == nil || b.rdb == nil {
		return errors.New("redis invalidation bus is unavailable")
	}
	return b.rdb.Publish(ctx, channel, "refresh").Err()
}

func (b *redisInvalidationBus) Subscribe(ctx context.Context, channel string, onMessage func()) error {
	if b == nil || b.rdb == nil {
		return errors.New("redis invalidation bus is unavailable")
	}
	subscription := b.rdb.Subscribe(ctx, channel)
	defer func() {
		_ = subscription.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-subscription.Channel():
			if !ok {
				return nil
			}
			if message != nil && onMessage != nil {
				onMessage()
			}
		}
	}
}
