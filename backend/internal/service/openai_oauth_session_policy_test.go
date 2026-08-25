package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

type oauthSessionPolicyCache struct {
	values           map[string]int64
	setErrorsByGroup map[int64]error
	getErrorsByKey   map[string]error
}

type oauthSessionPolicyAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *oauthSessionPolicyAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	account, ok := r.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account not found")
	}
	return account, nil
}

func (r *oauthSessionPolicyAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() && openAIStickyAccountMatchesGroup(account, &groupID) {
			accounts = append(accounts, *account)
		}
	}
	return accounts, nil
}

func (r *oauthSessionPolicyAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			accounts = append(accounts, *account)
		}
	}
	return accounts, nil
}

func (r *oauthSessionPolicyAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() && openAIStickyAccountMatchesGroup(account, nil) {
			accounts = append(accounts, *account)
		}
	}
	return accounts, nil
}

func (r *oauthSessionPolicyAccountRepo) ListOpenAISessionPolicyDiagnosticCandidates(_ context.Context) ([]Account, error) {
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == PlatformOpenAI {
			accounts = append(accounts, *account)
		}
	}
	return accounts, nil
}

func (c *oauthSessionPolicyCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *oauthSessionPolicyCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	if err := c.getErrorsByKey[c.key(groupID, sessionHash)]; err != nil {
		return 0, err
	}
	accountID, ok := c.values[c.key(groupID, sessionHash)]
	if !ok {
		return 0, ErrGatewayCacheMiss
	}
	return accountID, nil
}

func (c *oauthSessionPolicyCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	if err := c.setErrorsByGroup[groupID]; err != nil {
		return err
	}
	if c.values == nil {
		c.values = make(map[string]int64)
	}
	c.values[c.key(groupID, sessionHash)] = accountID
	return nil
}

func newOpenAIOAuthSessionPolicyAccount(id int64, groupIDs ...int64) Account {
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    append([]int64(nil), groupIDs...),
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": append([]int64(nil), groupIDs...),
				"scope_version":     "scope-a",
			},
		},
	}
}

func TestOpenAIOAuthSessionPolicy_APIKeyAccountsIgnoreLegacyPolicyData(t *testing.T) {
	groupID := int64(88001)
	account := &Account{
		ID:       88002,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{groupID},
				"scope_version":     "legacy-data",
			},
		},
	}

	require.False(t, account.IsOpenAIOAuthSessionPolicyApplicable())
	require.False(t, account.IsOpenAIOAuthSessionSharingEnabled())
	require.True(t, account.IsOpenAIOAuthSessionGroupAllowed(nil))
	require.True(t, account.IsOpenAIOAuthSessionGroupAllowed(&groupID))
	require.True(t, openAIOAuthSessionPolicyAllowsSchedulingGroup(account, nil))
	require.True(t, openAIOAuthSessionPolicyAllowsSchedulingGroup(account, &groupID))

	_, err := normalizeOpenAIOAuthSessionPolicyExtra(nil, PlatformOpenAI, AccountTypeAPIKey, account.Extra, []int64{groupID})
	require.Error(t, err, "the management write path must reject OAuth session policy for API-key accounts")
	require.True(t, infraerrors.IsBadRequest(err), "the management endpoint must expose this validation failure as a client error")
	require.Equal(t, "OPENAI_OAUTH_SESSION_POLICY_UNSUPPORTED_ACCOUNT", infraerrors.Reason(err))
}

