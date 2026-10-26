//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnsavedCredentialOwnerSurvivesSnapshotsWithoutCrossingAccountOrJSONBoundary(t *testing.T) {
	id := int64(71)
	original := &Account{Platform: PlatformStepFun, Type: AccountTypeOAuth,
		Credentials: map[string]any{"oauth_provider": PlatformStepFun, "oauth_region": "cn", "access_token": "selected-key"},
		ProxyID:     &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "original.example", Port: 8080},
	}
	frozen, err := snapshotCNControlAccount(original)
	require.NoError(t, err)
	encoded, err := json.Marshal(frozen)
	require.NoError(t, err)
	originalJSON, err := json.Marshal(original)
	require.NoError(t, err)
	require.JSONEq(t, string(originalJSON), string(encoded), "snapshot identity must remain private")
	var decoded Account
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	independent := &Account{Platform: PlatformStepFun, Type: AccountTypeOAuth,
		Credentials: map[string]any{"oauth_provider": PlatformStepFun, "oauth_region": "cn", "access_token": "selected-key"},
		ProxyID:     &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "original.example", Port: 8080},
	}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, original, "selected-key")
	ctx = withRequestCredentialOwner(ctx, original, "selected-key")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.stepfun.com/step_plan/v1/models", nil)
	require.NoError(t, err)
	recordTransmittedRequestCredentialOwner(req, snapshotRequestCredentialAccount(frozen), "selected-key")

	original.Credentials["access_token"] = "later-key"
	original.Proxy.Host = "later.example"
	for _, selected := range []*Account{original, frozen, snapshotRequestCredentialAccount(frozen)} {
		owner, err := requestCredentialAccount(req.Context(), selected)
		require.NoError(t, err, "snapshots of the same unsaved account must keep their acquired owner")
		require.Equal(t, "selected-key", owner.GetCredential("access_token"))
		route, err := requestCredentialOwnerProxyURL(req.Context(), selected)
		require.NoError(t, err)
		require.Equal(t, "http://original.example:8080", route)
	}
	for _, selected := range []*Account{independent, snapshotRequestCredentialAccount(independent), &decoded, snapshotRequestCredentialAccount(&decoded)} {
		owner, err := requestCredentialAccount(req.Context(), selected)
		require.Error(t, err, "identical credentials and JSON copies cannot borrow another unsaved account's evidence")
		require.Nil(t, owner)
	}
}
