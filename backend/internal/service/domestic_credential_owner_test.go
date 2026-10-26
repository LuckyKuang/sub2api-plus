//go:build unit

package service

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDomesticConversationLoginEpochAndAPIKeyReplacement(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformMiniMax, PlatformZhipu} {
		t.Run(platform, func(t *testing.T) {
			ctx := WithOutboundIdentityScope(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), nil)
			outboundIdentityScopeFromContext(ctx).conversation = "tenant-and-explicit-conversation"
			owner := &Account{ID: 41, Platform: platform, Type: AccountTypeOAuth, Credentials: map[string]any{
				domesticOAuthLoginIDCredential: "login-A", "access_token": "access-A", "refresh_token": "refresh-A",
			}}
			first, err := nativeConversationOwnerSession(ctx, owner)
			require.NoError(t, err)
			rotated := snapshotRequestCredentialAccount(owner)
			rotated.Credentials["access_token"], rotated.Credentials["refresh_token"] = "access-B", "refresh-B"
			afterRefresh, err := nativeConversationOwnerSession(ctx, rotated)
			require.NoError(t, err)
			require.Equal(t, first, afterRefresh, "refreshing both tokens does not create a new authorization")
			relinked := snapshotRequestCredentialAccount(rotated)
			relinked.Credentials[domesticOAuthLoginIDCredential] = "login-B"
			afterRelink, err := nativeConversationOwnerSession(ctx, relinked)
			require.NoError(t, err)
			require.NotEqual(t, first, afterRelink, "relinking the same account row creates a new owner scope")
			owner.Type = AccountTypeAPIKey
			owner.Credentials = map[string]any{"api_key": "key-A"}
			first, err = nativeConversationOwnerSession(ctx, owner)
			require.NoError(t, err)
			owner.Credentials["api_key"] = "key-B"
			replaced, err := nativeConversationOwnerSession(ctx, owner)
			require.NoError(t, err)
			require.NotEqual(t, first, replaced, "replacement API key must not inherit the old session")
		})
	}
}

func TestDomesticLegacyLoginScopeSurvivesRotationUntilRelink(t *testing.T) {
	owner := &Account{ID: 41, Platform: PlatformMiniMax, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old", "refresh_token": "old-refresh"}}
	first := domesticCredentialOwnerScope(context.Background(), owner)
	owner.Credentials["access_token"], owner.Credentials["refresh_token"] = "new", "new-refresh"
	require.Equal(t, first, domesticCredentialOwnerScope(context.Background(), owner))
	owner.Credentials[domesticOAuthLoginIDCredential] = "new-login"
	require.NotEqual(t, first, domesticCredentialOwnerScope(context.Background(), owner))
}

func TestDomesticAmbiguousCredentialEvidenceStopsPreparation(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu, PlatformStepFun} {
		owner := &Account{ID: 41, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "selected-key"}}
		ctx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), owner, "different-transmitted-key")
		require.Nil(t, domesticCredentialOwnerAccount(ctx, owner))
		id, err := nativeConversationOwnerSession(ctx, owner)
		require.Empty(t, id)
		require.Error(t, err)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://native.invalid/v1/messages", bytes.NewBufferString(`{"messages":[]}`))
		require.NoError(t, err)
		prepareAccountOutboundRequest(request, owner)
		require.Error(t, OutboundPreparationError(request.Context()), platform)
		require.Empty(t, request.Header.Get("x-request-id"))
		require.Empty(t, request.Header.Get("x-mavis-session-id"))
		require.Empty(t, request.Header.Get("x-deepseek-harness-session-id"))
	}
}

func TestDomesticOwnerScopeUsesTransmittedGrantAndShadowParent(t *testing.T) {
	owner := &Account{ID: 41, Platform: PlatformMiniMax, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "token-A", domesticOAuthLoginIDCredential: "login-A"}}
	parentID := owner.ID
	shadow := &Account{ID: 42, ParentAccountID: &parentID, Platform: PlatformMiniMax, Type: AccountTypeOAuth}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, "token-A")
	ctx = withRequestCredentialOwner(ctx, shadow, "token-A")
	require.Equal(t, domesticCredentialOwnerScope(context.Background(), owner), domesticCredentialOwnerScope(ctx, shadow))
	rotated := snapshotRequestCredentialAccount(owner)
	rotated.Credentials["access_token"] = "token-B"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://native.invalid/v1/messages", nil)
	require.NoError(t, err)
	recordTransmittedRequestCredentialOwner(request, rotated, "token-B")
	// Later mutation of the selected row cannot rewrite the already sent owner.
	owner.Credentials[domesticOAuthLoginIDCredential] = "login-B"
	require.Equal(t, domesticCredentialOwnerScope(context.Background(), rotated), domesticCredentialOwnerScope(request.Context(), shadow))
	require.NotEqual(t, domesticCredentialOwnerScope(context.Background(), owner), domesticCredentialOwnerScope(request.Context(), shadow))
}

func TestCNOAuthAuthorizationCreatesEpochAndRefreshRejectsRelink(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		flow := &cnoauth.Flow{Platform: platform, Region: "cn"}
		grant := &cnoauth.Grant{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
		first := &Account{ID: 41, Platform: platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(flow, grant)}
		second := &Account{ID: 41, Platform: platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(flow, grant)}
		id, err := uuid.Parse(first.GetCredential(domesticOAuthLoginIDCredential))
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), id.Version())
		require.NotEqual(t, first.GetCredential(domesticOAuthLoginIDCredential), second.GetCredential(domesticOAuthLoginIDCredential))
		require.False(t, sameCNOAuthOwner(first, second), platform)
		if platform == PlatformKimi || platform == PlatformMiniMax {
			refresher := &CNTokenRefresher{platform: platform, owner: first}
			require.False(t, refresher.CanRefresh(second), "an old attempt cannot adopt a new grant on the same row")
			rotated := snapshotRequestCredentialAccount(first)
			rotated.Credentials["access_token"], rotated.Credentials["refresh_token"] = "new-access", "new-refresh"
			require.True(t, refresher.CanRefresh(rotated))
		}
	}
}