func TestOpenAIOAuthSessionPolicy_APIKeyLegacyPolicyKeepsSessionAndResponseBindingsLocal(t *testing.T) {
	groupID := int64(88001)
	otherAPIKeyID := int64(88003)
	account := &Account{
		ID:          88002,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{groupID},
				"scope_version":     "legacy-data",
			},
		},
	}
	service := &OpenAIGatewayService{}

	first, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(88002, 9001, groupID), account, "session-1")
	require.NoError(t, err)
	second, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(otherAPIKeyID, 9001, groupID), account, "session-1")
	require.NoError(t, err)
	require.NotEqual(t, first, second, "API-key accounts must retain API-key-scoped session isolation")

	cache := &oauthSessionPolicyCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{*account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}
	require.NoError(t, store.BindResponseAccount(context.Background(), groupID, "resp-api-key-local", account.ID, time.Hour))
	// This response ID also exists in the OAuth shared namespace but belongs to
	// a different user. The local API-key binding must win: OAuth sharing policy
	// cannot deny, redirect, or otherwise affect an API-key continuation.
	cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, openAIOAuthSharedResponseOwnerCacheKey("resp-api-key-local"))] = 9002

	selection, err := svc.SelectAccountByPreviousResponseID(
		oauthSessionPolicyContext(9001), &groupID, "resp-api-key-local", "gpt-5.1", nil, false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection, "legacy OAuth policy data must not block an API-key response continuation")
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIOAuthSessionPolicy_APIKeySelectionIgnoresForeignSharedPreviousResponse(t *testing.T) {
	groupID := int64(88004)
	account := &Account{
		ID:          88005,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &oauthSessionPolicyCache{values: make(map[string]int64)}
	responseID := "resp-oauth-namespace-collision"
	cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, openAIOAuthSharedResponseOwnerCacheKey(responseID))] = 9002
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{*account}},
		cache:              cache,
		cfg:                newOpenAIWSV2TestConfig(),
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(
		oauthSessionPolicyContext(9001),
		&groupID,
		responseID,
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection, "a foreign OAuth shared response must not reject an API-key selection")
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIOAuthSessionPolicy_SharedOAuthRejectionFallsBackToAPIKey(t *testing.T) {
	groupID := int64(88006)
	responseID := "resp-shared-oauth-rejected"
	oauthAccount := newOpenAIOAuthSessionPolicyAccount(88007, groupID)
	oauthAccount.Priority = 1
	apiKeyAccount := Account{
		ID:          88008,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    2,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &oauthSessionPolicyCache{values: make(map[string]int64)}
	cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, openAIOAuthSharedResponseOwnerCacheKey(responseID))] = 9002
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{oauthAccount, apiKeyAccount}},
		cache:              cache,
		cfg:                newOpenAIWSV2TestConfig(),
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(
		oauthSessionPolicyContext(9001),
		&groupID,
		responseID,
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportResponsesWebsocketV2,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		false,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, apiKeyAccount.ID, selection.Account.ID, "OAuth sharing rejection must not prevent an API-key fallback")
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func (c *oauthSessionPolicyCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c *oauthSessionPolicyCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.values, c.key(groupID, sessionHash))
	return nil
}

func (c *oauthSessionPolicyCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

func (c *oauthSessionPolicyCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (c *oauthSessionPolicyCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (c *oauthSessionPolicyCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

func newOAuthSessionPolicyGinContext(apiKeyID, userID, groupID int64) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: apiKeyID, UserID: userID, GroupID: &groupID})
	return c
}

func oauthSessionPolicyContext(userID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.UserID, userID)
}

func TestOpenAIOAuthSessionPolicySharesUpstreamSessionOnlyWithinAllowedGroups(t *testing.T) {
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{11, 12},
				"scope_version":     "scope-a",
			},
		},
	}
	service := &OpenAIGatewayService{}

	first, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(101, 9001, 11), account, "session-1")
	require.NoError(t, err)
	second, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(202, 9001, 12), account, "session-1")
	require.NoError(t, err)
	require.Equal(t, first, second)

	_, err = service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(303, 9001, 13), account, "session-1")
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)

	differentAccount := *account
	differentAccount.ID = 78
	different, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(404, 9001, 11), &differentAccount, "session-1")
	require.NoError(t, err)
	require.NotEqual(t, first, different)
}

func TestOpenAIOAuthSessionPolicySeparatesUsersAndRequiresIdentity(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11)
	service := &OpenAIGatewayService{}

	first, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(101, 9001, 11), &account, "session-1")
	require.NoError(t, err)
	second, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(202, 9002, 11), &account, "session-1")
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	_, err = service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(303, 0, 11), &account, "session-1")
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSessionPolicySparkShadowUsesCredentialScopeWithinUser(t *testing.T) {
	parent := newOpenAIOAuthSessionPolicyAccount(77, 11)
	shadow := parent
	shadow.ID = 78
	shadow.ParentAccountID = &parent.ID
	service := &OpenAIGatewayService{}
	c := newOAuthSessionPolicyGinContext(101, 9001, 11)

	parentSession, err := service.resolveOpenAIUpstreamSessionID(c, &parent, "session-1")
	require.NoError(t, err)
	shadowSession, err := service.resolveOpenAIUpstreamSessionID(c, &shadow, "session-1")
	require.NoError(t, err)
	require.Equal(t, parentSession, shadowSession)
}

