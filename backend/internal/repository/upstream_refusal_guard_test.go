//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestUpstreamRefusalStore(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer func() { _ = client.Close() }()
	store := NewUpstreamRefusalStore(client)
	ctx := context.Background()
	cfg := service.DefaultUpstreamRefusalGuardConfig()
	cfg.CountWindowSeconds = 30
	cfg.UserCooldownSeconds = 20
	cfg.SessionBlockTTLSeconds = 60
	v := service.RefusalSession{BlockKey: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", UserID: 42, APIKeyID: 7, SessionID: "session", RequestID: "req", CreatedAt: time.Now()}
	for i := int64(1); i <= 3; i++ {
		n, err := store.Record(ctx, v, cfg)
		if err != nil || n != i {
			t.Fatalf("record %d: %d %v", i, n, err)
		}
		if i == 1 {
			mini.FastForward(15 * time.Second)
		}
		if i == 2 {
			users, err := store.ListUsers(ctx)
			if err != nil || len(users) != 1 || users[0].CountTTLSeconds < 29 {
				t.Fatalf("window not refreshed: %+v %v", users, err)
			}
		}
	}
	users, err := store.ListUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Count != 0 || users[0].CooldownRemainingSeconds < 19 {
		t.Fatalf("threshold: %+v %v", users, err)
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "session" {
		t.Fatalf("sessions: %+v %v", sessions, err)
	}
	mini.FastForward(46 * time.Second)
	hit, err := store.HasSession(ctx, v.BlockKey)
	if err != nil || hit {
		t.Fatalf("session TTL: %v %v", hit, err)
	}
	_, err = store.Record(ctx, v, cfg)
	if err != nil {
		t.Fatal(err)
	}
	n, err := store.ClearSession(ctx, v.BlockKey)
	if err != nil || n != 1 {
		t.Fatalf("clear session: %d %v", n, err)
	}
	n, err = store.ClearUser(ctx, 42)
	if err != nil || n != 1 {
		t.Fatalf("clear user: %d %v", n, err)
	}
	_, _ = store.Record(ctx, v, cfg)
	n, err = store.ClearAll(ctx)
	if err != nil || n != 2 {
		t.Fatalf("clear all: %d %v", n, err)
	}
}
