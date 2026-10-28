//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type grokMediaSlotsCache struct {
	testutil.StubConcurrencyCache
	mu                                             sync.Mutex
	accounts                                       map[string]int64
	users                                          map[string]int64
	acquired, released, userAcquired, userReleased int
	waiting, denied                                int
	full                                           bool
	queueFull                                      bool
	onWait                                         func()
	duplicateRelease                               bool
	peak                                           int
}

func (s *grokMediaSlotsCache) AcquireAccountSlot(_ context.Context, id int64, _ int, request string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.full || s.denied > 0 {
		if s.denied > 0 {
			s.denied--
		}
		return false, nil
	}
	s.accounts[request] = id
	s.acquired++
	if len(s.accounts) > s.peak {
		s.peak = len(s.accounts)
	}
	return true, nil
}

func (s *grokMediaSlotsCache) ReleaseAccountSlot(_ context.Context, _ int64, request string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[request]; !ok {
		s.duplicateRelease = true
	}
	delete(s.accounts, request)
	s.released++
	return nil
}

func (s *grokMediaSlotsCache) AcquireUserSlot(_ context.Context, id int64, _ int, request string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[request] = id
	s.userAcquired++
	return true, nil
}

func (s *grokMediaSlotsCache) ReleaseUserSlot(_ context.Context, _ int64, request string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[request]; !ok {
		s.duplicateRelease = true
	}
	delete(s.users, request)
	s.userReleased++
	return nil
}

func (s *grokMediaSlotsCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	s.mu.Lock()
	if s.queueFull {
		s.mu.Unlock()
		return false, nil
	}
	s.waiting++
	onWait := s.onWait
	s.mu.Unlock()
	if onWait != nil {
		onWait()
	}
	return true, nil
}

func (s *grokMediaSlotsCache) DecrementAccountWaitCount(context.Context, int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting--
	return nil
}

func (s *grokMediaSlotsCache) assertReleased(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Empty(t, s.accounts)
	require.Empty(t, s.users)
	require.Equal(t, s.acquired, s.released)
	require.Equal(t, s.userAcquired, s.userReleased)
	require.False(t, s.duplicateRelease)
	require.Zero(t, s.waiting)
}

type grokMediaSlotBindings struct {
	testutil.StubGatewayCache
	owner         int64
	writes        int
	key           string
	billed        map[string]bool
	billingOwners map[string]string
	pending       map[string][]byte
	pendingReads  int
}

func (s *grokMediaSlotBindings) SetGrokVideoPendingBilling(_ context.Context, key string, body []byte, _ time.Duration) error {
	if s.pending == nil {
		s.pending = make(map[string][]byte)
	}
	s.pending[key] = append([]byte(nil), body...)
	return nil
}
func (s *grokMediaSlotBindings) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.pendingReads++
	return s.pending[key], nil
}

func (s *grokMediaSlotBindings) GetSessionAccountID(_ context.Context, groupID int64, key string) (int64, error) {
	if groupID != 24 || key != s.key {
		return 0, service.ErrStickySessionNotFound
	}
	return s.owner, nil
}
func (s *grokMediaSlotBindings) SetSessionAccountID(_ context.Context, _ int64, key string, owner int64, _ time.Duration) error {
	s.key, s.owner = key, owner
	s.writes++
	return nil
}
func (s *grokMediaSlotBindings) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	s.writes++
	return nil
}
func (s *grokMediaSlotBindings) DeleteSessionAccountID(context.Context, int64, string) error {
	s.writes++
	return nil
}

func (s *grokMediaSlotBindings) ClaimGrokVideoBilled(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	if s.billed == nil {
		s.billed = make(map[string]bool)
	}
	if s.billed[key] {
		return false, nil
	}
	s.billed[key] = true
	if s.billingOwners == nil {
		s.billingOwners = make(map[string]string)
	}
	s.billingOwners[key] = owner
	return true, nil
}

func (s *grokMediaSlotBindings) CompleteGrokVideoBilled(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	if s.billingOwners[key] != owner {
		return false, nil
	}
	delete(s.billingOwners, key)
	return true, nil
}

