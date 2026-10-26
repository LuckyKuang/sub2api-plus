//go:build unit || !integration

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCNControlSnapshotPreservesSavedBindingAndOwnsProxyEvidence(t *testing.T) {
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	proxyID := int64(17)
	account := &Account{ID: 42, Platform: PlatformKimi, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "captured", "nested": map[string]any{"region": "cn"}},
		ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "captured.example", Port: 8080, ExpiresAt: &expires}}
	snapshot, err := snapshotCNControlAccount(account)
	require.NoError(t, err)
	require.Equal(t, account.ProxyID, snapshot.ProxyID)
	require.Equal(t, account.Proxy, snapshot.Proxy)
	require.NotSame(t, account.ProxyID, snapshot.ProxyID)
	require.NotSame(t, account.Proxy, snapshot.Proxy)
	require.NotSame(t, account.Proxy.ExpiresAt, snapshot.Proxy.ExpiresAt)
	snapshot.Proxy.Host = "mutated.example"
	*snapshot.Proxy.ExpiresAt = expires.Add(time.Hour)
	snapshotNested, ok := snapshot.Credentials["nested"].(map[string]any)
	require.True(t, ok)
	snapshotNested["region"] = "global"
	require.Equal(t, "captured.example", account.Proxy.Host)
	require.Equal(t, expires, *account.Proxy.ExpiresAt)
	originalNested, ok := account.Credentials["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "cn", originalNested["region"])

	account.ProxyID = nil // Management saved an explicit unbinding.
	unbound, err := snapshotCNControlAccount(account)
	require.NoError(t, err)
	require.Nil(t, unbound.ProxyID)
	require.Nil(t, unbound.Proxy, "a stale association cannot recreate the removed binding")
	proxyURL, err := unbound.StrictProxyURL()
	require.NoError(t, err)
	require.Empty(t, proxyURL)
	require.NotNil(t, account.Proxy, "normalization cannot mutate the caller's loaded association")
	require.Equal(t, "captured.example", account.Proxy.Host)
}
