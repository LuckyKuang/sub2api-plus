//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// openAIClientProfileFallbackUpstream records only actual outbound requests.
// A profile-rejected candidate must never reach this boundary.
type openAIClientProfileFallbackUpstream struct {
	service.HTTPUpstream

	mu         sync.Mutex
	accountIDs []int64
	body       string
}

func (u *openAIClientProfileFallbackUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *openAIClientProfileFallbackUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func newOpenAIClientProfileFallbackHandler(
	t *testing.T,
	accounts []service.Account,
	upstream service.HTTPUpstream,
	cache *concurrencyCacheMock,
) *OpenAIGatewayHandler {
	t.Helper()

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false

	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	gateway := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		service.NewConcurrencyService(cache),
		service.NewBillingService(cfg, nil),
		nil,
		billing,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return NewOpenAIGatewayHandler(
		gateway,
		service.NewConcurrencyService(cache),
		billing,
		&service.APIKeyService{},
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
}

func openAIClientProfileFallbackAccounts() []service.Account {
	return []service.Account{
		{
			ID:          8301,
			Name:        "restricted-codex-oauth",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			Credentials: map[string]any{
				"access_token": "oauth-token-must-not-be-read",
			},
			Extra: map[string]any{
				"codex_cli_only": true,
			},
		},
		{
			ID:          8302,
			Name:        "eligible-api-key",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    2,
			Credentials: map[string]any{
				"api_key":  "sk-eligible",
				"base_url": "https://upstream.example",
			},
		},
	}
}

func TestOpenAICountTokens_SkipsClientProfileIneligibleAccountAndUsesAPIKeyCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(830)
	cache := &concurrencyCacheMock{
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	}
	upstream := &openAIClientProfileFallbackUpstream{body: `{"input_tokens":42}`}
	h := newOpenAIClientProfileFallbackHandler(t, openAIClientProfileFallbackAccounts(), upstream, cache)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/messages/count_tokens",
		strings.NewReader(`{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}]}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      1830,
		GroupID: &groupID,
		User:    &service.User{ID: 1730, Status: service.StatusActive},
		Group: &service.Group{
			ID:                    groupID,
			Platform:              service.PlatformOpenAI,
			Status:                service.StatusActive,
			AllowMessagesDispatch: true,
		},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1730, Concurrency: 1})

	// Deliberately omit every official Codex profile signal. The first OAuth
	// candidate must be rejected locally, while the API-key candidate remains
	// eligible and independent of the OAuth-only policy domain.
	h.CountTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"input_tokens":42}`, recorder.Body.String())
	require.Equal(t, []int64{8302}, upstream.calls())
	require.Equal(t, int32(2), atomic.LoadInt32(&cache.releaseAccountCalled), "both the rejected and completed account slots must be released")
}

func TestOpenAIAlphaSearch_SkipsClientProfileIneligibleAccountAndUsesAPIKeyCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(831)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	}
	upstream := &openAIClientProfileFallbackUpstream{body: `{"output":"ok"}`}
	h := newOpenAIClientProfileFallbackHandler(t, openAIClientProfileFallbackAccounts(), upstream, cache)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/alpha/search",
		strings.NewReader(`{"id":"policy-fallback-search","model":"gpt-5.1","input":[]}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      1831,
		GroupID: &groupID,
		User:    &service.User{ID: 1731, Status: service.StatusActive},
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			Status:   service.StatusActive,
		},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1731, Concurrency: 1})

	// As above, no approved Codex profile is present. This proves the local
	// rejection releases its scheduler slot before the unrestricted API key
	// account forwards alpha/search.
	h.AlphaSearch(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"output":"ok"}`, recorder.Body.String())
	require.Equal(t, []int64{8302}, upstream.calls())
	require.Equal(t, int32(2), atomic.LoadInt32(&cache.releaseAccountCalled), "both the rejected and completed account slots must be released")
	require.Equal(t, int32(1), atomic.LoadInt32(&cache.releaseUserCalled), "the user slot must be released after alpha/search completes")
}
