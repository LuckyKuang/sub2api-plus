package service

import (
	"strconv"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
)

// The server-held login receipt needs only the credential owner, without the
// unrelated scheduling, group and account-list metadata.
type cnOAuthLoginOwner struct {
	ID                      int64          `json:"id"`
	Platform                string         `json:"platform"`
	Type                    string         `json:"type"`
	Credentials             map[string]any `json:"credentials"`
	ProxyID                 *int64         `json:"proxy_id,omitempty"`
	Proxy                   *Proxy         `json:"proxy,omitempty"`
	Status                  string         `json:"status"`
	ErrorMessage            string         `json:"error_message"`
	Schedulable             bool           `json:"schedulable"`
	RateLimitedAt           *time.Time     `json:"rate_limited_at"`
	RateLimitResetAt        *time.Time     `json:"rate_limit_reset_at"`
	OverloadUntil           *time.Time     `json:"overload_until"`
	TempUnschedulableUntil  *time.Time     `json:"temp_unschedulable_until"`
	TempUnschedulableReason string         `json:"temp_unschedulable_reason"`
	Extra                   map[string]any `json:"extra"`
}

func cnOAuthLoginOwnerFromAccount(a *Account) cnOAuthLoginOwner {
	return cnOAuthLoginOwner{ID: a.ID, Platform: a.Platform, Type: a.Type, Credentials: a.Credentials, ProxyID: a.ProxyID, Proxy: a.Proxy,
		Status: a.Status, ErrorMessage: a.ErrorMessage, Schedulable: a.Schedulable, RateLimitedAt: a.RateLimitedAt, RateLimitResetAt: a.RateLimitResetAt, OverloadUntil: a.OverloadUntil,
		TempUnschedulableUntil: a.TempUnschedulableUntil, TempUnschedulableReason: a.TempUnschedulableReason, Extra: a.Extra}
}

func (o cnOAuthLoginOwner) account() *Account {
	return &Account{ID: o.ID, Platform: o.Platform, Type: o.Type, Credentials: o.Credentials, ProxyID: o.ProxyID, Proxy: o.Proxy,
		Status: o.Status, ErrorMessage: o.ErrorMessage, Schedulable: o.Schedulable, RateLimitedAt: o.RateLimitedAt, RateLimitResetAt: o.RateLimitResetAt, OverloadUntil: o.OverloadUntil,
		TempUnschedulableUntil: o.TempUnschedulableUntil, TempUnschedulableReason: o.TempUnschedulableReason, Extra: o.Extra}
}

func cnOAuthSessionProxy(id *int64, route string) (*Proxy, error) {
	if id == nil {
		return nil, nil
	}
	_, parsed, err := proxyurl.Parse(route)
	if err != nil || parsed == nil {
		return nil, cnoauth.ErrSession
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		return nil, cnoauth.ErrSession
	}
	p := &Proxy{ID: *id, Protocol: parsed.Scheme, Host: parsed.Hostname(), Port: port, Status: StatusActive}
	if parsed.User != nil {
		p.Username = parsed.User.Username()
		p.Password, _ = parsed.User.Password()
	}
	if _, err := (&Account{ProxyID: id, Proxy: p}).StrictProxyURL(); err != nil {
		return nil, cnoauth.ErrSession
	}
	return p, nil
}
