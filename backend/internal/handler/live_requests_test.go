//go:build unit || !integration

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/liverequestcache"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/redisclient"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type liveOrderEngine struct {
	check func()
	block bool
}

func (e *liveOrderEngine) EffectiveMode() securityaudit.Mode                    { return securityaudit.ModeBlocking }
func (e *liveOrderEngine) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (e *liveOrderEngine) Evaluate(context.Context, securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.check()
	if e.block {
		return &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock, AllowNextStage: false}, nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}

func TestLiveObservationStartsAfterAuditAndKeepsWSTurnsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(LiveRequestContext(nil))
	r.POST("/v1/responses", func(c *gin.Context) {
		state := requestActivity(c)
		engine := &liveOrderEngine{check: func() { require.Empty(t, state.active, "audit must precede publishing an active request") }}
		coordinator := securityaudit.NewCoordinator(nil, engine)
		key := &service.APIKey{ID: 9, Name: "test-key"}
		subject := middleware2.AuthSubject{UserID: 7}
		payload := []byte(`{"model":"example","input":"hello"}`)
		decision := runSecurityAudit(c, nil, coordinator, nil, key, subject, "openai_responses", "example", payload, "first_turn")
		require.True(t, decision.AllowNextStage)
		require.Len(t, state.active, 1)
		firstID := state.active[1].ID
		engine.check = func() { require.Len(t, state.active, 1) }
		c.Set(securityAuditWSTurnContextKey, 2)
		runSecurityAudit(c, nil, coordinator, nil, key, subject, "openai_responses", "example", payload, "subsequent_turn")
		require.Len(t, state.active, 2)
		finishLiveRequestTurn(c, 1, &service.OpenAIForwardResult{Duration: time.Second}, nil)
		require.Len(t, state.active, 1)
		require.NotEqual(t, firstID, state.active[2].ID)
		// Repeating the audited ingress does not append a second logical turn.
		runSecurityAudit(c, nil, coordinator, nil, key, subject, "openai_responses", "example", payload, "subsequent_turn")
		require.Len(t, state.active, 1)
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestLiveObservationDoesNotPublishUnauditedOrRejectedAsActive(t *testing.T) {
	r := gin.New()
	r.Use(LiveRequestContext(nil))
	r.POST("/request", func(c *gin.Context) {
		state := requestActivity(c)
		require.Empty(t, state.active)
		engine := &liveOrderEngine{block: true, check: func() { require.Empty(t, state.active) }}
		d := runSecurityAudit(c, nil, securityaudit.NewCoordinator(nil, engine), nil, &service.APIKey{ID: 9}, middleware2.AuthSubject{UserID: 7}, "openai_responses", "example", []byte(`{"input":"hello"}`), "http")
		require.False(t, d.AllowNextStage)
		require.Empty(t, state.active, "audit rejection must not enter forwarding state")
		c.Status(http.StatusForbidden)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/request", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestLiveOutputCannotPublishBeforeAuditOrAfterTerminal(t *testing.T) {
	r := gin.New()
	r.Use(LiveRequestContext(nil))
	r.POST("/request", func(c *gin.Context) {
		s := requestActivity(c)
		s.output(0, 10, "text")
		require.Empty(t, s.active)
		observeAuditedRequest(c, &service.APIKey{ID: 9}, middleware2.AuthSubject{UserID: 7}, "model", "http", nil)
		s.output(0, 250, "text")
		require.Equal(t, "transmitting", s.active[0].Status)
		require.Equal(t, 250, *s.active[0].FirstOutputMs)
		s.mu.Lock()
		s.finishLocked(0, "failed", nil)
		s.mu.Unlock()
		s.output(0, 500, "text")
		require.Empty(t, s.active)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/request", nil))
}

func TestLiveHTTP200IsPendingAndStreamErrorRemainsFailed(t *testing.T) {
	tracker := service.NewLiveRequestService(liverequestcache.New(redisclient.New(t)))
	r := gin.New()
	r.Use(LiveRequestContext(tracker))
	r.POST("/:outcome", func(c *gin.Context) {
		observeAuditedRequest(c, &service.APIKey{ID: 9}, middleware2.AuthSubject{UserID: 7}, c.Param("outcome"), "http", nil)
		if c.Param("outcome") == "stream-error" {
			service.MarkOpsStreamError(c, "upstream_error", "private upstream diagnostic", 502)
		}
		c.Status(http.StatusOK)
	})
	for _, path := range []string{"/pending", "/stream-error"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	require.Eventually(t, func() bool {
		data, err := tracker.List(context.Background(), 7)
		if err != nil || len(data.Items) != 2 {
			return false
		}
		for _, item := range data.Items {
			if item.FinishedAt == nil {
				return false
			}
			if item.Model == "pending" {
				require.Equal(t, "finished", item.Status)
			} else {
				require.Equal(t, "failed", item.Status)
				require.Equal(t, "upstream_error", item.Reason)
			}
			require.Equal(t, 200, item.HTTPStatus)
			require.Nil(t, item.ActualCost)
			require.NotContains(t, item.Reason, "private")
		}
		return true
	}, time.Second, time.Millisecond)
}

// A retry is one logical turn. The proxy restarts its own numbering after failover.
func TestLiveObservationFailoverRetainsLogicalTurnAndNextTurnIsDistinct(t *testing.T) {
	r := gin.New()
	r.Use(LiveRequestContext(nil))
	r.POST("/request", func(c *gin.Context) {
		key := &service.APIKey{ID: 9}
		subject := middleware2.AuthSubject{UserID: 7}
		observeAuditedRequest(c, key, subject, "model", "first_turn", nil)
		state := requestActivity(c)
		first := state.active[1].ID
		beginLiveRequestAttempt(c, 10)
		finishLiveRequestTurn(c, 1, &service.OpenAIForwardResult{}, nil)
		c.Set(securityAuditWSTurnContextKey, 2)
		observeAuditedRequest(c, key, subject, "model", "subsequent_turn", nil)
		second := state.active[2].ID
		require.NotEqual(t, first, second)
		finishLiveRequestTurn(c, 2, nil, &service.UpstreamFailoverError{StatusCode: 503})
		require.Len(t, state.active, 1)
		beginLiveRequestAttempt(c, 11)
		require.Equal(t, second, state.active[2].ID)
		require.Equal(t, int64(11), state.active[2].AccountID)
		finishLiveRequestTurn(c, 1, &service.OpenAIForwardResult{Duration: time.Second}, nil)
		require.Empty(t, state.active)
		c.Set(securityAuditWSTurnContextKey, 2)
		observeAuditedRequest(c, key, subject, "model", "subsequent_turn", nil)
		require.Len(t, state.active, 1)
		require.NotEqual(t, second, state.active[3].ID)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/request", nil))
}

func TestLiveReadsRespectOwnerSupportTargetAndErrorVisibility(t *testing.T) {
	tracker := service.NewLiveRequestService(liverequestcache.New(redisclient.New(t)))
	now := time.Now().UTC()
	for _, record := range []service.LiveRequest{
		{ID: "owner-active", UserID: 7, Status: "processing", AccountID: 99},
		{ID: "owner-failed", UserID: 7, Status: "failed", AccountID: 99},
		{ID: "other", UserID: 8, Status: "processing", AccountID: 98},
	} {
		record.Version = 1
		record.StartedAt = now
		record.UpdatedAt = now
		tracker.Publish(record)
	}
	require.Eventually(t, func() bool {
		data, err := tracker.List(context.Background(), 0)
		return err == nil && len(data.Items) == 3
	}, time.Second, 5*time.Millisecond)
	h := &UsageHandler{liveRequests: tracker} // No setting service means error visibility is denied.
	for _, support := range []bool{false, true} {
		r := gin.New()
		r.GET("/usage/live", func(c *gin.Context) {
			actor := int64(7)
			role := "user"
			if support {
				actor = 8
				role = "admin"
				c.Set(middleware2.SupportReadTargetKey, middleware2.SupportReadTarget{Subject: middleware2.AuthSubject{UserID: 7}, Role: "user"})
			}
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: actor})
			c.Set(string(middleware2.ContextKeyUserRole), role)
			h.LiveRequests(c)
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/usage/live", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var body struct {
			Data service.LiveRequestSnapshot `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Len(t, body.Data.Items, 1)
		require.Equal(t, "owner-active", body.Data.Items[0].ID)
		require.Zero(t, body.Data.Items[0].AccountID)
	}
	r := gin.New()
	r.GET("/usage/live", h.LiveRequests)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/usage/live", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
