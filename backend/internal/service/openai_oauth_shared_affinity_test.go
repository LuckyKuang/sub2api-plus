//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Sharing is opt-in, same-user and policy-scoped. Ordinary routing must retain
// its API-key isolation; identical prompt content alone is not a shared session.
func oauthSharedAffinityRequest(s *OpenAIGatewayService, keyID, userID, groupID int64, session string) (context.Context, string) {
	c := newOAuthSessionPolicyGinContext(keyID, userID, groupID)
	c.Request = c.Request.WithContext(oauthSessionPolicyContext(userID))
	if session != "" {
		c.Request.Header.Set("session-id", session)
	}
	hash := s.GenerateSessionHash(c, []byte(`{"model":"gpt-5.2-codex","input":"same prompt"}`))
	return c.Request.Context(), hash
}

func TestOpenAIOAuthSharedAffinityAcrossAuthorizedKeys(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
	s := &OpenAIGatewayService{cache: &oauthSessionPolicyCache{}, accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}}}
	firstCtx, firstHash := oauthSharedAffinityRequest(s, 101, 9001, 11, "logical-session")
	secondCtx, secondHash := oauthSharedAffinityRequest(s, 202, 9001, 12, "logical-session")
	require.NotEqual(t, firstHash, secondHash, "ordinary routing must remain API-key scoped")
	group := int64(11)
	require.NoError(t, s.bindOpenAIOAuthSharedSession(firstCtx, &group, firstHash, &account, time.Hour))
	got, err := s.getOpenAIOAuthSharedSessionAccountID(secondCtx, secondHash)
	require.NoError(t, err)
	require.Equal(t, int64(77), got, "an explicitly shared session must retain the same account across authorized keys")
	got, err = s.getStickySessionAccountID(secondCtx, int64Ptr(12), secondHash)
	require.NoError(t, err)
	require.Equal(t, int64(77), got, "actual routing lookup must consume the shared affinity")
}

func TestOpenAIOAuthSharedAffinityRejectsChangedPolicyScope(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
	s := &OpenAIGatewayService{cache: &oauthSessionPolicyCache{}, accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}}}
	ctx, hash := oauthSharedAffinityRequest(s, 101, 9001, 11, "logical-session")
	group := int64(11)
	require.NoError(t, s.bindOpenAIOAuthSharedSession(ctx, &group, hash, &account, time.Hour))
	policy, ok := account.Extra[OpenAIOAuthSessionPolicyExtraKey].(map[string]any)
	require.True(t, ok)
	policy["scope_version"] = "scope-replaced"
	got, _ := s.getOpenAIOAuthSharedSessionAccountID(ctx, hash)
	require.Zero(t, got, "an old-policy binding must not become valid in a new sharing scope")
}

func TestOpenAIOAuthSharedAffinityIsolation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		userID       int64
		session      string
		firstSession string
	}{
		{"another user", 9002, "logical-session", "logical-session"},
		{"another conversation", 9001, "other-session", "logical-session"},
		{"content fallback is not a conversation", 9001, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
			s := &OpenAIGatewayService{cache: &oauthSessionPolicyCache{}, accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}}}
			ctx, hash := oauthSharedAffinityRequest(s, 101, 9001, 11, tc.firstSession)
			group := int64(11)
			require.NoError(t, s.bindOpenAIOAuthSharedSession(ctx, &group, hash, &account, time.Hour))
			otherCtx, otherHash := oauthSharedAffinityRequest(s, 202, tc.userID, 12, tc.session)
			got, _ := s.getOpenAIOAuthSharedSessionAccountID(otherCtx, otherHash)
			require.Zero(t, got)
		})
	}
}

func TestOpenAIOAuthSharedAffinityAllExplicitSessionSources(t *testing.T) {
	for _, source := range []string{"header", "cache key", "messages metadata"} {
		t.Run(source, func(t *testing.T) {
			account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
			s := &OpenAIGatewayService{cache: &oauthSessionPolicyCache{}, accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}}}
			request := func(key, group int64) (*gin.Context, string) {
				c := newOAuthSessionPolicyGinContext(key, 9001, group)
				c.Request = c.Request.WithContext(oauthSessionPolicyContext(9001))
				switch source {
				case "header":
					c.Request.Header.Set("session_id", "logical-session")
					return c, s.GenerateExplicitSessionHash(c, nil)
				case "cache key":
					return c, s.GenerateSessionHash(c, []byte(`{"prompt_cache_key":"logical-session"}`))
				default:
					return c, DeriveSessionHashFromSeed(ScopeOpenAISchedulingSeed(c, "logical-session"))
				}
			}
			first, hash := request(101, 11)
			second, otherHash := request(202, 12)
			group := int64(11)
			require.NoError(t, s.bindOpenAIOAuthSharedSession(first.Request.Context(), &group, hash, &account, time.Hour))
			got, err := s.getOpenAIOAuthSharedSessionAccountID(second.Request.Context(), otherHash)
			require.NoError(t, err)
			require.Equal(t, int64(77), got)
		})
	}
}

type oauthSharedStaleSchedulerCache struct {
	SchedulerCache
	account *Account
}

func (c *oauthSharedStaleSchedulerCache) GetAccount(context.Context, int64) (*Account, error) {
	return c.account, nil
}