func TestOpenAIOAuthSessionPolicyDisabledKeepsAPIKeyIsolation(t *testing.T) {
	account := &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	service := &OpenAIGatewayService{}

	first, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(101, 9001, 11), account, "session-1")
	require.NoError(t, err)
	second, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(202, 9001, 11), account, "session-1")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestOpenAIOAuthSessionPolicyIsAppliedByAllOutboundBuilders(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11, 12)
	account.Credentials = map[string]any{
		"access_token":       "oauth-token",
		"chatgpt_account_id": "chatgpt-account",
	}
	service := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.6","stream":true,"prompt_cache_key":"shared-session","input":"hello"}`)

	ordinaryContext := newOAuthSessionPolicyGinContext(101, 9001, 11)
	ordinaryContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	ordinary, err := service.buildUpstreamRequest(
		context.Background(), ordinaryContext, &account, body, "oauth-token", true, "shared-session", true,
	)
	require.NoError(t, err)

	passthroughContext := newOAuthSessionPolicyGinContext(202, 9001, 12)
	passthroughContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	passthrough, err := service.buildUpstreamRequestOpenAIPassthrough(
		context.Background(), passthroughContext, &account, body, "oauth-token",
	)
	require.NoError(t, err)

	wsContext := newOAuthSessionPolicyGinContext(303, 9001, 11)
	wsContext.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	wsContext.Request.Header.Set("conversation_id", "shared-session")
	wsHeaders, _, err := service.buildOpenAIWSHeaders(
		context.Background(),
		wsContext,
		&account,
		"oauth-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true,
		"",
		"",
		"shared-session",
		"gpt-5.6",
		"",
	)
	require.NoError(t, err)

	namespacedExpected, err := service.resolveOpenAIUpstreamSessionID(ordinaryContext, &account, "shared-session")
	require.NoError(t, err)
	expected := generateSessionUUID(namespacedExpected)
	require.Equal(t, expected, ordinary.Header.Get("session_id"))
	require.Equal(t, expected, ordinary.Header.Get(codexSessionIDHeader))
	require.Equal(t, expected, ordinary.Header.Get("conversation_id"))
	require.Equal(t, expected, passthrough.Header.Get("session_id"))
	require.Equal(t, expected, passthrough.Header.Get(codexSessionIDHeader))
	require.Equal(t, expected, passthrough.Header.Get("conversation_id"))
	require.Equal(t, expected, wsHeaders.Get("session_id"))
	require.Equal(t, expected, wsHeaders.Get(codexSessionIDHeader))
	require.Equal(t, namespacedExpected, wsHeaders.Get("conversation_id"))
}

func TestOpenAIOAuthSessionPolicyOutboundBuildersRejectUnauthorizedGroup(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11)
	account.Credentials = map[string]any{
		"access_token":       "oauth-token",
		"chatgpt_account_id": "chatgpt-account",
	}
	service := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.6","prompt_cache_key":"blocked-session","input":"hello"}`)

	buildContext := func(method string) *gin.Context {
		c := newOAuthSessionPolicyGinContext(101, 9001, 13)
		c.Request = httptest.NewRequest(method, "/v1/responses", bytes.NewReader(body))
		return c
	}

	_, err := service.buildUpstreamRequest(
		context.Background(), buildContext(http.MethodPost), &account, body, "oauth-token", true, "blocked-session", true,
	)
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)

	_, err = service.buildUpstreamRequestOpenAIPassthrough(
		context.Background(), buildContext(http.MethodPost), &account, body, "oauth-token",
	)
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)

	_, _, err = service.buildOpenAIWSHeaders(
		context.Background(),
		buildContext(http.MethodGet),
		&account,
		"oauth-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true,
		"",
		"",
		"blocked-session",
		"gpt-5.6",
		"",
	)
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSessionPolicyInvalidConfigurationFailsClosed(t *testing.T) {
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{},
			},
		},
	}
	service := &OpenAIGatewayService{}

	_, err := service.resolveOpenAIUpstreamSessionID(newOAuthSessionPolicyGinContext(101, 9001, 11), account, "session-1")
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSessionPolicySharedStickyBinding(t *testing.T) {
	cache := &oauthSessionPolicyCache{}
	service := &OpenAIGatewayService{cache: cache}
	groupID := int64(11)
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{11, 12},
				"scope_version":     "scope-a",
			},
		},
	}

	ctx := oauthSessionPolicyContext(9001)
	require.NoError(t, service.bindOpenAIOAuthSharedSession(ctx, &groupID, "sticky", account, time.Hour))
	accountID, err := service.getOpenAIOAuthSharedSessionAccountID(ctx, "sticky")
	require.NoError(t, err)
	require.Equal(t, account.ID, accountID)
}

