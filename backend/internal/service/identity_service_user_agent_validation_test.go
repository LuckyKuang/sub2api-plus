package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type stubIdentityCache struct {
	fingerprint *Fingerprint
	setCalls    int
	lastSet     *Fingerprint
}

func (s *stubIdentityCache) GetFingerprint(context.Context, int64) (*Fingerprint, error) {
	if s.fingerprint == nil {
		return nil, nil
	}
	clone := *s.fingerprint
	return &clone, nil
}
func (s *stubIdentityCache) SetFingerprint(_ context.Context, _ int64, fp *Fingerprint) error {
	s.setCalls++
	clone := *fp
	s.lastSet, s.fingerprint = &clone, &clone
	return nil
}
func (s *stubIdentityCache) GetMaskedSessionID(context.Context, int64) (string, error) {
	return "", nil
}
func (s *stubIdentityCache) SetMaskedSessionID(context.Context, int64, string) error { return nil }
func headersWithUA(ua string) http.Header                                            { return http.Header{"User-Agent": []string{ua}} }

func TestGetOrCreateFingerprintUsesTrustedIdentity(t *testing.T) {
	identity, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "claude", Version: "2.9.1"})
	require.NoError(t, err)
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	for _, cached := range []*Fingerprint{nil, {ClientID: "existing-device", UserAgent: "claude-cli/999.0.0-local (undefined, cli)", StainlessOS: "inbound-os", UpdatedAt: time.Now().Unix()}} {
		cache := &stubIdentityCache{fingerprint: cached}
		svc := NewIdentityService(cache)
		inbound := headersWithUA("claude-cli/999.0.0 (external, cli)")
		inbound.Set("X-Stainless-OS", "inbound-os")
		fp, err := svc.GetOrCreateFingerprint(ctx, 42, inbound)
		require.NoError(t, err)
		require.Equal(t, identity.UserAgent, fp.UserAgent)
		require.Equal(t, "Linux", fp.StainlessOS)
		require.NotEmpty(t, fp.ClientID)
		if cached != nil {
			require.Equal(t, cached.ClientID, fp.ClientID)
		}
		require.Equal(t, 1, cache.setCalls)
		again, err := svc.GetOrCreateFingerprint(ctx, 42, headersWithUA("curl/8.0"))
		require.NoError(t, err)
		require.Equal(t, fp, again)
		require.Equal(t, 1, cache.setCalls, "unchanged identity must not churn the cache")
	}
}

func TestGetOrCreateFingerprintFollowsConfiguredVersionWithoutChangingDevice(t *testing.T) {
	cache := &stubIdentityCache{fingerprint: &Fingerprint{ClientID: "device", UserAgent: "claude-cli/2.9.2 (external, cli)"}}
	svc := NewIdentityService(cache)
	identity, err := buildOutboundIdentity(OutboundIdentitySelection{Preset: "claude", Version: "2.9.1"})
	require.NoError(t, err)
	fp, err := svc.GetOrCreateFingerprint(outboundidentity.WithIdentity(context.Background(), identity), 42, nil)
	require.NoError(t, err)
	require.Equal(t, identity.UserAgent, fp.UserAgent, "old learned versions must not outrank configured identity")
	require.Equal(t, "device", fp.ClientID)
}
