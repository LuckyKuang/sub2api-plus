package dto

import (
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// AdminAPIKeySummary is the credential-free API-key read model exposed by
// administrator endpoints. It deliberately omits plaintext key material,
// authorization/export payloads, and complete IP allow/deny lists.
type AdminAPIKeySummary struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	Name    string `json:"name"`
	GroupID *int64 `json:"group_id"`
	Status  string `json:"status"`

	HasIPWhitelist  bool `json:"has_ip_whitelist"`
	IPWhitelistSize int  `json:"ip_whitelist_size"`
	HasIPBlacklist  bool `json:"has_ip_blacklist"`
	IPBlacklistSize int  `json:"ip_blacklist_size"`

	LastUsedAt         *time.Time `json:"last_used_at"`
	LastUsedIP         *string    `json:"last_used_ip"`
	Quota              float64    `json:"quota"`
	QuotaUsed          float64    `json:"quota_used"`
	ExpiresAt          *time.Time `json:"expires_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CurrentConcurrency int        `json:"current_concurrency"`

	RateLimit5h   float64    `json:"rate_limit_5h"`
	RateLimit1d   float64    `json:"rate_limit_1d"`
	RateLimit7d   float64    `json:"rate_limit_7d"`
	Usage5h       float64    `json:"usage_5h"`
	Usage1d       float64    `json:"usage_1d"`
	Usage7d       float64    `json:"usage_7d"`
	Window5hStart *time.Time `json:"window_5h_start"`
	Window1dStart *time.Time `json:"window_1d_start"`
	Window7dStart *time.Time `json:"window_7d_start"`
	Reset5hAt     *time.Time `json:"reset_5h_at,omitempty"`
	Reset1dAt     *time.Time `json:"reset_1d_at,omitempty"`
	Reset7dAt     *time.Time `json:"reset_7d_at,omitempty"`

	Group *Group `json:"group,omitempty"`
}

// AdminSupportUser is the minimal target identity shown in the read-only
// support view. Administrator-only notes, authentication identities, and
// credential-bearing child objects are intentionally excluded.
type AdminSupportUser struct {
	ID            int64      `json:"id"`
	Email         string     `json:"email"`
	Username      string     `json:"username"`
	Role          string     `json:"role"`
	Balance       float64    `json:"balance"`
	FrozenBalance float64    `json:"frozen_balance"`
	Concurrency   int        `json:"concurrency"`
	RPMLimit      int        `json:"rpm_limit"`
	Status        string     `json:"status"`
	AllowedGroups []int64    `json:"allowed_groups"`
	LastActiveAt  *time.Time `json:"last_active_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func AdminSupportUserFromService(u *service.User) *AdminSupportUser {
	if u == nil {
		return nil
	}
	return &AdminSupportUser{
		ID:            u.ID,
		Email:         u.Email,
		Username:      u.Username,
		Role:          u.Role,
		Balance:       u.Balance,
		FrozenBalance: u.FrozenBalance,
		Concurrency:   u.Concurrency,
		RPMLimit:      u.RPMLimit,
		Status:        u.Status,
		AllowedGroups: append([]int64(nil), u.AllowedGroups...),
		LastActiveAt:  u.LastActiveAt,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

// AdminAPIKeySummaryFromService converts an API key without copying any
// credential-bearing fields into the administrator response.
func AdminAPIKeySummaryFromService(k *service.APIKey) *AdminAPIKeySummary {
	if k == nil {
		return nil
	}
	out := &AdminAPIKeySummary{
		ID:                 k.ID,
		UserID:             k.UserID,
		Name:               k.Name,
		GroupID:            k.GroupID,
		Status:             k.Status,
		HasIPWhitelist:     len(k.IPWhitelist) > 0,
		IPWhitelistSize:    len(k.IPWhitelist),
		HasIPBlacklist:     len(k.IPBlacklist) > 0,
		IPBlacklistSize:    len(k.IPBlacklist),
		LastUsedAt:         k.LastUsedAt,
		LastUsedIP:         k.LastUsedIP,
		Quota:              k.Quota,
		QuotaUsed:          k.QuotaUsed,
		ExpiresAt:          k.ExpiresAt,
		CreatedAt:          k.CreatedAt,
		UpdatedAt:          k.UpdatedAt,
		CurrentConcurrency: k.CurrentConcurrency,
		RateLimit5h:        k.RateLimit5h,
		RateLimit1d:        k.RateLimit1d,
		RateLimit7d:        k.RateLimit7d,
		Usage5h:            k.EffectiveUsage5h(),
		Usage1d:            k.EffectiveUsage1d(),
		Usage7d:            k.EffectiveUsage7d(),
		Window5hStart:      k.Window5hStart,
		Window1dStart:      k.Window1dStart,
		Window7dStart:      k.Window7dStart,
		Group:              GroupFromServiceShallow(k.Group),
	}
	if k.Window5hStart != nil && !service.IsWindowExpired(k.Window5hStart, service.RateLimitWindow5h) {
		t := k.Window5hStart.Add(service.RateLimitWindow5h)
		out.Reset5hAt = &t
	}
	if k.Window1dStart != nil && !service.IsWindowExpired(k.Window1dStart, service.RateLimitWindow1d) {
		t := k.Window1dStart.Add(service.RateLimitWindow1d)
		out.Reset1dAt = &t
	}
	if k.Window7dStart != nil && !service.IsWindowExpired(k.Window7dStart, service.RateLimitWindow7d) {
		t := k.Window7dStart.Add(service.RateLimitWindow7d)
		out.Reset7dAt = &t
	}
	return out
}