func TestOpenAIOAuthSharedStickyWriteFailureDoesNotBlockAccountUse(t *testing.T) {
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)
	cache := &oauthSessionPolicyCache{
		setErrorsByGroup: map[int64]error{
			openAIOAuthSharedSessionCacheGroupID: fmt.Errorf("shared Redis unavailable"),
		},
	}
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: &account},
		},
	}

	ctx := oauthSessionPolicyContext(9001)
	require.NoError(t, service.BindStickySession(ctx, &groupID, "sticky-degraded", account.ID))
	accountID, err := cache.GetSessionAccountID(context.Background(), groupID, service.openAISessionCacheKey("sticky-degraded"))
	require.NoError(t, err)
	require.Equal(t, account.ID, accountID)
}

func TestOpenAIOAuthSharedResponseWriteFailureDoesNotCreateLocalBypass(t *testing.T) {
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)
	cache := &oauthSessionPolicyCache{
		setErrorsByGroup: map[int64]error{
			openAIOAuthSharedSessionCacheGroupID: errors.New("shared Redis unavailable"),
		},
	}
	service := &OpenAIGatewayService{cache: cache}
	store := NewOpenAIWSStateStore(cache)
	ctx := oauthSessionPolicyContext(9001)

	err := service.bindOpenAIResponseAccount(ctx, store, groupID, &account, "resp_bind_failure", time.Hour)
	require.Error(t, err)
	accountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_bind_failure")
	require.NoError(t, getErr)
	require.Zero(t, accountID)
}

func TestOpenAIOAuthSessionPolicyLegacySelectionWritesSharedStickyBinding(t *testing.T) {
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)
	cache := &oauthSessionPolicyCache{}
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: groupAwareStubOpenAIAccountRepo{
			stubOpenAIAccountRepo{accounts: []Account{account}},
		},
	}

	ctx := oauthSessionPolicyContext(9001)
	selected, err := service.SelectAccountForModelWithExclusions(ctx, &groupID, "legacy-selection", "", nil)
	require.NoError(t, err)
	require.Equal(t, account.ID, selected.ID)
	requireOpenAIOAuthStickyBindings(t, service, cache, 9001, groupID, "legacy-selection", account.ID)
}

func TestOpenAIOAuthSessionPolicyLoadAwareSelectionWritesSharedStickyBinding(t *testing.T) {
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)

	for _, tc := range []struct {
		name         string
		loadBatchErr error
	}{
		{name: "load batch success"},
		{name: "load batch fallback", loadBatchErr: fmt.Errorf("load batch unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &oauthSessionPolicyCache{}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			service := &OpenAIGatewayService{
				cache: cache,
				accountRepo: groupAwareStubOpenAIAccountRepo{
					stubOpenAIAccountRepo{accounts: []Account{account}},
				},
				cfg: cfg,
				concurrencyService: NewConcurrencyService(stubConcurrencyCache{
					loadBatchErr: tc.loadBatchErr,
				}),
			}

			sessionHash := "load-aware-" + tc.name
			ctx := oauthSessionPolicyContext(9001)
			selected, err := service.SelectAccountWithLoadAwareness(ctx, &groupID, sessionHash, "", nil)
			require.NoError(t, err)
			require.Equal(t, account.ID, selected.Account.ID)
			requireOpenAIOAuthStickyBindings(t, service, cache, 9001, groupID, sessionHash, account.ID)
		})
	}
}

