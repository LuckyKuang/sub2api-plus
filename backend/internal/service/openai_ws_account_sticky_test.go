package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_Hit(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()

	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_1", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_1", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_QuotaAutoPausedMiss(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          77,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"codex_5h_used_percent":                         96.0,
			"auto_pause_5h_threshold":                       0.95,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_quota", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_quota", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "超过 5h 配额阈值的账号不应继续命中 previous_response_id 粘连")

	// Auto-pause is transient, so the binding is preserved: the chain can resume on the
	// same account once the quota window resets.
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_quota")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_RateLimitedMiss(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	account := Account{
		ID:               12,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		GroupIDs:         []int64{groupID},
		RateLimitResetAt: &rateLimitedUntil,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_rl", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_rl", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "限额中的账号不应继续命中 previous_response_id 粘连")
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_rl")
	require.NoError(t, getErr)
	require.Zero(t, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_DBRuntimeRecheckRateLimitedMiss(t *testing.T) {
	ctx := context.Background()
	groupID := int64(24)
	rateLimitedUntil := time.Now().Add(30 * time.Minute)
	staleAccount := &Account{
		ID:          13,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	dbAccount := Account{
		ID:               13,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		GroupIDs:         []int64{groupID},
		RateLimitResetAt: &rateLimitedUntil,
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	snapshotCache := &openAISnapshotCacheStub{
		accountsByID: map[int64]*Account{dbAccount.ID: staleAccount},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{dbAccount}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_db_rl", dbAccount.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_db_rl", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "DB 中已限流的账号不应继续命中 previous_response_id 粘连")
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_db_rl")
	require.NoError(t, getErr)
	require.Zero(t, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_Excluded(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          8,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_2", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_2", "gpt-5.1", map[int64]struct{}{account.ID: {}}, false)
	require.NoError(t, err)
	require.Nil(t, selection)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_APIKeyForceHTTPHit(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_ws_force_http":            true,
			"responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_force_http", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_force_http", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection, "API-key HTTP continuation must retain the key/project that created the response")
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_OAuthForceHTTPIgnored(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{
		ID:          12,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Extra: map[string]any{
			"openai_ws_force_http":            true,
			"responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                newOpenAIWSV2TestConfig(),
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_oauth_force_http", account.ID, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_oauth_force_http", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection, "OAuth HTTP fallback cannot preserve WSv2 continuation state")
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_BusyKeepsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	accounts := []Account{
		{
			ID:          21,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    0,
			GroupIDs:    []int64{groupID},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID:          22,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    9,
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			},
		},
	}

	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 2
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 30 * time.Second

	concurrencyCache := stubConcurrencyCache{
		acquireResults: map[int64]bool{
			21: false, // previous_response 命中的账号繁忙
			22: true,  // 次优账号可用（若回退会命中）
		},
		waitCounts: map[int64]int{
			21: 999,
		},
	}

	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: accounts},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_busy", 21, time.Hour))

	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_prev_busy", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(21), selection.Account.ID, "busy previous_response sticky account should remain selected")
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(21), selection.WaitPlan.AccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_CapabilityMismatchKeepsSticky(t *testing.T) {
	ctx := context.Background()
	groupID := int64(25)
	account := Account{
		ID:          31,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{
			"openai_capabilities": []any{"chat_completions"},
		},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	cfg := newOpenAIWSV2TestConfig()
	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{account}},
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
		openaiWSStateStore: store,
	}

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_prev_capability", account.ID, time.Hour))

	selection, err := svc.selectAccountByPreviousResponseIDForCapability(
		ctx,
		&groupID,
		"resp_prev_capability",
		"text-embedding-3-small",
		nil,
		OpenAIEndpointCapabilityEmbeddings,
		false,
	)
	require.NoError(t, err)
	require.Nil(t, selection)
	boundAccountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_prev_capability")
	require.NoError(t, getErr)
	require.Equal(t, account.ID, boundAccountID)
}

func TestOpenAIGatewayService_SelectAccountByPreviousResponseID_GroupRemovalInvalidatesAllAccountTypes(t *testing.T) {
	ctx := context.Background()
	groupID := int64(26)
	otherGroupID := int64(27)

	tests := []struct {
		name          string
		accountType   string
		sharingPolicy bool
	}{
		{name: "api key", accountType: AccountTypeAPIKey},
		{name: "ordinary oauth", accountType: AccountTypeOAuth},
		{name: "sharing oauth", accountType: AccountTypeOAuth, sharingPolicy: true},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountID := int64(4100 + index)
			ownerUserID := int64(9300 + index)
			requestCtx := ctx
			if tt.sharingPolicy {
				requestCtx = oauthSessionPolicyContext(ownerUserID)
			}
			wsEnabledKey := "openai_apikey_responses_websockets_v2_enabled"
			if tt.accountType == AccountTypeOAuth {
				wsEnabledKey = "openai_oauth_responses_websockets_v2_enabled"
			}
			stale := &Account{
				ID:          accountID,
				Platform:    PlatformOpenAI,
				Type:        tt.accountType,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				GroupIDs:    []int64{groupID},
				Extra:       map[string]any{wsEnabledKey: true},
			}
			fresh := *stale
			fresh.GroupIDs = []int64{otherGroupID}
			fresh.Extra = map[string]any{wsEnabledKey: true}
			if tt.sharingPolicy {
				stale.Extra[OpenAIOAuthSessionPolicyExtraKey] = map[string]any{
					"enabled":           true,
					"allowed_group_ids": []int64{groupID},
					"scope_version":     "scope-before-edit",
				}
				fresh.Extra[OpenAIOAuthSessionPolicyExtraKey] = map[string]any{
					"enabled":           true,
					"allowed_group_ids": []int64{otherGroupID},
					"scope_version":     "scope-after-edit",
				}
			}

			cache := &stubGatewayCache{}
			store := NewOpenAIWSStateStore(cache)
			cfg := newOpenAIWSV2TestConfig()
			cfg.RunMode = config.RunModeStandard
			svc := &OpenAIGatewayService{
				accountRepo:        stubOpenAIAccountRepo{accounts: []Account{fresh}},
				cache:              cache,
				cfg:                cfg,
				concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
				openaiWSStateStore: store,
				schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					accountsByID: map[int64]*Account{accountID: stale},
				}},
			}
			responseID := fmt.Sprintf("resp_group_removed_%d", index)
			if tt.sharingPolicy {
				require.NoError(t, svc.bindOpenAIResponseAccount(requestCtx, store, groupID, stale, responseID, time.Hour))
			} else {
				require.NoError(t, store.BindResponseAccount(requestCtx, groupID, responseID, accountID, time.Hour))
			}

			selection, err := svc.SelectAccountByPreviousResponseID(requestCtx, &groupID, responseID, "gpt-5.1", nil, false)
			require.NoError(t, err)
			require.Nil(t, selection)
			boundAccountID, getErr := store.GetResponseAccount(requestCtx, groupID, responseID)
			require.NoError(t, getErr)
			require.Zero(t, boundAccountID, "stale group-local response binding must be removed")

			if tt.sharingPolicy {
				ownerID, ownerErr := cache.GetSessionAccountID(requestCtx, openAIOAuthSharedSessionCacheGroupID, openAIOAuthSharedResponseOwnerCacheKey(responseID))
				require.NoError(t, ownerErr)
				require.Equal(t, ownerUserID, ownerID, "global OAuth response owner marker must remain fail-closed")
				oldScopeKey := openAIOAuthSharedResponseScopeCacheKey(stale, ownerUserID, responseID)
				scopedAccountID, scopeErr := cache.GetSessionAccountID(requestCtx, openAIOAuthSharedSessionCacheGroupID, oldScopeKey)
				require.NoError(t, scopeErr)
				require.Equal(t, accountID, scopedAccountID, "old OAuth scope marker must not be deleted by a group-local invalidation")
			}
		})
	}
}

func TestOpenAIGatewayService_RevalidateOpenAIAccountForWebSocketTurn_GroupRemovalInvalidatesAllAccountTypes(t *testing.T) {
	ctx := context.Background()
	groupID := int64(28)
	otherGroupID := int64(29)

	tests := []struct {
		name          string
		accountType   string
		sharingPolicy bool
	}{
		{name: "api key", accountType: AccountTypeAPIKey},
		{name: "ordinary oauth", accountType: AccountTypeOAuth},
		{name: "sharing oauth", accountType: AccountTypeOAuth, sharingPolicy: true},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountID := int64(4200 + index)
			stale := &Account{
				ID:          accountID,
				Platform:    PlatformOpenAI,
				Type:        tt.accountType,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				GroupIDs:    []int64{groupID},
				Extra:       map[string]any{},
			}
			fresh := *stale
			fresh.GroupIDs = []int64{otherGroupID}
			fresh.Extra = map[string]any{}
			if tt.sharingPolicy {
				stale.Extra[OpenAIOAuthSessionPolicyExtraKey] = map[string]any{
					"enabled":           true,
					"allowed_group_ids": []int64{groupID},
					"scope_version":     "scope-before-edit",
				}
				fresh.Extra[OpenAIOAuthSessionPolicyExtraKey] = map[string]any{
					"enabled":           true,
					"allowed_group_ids": []int64{otherGroupID},
					"scope_version":     "scope-after-edit",
				}
			}

			cfg := newOpenAIWSV2TestConfig()
			cfg.RunMode = config.RunModeStandard
			svc := &OpenAIGatewayService{
				accountRepo: stubOpenAIAccountRepo{accounts: []Account{fresh}},
				cfg:         cfg,
			}

			latest, err := svc.RevalidateOpenAIAccountForWebSocketTurn(
				ctx,
				stale,
				&groupID,
				PlatformOpenAI,
				"gpt-5.1",
				OpenAIUpstreamTransportAny,
				OpenAIEndpointCapabilityChatCompletions,
			)
			require.NoError(t, err)
			require.Nil(t, latest, "an established websocket must stop before its next turn after the account leaves the group")
		})
	}

	t.Run("repository failure fails closed", func(t *testing.T) {
		selected := &Account{ID: 4299, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, GroupIDs: []int64{groupID}}
		cfg := newOpenAIWSV2TestConfig()
		cfg.RunMode = config.RunModeStandard
		svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{}, cfg: cfg}

		latest, err := svc.RevalidateOpenAIAccountForWebSocketTurn(
			ctx,
			selected,
			&groupID,
			PlatformOpenAI,
			"gpt-5.1",
			OpenAIUpstreamTransportAny,
			OpenAIEndpointCapabilityChatCompletions,
		)
		require.Error(t, err)
		require.Nil(t, latest)
	})
}

