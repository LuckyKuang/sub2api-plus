package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type UpstreamRefusalRule struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	StatusCodes     []int    `json:"status_codes"`
	ErrorCodes      []string `json:"error_codes"`
	ErrorTypes      []string `json:"error_types"`
	MessageKeywords []string `json:"message_keywords"`
}
type UpstreamRefusalGuardConfig struct {
	Enabled                bool                  `json:"enabled"`
	UserThreshold          int                   `json:"user_threshold"`
	CountWindowSeconds     int                   `json:"count_window_seconds"`
	UserCooldownSeconds    int                   `json:"user_cooldown_seconds"`
	SessionBlockTTLSeconds int                   `json:"session_block_ttl_seconds"`
	Rules                  []UpstreamRefusalRule `json:"rules"`
}

func DefaultUpstreamRefusalGuardConfig() UpstreamRefusalGuardConfig {
	return UpstreamRefusalGuardConfig{UserThreshold: 3, CountWindowSeconds: 300, UserCooldownSeconds: 300, SessionBlockTTLSeconds: 2592000, Rules: []UpstreamRefusalRule{
		{ID: "vm2api_refusal_guard", Name: "vm2api refusal guard", Enabled: true, StatusCodes: []int{400}, ErrorCodes: []string{"refusal_guard"}, ErrorTypes: []string{"policy_blocked"}},
		{ID: "anthropic_usage_policy", Name: "Anthropic usage policy", Enabled: true, StatusCodes: []int{400}, MessageKeywords: []string{"violate our Usage Policy"}},
	}}
}
func (cfg UpstreamRefusalGuardConfig) Validate() error {
	for _, bound := range []struct {
		name            string
		value, min, max int
	}{
		{"user_threshold", cfg.UserThreshold, 1, 100}, {"count_window_seconds", cfg.CountWindowSeconds, 10, 86400},
		{"user_cooldown_seconds", cfg.UserCooldownSeconds, 10, 86400}, {"session_block_ttl_seconds", cfg.SessionBlockTTLSeconds, 60, 7776000},
	} {
		if bound.value < bound.min || bound.value > bound.max {
			return fmt.Errorf("%s must be between %d and %d", bound.name, bound.min, bound.max)
		}
	}
	ids := make(map[string]bool)
	for _, r := range cfg.Rules {
		if strings.TrimSpace(r.ID) == "" || ids[r.ID] || len(r.ErrorCodes)+len(r.ErrorTypes)+len(r.MessageKeywords) == 0 {
			return fmt.Errorf("invalid or duplicate rule: %q", r.ID)
		}
		ids[r.ID] = true
		for _, status := range r.StatusCodes {
			if status < 100 || status > 599 {
				return fmt.Errorf("invalid status code in rule %q", r.ID)
			}
		}
	}
	return nil
}
func matchesRefusalField(value string, candidates []string, substring bool) bool {
	for _, candidate := range candidates {
		if candidate != "" && ((substring && strings.Contains(strings.ToLower(value), strings.ToLower(candidate))) || (!substring && strings.EqualFold(value, candidate))) {
			return true
		}
	}
	return false
}
func MatchUpstreamRefusal(cfg UpstreamRefusalGuardConfig, status int, body []byte) (string, bool) {
	if !cfg.Enabled || !gjson.ValidBytes(body) {
		return "", false
	}
	code := gjson.GetBytes(body, "error.code").String()
	typ := gjson.GetBytes(body, "error.type").String()
	msg := gjson.GetBytes(body, "error.message").String()
	if typ == "" {
		typ = gjson.GetBytes(body, "type").String()
	}
	if msg == "" {
		msg = gjson.GetBytes(body, "message").String()
	}
	for _, r := range cfg.Rules {
		if !r.Enabled || len(r.ErrorCodes)+len(r.ErrorTypes)+len(r.MessageKeywords) == 0 {
			continue
		}
		if len(r.StatusCodes) > 0 {
			found := false
			for _, s := range r.StatusCodes {
				if s == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if matchesRefusalField(code, r.ErrorCodes, false) || matchesRefusalField(typ, r.ErrorTypes, false) || matchesRefusalField(msg, r.MessageKeywords, true) {
			return r.ID, true
		}
	}
	return "", false
}

type cachedUpstreamRefusalGuardConfig struct {
	config    UpstreamRefusalGuardConfig
	expiresAt int64
}

func (s *SettingService) GetUpstreamRefusalGuardConfig(ctx context.Context) UpstreamRefusalGuardConfig {
	fallback := DefaultUpstreamRefusalGuardConfig()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if v, ok := s.refusalGuardCache.Load().(*cachedUpstreamRefusalGuardConfig); ok && v != nil && time.Now().UnixNano() < v.expiresAt {
		return v.config
	}
	value, _, _ := s.refusalGuardSF.Do("config", func() (any, error) {
		if v, ok := s.refusalGuardCache.Load().(*cachedUpstreamRefusalGuardConfig); ok && v != nil && time.Now().UnixNano() < v.expiresAt {
			return v.config, nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyUpstreamRefusalGuardConfig)
		ttl := 60 * time.Second
		cfg := fallback
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			ttl = 5 * time.Second
			slog.Warn("refusal_guard.config_read_failed", "stage", "config", "error_code", "setting_read_failed")
		} else if err == nil {
			if json.Unmarshal([]byte(raw), &cfg) != nil || cfg.Validate() != nil {
				cfg = fallback
				ttl = 5 * time.Second
				slog.Warn("refusal_guard.config_invalid", "stage", "config", "error_code", "invalid_config")
			}
		}
		s.refusalGuardCache.Store(&cachedUpstreamRefusalGuardConfig{cfg, time.Now().Add(ttl).UnixNano()})
		return cfg, nil
	})
	if cfg, ok := value.(UpstreamRefusalGuardConfig); ok {
		return cfg
	}
	return fallback
}
func (s *SettingService) UpdateUpstreamRefusalGuardConfig(ctx context.Context, cfg UpstreamRefusalGuardConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err = s.settingRepo.Set(ctx, SettingKeyUpstreamRefusalGuardConfig, string(raw)); err != nil {
		return err
	}
	s.refusalGuardCache.Store(&cachedUpstreamRefusalGuardConfig{cfg, time.Now().Add(60 * time.Second).UnixNano()})
	return nil
}

const upstreamRefusalMarkKey = "upstream_refusal_guard_mark"

type UpstreamRefusalMark struct {
	RuleID string
	Status int
}

func GetUpstreamRefusalMark(c *gin.Context) (UpstreamRefusalMark, bool) {
	if c == nil {
		return UpstreamRefusalMark{}, false
	}
	v, ok := c.Get(upstreamRefusalMarkKey)
	if !ok {
		return UpstreamRefusalMark{}, false
	}
	mark, ok := v.(UpstreamRefusalMark)
	return mark, ok
}
func DetectUpstreamRefusal(c *gin.Context, settings *SettingService, ctx context.Context, status int, body []byte) {
	if c == nil || settings == nil || !c.GetBool("upstream_refusal_guard_eligible") {
		return
	}
	if ruleID, ok := MatchUpstreamRefusal(settings.GetUpstreamRefusalGuardConfig(ctx), status, body); ok {
		markUpstreamRefusal(c, ruleID, status)
	}
}
func markUpstreamRefusal(c *gin.Context, ruleID string, status int) {
	if c != nil && c.GetBool("upstream_refusal_guard_eligible") {
		c.Set(upstreamRefusalMarkKey, UpstreamRefusalMark{ruleID, status})
	}
}