func requireOpenAIOAuthStickyBindings(t *testing.T, service *OpenAIGatewayService, cache *oauthSessionPolicyCache, userID, groupID int64, sessionHash string, accountID int64) {
	t.Helper()

	localID, err := cache.GetSessionAccountID(context.Background(), groupID, service.openAISessionCacheKey(sessionHash))
	require.NoError(t, err)
	require.Equal(t, accountID, localID)
	sharedID, err := cache.GetSessionAccountID(context.Background(), openAIOAuthSharedSessionCacheGroupID, service.openAIOAuthSharedSessionCacheKey(userID, sessionHash))
	require.NoError(t, err)
	require.Equal(t, accountID, sharedID)
}

func TestOpenAIOAuthSharedPreviousResponseCacheMissKeepsLegacyContinuationAvailable(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11)
	service := &OpenAIGatewayService{
		cache: &oauthSessionPolicyCache{},
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: &account},
		},
	}

	err := service.validateOpenAISharedPreviousResponseAccountSelection(oauthSessionPolicyContext(9001), nil, "resp_not_shared", &account)
	require.NoError(t, err)
}

func TestOpenAIOAuthLegacySharedPreviousResponseWithoutUserOwnerIsRejected(t *testing.T) {
	cache := &oauthSessionPolicyCache{values: make(map[string]int64)}
	legacyKey := openAIOAuthLegacySharedResponseCacheKey("resp_legacy_shared")
	cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, legacyKey)] = 77
	account := newOpenAIOAuthSessionPolicyAccount(77, 11)
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: &account},
		},
	}

	err := service.validateOpenAISharedPreviousResponseAccountSelection(oauthSessionPolicyContext(9001), nil, "resp_legacy_shared", &account)
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSharedPreviousResponseRequiresCurrentPolicyScope(t *testing.T) {
	cache := &oauthSessionPolicyCache{}
	groupID := int64(11)
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{11, 12},
				"scope_version":     "scope-a",
			},
		},
	}
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: account},
		},
	}

	ctx := oauthSessionPolicyContext(9001)
	require.NoError(t, service.bindOpenAIOAuthSharedResponseAccount(ctx, account, "resp_scope", time.Hour))
	accountID, err := service.getOpenAIOAuthSharedResponseAccount(ctx, &groupID, "resp_scope")
	require.NoError(t, err)
	require.Equal(t, account.ID, accountID)

	policy, ok := account.Extra[OpenAIOAuthSessionPolicyExtraKey].(map[string]any)
	require.True(t, ok)
	policy["scope_version"] = "scope-b"
	_, err = service.getOpenAIOAuthSharedResponseAccount(ctx, &groupID, "resp_scope")
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSharedPreviousResponseSeparatesUsers(t *testing.T) {
	cache := &oauthSessionPolicyCache{}
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: &account},
		},
	}

	ownerCtx := oauthSessionPolicyContext(9001)
	require.NoError(t, service.bindOpenAIOAuthSharedResponseAccount(ownerCtx, &account, "resp_user", time.Hour))
	_, err := service.getOpenAIOAuthSharedResponseAccount(oauthSessionPolicyContext(9002), &groupID, "resp_user")
	require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
}

