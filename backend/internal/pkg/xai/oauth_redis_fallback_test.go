//go:build unit

package xai

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreRedisFallbackIsLimitedToFailedWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisSessionStore(client)
	defer store.Stop()
	session := func(state string) *OAuthSession { return &OAuthSession{State: state, CreatedAt: time.Now()} }

	store.Set("remote", session("remote"))
	require.NoError(t, store.remote.Delete(context.Background(), "remote"))
	_, ok := store.Get("remote")
	require.False(t, ok, "a remote miss must not revive the stale local copy")

	mr.Close()
	store.Set("local-only", session("local"))
	got, ok := store.Get("local-only")
	require.True(t, ok)
	require.Equal(t, "local", got.State)
	require.True(t, store.TryConsumeSession("local-only"))
	require.False(t, store.TryConsumeSession("local-only"))
}

func TestSessionStoreRedisRetainsOIDCBindingAcrossReplicas(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	first, second := NewRedisSessionStore(client), NewRedisSessionStore(client)
	defer first.Stop()
	defer second.Stop()
	want := &OAuthSession{
		State: "state", Nonce: "bound-nonce", Issuer: OAuthIssuer, ClientID: "client",
		CodeVerifier: "pkce-verifier", CreatedAt: time.Now(),
		Identity: outboundidentity.Identity{Preset: "grok", UserAgent: "grok-shell/1.0.45 (linux; x86_64)", Version: "1.0.45"},
	}
	first.Set("cross-replica", want)
	got, ok := second.Get("cross-replica")
	require.True(t, ok)
	require.Equal(t, want.Nonce, got.Nonce)
	require.Equal(t, want.Issuer, got.Issuer)
	require.Equal(t, want.CodeVerifier, got.CodeVerifier)
	require.Equal(t, want.Identity, got.Identity)
	require.True(t, second.TryConsumeSession("cross-replica"))
	require.False(t, first.TryConsumeSession("cross-replica"))
}