func TestOpenAIOAuthSharedAffinityPolicyRevocationIgnoresStaleScheduler(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
	stale := snapshotRequestCredentialAccount(&account)
	s := &OpenAIGatewayService{
		cache:             &oauthSessionPolicyCache{},
		accountRepo:       &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &oauthSharedStaleSchedulerCache{account: stale}},
	}
	ctx, hash := oauthSharedAffinityRequest(s, 101, 9001, 11, "logical-session")
	group := int64(11)
	require.NoError(t, s.bindOpenAIOAuthSharedSession(ctx, &group, hash, &account, time.Hour))
	policy, ok := account.Extra[OpenAIOAuthSessionPolicyExtraKey].(map[string]any)
	require.True(t, ok)
	policy["scope_version"] = "revoked-scope"
	otherCtx, otherHash := oauthSharedAffinityRequest(s, 202, 9001, 12, "logical-session")
	got, _ := s.getOpenAIOAuthSharedSessionAccountID(otherCtx, otherHash)
	require.Zero(t, got, "a pending scheduler invalidation cannot revive a revoked sharing policy")
}

func TestOpenAIOAuthSharedAffinityPreservesWSExecutionContext(t *testing.T) {
	s := &OpenAIGatewayService{}
	c := newOAuthSessionPolicyGinContext(101, 9001, 11)
	c.Request = c.Request.WithContext(oauthSessionPolicyContext(9001))
	type reservationKey struct{}
	execution, cancel := context.WithCancel(context.WithValue(c.Request.Context(), reservationKey{}, "held"))
	defer cancel()
	c.Request.Header.Set("session-id", "logical-session")
	hash := s.GenerateSessionHashWithFallback(c, nil, "connection-only-fallback")
	execution = WithOpenAIOAuthSharedAffinity(execution, c)
	require.Equal(t, "held", execution.Value(reservationKey{}))
	require.NotEmpty(t, openAIOAuthSharedAffinityDigest(execution, hash))
	cancel()
	require.ErrorIs(t, execution.Err(), context.Canceled)
}

func TestOpenAIOAuthSharedAffinityRequiresOptInAndExplicitConversation(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
		if explicit {
			account.Extra = nil
		}
		cache := &oauthSessionPolicyCache{}
		s := &OpenAIGatewayService{cache: cache}
		session := ""
		if explicit {
			session = "logical-session"
		}
		ctx, hash := oauthSharedAffinityRequest(s, 101, 9001, 11, session)
		group := int64(11)
		require.NoError(t, s.bindOpenAIOAuthSharedSession(ctx, &group, hash, &account, time.Hour))
		require.Empty(t, cache.values, "neither ordinary accounts nor content-only fallback can publish a shared binding")
	}
}

func TestOpenAIOAuthSharedAffinityCannotPublishDuringUncommittedSelection(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
	cache := &oauthSessionPolicyCache{}
	svc := &OpenAIGatewayService{cache: cache, accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}}}
	ctx, hash := oauthSharedAffinityRequest(svc, 101, 9001, 11, "logical-session")
	selection := context.WithValue(ctx, openAISessionRouteSelectionKey{}, true)
	group := int64(11)
	require.NoError(t, svc.bindOpenAIStickySessionAccount(selection, &group, hash, &account, time.Hour))
	require.Empty(t, cache.values, "a selected candidate is not an admitted route owner and cannot publish a cross-key binding")
	otherCtx, otherHash := oauthSharedAffinityRequest(svc, 202, 9001, 12, "logical-session")
	id, err := svc.getOpenAIOAuthSharedSessionAccountID(otherCtx, otherHash)
	require.ErrorIs(t, err, ErrStickySessionNotFound)
	require.Zero(t, id)
	require.NoError(t, svc.bindOpenAIStickySessionAccount(ctx, &group, hash, &account, time.Hour))
	id, err = svc.getOpenAIOAuthSharedSessionAccountID(otherCtx, otherHash)
	require.NoError(t, err)
	require.Equal(t, int64(77), id, "only the committed phase publishes the authorized shared route")
}

func TestOpenAIOAuthSharedAffinityCacheOnlyWSWaitsForAdmission(t *testing.T) {
	for _, advanced := range []string{"false", "true"} {
		t.Run("advanced="+advanced, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
			account.Credentials = map[string]any{"access_token": "selected-grant", "chatgpt_account_id": "selected-principal"}
			account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
			cache := &oauthSessionPolicyCache{}
			svc := &OpenAIGatewayService{
				cache: cache, cfg: newOpenAIWSV2TestConfig(),
				accountRepo:        &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{77: &account}},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(advanced),
			}
			ctx, hash := oauthSharedAffinityRequest(svc, 101, 9001, 11, "cache-only-ws-session")
			group := int64(11)
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &group, "", hash, "gpt-5.2-codex", nil,
				OpenAIUpstreamTransportResponsesWebsocketV2Ingress, OpenAIEndpointCapabilityResponses, false, false, false, PlatformOpenAI)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.EqualValues(t, 77, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				defer selection.ReleaseFunc()
			}
			require.Empty(t, cache.values, "a cache-only WS candidate must not publish affinity before handler admission")
			admitted := ContextWithSelectionProfitGate(ctx, selection)
			require.NoError(t, svc.BindStickySessionAfterProfitAdmission(admitted, &group, hash, selection.Account.ID))
			otherCtx, otherHash := oauthSharedAffinityRequest(svc, 202, 9001, 12, "cache-only-ws-session")
			owner, err := svc.getOpenAIOAuthSharedSessionAccountID(otherCtx, otherHash)
			require.NoError(t, err)
			require.EqualValues(t, 77, owner, "successful admission must publish the authorized cross-key owner")
		})
	}
}