func TestOpenAIOAuthSharedPreviousResponseRedisErrorsFailClosed(t *testing.T) {
	groupID := int64(11)
	account := newOpenAIOAuthSessionPolicyAccount(77, groupID)
	ctx := oauthSessionPolicyContext(9001)

	t.Run("owner lookup error", func(t *testing.T) {
		cache := &oauthSessionPolicyCache{}
		service := &OpenAIGatewayService{
			cache: cache,
			accountRepo: &oauthSessionPolicyAccountRepo{
				accounts: map[int64]*Account{account.ID: &account},
			},
		}
		require.NoError(t, service.bindOpenAIOAuthSharedResponseAccount(ctx, &account, "resp_owner_error", time.Hour))
		ownerKey := openAIOAuthSharedResponseOwnerCacheKey("resp_owner_error")
		cache.getErrorsByKey = map[string]error{
			cache.key(openAIOAuthSharedSessionCacheGroupID, ownerKey): errors.New("redis timeout"),
		}

		_, err := service.getOpenAIOAuthSharedResponseAccount(ctx, &groupID, "resp_owner_error")
		require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
	})

	t.Run("scope lookup error", func(t *testing.T) {
		cache := &oauthSessionPolicyCache{}
		service := &OpenAIGatewayService{
			cache: cache,
			accountRepo: &oauthSessionPolicyAccountRepo{
				accounts: map[int64]*Account{account.ID: &account},
			},
		}
		require.NoError(t, service.bindOpenAIOAuthSharedResponseAccount(ctx, &account, "resp_scope_error", time.Hour))
		scopeKey := openAIOAuthSharedResponseScopeCacheKey(&account, 9001, "resp_scope_error")
		cache.getErrorsByKey = map[string]error{
			cache.key(openAIOAuthSharedSessionCacheGroupID, scopeKey): errors.New("redis timeout"),
		}

		_, err := service.getOpenAIOAuthSharedResponseAccount(ctx, &groupID, "resp_scope_error")
		require.ErrorIs(t, err, ErrOpenAIOAuthSessionAccessDenied)
	})
}

func TestOpenAIOAuthCompatSessionCacheSharesOnlyWithinUser(t *testing.T) {
	account := newOpenAIOAuthSessionPolicyAccount(77, 11)
	firstKey := openAICompatSessionResponseKey(newOAuthSessionPolicyGinContext(101, 9001, 11), &account, "prompt-cache")
	secondKey := openAICompatSessionResponseKey(newOAuthSessionPolicyGinContext(202, 9001, 11), &account, "prompt-cache")
	otherUserKey := openAICompatSessionResponseKey(newOAuthSessionPolicyGinContext(303, 9002, 11), &account, "prompt-cache")

	require.NotEmpty(t, firstKey)
	require.Equal(t, firstKey, secondKey)
	require.NotEqual(t, firstKey, otherUserKey)
}

func TestOpenAIOAuthSessionPolicyUnauthorizedGroupCannotEvictSharedStickyBinding(t *testing.T) {
	cache := &oauthSessionPolicyCache{}
	allowedGroupID := int64(11)
	blockedGroupID := int64(13)
	account := &Account{
		ID:          77,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			OpenAIOAuthSessionPolicyExtraKey: map[string]any{
				"enabled":           true,
				"allowed_group_ids": []int64{allowedGroupID},
				"scope_version":     "scope-a",
			},
		},
	}
	service := &OpenAIGatewayService{
		cache: cache,
		accountRepo: &oauthSessionPolicyAccountRepo{
			accounts: map[int64]*Account{account.ID: account},
		},
	}

	ctx := oauthSessionPolicyContext(9001)
	require.NoError(t, service.bindOpenAIOAuthSharedSession(ctx, &allowedGroupID, "sticky", account, time.Hour))
	require.NoError(t, service.deleteStickySessionAccountID(ctx, &blockedGroupID, "sticky"))

	accountID, err := service.getOpenAIOAuthSharedSessionAccountID(ctx, "sticky")
	require.NoError(t, err)
	require.Equal(t, account.ID, accountID)
}

func TestNormalizeOpenAIOAuthSessionPolicyRequiresExactAccountGroups(t *testing.T) {
	_, err := normalizeOpenAIOAuthSessionPolicyExtra(nil, PlatformOpenAI, AccountTypeOAuth, map[string]any{
		OpenAIOAuthSessionPolicyExtraKey: map[string]any{
			"enabled":           true,
			"allowed_group_ids": []int64{11, 12},
		},
	}, []int64{11})
	require.Error(t, err)

	normalized, err := normalizeOpenAIOAuthSessionPolicyExtra(nil, PlatformOpenAI, AccountTypeOAuth, map[string]any{
		OpenAIOAuthSessionPolicyExtraKey: map[string]any{
			"enabled":           true,
			"allowed_group_ids": []int64{12, 11},
		},
	}, []int64{11, 12})
	require.NoError(t, err)
	policy, ok := normalized[OpenAIOAuthSessionPolicyExtraKey].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []int64{11, 12}, policy["allowed_group_ids"])
	require.NotEmpty(t, policy["scope_version"])
}

func (c *oauthSessionPolicyCache) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (c *oauthSessionPolicyCache) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}