func TestOpenAIGatewayService_RevalidateOpenAIAccountForWebSocketTurn_AppliesCurrentHardEligibility(t *testing.T) {
	ctx := context.Background()
	groupID := int64(30)
	base := Account{
		ID:          4300,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
		GroupIDs:    []int64{groupID},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
		},
	}

	tests := []struct {
		name              string
		mutate            func(*Account)
		model             string
		requiredTransport OpenAIUpstreamTransport
	}{
		{
			name: "disabled account",
			mutate: func(account *Account) {
				account.Status = StatusDisabled
			},
			model: "gpt-5.1",
		},
		{
			name: "unschedulable account",
			mutate: func(account *Account) {
				account.Schedulable = false
			},
			model: "gpt-5.1",
		},
		{
			name: "model removed",
			mutate: func(account *Account) {
				account.Credentials = map[string]any{
					"model_mapping": map[string]any{"gpt-other": "gpt-other"},
				}
			},
			model: "gpt-5.1",
		},
		{
			name: "websocket transport disabled",
			mutate: func(account *Account) {
				account.Extra = map[string]any{
					"openai_apikey_responses_websockets_v2_enabled": false,
				}
			},
			model:             "gpt-5.1",
			requiredTransport: OpenAIUpstreamTransportResponsesWebsocketV2Ingress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fresh := base
			fresh.GroupIDs = append([]int64(nil), base.GroupIDs...)
			fresh.Extra = map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
			}
			tt.mutate(&fresh)
			cfg := newOpenAIWSV2TestConfig()
			cfg.RunMode = config.RunModeStandard
			svc := &OpenAIGatewayService{
				accountRepo: stubOpenAIAccountRepo{accounts: []Account{fresh}},
				cfg:         cfg,
			}

			requiredTransport := tt.requiredTransport
			if requiredTransport == "" {
				requiredTransport = OpenAIUpstreamTransportAny
			}
			latest, err := svc.RevalidateOpenAIAccountForWebSocketTurn(
				ctx,
				&base,
				&groupID,
				PlatformOpenAI,
				tt.model,
				requiredTransport,
				OpenAIEndpointCapabilityChatCompletions,
			)
			require.NoError(t, err)
			require.Nil(t, latest, "an established websocket must stop when a current hard scheduling gate fails")
		})
	}

	t.Run("eligible account refreshes current concurrency", func(t *testing.T) {
		fresh := base
		fresh.Concurrency = 1
		cfg := newOpenAIWSV2TestConfig()
		cfg.RunMode = config.RunModeStandard
		svc := &OpenAIGatewayService{
			accountRepo: stubOpenAIAccountRepo{accounts: []Account{fresh}},
			cfg:         cfg,
		}

		latest, err := svc.RevalidateOpenAIAccountForWebSocketTurn(
			ctx,
			&base,
			&groupID,
			PlatformOpenAI,
			"gpt-5.1",
			OpenAIUpstreamTransportAny,
			OpenAIEndpointCapabilityChatCompletions,
		)
		require.NoError(t, err)
		require.NotNil(t, latest)
		require.Equal(t, 1, latest.Concurrency)
	})
}

func newOpenAIWSV2TestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	return cfg
}
