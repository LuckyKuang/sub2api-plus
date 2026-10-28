//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Discovery is a control-plane observation of one acquired grant and saved
// route. A snapshot CAS conflict cannot authorize adopting a replacement grant.
func TestGrokCatalogPersistenceRejectsOwnerChange(t *testing.T) {
	for _, duringRetry := range []bool{false, true} {
		for _, change := range []string{"credential", "proxy", "type", "status", "proxy_status", "proxy_expiry"} {
			t.Run(change+"/retry="+map[bool]string{false: "false", true: "true"}[duringRetry], func(t *testing.T) {
				account := grokOAuthModelSyncTestAccount("")
				proxyID := int64(41)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080, Status: StatusActive}
				repo := newGrokCatalogMemoryRepo(account)
				owner, err := snapshotCNControlAccount(account)
				require.NoError(t, err)
				mutate := func(current *Account) {
					switch change {
					case "credential":
						current.Credentials["access_token"] = "replacement-grant"
					case "proxy":
						proxyID := int64(42)
						current.ProxyID = &proxyID
					case "type":
						current.Type = AccountTypeAPIKey
					case "status":
						current.Status = "disabled"
					case "proxy_status":
						current.Proxy.Status = "inactive"
					case "proxy_expiry":
						expires := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
						current.Proxy.ExpiresAt = &expires
					}
				}
				if duringRetry {
					repo.conflictsRemaining = 1
					repo.onConflict = mutate
				} else {
					mutate(repo.account)
				}
				scope := grokCatalogPersistScope(t, account.ID)
				observation := grokCatalogPersistSuccess(scope, "2026-10-28T01:00:00Z", "2026-10-28T01:00:01Z", "grok-observed")
				observation.owner = owner
				_, err = PersistGrokCatalogObservation(context.Background(), repo, owner, observation)
				require.ErrorIs(t, err, errGrokCatalogAccountMismatch)
				require.Nil(t, parseGrokObservedModels(repo.account.Extra))
				require.Zero(t, repo.updateExtraCalls)
			})
		}
	}
}

func TestGrokCatalogObservationCapturesAcquiredOwner(t *testing.T) {
	selected := grokOAuthModelSyncTestAccount("https://old.example.test")
	acquired, err := snapshotCNControlAccount(selected)
	require.NoError(t, err)
	acquired.Credentials["access_token"] = "acquired-grant"
	acquired.Credentials["base_url"] = "https://new.example.test"
	ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), acquired, "acquired-grant")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://new.example.test/v1/models", nil)
	require.NoError(t, err)
	observation := buildGrokCatalogObservation(selected, req, time.Now(), []string{"grok-observed"}, nil)
	require.Equal(t, "acquired-grant", observation.owner.GetCredential("access_token"))
	require.Equal(t, "https://new.example.test/v1/models", observation.Attempt.Scope.Endpoint)
	acquired.Credentials["access_token"] = "later-grant"
	require.Equal(t, "acquired-grant", observation.owner.GetCredential("access_token"))
}

func TestGrokCatalogFreshnessIsBoundToGrantAndProxy(t *testing.T) {
	account := grokOAuthModelSyncTestAccount("")
	scope := bindGrokCatalogScopeOwner(grokCatalogPersistScope(t, account.ID), account)
	now := time.Now().UTC()
	snapshot := &GrokObservedModelsSnapshot{Models: []string{"grok-observed"}, FetchedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), Scope: scope}
	require.True(t, grokObservedSnapshotFresh(snapshot, scope, now))
	rotated, err := snapshotCNControlAccount(account)
	require.NoError(t, err)
	rotated.Credentials["access_token"] = "replacement-grant"
	newScope := bindGrokCatalogScopeOwner(grokCatalogPersistScope(t, account.ID), rotated)
	require.False(t, grokObservedSnapshotFresh(snapshot, newScope, now))
	require.Equal(t, grokModelDiagnosticStateSourceChanged, EvaluateGrokModelDiagnostics(rotated, newScope, snapshot, now).State)
	proxyID := int64(9)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example.test", Port: 8080}
	proxyScope := bindGrokCatalogScopeOwner(grokCatalogPersistScope(t, account.ID), account)
	require.False(t, grokObservedSnapshotFresh(snapshot, proxyScope, now))
	proxySnapshot := *snapshot
	proxySnapshot.Scope = proxyScope
	require.True(t, grokObservedSnapshotFresh(&proxySnapshot, proxyScope, now))
	account.Proxy.Status = "inactive"
	statusScope := bindGrokCatalogScopeOwner(grokCatalogPersistScope(t, account.ID), account)
	require.False(t, grokObservedSnapshotFresh(&proxySnapshot, statusScope, now))
	account.Proxy.Status = ""
	expires := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	account.Proxy.ExpiresAt = &expires
	expiryScope := bindGrokCatalogScopeOwner(grokCatalogPersistScope(t, account.ID), account)
	require.False(t, grokObservedSnapshotFresh(&proxySnapshot, expiryScope, now))
	legacy := *scope
	legacy.OwnerFingerprint = ""
	snapshot.Scope = &legacy
	require.False(t, grokObservedSnapshotFresh(snapshot, scope, now))
}
