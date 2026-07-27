package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/redis/go-redis/v9"
)

var ErrIPAccessRuleNotFound = infraerrors.NotFound("IP_ACCESS_RULE_NOT_FOUND", "IP access rule not found")

const (
	SettingKeyIPAccessControlEnabled       = "ip_access_control_enabled"
	SettingKeyLoginFailureAutoBlockEnabled = "login_failure_auto_block_enabled"
	SettingKeyLoginFailureIPThreshold      = "login_failure_ip_threshold"
	SettingKeyLoginFailureWindowMinutes    = "login_failure_window_minutes"
	SettingKeyLoginFailureBlockMinutes     = "login_failure_block_minutes"

	defaultLoginFailureIPThreshold   = 8
	defaultLoginFailureWindowMinutes = 15
	defaultLoginFailureBlockMinutes  = 24 * 60
	ipAccessSettingsCacheTTL         = 5 * time.Second
	ipAccessRulesCacheTTL            = 3 * time.Second
	ipAccessFailureCleanupInterval   = time.Hour
	ipAccessBlockHitFlushInterval    = time.Minute
	ipAccessBlockHitWriteTimeout     = 2 * time.Second
	ipAccessBlockHitTrackedIPLimit   = 4096
	ipAccessBlockHitWriteConcurrency = 4
	ipAccessInvalidationChannel      = "sub2api:ip_access_control:invalidate"
)

type IPAccessRuleKind string

const (
	IPAccessRuleKindManualBlock IPAccessRuleKind = "manual_block"
	IPAccessRuleKindAutoBlock   IPAccessRuleKind = "auto_block"
	IPAccessRuleKindAllow       IPAccessRuleKind = "allow"
)

type IPAccessRuleStatus string

const (
	IPAccessRuleStatusActive   IPAccessRuleStatus = "active"
	IPAccessRuleStatusReleased IPAccessRuleStatus = "released"
	IPAccessRuleStatusExpired  IPAccessRuleStatus = "expired"
)

type IPAccessControlSettings struct {
	EnforcementEnabled     bool `json:"enforcement_enabled"`
	LoginFailureAutoBlock  bool `json:"login_failure_auto_block_enabled"`
	LoginFailureThreshold  int  `json:"login_failure_threshold"`
	LoginFailureWindowMins int  `json:"login_failure_window_minutes"`
	LoginFailureBlockMins  int  `json:"login_failure_block_minutes"`
}

func DefaultIPAccessControlSettings() IPAccessControlSettings {
	return IPAccessControlSettings{
		EnforcementEnabled:     false,
		LoginFailureAutoBlock:  false,
		LoginFailureThreshold:  defaultLoginFailureIPThreshold,
		LoginFailureWindowMins: defaultLoginFailureWindowMinutes,
		LoginFailureBlockMins:  defaultLoginFailureBlockMinutes,
	}
}

func (s IPAccessControlSettings) Validate() (IPAccessControlSettings, error) {
	if s.LoginFailureThreshold < 2 || s.LoginFailureThreshold > 100 {
		return s, infraerrors.BadRequest("IP_ACCESS_SETTINGS_INVALID", "login failure threshold must be between 2 and 100")
	}
	if s.LoginFailureWindowMins < 1 || s.LoginFailureWindowMins > 24*60 {
		return s, infraerrors.BadRequest("IP_ACCESS_SETTINGS_INVALID", "login failure window must be between 1 and 1440 minutes")
	}
	if s.LoginFailureBlockMins < 1 || s.LoginFailureBlockMins > 365*24*60 {
		return s, infraerrors.BadRequest("IP_ACCESS_SETTINGS_INVALID", "login failure block duration must be between 1 and 525600 minutes")
	}
	if !s.EnforcementEnabled {
		s.LoginFailureAutoBlock = false
	}
	return s, nil
}

