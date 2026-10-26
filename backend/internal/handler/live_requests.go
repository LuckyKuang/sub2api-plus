package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const liveRequestContextKey = "sub2api.live_requests"

type liveRequestContext struct {
	mu            sync.Mutex
	service       *service.LiveRequestService
	active        map[int]service.LiveRequest
	seen          map[int]bool
	started       time.Time
	stop          chan struct{}
	heartbeat     bool
	attemptOffset int
	lastTurn      int
	accountID     int64
	retrying      map[int]bool
}

// The middleware only attaches local metadata. Publishing starts after the
// existing security audit has returned; unauthenticated traffic is not recorded.
func LiveRequestContext(tracker *service.LiveRequestService) gin.HandlerFunc {
	return func(c *gin.Context) {
		state := &liveRequestContext{service: tracker, active: map[int]service.LiveRequest{}, seen: map[int]bool{}, retrying: map[int]bool{}, started: time.Now().UTC(), stop: make(chan struct{})}
		c.Set(liveRequestContextKey, state)
		c.Request = c.Request.WithContext(service.WithLiveOutputObserver(c.Request.Context(), state.output))
		returned := false
		defer func() {
			close(state.stop)
			state.mu.Lock()
			defer state.mu.Unlock()
			status := "finished"
			if c.Writer.Status() >= 400 || len(service.GetOpsStreamErrors(c)) > 0 {
				status = "failed"
			}
			if c.Request.Context().Err() != nil {
				status = "cancelled"
			}
			if !returned {
				status = "unknown"
			}
			for turn := range state.active {
				outcome := status
				if state.retrying[turn] && returned && status == "finished" {
					outcome = "failed"
				}
				if turn == 0 {
					record := state.active[turn]
					record.AccountID = c.GetInt64(opsAccountIDKey)
					record.HTTPStatus = c.Writer.Status()
					if record.HTTPStatus == 401 || record.HTTPStatus == 403 || record.HTTPStatus == 429 {
						outcome = "rejected"
					}
					if outcome == "failed" {
						record.Reason = "upstream_error"
					}
					if outcome == "rejected" {
						record.Reason = "request_rejected"
					}
					if outcome == "cancelled" {
						record.Reason = "client_disconnected"
					}
					state.active[turn] = record
				}
				state.finishLocked(turn, outcome, nil)
			}
		}()
		c.Next()
		returned = true
	}
}

func requestActivity(c *gin.Context) *liveRequestContext {
	if c == nil {
		return nil
	}
	value, _ := c.Get(liveRequestContextKey)
	state, _ := value.(*liveRequestContext)
	return state
}

func (s *liveRequestContext) output(turn, ms int, kind string) {
	defer func() {
		if recover() != nil {
			slog.Error("live_request_observation_exception", "stage", "first_output", "reason", "observer_panic")
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if turn > 0 {
		turn += s.attemptOffset
	}
	r, ok := s.active[turn]
	if !ok {
		return
	}
	r.Status, r.FirstOutputMs, r.FirstOutputKind = "transmitting", &ms, kind
	r.Version++
	r.UpdatedAt = time.Now().UTC()
	r.ElapsedMs = r.UpdatedAt.Sub(r.StartedAt).Milliseconds()
	s.active[turn] = r
	s.service.Publish(r)
}

func observeAuditedRequest(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, model, stage string, decision *securityaudit.Decision) {
	defer func() {
		if recover() != nil {
			slog.Error("live_request_observation_exception", "stage", stage, "reason", "observer_panic")
		}
	}()
	state := requestActivity(c)
	if state == nil || apiKey == nil || subject.UserID <= 0 {
		return
	}
	turn := 0
	switch stage {
	case "first_turn":
		turn = 1
	case "subsequent_turn":
		turn, _ = securityAuditWSTurn(c)
		if turn < 1 {
			return
		}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if turn > 0 {
		turn += state.attemptOffset
	}
	if state.seen[turn] {
		return
	}
	state.seen[turn] = true
	if turn > state.lastTurn {
		state.lastTurn = turn
	}
	// Long-lived sockets must not retain a key for every historical turn.
	for previous := range state.seen {
		if previous > 0 && previous < turn-64 {
			delete(state.seen, previous)
		}
	}
	now := time.Now().UTC()
	start := state.started
	kind := "sync"
	if c.GetBool(opsStreamKey) {
		kind = "stream"
	}
	if turn > 0 {
		kind = "ws_v2"
		start = now
	}
	if stage == "live_sideband" {
		kind = "live"
		start = now
	}
	requestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)
	record := service.LiveRequest{ID: uuid.NewString(), Version: 1, UserID: subject.UserID, APIKeyID: apiKey.ID, KeyName: apiKey.Name,
		Model: model, Endpoint: c.FullPath(), RequestID: requestID, RequestType: kind, Turn: turn, Status: "processing", StartedAt: start, UpdatedAt: now, ElapsedMs: now.Sub(start).Milliseconds()}
	ua, clientIP := c.Request.UserAgent(), ip.GetClientIP(c)
	record.UserAgent, record.IPAddress = &ua, &clientIP
	if turn == 0 {
		c.Request = c.Request.WithContext(service.WithLiveUsageBinding(c.Request.Context(), state.service, record.ID, record.UserID, record.APIKeyID))
	}
	if apiKey.Group != nil {
		record.GroupName = apiKey.Group.Name
	}
	state.active[turn] = record
	if decision != nil && !decision.AllowNextStage {
		record.Reason = "policy"
		state.active[turn] = record
		state.finishLocked(turn, "rejected", nil)
	} else {
		record.AccountID = state.accountID
		state.active[turn] = record
		state.service.Publish(record)
	}
	if !state.heartbeat {
		state.heartbeat = true
		go state.renew()
	}
}

func (s *liveRequestContext) renew() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for turn, record := range s.active {
				record.Version++
				record.UpdatedAt = now.UTC()
				record.ElapsedMs = now.Sub(record.StartedAt).Milliseconds()
				s.active[turn] = record
				s.service.Publish(record)
			}
			s.mu.Unlock()
		}
	}
}

