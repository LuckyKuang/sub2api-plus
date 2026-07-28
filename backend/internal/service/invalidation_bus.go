package service

import "context"

// InvalidationBus distributes best-effort cache invalidation events across API
// instances. Infrastructure implementations own their transport details.
type InvalidationBus interface {
	Publish(ctx context.Context, channel string) error
	Subscribe(ctx context.Context, channel string, onMessage func()) error
}