// AutomaticBlockingActive is deliberately stricter than the UI flag alone:
// turning off global enforcement also stops collection and new auto bans.
func (s IPAccessControlSettings) AutomaticBlockingActive() bool {
	return s.EnforcementEnabled && s.LoginFailureAutoBlock
}

type IPAccessRule struct {
	ID               int64              `json:"id"`
	IPOrCIDR         string             `json:"ip_or_cidr"`
	RuleKind         IPAccessRuleKind   `json:"rule_kind"`
	Status           IPAccessRuleStatus `json:"status"`
	Reason           string             `json:"reason"`
	FailureCount     int                `json:"failure_count"`
	FirstFailedAt    *time.Time         `json:"first_failed_at,omitempty"`
	LastFailedAt     *time.Time         `json:"last_failed_at,omitempty"`
	BlockedAt        *time.Time         `json:"blocked_at,omitempty"`
	ExpiresAt        *time.Time         `json:"expires_at,omitempty"`
	LastSeenAt       *time.Time         `json:"last_seen_at,omitempty"`
	HitCount         int64              `json:"hit_count"`
	CreatedByUserID  *int64             `json:"created_by_user_id,omitempty"`
	ReleasedByUserID *int64             `json:"released_by_user_id,omitempty"`
	ReleasedAt       *time.Time         `json:"released_at,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type IPAccessRuleFilter struct {
	Page     int
	PageSize int
	Status   IPAccessRuleStatus
	Query    string
}

type IPAccessRuleList struct {
	Items    []*IPAccessRule
	Total    int64
	Page     int
	PageSize int
}

type IPLoginFailureState struct {
	NormalizedIP     string    `json:"normalized_ip"`
	FailureCount     int       `json:"failure_count"`
	WindowStartedAt  time.Time `json:"window_started_at"`
	LastFailedAt     time.Time `json:"last_failed_at"`
	WindowExpiresAt  time.Time `json:"window_expires_at"`
	CurrentlyBlocked bool      `json:"currently_blocked"`
	AutoBlockRuleID  *int64    `json:"auto_block_rule_id,omitempty"`
}

type IPLoginFailureStateFilter struct {
	Page     int
	PageSize int
	Query    string
}

type IPLoginFailureStateList struct {
	Items    []*IPLoginFailureState
	Total    int64
	Page     int
	PageSize int
}

type LoginFailureRecordResult struct {
	FailureCount int
	Blocked      bool
	Rule         *IPAccessRule
}

type IPAccessControlRepository interface {
	ListIPAccessRules(ctx context.Context, filter IPAccessRuleFilter) (*IPAccessRuleList, error)
	ListActiveIPAccessRules(ctx context.Context) ([]*IPAccessRule, error)
	CreateManualIPAccessRule(ctx context.Context, rule *IPAccessRule) (*IPAccessRule, error)
	ReleaseIPAccessRuleAndReset(ctx context.Context, id, actorUserID int64) (*IPAccessRule, error)
	ListIPLoginFailureStates(ctx context.Context, filter IPLoginFailureStateFilter, window time.Duration) (*IPLoginFailureStateList, error)
	ResetIPLoginFailureState(ctx context.Context, normalizedIP string) error
	RecordFailedLogin(ctx context.Context, normalizedIP string, threshold int, window, blockFor time.Duration) (*LoginFailureRecordResult, error)
	RecordIPAccessRuleHit(ctx context.Context, normalizedIP string) error
}

type cachedIPAccessSettings struct {
	value     IPAccessControlSettings
	expiresAt time.Time
	valid     bool
}

type cachedIPAccessRules struct {
	rules      []*IPAccessRule
	allowRules *ip.CompiledIPRules
	blockRules *ip.CompiledIPRules
	expiresAt  time.Time
	valid      bool
}

// IPAccessDecision is the single policy result used by protocol adapters.
// Callers must not turn an unavailable/unsafe decision into an implicit allow.
type IPAccessDecision struct {
	Allowed      bool
	Blocked      bool
	EffectiveIP  string
	DirectPeerIP string
	Source       ip.ClientIdentitySource
	Reason       string
}

var ErrIPAccessIdentityUnavailable = errors.New("IP access control client identity unavailable")

// IPAccessFailureStateCleanupRepository is intentionally optional to preserve
// compact fake repositories. The production PostgreSQL repository implements
// it and removes stale counters outside the authentication request path.
type IPAccessFailureStateCleanupRepository interface {
	CleanupExpiredIPLoginFailureStates(ctx context.Context, before time.Time, limit int) (int64, error)
}

// IPAccessControlService owns the durable global block rules and the local
// password/TOTP failure counters. Rules are cached briefly for request-path
// efficiency, while every administrative mutation explicitly invalidates it.
type IPAccessControlService struct {
	settings SettingRepository
	repo     IPAccessControlRepository

	mu             sync.RWMutex
	settingsCache  cachedIPAccessSettings
	rulesCache     cachedIPAccessRules
	emergencyAllow *ip.CompiledIPRules
	emergencyCount int

	blockHitMu           sync.Mutex
	blockHitLastRecorded map[string]time.Time
	blockHitWriteSlots   chan struct{}

	redis            *redis.Client
	invalidationOnce sync.Once
	cleanupOnce      sync.Once
}

func NewIPAccessControlService(settings SettingRepository, repo IPAccessControlRepository) *IPAccessControlService {
	return &IPAccessControlService{
		settings:             settings,
		repo:                 repo,
		blockHitLastRecorded: make(map[string]time.Time),
		blockHitWriteSlots:   make(chan struct{}, ipAccessBlockHitWriteConcurrency),
	}
}

// ConfigureEmergencyAllowlist installs a deployment-controlled recovery path.
// It is intentionally not persisted in system settings, so a blocked admin can
// recover by changing container configuration and restarting the application.
func (s *IPAccessControlService) ConfigureEmergencyAllowlist(values []string) error {
	if s == nil {
		return errors.New("ip access control unavailable")
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = ip.NormalizeNonGlobalIPOrCIDR(value)
		if value == "" {
			return fmt.Errorf("invalid IP access emergency allowlist entry: must be a non-global IP or CIDR")
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	s.mu.Lock()
	s.emergencyAllow = ip.CompileIPRules(normalized)
	s.emergencyCount = len(normalized)
	s.mu.Unlock()
	return nil
}

func (s *IPAccessControlService) EmergencyAllowlistStatus() (configured bool, count int) {
	if s == nil {
		return false, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.emergencyCount > 0, s.emergencyCount
}

// SetInvalidationClient enables immediate cache invalidation across API
// instances. It is intentionally optional so compact standalone and unit-test
// deployments retain the same construction contract.
func (s *IPAccessControlService) SetInvalidationClient(client *redis.Client) {
	if s == nil {
		return
	}
	s.redis = client
}

// StartInvalidationSubscriber keeps the local security snapshot coherent when
// another instance changes a rule or the global settings. Redis failure never
// weakens the local policy: the normal short TTL remains the fallback.
func (s *IPAccessControlService) StartInvalidationSubscriber(ctx context.Context) {
	if s == nil || s.redis == nil {
		return
	}
	s.invalidationOnce.Do(func() {
		go func() {
			subscription := s.redis.Subscribe(ctx, ipAccessInvalidationChannel)
			defer func() { _ = subscription.Close() }()
			for {
				select {
				case <-ctx.Done():
					return
				case message, ok := <-subscription.Channel():
					if !ok {
						return
					}
					if message != nil {
						s.invalidateAll()
					}
				}
			}
		}()
	})
}

// StartFailureStateCleanup keeps expired failure counters bounded even when a
// deployment receives no further failed logins. Rule history is never touched.
func (s *IPAccessControlService) StartFailureStateCleanup(ctx context.Context) {
	if s == nil {
		return
	}
	repo, ok := s.repo.(IPAccessFailureStateCleanupRepository)
	if !ok || repo == nil {
		return
	}
	s.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(ipAccessFailureCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					deleted, err := s.cleanupExpiredFailureStates(ctx, repo)
					if err != nil {
						slog.Warn("IP access failure-state cleanup failed", "error", err)
					} else if deleted > 0 {
						slog.Info("IP access failure-state cleanup completed", "deleted", deleted)
					}
				}
			}
		}()
	})
}

func (s *IPAccessControlService) cleanupExpiredFailureStates(ctx context.Context, repo IPAccessFailureStateCleanupRepository) (int64, error) {
	if s == nil || repo == nil {
		return 0, nil
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return 0, err
	}
	before := time.Now().Add(-time.Duration(settings.LoginFailureWindowMins) * time.Minute)
	return repo.CleanupExpiredIPLoginFailureStates(ctx, before, 1000)
}

// Warmup loads the first complete security snapshot before production HTTP
// traffic is accepted. A missing settings or rules snapshot must never be
// interpreted as access control being disabled.
func (s *IPAccessControlService) Warmup(ctx context.Context) error {
	if s == nil || s.settings == nil || s.repo == nil {
		return errors.New("ip access control unavailable")
	}
	if _, err := s.GetSettings(ctx); err != nil {
		return err
	}
	if _, err := s.activeRules(ctx); err != nil {
		return err
	}
	return nil
}

// SecuritySnapshotReady reports whether both halves of the last-known-good
// security snapshot have been loaded. Snapshot TTL expiry does not make the
// service unready because refresh failures deliberately retain stale policy.
func (s *IPAccessControlService) SecuritySnapshotReady() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settingsCache.valid && s.rulesCache.valid
}

func (s *IPAccessControlService) GetSettings(ctx context.Context) (IPAccessControlSettings, error) {
	if s == nil || s.settings == nil {
		return DefaultIPAccessControlSettings(), errors.New("ip access control settings unavailable")
	}
	now := time.Now()
	s.mu.RLock()
	cached := s.settingsCache
	s.mu.RUnlock()
	if now.Before(cached.expiresAt) {
		return cached.value, nil
	}

	keys := []string{
		SettingKeyIPAccessControlEnabled,
		SettingKeyLoginFailureAutoBlockEnabled,
		SettingKeyLoginFailureIPThreshold,
		SettingKeyLoginFailureWindowMinutes,
		SettingKeyLoginFailureBlockMinutes,
	}
	values, err := s.settings.GetMultiple(ctx, keys)
	if err != nil {
		s.mu.Lock()
		fallback := s.settingsCache
		if fallback.valid {
			fallback.expiresAt = now.Add(ipAccessSettingsCacheTTL)
			s.settingsCache = fallback
		}
		s.mu.Unlock()
		if fallback.valid {
			return fallback.value, nil
		}
		return DefaultIPAccessControlSettings(), err
	}
	settings := DefaultIPAccessControlSettings()
	settings.EnforcementEnabled = parseIPAccessBool(values[SettingKeyIPAccessControlEnabled], settings.EnforcementEnabled)
	settings.LoginFailureAutoBlock = parseIPAccessBool(values[SettingKeyLoginFailureAutoBlockEnabled], settings.LoginFailureAutoBlock)
	settings.LoginFailureThreshold = parseBoundedIPAccessInt(values[SettingKeyLoginFailureIPThreshold], settings.LoginFailureThreshold, 2, 100)
	settings.LoginFailureWindowMins = parseBoundedIPAccessInt(values[SettingKeyLoginFailureWindowMinutes], settings.LoginFailureWindowMins, 1, 24*60)
	settings.LoginFailureBlockMins = parseBoundedIPAccessInt(values[SettingKeyLoginFailureBlockMinutes], settings.LoginFailureBlockMins, 1, 365*24*60)
	settings, _ = settings.Validate()
	s.mu.Lock()
	s.settingsCache = cachedIPAccessSettings{value: settings, expiresAt: now.Add(ipAccessSettingsCacheTTL), valid: true}
	s.mu.Unlock()
	return settings, nil
}

func (s *IPAccessControlService) UpdateSettings(ctx context.Context, settings IPAccessControlSettings) (IPAccessControlSettings, error) {
	if s == nil || s.settings == nil {
		return DefaultIPAccessControlSettings(), errors.New("ip access control settings unavailable")
	}
	validated, err := settings.Validate()
	if err != nil {
		return DefaultIPAccessControlSettings(), err
	}
	updates := map[string]string{
		SettingKeyIPAccessControlEnabled:       strconv.FormatBool(validated.EnforcementEnabled),
		SettingKeyLoginFailureAutoBlockEnabled: strconv.FormatBool(validated.LoginFailureAutoBlock),
		SettingKeyLoginFailureIPThreshold:      strconv.Itoa(validated.LoginFailureThreshold),
		SettingKeyLoginFailureWindowMinutes:    strconv.Itoa(validated.LoginFailureWindowMins),
		SettingKeyLoginFailureBlockMinutes:     strconv.Itoa(validated.LoginFailureBlockMins),
	}
	if err := s.settings.SetMultiple(ctx, updates); err != nil {
		return DefaultIPAccessControlSettings(), err
	}
	s.mu.Lock()
	s.settingsCache = cachedIPAccessSettings{value: validated, expiresAt: time.Now().Add(ipAccessSettingsCacheTTL), valid: true}
	s.rulesCache = cachedIPAccessRules{}
	s.mu.Unlock()
	s.publishInvalidation(ctx)
	return validated, nil
}

func (s *IPAccessControlService) ListRules(ctx context.Context, filter IPAccessRuleFilter) (*IPAccessRuleList, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("ip access control unavailable")
	}
	switch filter.Status {
	case "", IPAccessRuleStatusActive, IPAccessRuleStatusReleased, IPAccessRuleStatusExpired:
	default:
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_STATUS_INVALID", "invalid IP access rule status")
	}
	return s.repo.ListIPAccessRules(ctx, filter)
}

func (s *IPAccessControlService) AddManualRule(ctx context.Context, value string, kind IPAccessRuleKind, reason string, expiresAt *time.Time, actorUserID int64) (*IPAccessRule, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("ip access control unavailable")
	}
	value = ip.NormalizeNonGlobalIPOrCIDR(value)
	if value == "" {
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_INVALID", "IP rule must be a non-global IP address or CIDR")
	}
	if kind != IPAccessRuleKindManualBlock && kind != IPAccessRuleKindAllow {
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_INVALID", "manual rule kind must be manual_block or allow")
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_INVALID", "expiration must be in the future")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_INVALID", "reason is too long")
	}
	rule := &IPAccessRule{
		IPOrCIDR:        value,
		RuleKind:        kind,
		Status:          IPAccessRuleStatusActive,
		Reason:          reason,
		ExpiresAt:       expiresAt,
		CreatedByUserID: &actorUserID,
	}
	created, err := s.repo.CreateManualIPAccessRule(ctx, rule)
	if err == nil {
		s.invalidateRules()
		s.publishInvalidation(ctx)
	}
	return created, err
}

func (s *IPAccessControlService) ReleaseRuleAndReset(ctx context.Context, id, actorUserID int64) (*IPAccessRule, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("ip access control unavailable")
	}
	rule, err := s.repo.ReleaseIPAccessRuleAndReset(ctx, id, actorUserID)
	if err == nil {
		s.invalidateRules()
		s.publishInvalidation(ctx)
	}
	return rule, err
}

func (s *IPAccessControlService) ResetFailureState(ctx context.Context, rawIP string) error {
	if s == nil || s.repo == nil {
		return errors.New("ip access control unavailable")
	}
	normalized := ip.NormalizeIP(rawIP)
	if normalized == "" {
		return infraerrors.BadRequest("IP_LOGIN_FAILURE_IP_INVALID", "invalid IP address")
	}
	return s.repo.ResetIPLoginFailureState(ctx, normalized)
}

// ClearSuccessfulLogin removes an IP's failure streak after any completed
// authentication flow, including local password/TOTP and external OAuth. It
// intentionally does nothing while automatic blocking is disabled, so the
// feature has no authentication-state writes until an administrator enables it.
func (s *IPAccessControlService) ClearSuccessfulLogin(ctx context.Context, rawIP string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.AutomaticBlockingActive() {
		return nil
	}
	normalized := ip.NormalizeIP(rawIP)
	if normalized == "" {
		return nil
	}
	return s.repo.ResetIPLoginFailureState(ctx, normalized)
}

// ClearSuccessfulLocalLogin is retained for package callers compiled against
// the initial feature implementation. Its semantics now cover every completed
// authentication method.
func (s *IPAccessControlService) ClearSuccessfulLocalLogin(ctx context.Context, rawIP string) error {
	return s.ClearSuccessfulLogin(ctx, rawIP)
}

func (s *IPAccessControlService) ListFailureStates(ctx context.Context, filter IPLoginFailureStateFilter) (*IPLoginFailureStateList, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("ip access control unavailable")
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.repo.ListIPLoginFailureStates(
		ctx,
		filter,
		time.Duration(settings.LoginFailureWindowMins)*time.Minute,
	)
	if err != nil {
		return nil, err
	}
	if !settings.EnforcementEnabled {
		for _, state := range list.Items {
			if state != nil {
				state.CurrentlyBlocked = false
			}
		}
	}
	return list, nil
}

// RecordFailedLogin accepts only the trusted, already normalized client IP.
// It intentionally does not run when the automatic-control switch is off.
func (s *IPAccessControlService) RecordFailedLogin(ctx context.Context, rawIP string) (*LoginFailureRecordResult, error) {
	return s.RecordFailedLoginForIdentity(ctx, ip.ClientIdentity{
		EffectiveIP:        ip.NormalizeIP(rawIP),
		SafeForEnforcement: true,
	})
}

// RecordFailedLoginForIdentity records a credential failure only for a source
// verified by the global client-identity resolver.
func (s *IPAccessControlService) RecordFailedLoginForIdentity(ctx context.Context, identity ip.ClientIdentity) (*LoginFailureRecordResult, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("ip access control unavailable")
	}
	settings, err := s.GetSettings(ctx)
	if err != nil || !settings.AutomaticBlockingActive() {
		return nil, err
	}
	if !identity.SafeForEnforcement || identity.EffectiveIP == "" {
		return nil, ErrIPAccessIdentityUnavailable
	}
	normalized := ip.NormalizeIP(identity.EffectiveIP)
	if normalized == "" {
		return nil, ErrIPAccessIdentityUnavailable
	}
	result, err := s.repo.RecordFailedLogin(
		ctx,
		normalized,
		settings.LoginFailureThreshold,
		time.Duration(settings.LoginFailureWindowMins)*time.Minute,
		time.Duration(settings.LoginFailureBlockMins)*time.Minute,
	)
	if err == nil && result != nil && result.Blocked {
		s.invalidateRules()
		s.publishInvalidation(ctx)
	}
	return result, err
}

// Evaluate applies the compiled global policy to one verified request identity.
// With enforcement disabled it deliberately permits requests without demanding
// a proxy identity, so a configuration repair does not take the whole service
// down before the security switch is enabled.
func (s *IPAccessControlService) Evaluate(ctx context.Context, identity ip.ClientIdentity) (IPAccessDecision, error) {
	decision := IPAccessDecision{
		Allowed:      true,
		EffectiveIP:  identity.EffectiveIP,
		DirectPeerIP: identity.DirectPeerIP,
		Source:       identity.Source,
		Reason:       identity.FailureReason,
	}
	if s == nil || s.repo == nil {
		return decision, errors.New("ip access control unavailable")
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return decision, err
	}
	if !settings.EnforcementEnabled {
		return decision, nil
	}
	s.mu.RLock()
	emergencyAllow := s.emergencyAllow
	s.mu.RUnlock()
	// Break-glass rules normally match the verified client identity. They may
	// match the transport peer only while an operator is repairing an unsafe
	// proxy chain. They are static deployment configuration, never
	// user-controlled request headers.
	if ip.MatchesCompiledIPRules(identity.EffectiveIP, emergencyAllow) ||
		(!identity.SafeForEnforcement && ip.MatchesCompiledIPRules(identity.DirectPeerIP, emergencyAllow)) {
		decision.Allowed = true
		decision.Reason = "emergency_allowlist"
		return decision, nil
	}
	if !identity.SafeForEnforcement || ip.NormalizeIP(identity.EffectiveIP) == "" {
		decision.Allowed = false
		decision.Reason = identity.FailureReason
		if decision.Reason == "" {
			decision.Reason = "identity_unavailable"
		}
		return decision, ErrIPAccessIdentityUnavailable
	}

	snapshot, err := s.activeRuleSnapshot(ctx)
	if err != nil {
		return decision, err
	}
	if ip.MatchesCompiledIPRules(identity.EffectiveIP, snapshot.allowRules) {
		return decision, nil
	}
	if ip.MatchesCompiledIPRules(identity.EffectiveIP, snapshot.blockRules) {
		decision.Allowed = false
		decision.Blocked = true
		decision.Reason = "blocked_rule"
		s.recordBlockedRuleHit(ip.NormalizeIP(identity.EffectiveIP))
	}
	return decision, nil
}

// IsBlocked remains for compatibility with existing service consumers. New
// request paths must call Evaluate with a ClientIdentity from the resolver.
func (s *IPAccessControlService) IsBlocked(ctx context.Context, rawIP string) (bool, error) {
	normalized := ip.NormalizeIP(rawIP)
	decision, err := s.Evaluate(ctx, ip.ClientIdentity{EffectiveIP: normalized, SafeForEnforcement: normalized != ""})
	return decision.Blocked, err
}

func (s *IPAccessControlService) activeRules(ctx context.Context) ([]*IPAccessRule, error) {
	snapshot, err := s.activeRuleSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return unexpiredIPAccessRules(snapshot.rules, time.Now()), nil
}

func (s *IPAccessControlService) activeRuleSnapshot(ctx context.Context) (cachedIPAccessRules, error) {
	now := time.Now()
	s.mu.RLock()
	cached := s.rulesCache
	s.mu.RUnlock()
	if cached.valid && now.Before(cached.expiresAt) {
		return cached, nil
	}
	rules, err := s.repo.ListActiveIPAccessRules(ctx)
	if err != nil {
		s.mu.Lock()
		fallback := s.rulesCache
		if fallback.valid {
			fallback = compileIPAccessRules(fallback.rules, now)
			s.rulesCache = fallback
		}
		s.mu.Unlock()
		if fallback.valid {
			return fallback, nil
		}
		return cachedIPAccessRules{}, err
	}
	compiled := compileIPAccessRules(rules, now)
	s.mu.Lock()
	s.rulesCache = compiled
	s.mu.Unlock()
	return compiled, nil
}

func (s *IPAccessControlService) invalidateRules() {
	s.mu.Lock()
	s.rulesCache = cachedIPAccessRules{}
	s.mu.Unlock()
}

func (s *IPAccessControlService) invalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.settingsCache = cachedIPAccessSettings{}
	s.rulesCache = cachedIPAccessRules{}
	s.mu.Unlock()
}

func (s *IPAccessControlService) publishInvalidation(ctx context.Context) {
	if s == nil || s.redis == nil {
		return
	}
	// The durable database change has already committed. A transient fan-out
	// error must not turn that successful operation into an ambiguous failure;
	// local invalidation is immediate and the normal cache TTL is the fallback.
	if err := s.redis.Publish(ctx, ipAccessInvalidationChannel, "refresh").Err(); err != nil {
		slog.Warn("IP access control invalidation publish failed", "error", err)
	}
}

// recordBlockedRuleHit keeps denied-request audit data without putting a
// database write on every blocked request. Each IP is recorded at most once
// per interval, and the small background pool prevents an attack from growing
// unbounded write concurrency.
func (s *IPAccessControlService) recordBlockedRuleHit(normalizedIP string) {
	if s == nil || s.repo == nil || normalizedIP == "" {
		return
	}
	now := time.Now()
	s.blockHitMu.Lock()
	if s.blockHitLastRecorded == nil {
		s.blockHitLastRecorded = make(map[string]time.Time)
	}
	if s.blockHitWriteSlots == nil {
		s.blockHitWriteSlots = make(chan struct{}, ipAccessBlockHitWriteConcurrency)
	}
	if lastRecorded, ok := s.blockHitLastRecorded[normalizedIP]; ok && now.Sub(lastRecorded) < ipAccessBlockHitFlushInterval {
		s.blockHitMu.Unlock()
		return
	}
	if len(s.blockHitLastRecorded) >= ipAccessBlockHitTrackedIPLimit {
		cutoff := now.Add(-ipAccessBlockHitFlushInterval)
		for trackedIP, lastRecorded := range s.blockHitLastRecorded {
			if lastRecorded.Before(cutoff) {
				delete(s.blockHitLastRecorded, trackedIP)
			}
		}
		if len(s.blockHitLastRecorded) >= ipAccessBlockHitTrackedIPLimit {
			s.blockHitMu.Unlock()
			return
		}
	}
	select {
	case s.blockHitWriteSlots <- struct{}{}:
		s.blockHitLastRecorded[normalizedIP] = now
		s.blockHitMu.Unlock()
	default:
		s.blockHitMu.Unlock()
		return
	}

	go func() {
		defer func() { <-s.blockHitWriteSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), ipAccessBlockHitWriteTimeout)
		defer cancel()
		// This is best-effort observability. The access decision was already
		// made from the in-memory security snapshot and must not be weakened by
		// an audit storage failure.
		_ = s.repo.RecordIPAccessRuleHit(ctx, normalizedIP)
	}()
}

func parseIPAccessBool(value string, fallback bool) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func parseBoundedIPAccessInt(value string, fallback, minimum, maximum int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum || parsed > maximum {
		return fallback
	}
	return parsed
}

func unexpiredIPAccessRules(rules []*IPAccessRule, now time.Time) []*IPAccessRule {
	result := make([]*IPAccessRule, 0, len(rules))
	for _, rule := range rules {
		if rule == nil || rule.Status != IPAccessRuleStatusActive {
			continue
		}
		if rule.ExpiresAt != nil && !rule.ExpiresAt.After(now) {
			continue
		}
		result = append(result, rule)
	}
	return result
}

func compileIPAccessRules(rules []*IPAccessRule, now time.Time) cachedIPAccessRules {
	allow := make([]string, 0, len(rules))
	block := make([]string, 0, len(rules))
	active := make([]*IPAccessRule, 0, len(rules))
	expiresAt := now.Add(ipAccessRulesCacheTTL)
	for _, rule := range rules {
		if rule == nil || rule.Status != IPAccessRuleStatusActive {
			continue
		}
		if rule.ExpiresAt != nil {
			if !rule.ExpiresAt.After(now) {
				continue
			}
			if rule.ExpiresAt.Before(expiresAt) {
				expiresAt = *rule.ExpiresAt
			}
		}
		active = append(active, rule)
		switch rule.RuleKind {
		case IPAccessRuleKindAllow:
			allow = append(allow, rule.IPOrCIDR)
		case IPAccessRuleKindManualBlock, IPAccessRuleKindAutoBlock:
			block = append(block, rule.IPOrCIDR)
		}
	}
	return cachedIPAccessRules{
		rules:      active,
		allowRules: ip.CompileIPRules(allow),
		blockRules: ip.CompileIPRules(block),
		expiresAt:  expiresAt,
		valid:      true,
	}
}
