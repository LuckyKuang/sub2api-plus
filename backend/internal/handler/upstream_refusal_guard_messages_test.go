//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	//nolint:depguard // End-to-end test must exercise the production Redis repository.
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	//nolint:depguard // End-to-end Redis test needs a client for miniredis.
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type refusalSelectionCache struct {
	fakeSchedulerCache
	calls atomic.Int64
}

func (s *refusalSelectionCache) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	s.calls.Add(1)
	return s.fakeSchedulerCache.GetSnapshot(ctx, bucket)
}

type refusalConcurrencyCache struct {
	fakeConcurrencyCache
	calls atomic.Int64
}

func (s *refusalConcurrencyCache) AcquireUserSlot(ctx context.Context, id int64, n int, key string) (bool, error) {
	s.calls.Add(1)
	return true, nil
}

type refusalHTTPUpstream struct {
	client *http.Client
	url    string
	calls  atomic.Int64
}

func (s *refusalHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.calls.Add(1)
	return s.client.Do(req)
}
func (s *refusalHTTPUpstream) DoWithTLS(req *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, p, id, n)
}

func newRefusalMessagesFixture(t *testing.T, guard *service.UpstreamRefusalGuardService, settings *service.SettingService, upstream service.HTTPUpstream) (*GatewayHandler, *refusalSelectionCache, *refusalConcurrencyCache, func()) {
	t.Helper()
	groupID := int64(2001)
	accountID := int64(1001)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	account := &service.Account{ID: accountID, Name: "anthropic-test", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}}}
	if remote, ok := upstream.(*refusalHTTPUpstream); ok && remote.url != "" {
		account.Credentials = map[string]any{"api_key": "sk-test", "base_url": remote.url}
		account.Extra = map[string]any{"anthropic_passthrough": true}
	}
	scheduler := &refusalSelectionCache{fakeSchedulerCache: fakeSchedulerCache{accounts: []*service.Account{account}}}
	concurrency := &refusalConcurrencyCache{}
	billingCfg := &config.Config{RunMode: config.RunModeSimple}
	billingCfg.Security.URLAllowlist.AllowInsecureHTTP = true
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, billingCfg, nil)
	gw := service.NewGatewayService(nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, billingCfg, service.NewSchedulerSnapshotService(scheduler, nil, nil, nil, nil), nil, nil, nil, billing, nil, upstream, nil, nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{gatewayService: gw, refusalGuard: guard, billingCacheService: billing, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(concurrency), SSEPingFormatClaude, 0), maxAccountSwitches: 1, maxAccountSwitchesGemini: 1}
	return h, scheduler, concurrency, billing.Stop
}
func refusalMessagesRequest(h *GatewayHandler, session string) (*httptest.ResponseRecorder, *gin.Context) {
	groupID := int64(2001)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	body, _ := json.Marshal(map[string]any{"model": "claude-sonnet-4-5", "max_tokens": 256, "metadata": map[string]any{"user_id": `{"device_id":"device-test","session_id":"` + session + `"}`}, "messages": []any{map[string]any{"role": "user", "content": "test request"}}})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.Group, group))
	c.Request = request
	key := &service.APIKey{ID: 3001, UserID: 4001, GroupID: &groupID, Status: service.StatusActive, User: &service.User{ID: 4001, Concurrency: 10, Balance: 100}, Group: group}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 10})
	h.Messages(c)
	return recorder, c
}
func TestMessagesRefusalGuardBookkeeping(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		enabled    bool
		wantCount  int64
	}{
		{"vm2api refusal", `{"error":{"type":"policy_blocked","code":"refusal_guard","message":"Do not retry this request."}}`, true, 1},
		{"ordinary 400", `{"error":{"type":"invalid_request_error","message":"max_tokens too large"}}`, true, 0},
		{"disabled", `{"error":{"type":"policy_blocked","code":"refusal_guard","message":"Do not retry this request."}}`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redisServer := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			store := repository.NewUpstreamRefusalStore(rdb)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/messages", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer remote.Close()
			cfg := service.DefaultUpstreamRefusalGuardConfig()
			cfg.Enabled = tc.enabled
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyUpstreamRefusalGuardConfig: string(raw)}}, nil)
			guard := service.NewUpstreamRefusalGuardService(settings, store, nil)
			upstream := &refusalHTTPUpstream{client: remote.Client(), url: remote.URL}
			h, selection, concurrency, cleanup := newRefusalMessagesFixture(t, guard, settings, upstream)
			defer cleanup()
			first, _ := refusalMessagesRequest(h, "first")
			require.Equal(t, 400, first.Code, first.Body.String())
			require.EqualValues(t, 1, upstream.calls.Load(), "must reach upstream Forward")
			require.Positive(t, selection.calls.Load())
			require.Positive(t, concurrency.calls.Load())
			users, err := store.ListUsers(context.Background())
			require.NoError(t, err)
			if tc.wantCount == 0 {
				require.Empty(t, users)
				sessions, err := store.ListSessions(context.Background())
				require.NoError(t, err)
				require.Empty(t, sessions)
				again, _ := refusalMessagesRequest(h, "first")
				require.Equal(t, 400, again.Code, "ordinary/disabled refusals must not intercept a repeated session")
				require.EqualValues(t, 2, upstream.calls.Load())
				return
			}
			require.Len(t, users, 1)
			require.Equal(t, tc.wantCount, users[0].Count)
			sessions, err := store.ListSessions(context.Background())
			require.NoError(t, err)
			require.Len(t, sessions, 1)
			require.Equal(t, "first", sessions[0].SessionID)
			again, _ := refusalMessagesRequest(h, "first")
			require.Equal(t, 403, again.Code, again.Body.String())
			require.Contains(t, again.Body.String(), "session_blocked_by_upstream_refusal")
			require.EqualValues(t, 1, upstream.calls.Load())
			for _, session := range []string{"second", "third"} {
				rec, _ := refusalMessagesRequest(h, session)
				require.Equal(t, 400, rec.Code, rec.Body.String())
			}
			users, err = store.ListUsers(context.Background())
			require.NoError(t, err)
			require.Len(t, users, 1)
			require.Zero(t, users[0].Count)
			require.Positive(t, users[0].CooldownRemainingSeconds)
			fourth, _ := refusalMessagesRequest(h, "fourth")
			require.Equal(t, 429, fourth.Code, fourth.Body.String())
			require.NotEmpty(t, fourth.Header().Get("Retry-After"))
			require.EqualValues(t, 3, upstream.calls.Load())
		})
	}
}