func (s *grokMediaSlotBindings) ReleaseGrokVideoBilled(_ context.Context, key, owner string) error {
	if s.billingOwners[key] == owner {
		delete(s.billingOwners, key)
		delete(s.billed, key)
	}
	return nil
}

type grokMediaSlotRepo struct {
	openAIImagesFailoverAccountRepo
	mismatch bool
}

func (r grokMediaSlotRepo) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if r.mismatch {
		return r.openAIImagesFailoverAccountRepo.GetByID(ctx, 2)
	}
	return r.openAIImagesFailoverAccountRepo.GetByID(ctx, id)
}

type grokMediaSlotUpstream struct {
	service.HTTPUpstream
	call  func(*http.Request, int64) (*http.Response, error)
	calls int
}

func (u *grokMediaSlotUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	u.calls++
	return u.call(req, id)
}

type grokMediaSlotProber func(context.Context, int64) (bool, string, error)

func (p grokMediaSlotProber) ProbeMediaEligibility(ctx context.Context, id int64) (bool, string, error) {
	return p(ctx, id)
}

func newGrokMediaSlotHandler(t *testing.T, oauth, mismatch bool, platforms ...string) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *grokMediaSlotBindings, *grokMediaSlotUpstream) {
	t.Helper()
	return newGrokMediaSlotHandlerWithRates(t, oauth, mismatch, nil, platforms...)
}