func (s *liveRequestContext) finishLocked(turn int, status string, result *service.OpenAIForwardResult) {
	record, exists := s.active[turn]
	if !exists {
		return
	}
	now := time.Now().UTC()
	record.Version++
	record.Status = status
	record.UpdatedAt = now
	record.FinishedAt = &now
	record.ElapsedMs = now.Sub(record.StartedAt).Milliseconds()
	if result != nil {
		duration := result.Duration.Milliseconds()
		record.DurationMs = &duration
		record.InputTokens = &result.Usage.InputTokens
		record.OutputTokens = &result.Usage.OutputTokens
		record.ImageOutputTokens = &result.Usage.ImageOutputTokens
		record.AudioOutputTokens = &result.Usage.AudioOutputTokens
		record.FirstByteMs = result.FirstByteMs
		record.FirstTokenMs = result.FirstTokenMs
		record.FirstOutputKind = result.FirstOutputKind
		record.FirstOutputMs, record.LastTokenMs = result.FirstOutputMs, result.LastTokenMs
		record.TimingVersion = 1
		complete := result.UsageComplete()
		record.IsComplete = &complete
	}
	if status == "failed" {
		record.Reason = "upstream_error"
	}
	if status == "client_disconnected" || status == "cancelled" {
		record.Reason = "client_disconnected"
	}
	if status == "incomplete" {
		record.Reason = "incomplete"
	}
	delete(s.active, turn)
	delete(s.retrying, turn)
	s.service.Publish(record)
}

// Proxy turn numbers restart at one after failover. Keep a separate observer
// offset so retries retain the logical ID without changing audit turn semantics.
func beginLiveRequestAttempt(c *gin.Context, accountID int64) {
	s := requestActivity(c)
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attemptOffset = s.lastTurn
	for turn, record := range s.active {
		if turn > 0 && turn-1 < s.attemptOffset {
			s.attemptOffset = turn - 1
		}
		record.AccountID = accountID
		record.Version++
		record.UpdatedAt = time.Now().UTC()
		s.active[turn] = record
		s.service.Publish(record)
	}
	s.accountID = accountID
}

func finishLiveRequestTurn(c *gin.Context, turn int, result *service.OpenAIForwardResult, err error) {
	state := requestActivity(c)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	turn += state.attemptOffset
	var failover *service.UpstreamFailoverError
	if errors.As(err, &failover) {
		state.retrying[turn] = true
		return
	}
	status := "finished"
	if err != nil {
		status = "failed"
	} else if result != nil {
		if result.ClientDisconnect {
			status = "client_disconnected"
		} else if result.UsageComplete() {
			status = "completed"
		} else {
			status = "incomplete"
		}
	}
	state.finishLocked(turn, status, result)
}

func liveTurnUsageContext(c *gin.Context, ctx context.Context, turn int) context.Context {
	s := requestActivity(c)
	if s == nil {
		return ctx
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.active[turn+s.attemptOffset]
	if !ok {
		return ctx
	}
	return service.WithLiveUsageBinding(ctx, s.service, r.ID, r.UserID, r.APIKeyID)
}

func (h *UsageHandler) SetLiveRequests(s *service.LiveRequestService) {
	h.liveRequests = s
	if h.usageService != nil {
		s.SetUsageReader(h.usageService)
	}
}

func (h *UsageHandler) LiveRequests(c *gin.Context) {
	subject, ok := middleware2.GetReadSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	data, err := h.liveRequests.List(ctx, subject.UserID)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Live requests unavailable")
		return
	}
	allowErrors := h.settingService != nil && h.settingService.IsUserErrorViewAllowed(c.Request.Context())
	if !allowErrors {
		filtered := data.Items[:0]
		for _, record := range data.Items {
			if record.Status != "failed" && record.Status != "rejected" {
				filtered = append(filtered, record)
			}
		}
		data.Items = filtered
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, data)
}
