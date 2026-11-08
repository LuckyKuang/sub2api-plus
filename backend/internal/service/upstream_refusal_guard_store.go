package service

import (
	"context"
	"log/slog"
	"time"
)

type RefusalSession struct {
	BlockKey         string    `json:"block_key"`
	UserID           int64     `json:"user_id"`
	APIKeyID         int64     `json:"api_key_id"`
	SessionID        string    `json:"session_id"`
	RequestID        string    `json:"request_id"`
	RuleID           string    `json:"rule_id"`
	Model            string    `json:"model"`
	CreatedAt        time.Time `json:"created_at"`
	RemainingSeconds int64     `json:"remaining_seconds"`
}
type RefusalUser struct {
	UserID                   int64     `json:"user_id"`
	Email                    string    `json:"email"`
	Count                    int64     `json:"count"`
	CountTTLSeconds          int64     `json:"count_ttl_seconds"`
	CooldownUntil            time.Time `json:"cooldown_until"`
	CooldownRemainingSeconds int64     `json:"cooldown_remaining_seconds"`
}
type RefusalCooldown struct {
	UserID           int64     `json:"user_id"`
	Until            time.Time `json:"until"`
	TriggerRequestID string    `json:"trigger_request_id"`
	Count            int64     `json:"count"`
}
type UpstreamRefusalStore interface {
	HasSession(context.Context, string) (bool, error)
	CooldownRemaining(context.Context, int64) (time.Duration, error)
	Record(context.Context, RefusalSession, UpstreamRefusalGuardConfig) (int64, error)
	ListUsers(context.Context) ([]RefusalUser, error)
	ListSessions(context.Context) ([]RefusalSession, error)
	ClearUser(context.Context, int64) (int64, error)
	ClearSession(context.Context, string) (int64, error)
	ClearAll(context.Context) (int64, error)
}
type UpstreamRefusalGuardService struct {
	settings *SettingService
	store    UpstreamRefusalStore
	users    UserRepository
}

func NewUpstreamRefusalGuardService(settings *SettingService, store UpstreamRefusalStore, users UserRepository) *UpstreamRefusalGuardService {
	return &UpstreamRefusalGuardService{settings, store, users}
}
func (s *UpstreamRefusalGuardService) Config(ctx context.Context) UpstreamRefusalGuardConfig {
	if s == nil {
		return DefaultUpstreamRefusalGuardConfig()
	}
	return s.settings.GetUpstreamRefusalGuardConfig(ctx)
}
func (s *UpstreamRefusalGuardService) UpdateConfig(ctx context.Context, c UpstreamRefusalGuardConfig) error {
	return s.settings.UpdateUpstreamRefusalGuardConfig(ctx, c)
}
func refusalLog(requestID string, userID, apiKeyID int64, stage string) {
	slog.Warn("upstream_refusal_guard.redis_failed", "request_id", requestID, "user_id", userID, "api_key_id", apiKeyID, "endpoint", "/v1/messages", "stage", stage, "error_code", "redis_unavailable")
}
func (s *UpstreamRefusalGuardService) Check(ctx context.Context, userID, apiKeyID int64, sessionID, requestID string) (bool, time.Duration) {
	if s == nil || s.store == nil || !s.Config(ctx).Enabled {
		return false, 0
	}
	if key := contentModerationSessionBlockKey(userID, apiKeyID, sessionID); key != "" {
		hit, err := s.store.HasSession(ctx, key)
		if err != nil {
			refusalLog(requestID, userID, apiKeyID, "session_check")
			return false, 0
		} else if hit {
			return true, 0
		}
	}
	if userID > 0 {
		remaining, err := s.store.CooldownRemaining(ctx, userID)
		if err != nil {
			refusalLog(requestID, userID, apiKeyID, "cooldown_check")
		} else {
			return false, remaining
		}
	}
	return false, 0
}
func (s *UpstreamRefusalGuardService) RecordRefusal(ctx context.Context, userID, apiKeyID int64, sessionID, requestID, ruleID, model string) {
	if s == nil || s.store == nil {
		return
	}
	cfg := s.Config(ctx)
	if !cfg.Enabled {
		return
	}
	key := contentModerationSessionBlockKey(userID, apiKeyID, sessionID)
	if runes := []rune(sessionID); len(runes) > 128 {
		sessionID = string(runes[:128])
	}
	session := RefusalSession{BlockKey: key, UserID: userID, APIKeyID: apiKeyID, SessionID: sessionID, RequestID: requestID, RuleID: ruleID, Model: model, CreatedAt: time.Now().UTC()}
	if _, err := s.store.Record(ctx, session, cfg); err != nil {
		refusalLog(requestID, userID, apiKeyID, "record")
	}
}
func (s *UpstreamRefusalGuardService) ListUsers(ctx context.Context) ([]RefusalUser, error) {
	items, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	if s.users != nil {
		for i := range items {
			if user, getErr := s.users.GetByID(ctx, items[i].UserID); getErr == nil && user != nil {
				items[i].Email = user.Email
			}
		}
	}
	return items, nil
}
func (s *UpstreamRefusalGuardService) ListSessions(ctx context.Context) ([]RefusalSession, error) {
	return s.store.ListSessions(ctx)
}
func (s *UpstreamRefusalGuardService) ClearUser(ctx context.Context, id int64) (int64, error) {
	return s.store.ClearUser(ctx, id)
}
func (s *UpstreamRefusalGuardService) ClearSession(ctx context.Context, key string) (int64, error) {
	return s.store.ClearSession(ctx, key)
}
func (s *UpstreamRefusalGuardService) ClearAll(ctx context.Context) (int64, error) {
	return s.store.ClearAll(ctx)
}
