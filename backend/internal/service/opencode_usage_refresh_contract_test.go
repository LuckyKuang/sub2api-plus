//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type openCodeUsageSnapshotRepo struct {
	AccountRepository
	openCodeGoUsageRepository
	snapshot *OpenCodeGoUsageSnapshot
}

func (r *openCodeUsageSnapshotRepo) UpdateOpenCodeGoUsageSnapshot(_ context.Context, _ *Account, snapshot *OpenCodeGoUsageSnapshot) error {
	r.snapshot = snapshot
	return nil
}

func TestOpenCodeUsageRetryAfterHasAbsoluteDayCeiling(t *testing.T) {
	for _, hint := range []time.Duration{-time.Hour, 0, 3 * time.Hour, 24 * time.Hour, 365 * 24 * time.Hour} {
		for i := 0; i < 100; i++ {
			delay := nextOpenCodeGoUsageDelay(30, 20, hint)
			require.GreaterOrEqual(t, delay, time.Minute)
			require.LessOrEqual(t, delay, 24*time.Hour, "jitter must not exceed the absolute ceiling")
			if hint >= 24*time.Hour {
				require.Equal(t, 24*time.Hour, delay)
			} else if hint > 0 {
				require.GreaterOrEqual(t, delay, hint)
			}
		}
	}
}

func TestOpenCodeUsageForbiddenDoesNotClaimNoSubscription(t *testing.T) {
	account := &Account{ID: 11, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key", outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok", Version: "3.1.4"}}}
	repo := &openCodeUsageSnapshotRepo{}
	upstream := &commandCodeAlphaUpstream{responses: map[string]commandCodeAlphaResponse{
		"/api/usage": {status: http.StatusForbidden, body: `{"error":"forbidden"}`},
	}}
	transport := &providerIdentityUpstream{HTTPUpstream: upstream, check: func(req *http.Request) {
		require.Equal(t, "grok-shell/3.1.4 (linux; x86_64)", req.Header.Get("User-Agent"))
		identity, ok := outboundidentity.FromContext(req.Context())
		require.True(t, ok)
		require.Equal(t, account.ID, identity.AccountID)
		// Test the actual configured usage URL without duplicating its path.
		upstream.responses[req.URL.Path] = commandCodeAlphaResponse{status: http.StatusForbidden, body: `{"error":"forbidden"}`}
	}}
	svc := NewOpenCodeGoUsageService(repo, transport, nil)
	t.Cleanup(svc.Stop)
	snapshot, err := svc.refreshLoadedAccount(context.Background(), account, 30)
	require.NoError(t, err)
	require.Equal(t, 403, snapshot.HTTPStatus)
	require.Equal(t, OpenCodeGoUsageStatusFailed, snapshot.Status)
	require.Equal(t, "forbidden", snapshot.LastError)
	require.Same(t, snapshot, repo.snapshot)
}
