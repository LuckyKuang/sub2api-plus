//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type refusalGuardTestStore struct {
	blocked   bool
	remaining time.Duration
	err       error
	count     int
}

func (s *refusalGuardTestStore) HasSession(context.Context, string) (bool, error) {
	s.count++
	return s.blocked, s.err
}
func (s *refusalGuardTestStore) CooldownRemaining(context.Context, int64) (time.Duration, error) {
	s.count++
	return s.remaining, s.err
}
func (s *refusalGuardTestStore) Record(context.Context, service.RefusalSession, service.UpstreamRefusalGuardConfig) (int64, error) {
	return 1, nil
}
func (s *refusalGuardTestStore) ListUsers(context.Context) ([]service.RefusalUser, error) {
	return nil, nil
}
func (s *refusalGuardTestStore) ListSessions(context.Context) ([]service.RefusalSession, error) {
	return nil, nil
}
func (s *refusalGuardTestStore) ClearUser(context.Context, int64) (int64, error)     { return 0, nil }
func (s *refusalGuardTestStore) ClearSession(context.Context, string) (int64, error) { return 0, nil }
func (s *refusalGuardTestStore) ClearAll(context.Context) (int64, error)             { return 0, nil }
func TestUpstreamRefusalGuardResponses(t *testing.T) {
	cfg := service.DefaultUpstreamRefusalGuardConfig()
	cfg.Enabled = true
	raw, _ := json.Marshal(cfg)
	for _, tc := range []struct {
		name   string
		store  refusalGuardTestStore
		status int
		code   string
		retry  string
	}{
		{"session", refusalGuardTestStore{blocked: true, remaining: time.Minute}, 403, "session_blocked_by_upstream_refusal", ""},
		{"cooldown", refusalGuardTestStore{remaining: time.Second + time.Millisecond}, 429, "user_cooldown_by_upstream_refusal", "2"},
		{"redis failure", refusalGuardTestStore{err: errors.New("redis unavailable")}, 0, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyUpstreamRefusalGuardConfig: string(raw)}}, nil)
			store := tc.store
			h := &GatewayHandler{refusalGuard: service.NewUpstreamRefusalGuardService(settings, &store, nil)}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			hit := h.checkUpstreamRefusal(c, 42, 9, "session", "request")
			if hit != (tc.status != 0) || (tc.status != 0 && recorder.Code != tc.status) {
				t.Fatalf("status %d hit %v", recorder.Code, hit)
			}
			if store.count == 0 {
				t.Fatal("store not queried")
			}
			if recorder.Header().Get("Retry-After") != tc.retry || !strings.Contains(recorder.Body.String(), tc.code) && tc.code != "" {
				t.Fatalf("response: %s", recorder.Body.String())
			}
		})
	}
}