func newGrokMediaSlotHandlerWithRates(t *testing.T, oauth, mismatch bool, rates service.UserGroupRateRepository, platforms ...string) (*OpenAIGatewayHandler, *grokMediaSlotsCache, *grokMediaSlotBindings, *grokMediaSlotUpstream) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	accounts := make([]service.Account, 3)
	for i := range accounts {
		accounts[i] = service.Account{ID: int64(i + 1), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 50, Priority: i,
			GroupIDs: []int64{24}, Credentials: map[string]any{"api_key": "test-key", "access_token": "test-token"}}
		if len(platforms) > 0 {
			accounts[i].Platform = platforms[0]
			accounts[i].Credentials["base_url"] = "https://ark.cn-beijing.volces.com/api/v3"
			accounts[i].Credentials["openai_capabilities"] = []string{"seedance"}
		}
		if oauth {
			accounts[i].Type = service.AccountTypeOAuth
			accounts[i].Credentials["refresh_token"] = "test-refresh"
			accounts[i].Credentials["expires_at"] = time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
		}
	}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	bindings := &grokMediaSlotBindings{owner: 1}
	upstream := &grokMediaSlotUpstream{call: func(*http.Request, int64) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"request_id":"task","status":"pending"}`))}, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 20 * time.Millisecond
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
	repo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: accounts}, mismatch: mismatch}
	provider := service.NewGrokTokenProvider(repo, nil)
	if oauth {
		_, err := provider.GetAccessToken(context.Background(), &accounts[1])
		require.NoError(t, err)
	}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, rates, bindings, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, provider, nil, nil, nil, nil, nil, nil)
	groupID := int64(24)
	require.NoError(t, gateway.BindGrokMediaVideoRequestAccount(context.Background(), &groupID, "task", 10, 20, 1))
	bindings.writes = 0
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	handler.unfinishedTasks = service.NewGatewayUnfinishedTaskService(&testutil.StubGatewayUnfinishedTaskRepo{}, nil)
	return handler, slots, bindings, upstream
}

func grokMediaSlotContext(ctx context.Context, generation bool) (*gin.Context, *httptest.ResponseRecorder) {
	groupID := int64(24)
	method, path, body := http.MethodGet, "/v1/videos/task", ""
	if generation {
		method, path, body = http.MethodPost, "/v1/videos/generations", `{"model":"grok-imagine-video","prompt":"test","duration":6}`
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	if !generation {
		req.Header.Set("session-id", "unrelated-client-session")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "request_id", Value: "task"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: service.PlatformGrok, AllowImageGeneration: true}, User: &service.User{ID: 10}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 10, Concurrency: 5})
	return c, w
}

func TestGrokMediaLookupSlotLifecycle(t *testing.T) {
	for _, scenario := range []string{"normal", "mismatch acquired", "mismatch wait", "full", "queue full", "cancel while waiting", "wait then acquired", "upstream error", "cancel", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, strings.HasPrefix(scenario, "mismatch"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "full" || scenario == "mismatch wait" || scenario == "queue full" || scenario == "cancel while waiting" {
				slots.full = true
			}
			if scenario == "queue full" {
				slots.queueFull = true
			}
			if scenario == "cancel while waiting" {
				slots.onWait = cancel
			}
			if scenario == "wait then acquired" {
				slots.denied = 1
			}
			original := upstream.call
			upstream.call = func(req *http.Request, id int64) (*http.Response, error) {
				require.Equal(t, int64(1), id)
				switch scenario {
				case "upstream error":
					return nil, errors.New("upstream unavailable")
				case "cancel":
					cancel()
					return nil, context.Canceled
				case "panic":
					panic("test upstream panic")
				}
				return original(req, id)
			}
			for range 20 {
				c, w := grokMediaSlotContext(ctx, false)
				h.GrokVideoStatus(c)
				slots.assertReleased(t)
				if strings.HasPrefix(scenario, "mismatch") {
					require.Equal(t, 404, w.Code)
				}
				if scenario == "normal" || scenario == "wait then acquired" {
					require.Equal(t, 200, w.Code)
				}
				if scenario == "full" || scenario == "queue full" {
					require.Equal(t, http.StatusTooManyRequests, w.Code)
				}
			}
			require.Zero(t, bindings.writes, "lookups must preserve owner and TTL")
			if scenario == "mismatch acquired" {
				require.Equal(t, 20, slots.released)
			}
			if slots.full {
				require.Zero(t, slots.released)
				require.Zero(t, upstream.calls)
			}
			if scenario == "full" || strings.HasPrefix(scenario, "mismatch") {
				require.Zero(t, upstream.calls)
			}
			switch scenario {
			case "normal", "wait then acquired", "upstream error", "panic":
				require.Equal(t, 20, upstream.calls)
				require.Equal(t, 20, slots.released)
			case "cancel":
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, 1, slots.released)
			}
		})
	}
}

func TestGrokMediaEligibilityReleasesBeforeSwitch(t *testing.T) {
	for _, scenario := range []string{"switch", "exhausted", "cancel during probe"} {
		t.Run(scenario, func(t *testing.T) {
			h, slots, _, upstream := newGrokMediaSlotHandler(t, true, false)
			h.maxAccountSwitches = 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probes := 0
			h.grokMediaEligibilityProber = grokMediaSlotProber(func(context.Context, int64) (bool, string, error) {
				probes++
				if scenario == "cancel during probe" {
					cancel()
					// A probe is still upstream work until it returns. A client
					// disconnect must not free capacity for another request early.
					slots.mu.Lock()
					activeAccounts, activeUsers := len(slots.accounts), len(slots.users)
					slots.mu.Unlock()
					require.Equal(t, 1, activeAccounts)
					require.Equal(t, 1, activeUsers)
				}
				if scenario == "switch" && probes == 2 {
					return true, "eligible", nil
				}
				return false, "billing_unobserved", errors.New("probe failed")
			})
			c, w := grokMediaSlotContext(ctx, true)
			h.GrokVideoGeneration(c)
			slots.assertReleased(t)
			require.Equal(t, 1, slots.peak, "rejected attempts must release before the next selection")
			if scenario == "switch" {
				events, _ := c.Get(service.OpsUpstreamErrorsKey)
				detail, err := json.Marshal(events)
				require.NoError(t, err)
				require.Equal(t, 200, w.Code, "body=%s probes=%d upstream=%d events=%s", w.Body.String(), probes, upstream.calls, detail)
				require.Equal(t, 1, upstream.calls)
			}
			if scenario == "exhausted" {
				require.Equal(t, 503, w.Code)
				require.Equal(t, 2, slots.released)
				require.Zero(t, upstream.calls)
			}
			if scenario == "cancel during probe" {
				require.Equal(t, 1, probes, "disconnect must not select or probe a replacement account")
				require.Equal(t, 1, slots.released)
				require.Zero(t, upstream.calls, "a cancelled probe must not start media generation")
				require.Equal(t, statusClientClosedRequest, c.Writer.Status())
				require.Zero(t, w.Body.Len())
			}
		})
	}
}

func TestGrokMediaVideoLookupOwnerIsolation(t *testing.T) {
	for _, other := range []string{"user", "api key", "group", "task"} {
		t.Run(other, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			c, w := grokMediaSlotContext(context.Background(), false)
			key, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			switch other {
			case "user":
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11, Concurrency: 5})
			case "api key":
				key.ID = 21
			case "group":
				groupID := int64(25)
				key.GroupID = &groupID
			case "task":
				c.Params = gin.Params{{Key: "request_id", Value: "other-task"}}
			}
			h.GrokVideoStatus(c)
			require.Equal(t, http.StatusNotFound, w.Code)
			slots.assertReleased(t)
			require.Zero(t, slots.acquired)
			require.Zero(t, upstream.calls)
			require.Zero(t, bindings.writes)
		})
	}
}

func TestGrokMediaVideoCompletionStillClaimsBillingOnce(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID,
		service.GrokVideoPendingBilling{Model: "grok-imagine-video", VideoResolution: "720p", VideoDurationSeconds: 6}))
	pollByte := 25
	result := &service.OpenAIForwardResult{ResponseID: "task", Model: "grok-imagine-video",
		VideoCount: 1, VideoDurationSeconds: 6, FirstByteMs: &pollByte}
	for i := range 20 {
		_, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
		if i == 0 {
			require.NotNil(t, bill)
			require.Equal(t, service.StableGrokVideoBillingRequestID("task"), bill.RequestID)
			require.Nil(t, bill.FirstByteMs, "an async task settlement cannot borrow its discovery poll's clock")
			require.Equal(t, 25, *result.FirstByteMs, "the original HTTP response retains its own timing")
		} else {
			require.Nil(t, bill)
		}
	}
	require.Len(t, bindings.billed, 1)
}

func TestGrokVideoMissingPendingNeverRepricesOrBurnsSettlementClaim(t *testing.T) {
	for _, duration := range []int{0, 8} {
		t.Run(fmt.Sprintf("status_duration_%d", duration), func(t *testing.T) {
			h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
			result := &service.OpenAIForwardResult{ResponseID: "task", Model: "grok-imagine-video", VideoCount: 1, VideoDurationSeconds: duration}
			_, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
			require.Nil(t, bill, "status cannot reconstruct a missing create-time price or resolution")
			require.Empty(t, bindings.billed, "storage recovery must retain the ability to settle later")
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID,
				service.GrokVideoPendingBilling{Model: "grok-imagine-video", VideoResolution: "1080p", VideoDurationSeconds: 12}))
			_, bill = prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
			require.NotNil(t, bill)
			require.Equal(t, "1080p", bill.VideoResolution)
			require.Len(t, bindings.billed, 1)
		})
	}
}

func TestGrokVideoMalformedPricingSnapshotDoesNotBurnSettlementClaim(t *testing.T) {
	h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	subject := middleware2.AuthSubject{UserID: 10, Concurrency: 5}
	// Valid JSON with an unsupported snapshot schema is a corrupted persisted
	// billing generation. It must remain retryable after storage is repaired.
	pending := service.GrokVideoPendingBilling{Model: "grok-imagine-video", VideoDurationSeconds: 6, PricingSnapshot: json.RawMessage(`{"schema_version":-1}`)}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID, pending))
	result := &service.OpenAIForwardResult{ResponseID: "task", VideoCount: 1, VideoDurationSeconds: 6}
	_, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
	require.Nil(t, bill)
	require.Empty(t, bindings.billed, "a corrupt pricing generation must fail before claiming one-time settlement")
	pending.PricingSnapshot = nil // A historical task without this new field keeps its documented legacy behavior.
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(c.Request.Context(), "task", subject.UserID, key.ID, pending))
	_, bill = prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
	require.NotNil(t, bill, "repair must permit the first actual settlement")
	require.Len(t, bindings.billed, 1)
	_, bill = prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", result)
	require.Nil(t, bill, "the repaired settlement still occurs once")
}

func TestGrokVideoMissingDurationPricePreservesClaimForRecoveredSnapshot(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(fmt.Sprintf("free=%t", free), func(t *testing.T) {
			h, _, bindings, _ := newGrokMediaSlotHandler(t, false, false)
			c, _ := grokMediaSlotContext(context.Background(), false)
			key, _ := middleware2.GetAPIKeyFromContext(c)
			if free {
				zero := 0.0
				key.Group.VideoPrice480P = &zero
				key.Group.VideoPrice720P = &zero
				key.Group.VideoPrice1080P = &zero
			}
			subject := middleware2.AuthSubject{UserID: 10}
			create := h.gatewayService.WithBillingPricingSnapshot(c.Request.Context(), key, time.Now(), "public-video")
			good, err := service.ExportBillingPricingSnapshot(create, "grok-imagine-video-1.5")
			require.NoError(t, err)
			var broken map[string]any
			require.NoError(t, json.Unmarshal(good, &broken))
			videos := broken["videos"].(map[string]any)
			delete(videos, "grok-imagine-video-1.5")
			missing, err := json.Marshal(broken)
			require.NoError(t, err)
			pending := service.GrokVideoPendingBilling{Model: "public-video", OriginalModel: "public-video", BillingModel: "grok-imagine-video-1.5", UpstreamModel: "grok-imagine-video-1.5", VideoResolution: "720p", VideoDurationSeconds: 8, PricingSnapshot: missing}
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(create, "repair-video", subject.UserID, key.ID, pending))
			status := &service.OpenAIForwardResult{VideoCount: 1, ResponseID: "repair-video", VideoDurationSeconds: 8}
			_, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "repair-video", status)
			require.Nil(t, bill)
			require.Empty(t, bindings.billed, "missing duration prices must not consume the claim or schedule billing")
			pending.PricingSnapshot = good
			require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(create, "repair-video", subject.UserID, key.ID, pending))
			ctx, bill := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "repair-video", status)
			require.NotNil(t, bill)
			require.Len(t, bindings.billed, 1)
			require.NoError(t, service.ValidateBillingPricingMeter(ctx, "grok-imagine-video-1.5", "second"))
			_, bill = prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "repair-video", status)
			require.Nil(t, bill)
			require.Len(t, bindings.billed, 1)
		})
	}
}

func TestGrokVideoCompletionDoesNotReplaceSubmittedSKUWithStatusDefault(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false)
	c, _ := grokMediaSlotContext(context.Background(), false)
	key, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key.Group.BillingPolicyVersion = 12
	createCtx := h.gatewayService.WithBillingPricingSnapshot(c.Request.Context(), key, time.Now(), "public-video")
	subject := middleware2.AuthSubject{UserID: 10}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(createCtx, "task", subject.UserID, key.ID, service.GrokVideoPendingBilling{
		Model: "public-video", OriginalModel: "public-video", BillingModel: "grok-imagine-video-1.5", UpstreamModel: "grok-imagine-video-1.5", VideoResolution: "720p", VideoDurationSeconds: 8,
	}))
	// The upstream's documented status omits model, so extraction already
	// assigned the generic family. It must not select another frozen card.
	status := service.ExtractGrokVideoBillingFromStatusBody([]byte(`{"status":"done","video":{"url":"https://video.invalid/a.mp4","duration":8}}`), nil, "task")
	require.NotNil(t, status)
	require.Equal(t, "grok-imagine-video", status.BillingModel)
	_, billed := prepareGrokVideoCompletionBilling(c.Request.Context(), h, zap.NewNop(), key, subject, "task", status)
	require.NotNil(t, billed)
	require.Equal(t, "grok-imagine-video-1.5", billed.BillingModel)
	require.Equal(t, "grok-imagine-video-1.5", billed.UpstreamModel)
	require.Equal(t, "720p", billed.VideoResolution)
}
