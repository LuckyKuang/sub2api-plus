//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/concurrencycache"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountWaitPlanBoundsMultiUserPressureDuringSlowUpstream429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache, _ := concurrencycache.New(t)
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	plan := &service.AccountWaitPlan{MaxConcurrency: 1, MaxWaiting: 2, Timeout: 30 * time.Second}
	requestsCtx, cancelRequests := context.WithCancel(context.Background())
	var wireCalls atomic.Int32
	var gateMu sync.Mutex
	gate := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wireCalls.Add(1)
		gateMu.Lock()
		current := gate
		gateMu.Unlock()
		select {
		case <-current:
			w.WriteHeader(http.StatusTooManyRequests)
		case <-r.Context().Done():
		}
	}))
	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.POST("/execute", func(c *gin.Context) {
		userID, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		userRelease, err := helper.AcquireUserSlot(c, userID, 3)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		defer userRelease()
		accountRelease, err := helper.AcquireAccountSlotForWaitPlan(c, 87001, plan, false, new(bool))
		if err != nil {
			status, _, code, _ := concurrencyErrorResponse(err, "account")
			c.String(status, code)
			return
		}
		defer accountRelease()
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, provider.URL, nil)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		response, err := provider.Client().Do(request)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		c.String(response.StatusCode, "provider-limited")
	})
	server := httptest.NewServer(router)
	defer func() {
		cancelRequests()
		gateMu.Lock()
		select {
		case <-gate:
		default:
			close(gate)
		}
		gateMu.Unlock()
		server.Close()
		provider.Close()
	}()
	client := server.Client()
	// A wave includes twenty sequential overflow requests. Those keep their
	// own short deadline; the admitted request and two legitimate waiters must
	// remain alive until that complete wave has been checked.
	client.Timeout = 30 * time.Second
	type result struct {
		status int
		body   string
		err    error
	}
	start := func(ctx context.Context, userID int64) <-chan result {
		done := make(chan result, 1)
		go func() {
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/execute", nil)
			if err != nil {
				done <- result{err: err}
				return
			}
			request.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
			response, err := client.Do(request)
			if err != nil {
				done <- result{err: err}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			done <- result{status: response.StatusCode, body: string(body), err: err}
		}()
		return done
	}
	waitCount := func(want int) {
		require.Eventually(t, func() bool {
			count, err := cache.GetAccountWaitingCount(context.Background(), 87001)
			return err == nil && count == want
		}, time.Second, 5*time.Millisecond)
	}
	for round := int64(0); round < 3; round++ {
		gateMu.Lock()
		gate = make(chan struct{})
		gateMu.Unlock()
		base := int64(88000) + round*100
		first := start(requestsCtx, base)
		require.Eventually(t, func() bool { return wireCalls.Load() == int32(round*4+1) }, time.Second, 5*time.Millisecond)
		cancelCtx, cancel := context.WithCancel(requestsCtx)
		second := start(cancelCtx, base+1)
		defer cancel()
		waitCount(1)
		third := start(requestsCtx, base+2)
		waitCount(2)
		for n := int64(3); n < 23; n++ {
			began := time.Now()
			overflowCtx, cancelOverflow := context.WithTimeout(requestsCtx, time.Second)
			got := <-start(overflowCtx, base+n)
			cancelOverflow()
			require.NoError(t, got.err)
			require.Equal(t, http.StatusTooManyRequests, got.status)
			require.Equal(t, gatewayQueueFullCode, got.body)
			require.Less(t, time.Since(began), time.Second, "overflow must not enter the slow account queue")
		}
		require.Equal(t, int32(round*4+1), wireCalls.Load(), "overflow cannot create another upstream request")
		waitCount(2)
		healthStarted := time.Now()
		healthCtx, cancelHealth := context.WithTimeout(requestsCtx, time.Second)
		healthRequest, err := http.NewRequestWithContext(healthCtx, http.MethodGet, server.URL+"/health", nil)
		require.NoError(t, err)
		healthResponse, err := client.Do(healthRequest)
		cancelHealth()
		require.NoError(t, err, "waiting executions must not block independent fixture health handling")
		require.NoError(t, healthResponse.Body.Close())
		require.Equal(t, http.StatusOK, healthResponse.StatusCode)
		require.Less(t, time.Since(healthStarted), time.Second)
		require.Equal(t, int32(round*4+1), wireCalls.Load(), "fixture health handling must not issue an upstream request")
		cancel()
		require.Error(t, (<-second).err)
		waitCount(1)
		fourth := start(requestsCtx, base+23)
		waitCount(2)
		gateMu.Lock()
		close(gate)
		gateMu.Unlock()
		for _, pending := range []<-chan result{first, third, fourth} {
			got := <-pending
			require.NoError(t, got.err)
			require.Equal(t, http.StatusTooManyRequests, got.status)
			require.Equal(t, "provider-limited", got.body)
		}
		waitCount(0)
		got := <-start(requestsCtx, base+24)
		require.NoError(t, got.err)
		require.Equal(t, "provider-limited", got.body, "capacity must be reusable after the rate-limited wave drains")
		require.Eventually(t, func() bool {
			for id := base; id <= base+24; id++ {
				count, err := cache.GetUserConcurrency(context.Background(), id)
				if err != nil || count != 0 {
					return false
				}
			}
			count, err := cache.GetAccountConcurrency(context.Background(), 87001)
			return err == nil && count == 0
		}, time.Second, 5*time.Millisecond, "rate-limited and cancelled requests must release all user/account owners")
	}
}

type accountWaitFailureCache struct {
	helperConcurrencyCacheStub
	failure    error
	queueCalls int
}

func (c *accountWaitFailureCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	c.queueCalls++
	return false, c.failure
}

func TestAccountWaitPlanRejectsUnavailableQueueBeforePolling(t *testing.T) {
	cache := &accountWaitFailureCache{failure: errors.New("queue unavailable")}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	c, response := newHelperTestContext(http.MethodPost, "/v1/responses")
	release, err := helper.AcquireAccountSlotForWaitPlan(c, 7, &service.AccountWaitPlan{MaxConcurrency: 1, MaxWaiting: 2, Timeout: 100 * time.Millisecond}, true, new(bool))
	require.ErrorIs(t, err, cache.failure)
	require.Nil(t, release)
	require.Equal(t, 1, cache.queueCalls)
	require.Equal(t, 1, cache.accountAcquireCalls, "queue failure must not start account polling")
	require.Zero(t, cache.accountReleaseCalls)
	require.False(t, response.Flushed)
}