func TestMessagesRefusalGuardSecurityAuditFirst(t *testing.T) {
	moderation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"category_scores":{"sexual":0.99}}]}`))
	}))
	defer moderation.Close()
	cfg := service.DefaultUpstreamRefusalGuardConfig()
	cfg.Enabled = true
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	moderationCfg := &service.ContentModerationConfig{Enabled: true, Mode: service.ContentModerationModePreBlock, BaseURL: moderation.URL, Model: "omni-moderation-latest", APIKeys: []string{"sk-test"}, SampleRate: 100, AllGroups: true, BlockMessage: "audit blocked", BlockStatus: 403, Thresholds: map[string]float64{"sexual": 0.5}}
	moderationRaw, err := json.Marshal(moderationCfg)
	require.NoError(t, err)
	settingsRepo := &contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyRiskControlEnabled: "true", service.SettingKeyContentModerationConfig: string(moderationRaw), service.SettingKeyUpstreamRefusalGuardConfig: string(raw)}}
	settings := service.NewSettingService(settingsRepo, nil)
	store := &refusalGuardTestStore{blocked: true}
	guard := service.NewUpstreamRefusalGuardService(settings, store, nil)
	upstream := &refusalHTTPUpstream{client: http.DefaultClient}
	h, selection, concurrency, cleanup := newRefusalMessagesFixture(t, guard, settings, upstream)
	defer cleanup()
	h.contentModerationService = service.NewContentModerationService(settingsRepo, &contentModerationHandlerTestRepo{}, nil, nil, nil, nil, nil, nil)
	rec, _ := refusalMessagesRequest(h, "audit-blocked")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "session_blocked_by_upstream_refusal")
	require.Zero(t, store.count, "security audit must precede every guard lookup")
	require.Zero(t, selection.calls.Load())
	require.Zero(t, concurrency.calls.Load())
	require.Zero(t, upstream.calls.Load())
}

func TestMessagesRefusalGuardBeforeSelection(t *testing.T) {
	cfg := service.DefaultUpstreamRefusalGuardConfig()
	cfg.Enabled = true
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		blocked  bool
		cooldown time.Duration
		status   int
		code     string
		retry    string
	}{
		{"blocked session", true, 0, 403, "session_blocked_by_upstream_refusal", ""},
		{"user cooldown", false, 1500 * time.Millisecond, 429, "user_cooldown_by_upstream_refusal", "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyUpstreamRefusalGuardConfig: string(raw)}}, nil)
			store := &refusalGuardTestStore{blocked: tc.blocked, remaining: tc.cooldown}
			guard := service.NewUpstreamRefusalGuardService(settings, store, nil)
			upstream := &refusalHTTPUpstream{client: http.DefaultClient}
			h, selection, concurrency, cleanup := newRefusalMessagesFixture(t, guard, settings, upstream)
			defer cleanup()
			rec, c := refusalMessagesRequest(h, "blocked")
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.code)
			require.Equal(t, tc.retry, rec.Header().Get("Retry-After"))
			require.Zero(t, selection.calls.Load(), "account selection")
			require.Zero(t, concurrency.calls.Load(), "user concurrency")
			require.Zero(t, upstream.calls.Load(), "upstream requests")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
			require.Positive(t, store.count)
		})
	}
}
