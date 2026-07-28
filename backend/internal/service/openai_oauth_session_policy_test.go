package service

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
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

func (c *oauthSessionPolicyCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *oauthSessionPolicyCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	if err := c.getErrorsByKey[c.key(groupID, sessionHash)]; err != nil {
		return 0, err
	}
	accountID, ok := c.values[c.key(groupID, sessionHash)]
	if !ok {
		return 0, redis.Nil
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

func (c *oauthSessionPolicyCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c *oauthSessionPolicyCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(c.values, c.key(groupID, sessionHash))
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
	service := &OpenAIGatewayService{cache: &oauthSessionPolicyCache{}}

	err := service.validateOpenAIOAuthSharedPreviousResponseAccess(oauthSessionPolicyContext(9001), nil, "resp_not_shared")
	require.NoError(t, err)
}

func TestOpenAIOAuthLegacySharedPreviousResponseWithoutUserOwnerIsRejected(t *testing.T) {
	cache := &oauthSessionPolicyCache{values: make(map[string]int64)}
	legacyKey := openAIOAuthLegacySharedResponseCacheKey("resp_legacy_shared")
	cache.values[cache.key(openAIOAuthSharedSessionCacheGroupID, legacyKey)] = 77
	service := &OpenAIGatewayService{cache: cache}

	err := service.validateOpenAIOAuthSharedPreviousResponseAccess(oauthSessionPolicyContext(9001), nil, "resp_legacy_shared")
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
	policy := normalized[OpenAIOAuthSessionPolicyExtraKey].(map[string]any)
	require.Equal(t, []int64{11, 12}, policy["allowed_group_ids"])
	require.NotEmpty(t, policy["scope_version"])
}
