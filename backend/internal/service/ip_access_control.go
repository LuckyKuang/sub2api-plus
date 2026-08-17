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

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"golang.org/x/sync/singleflight"
)

var (
	ErrIPAccessRuleNotFound        = infraerrors.NotFound("IP_ACCESS_RULE_NOT_FOUND", "IP access rule not found")
	ErrIPAccessEnforcementDisabled = infraerrors.Conflict(
		"IP_ACCESS_ENFORCEMENT_DISABLED",
		"global IP access enforcement must be enabled before manually blocking a login failure source",
	)
	ErrIPBlockSuppressedByAllow = infraerrors.Conflict(
		"IP_BLOCK_SUPPRESSED_BY_ALLOW",
		"an active allow rule covers this IP address",
	)
	ErrIPBlockSuppressedByEmergencyAllow = infraerrors.Conflict(
		"IP_BLOCK_SUPPRESSED_BY_EMERGENCY_ALLOW",
		"the deployment emergency allowlist covers this IP address",
	)
	ErrIPAccessIdentityUnsafe = infraerrors.Conflict(
		"IP_ACCESS_IDENTITY_UNSAFE",
		"configure a safe client-IP proxy chain before manually blocking a login failure source",
	)
)

const (
	SettingKeyIPAccessControlEnabled       = "ip_access_control_enabled"
	SettingKeyLoginFailureAutoBlockEnabled = "login_failure_auto_block_enabled"
	SettingKeyLoginFailureIPThreshold      = "login_failure_ip_threshold"
	SettingKeyLoginFailureWindowMinutes    = "login_failure_window_minutes"
	SettingKeyLoginFailureBlockMinutes     = "login_failure_block_minutes"

	defaultLoginFailureIPThreshold   = 8
	defaultLoginFailureWindowMinutes = 15
	defaultLoginFailureBlockMinutes  = 24 * 60
	ipAccessSecurityRefreshInterval  = 30 * time.Second
	ipAccessSecurityRefreshTimeout   = 10 * time.Second
	ipAccessSecurityMaxStaleness     = 2 * time.Minute
	ipAccessFailureCleanupInterval   = time.Hour
	ipAccessBlockHitFlushInterval    = time.Minute
	ipAccessBlockHitWriteTimeout     = 2 * time.Second
	ipAccessBlockHitTrackedIPLimit   = 4096
	ipAccessBlockHitWriteConcurrency = 4
	ipAccessInvalidationChannel      = "sub2api:ip_access_control:invalidate"
	defaultManualFailureBlockReason  = "manual block from login failure status"
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
	// FeatureEnabled is the system-settings master switch. It is not written by
	// the IP page UpdateSettings path; turning it off must keep enforcement_enabled
	// and stored rules intact while still blocking all runtime enforcement.
	FeatureEnabled         bool `json:"feature_enabled"`
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
	return s.FeatureEnabled && s.EnforcementEnabled && s.LoginFailureAutoBlock
}

// RuntimeEnforcementActive is the request-path gate: both the feature switch
// and the IP-page enforcement toggle must be on before any block is applied.
func (s IPAccessControlSettings) RuntimeEnforcementActive() bool {
	return s.FeatureEnabled && s.EnforcementEnabled
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
	NormalizedIP              string            `json:"normalized_ip"`
	FailureCount              int               `json:"failure_count"`
	FailureThreshold          int               `json:"failure_threshold"`
	WindowStartedAt           time.Time         `json:"window_started_at"`
	LastFailedAt              time.Time         `json:"last_failed_at"`
	WindowExpiresAt           time.Time         `json:"window_expires_at"`
	ActiveBlockRule           bool              `json:"active_block_rule"`
	BlockRuleID               *int64            `json:"block_rule_id,omitempty"`
	BlockRuleKind             *IPAccessRuleKind `json:"block_rule_kind,omitempty"`
	BlockRuleIPOrCIDR         *string           `json:"block_rule_ip_or_cidr,omitempty"`
	BlockedAt                 *time.Time        `json:"blocked_at,omitempty"`
	BlockExpiresAt            *time.Time        `json:"block_expires_at,omitempty"`
	RuntimeEnforcementEnabled bool              `json:"runtime_enforcement_enabled"`
	SuppressedByAllowRule     bool              `json:"suppressed_by_allow_rule"`
	EmergencyAllowlisted      bool              `json:"emergency_allowlisted"`
	EffectivelyBlocked        bool              `json:"effectively_blocked"`
	AsOf                      time.Time         `json:"as_of"`
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

type IPFailureStateBlockRepositoryResult struct {
	Rule           *IPAccessRule
	AlreadyBlocked bool
}

type IPFailureStateBlockResult struct {
	Rule                  *IPAccessRule `json:"rule"`
	AlreadyBlocked        bool          `json:"already_blocked"`
	EffectivelyBlocked    bool          `json:"effectively_blocked"`
	SuppressedByAllowRule bool          `json:"suppressed_by_allow_rule"`
	AsOf                  time.Time     `json:"as_of"`
}

type IPAccessControlRepository interface {
	ListIPAccessRules(ctx context.Context, filter IPAccessRuleFilter) (*IPAccessRuleList, error)
	ListActiveIPAccessRules(ctx context.Context) ([]*IPAccessRule, error)
	CreateManualIPAccessRule(ctx context.Context, rule *IPAccessRule) (*IPAccessRule, error)
	CreateManualIPBlockForFailureState(ctx context.Context, normalizedIP, reason string, expiresAt time.Time, actorUserID int64) (*IPFailureStateBlockRepositoryResult, error)
	ReleaseIPAccessRuleAndReset(ctx context.Context, id, actorUserID int64) (*IPAccessRule, error)
	ListIPLoginFailureStates(ctx context.Context, filter IPLoginFailureStateFilter, window time.Duration) (*IPLoginFailureStateList, error)
	ResetIPLoginFailureState(ctx context.Context, normalizedIP string) error
	RecordFailedLogin(ctx context.Context, normalizedIP string, threshold int, window, blockFor time.Duration) (*LoginFailureRecordResult, error)
	RecordIPAccessRuleHit(ctx context.Context, normalizedIP string) error
}

type cachedIPAccessSettings struct {
	value    IPAccessControlSettings
	loadedAt time.Time
	valid    bool
}

type cachedIPAccessRules struct {
	rules      []*IPAccessRule
	allowRules *ip.CompiledIPRules
	blockRules *ip.CompiledIPRules
	nextExpiry time.Time
	loadedAt   time.Time
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

// IPAccessControlService owns durable global rules and source-IP credential
// failure windows. Requests use a prewarmed in-process policy snapshot; local
// mutations patch it immediately and background reconciliation keeps it fresh.
type IPAccessControlService struct {
	settings SettingRepository
	repo     IPAccessControlRepository

	mu             sync.RWMutex
	settingsCache  cachedIPAccessSettings
	rulesCache     cachedIPAccessRules
	mutationEpoch  uint64
	emergencyAllow *ip.CompiledIPRules
	emergencyCount int

	blockHitMu           sync.Mutex
	blockHitLastRecorded map[string]time.Time
	blockHitWriteSlots   chan struct{}

	invalidationBus  InvalidationBus
	invalidationOnce sync.Once
	cleanupOnce      sync.Once
	refreshOnce      sync.Once
	refreshGroup     singleflight.Group
	refreshCh        chan struct{}
}

func NewIPAccessControlService(settings SettingRepository, repo IPAccessControlRepository) *IPAccessControlService {
	return &IPAccessControlService{
		settings:             settings,
		repo:                 repo,
		blockHitLastRecorded: make(map[string]time.Time),
		blockHitWriteSlots:   make(chan struct{}, ipAccessBlockHitWriteConcurrency),
		refreshCh:            make(chan struct{}, 1),
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

// SetInvalidationBus enables immediate snapshot refresh signals across API
// instances. It is intentionally optional so compact standalone and unit-test
// deployments retain the same construction contract.
func (s *IPAccessControlService) SetInvalidationBus(bus InvalidationBus) {
	if s == nil {
		return
	}
	s.invalidationBus = bus
}

// StartInvalidationSubscriber keeps the local security snapshot coherent when
// another instance changes a rule or the global settings. Pub/Sub is only the
// fast path: reconnect and periodic reconciliation cover disconnects and
// messages missed while this instance was offline.
func (s *IPAccessControlService) StartInvalidationSubscriber(ctx context.Context) {
	if s == nil || s.invalidationBus == nil {
		return
	}
	s.invalidationOnce.Do(func() {
		go func() {
			backoff := time.Second
			for ctx.Err() == nil {
				err := s.invalidationBus.Subscribe(ctx, ipAccessInvalidationChannel, s.requestSecurityRefresh)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					slog.Warn("IP access control invalidation subscription ended", "error", err)
				} else {
					slog.Warn("IP access control invalidation subscription ended")
				}
				s.requestSecurityRefresh()
				timer := time.NewTimer(backoff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				if backoff < 30*time.Second {
					backoff *= 2
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
				}
			}
		}()
	})
}

// StartSecuritySnapshotRefresh moves all steady-state PostgreSQL refreshes off
// the request path. Warmup remains synchronous and is required before traffic.
func (s *IPAccessControlService) StartSecuritySnapshotRefresh(ctx context.Context) {
	if s == nil {
		return
	}
	s.refreshOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(ipAccessSecurityRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-s.refreshCh:
				}
				refreshCtx, cancel := context.WithTimeout(ctx, ipAccessSecurityRefreshTimeout)
				err := s.refreshSecuritySnapshot(refreshCtx)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("IP access control security snapshot refresh failed", "error", err)
				}
			}
		}()
	})
}

func (s *IPAccessControlService) requestSecurityRefresh() {
	if s == nil {
		return
	}
	select {
	case s.refreshCh <- struct{}{}:
	default:
	}
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
	return s.refreshSecuritySnapshot(ctx)
}

// SecuritySnapshotReady reports whether both halves of the last-known-good
// security snapshot have been loaded recently enough to remain trustworthy.
// This prevents a stale allow rule or stale disabled switch from being used
// indefinitely while every policy refresh is failing.
func (s *IPAccessControlService) SecuritySnapshotReady() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return securitySnapshotFresh(s.settingsCache, s.rulesCache, time.Now())
}

func (s *IPAccessControlService) GetSettings(ctx context.Context) (IPAccessControlSettings, error) {
	if s == nil || s.settings == nil {
		return DefaultIPAccessControlSettings(), errors.New("ip access control settings unavailable")
	}
	s.mu.RLock()
	cached := s.settingsCache
	rules := s.rulesCache
	s.mu.RUnlock()
	if securitySnapshotFresh(cached, rules, time.Now()) {
		return cached.value, nil
	}
	if cached.valid && rules.valid {
		s.requestSecurityRefresh()
		return DefaultIPAccessControlSettings(), errors.New("IP access control security snapshot is too stale")
	}
	if err := s.refreshSecuritySnapshot(ctx); err != nil {
		return DefaultIPAccessControlSettings(), err
	}
	s.mu.RLock()
	cached = s.settingsCache
	s.mu.RUnlock()
	return cached.value, nil
}

func (s *IPAccessControlService) loadSettings(ctx context.Context) (IPAccessControlSettings, error) {
	keys := []string{
		SettingKeyGlobalIPAccessControlEnabled,
		SettingKeyIPAccessControlEnabled,
		SettingKeyLoginFailureAutoBlockEnabled,
		SettingKeyLoginFailureIPThreshold,
		SettingKeyLoginFailureWindowMinutes,
		SettingKeyLoginFailureBlockMinutes,
	}
	values, err := s.settings.GetMultiple(ctx, keys)
	if err != nil {
		return DefaultIPAccessControlSettings(), err
	}
	settings := DefaultIPAccessControlSettings()
	settings.EnforcementEnabled = parseIPAccessBool(values[SettingKeyIPAccessControlEnabled], settings.EnforcementEnabled)
	settings.LoginFailureAutoBlock = parseIPAccessBool(values[SettingKeyLoginFailureAutoBlockEnabled], settings.LoginFailureAutoBlock)
	settings.LoginFailureThreshold = parseBoundedIPAccessInt(values[SettingKeyLoginFailureIPThreshold], settings.LoginFailureThreshold, 2, 100)
	settings.LoginFailureWindowMins = parseBoundedIPAccessInt(values[SettingKeyLoginFailureWindowMinutes], settings.LoginFailureWindowMins, 1, 24*60)
	settings.LoginFailureBlockMins = parseBoundedIPAccessInt(values[SettingKeyLoginFailureBlockMinutes], settings.LoginFailureBlockMins, 1, 365*24*60)
	settings.FeatureEnabled = strings.TrimSpace(values[SettingKeyGlobalIPAccessControlEnabled]) == "true"
	settings, _ = settings.Validate()
	return settings, nil
}

func (s *IPAccessControlService) refreshSecuritySnapshot(ctx context.Context) error {
	if s == nil || s.settings == nil || s.repo == nil {
		return errors.New("ip access control unavailable")
	}
	_, err, _ := s.refreshGroup.Do("complete", func() (any, error) {
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s.mu.RLock()
			startedAtEpoch := s.mutationEpoch
			s.mu.RUnlock()

			settings, err := s.loadSettings(ctx)
			if err != nil {
				return nil, err
			}
			rules, err := s.repo.ListActiveIPAccessRules(ctx)
			if err != nil {
				return nil, err
			}
			now := time.Now()
			compiled := compileIPAccessRules(rules, now)
			compiled.loadedAt = now

			s.mu.Lock()
			if s.mutationEpoch != startedAtEpoch {
				// A local durable mutation committed while the database reads were
				// in flight. Installing their older view would temporarily undo the
				// immediate block/release/settings patch, so retry from a new epoch.
				s.mu.Unlock()
				continue
			}
			s.settingsCache = cachedIPAccessSettings{value: settings, loadedAt: now, valid: true}
			s.rulesCache = compiled
			s.mu.Unlock()
			return nil, nil
		}
	})
	return err
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
	// The IP page must not persist or clobber the system-settings master switch.
	// Keep the in-memory snapshot aligned with Evaluate / AutomaticBlockingActive.
	s.mu.RLock()
	settingsCacheValid := s.settingsCache.valid
	featureEnabled := settingsCacheValid && s.settingsCache.value.FeatureEnabled
	s.mu.RUnlock()
	if raw, err := s.settings.GetValue(ctx, SettingKeyGlobalIPAccessControlEnabled); err == nil {
		featureEnabled = strings.TrimSpace(raw) == "true"
	} else if !settingsCacheValid {
		featureEnabled = false
	}
	validated.FeatureEnabled = featureEnabled
	s.mu.Lock()
	s.mutationEpoch++
	s.settingsCache = cachedIPAccessSettings{value: validated, loadedAt: time.Now(), valid: true}
	s.mu.Unlock()
	s.requestSecurityRefresh()
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
		s.applyCommittedRule(created)
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
		s.applyCommittedRule(rule)
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

// BlockFailureState creates an exact-IP manual block from the failure-state
// management surface. Unlike the generic rule editor, this action promises an
// immediately enforced result, so it verifies the complete post-commit
// snapshot before reporting success and never clears the failure counter.
func (s *IPAccessControlService) BlockFailureState(ctx context.Context, rawIP, reason string, actorUserID int64) (*IPFailureStateBlockResult, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "IP access control is unavailable")
	}
	normalized := ip.NormalizeIP(rawIP)
	if normalized == "" {
		return nil, infraerrors.BadRequest("IP_LOGIN_FAILURE_IP_INVALID", "invalid IP address")
	}
	if actorUserID <= 0 {
		return nil, infraerrors.BadRequest("IP_ACCESS_ACTOR_INVALID", "authenticated administrator is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = defaultManualFailureBlockReason
	}
	if len(reason) > 1000 {
		return nil, infraerrors.BadRequest("IP_ACCESS_RULE_INVALID", "reason is too long")
	}

	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "IP access control state could not be confirmed").WithCause(err)
	}
	if !settings.RuntimeEnforcementActive() {
		return nil, ErrIPAccessEnforcementDisabled
	}
	s.mu.RLock()
	emergencyAllow := s.emergencyAllow
	s.mu.RUnlock()
	if ip.MatchesCompiledIPRules(normalized, emergencyAllow) {
		return nil, ErrIPBlockSuppressedByEmergencyAllow
	}

	expiresAt := time.Now().UTC().Add(time.Duration(settings.LoginFailureBlockMins) * time.Minute)
	repoResult, err := s.repo.CreateManualIPBlockForFailureState(ctx, normalized, reason, expiresAt, actorUserID)
	if err != nil {
		if errors.Is(err, ErrIPBlockSuppressedByAllow) {
			return nil, ErrIPBlockSuppressedByAllow
		}
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block could not be confirmed").WithCause(err)
	}
	if repoResult == nil || !activeBlockRuleMatchesIP(repoResult.Rule, normalized, time.Now()) {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block result is not an active rule for the requested IP")
	}

	s.applyCommittedRule(repoResult.Rule)
	if !repoResult.AlreadyBlocked {
		s.publishInvalidation(ctx)
	}
	// A row patch is sufficient for the hot path in the common case, but this
	// action makes a stronger management-plane promise. Reload the complete
	// snapshot so a concurrently added/removed allow or settings change is
	// reflected before we claim that the IP is effectively blocked.
	if err := s.refreshSecuritySnapshot(ctx); err != nil {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block was stored but runtime enforcement could not be confirmed").WithCause(err)
	}

	confirmedSettings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block runtime settings could not be confirmed").WithCause(err)
	}
	if !confirmedSettings.RuntimeEnforcementActive() {
		return nil, ErrIPAccessEnforcementDisabled
	}
	snapshot, err := s.activeRuleSnapshot(ctx)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block runtime rules could not be confirmed").WithCause(err)
	}
	s.mu.RLock()
	emergencyAllow = s.emergencyAllow
	s.mu.RUnlock()
	if ip.MatchesCompiledIPRules(normalized, emergencyAllow) {
		return nil, ErrIPBlockSuppressedByEmergencyAllow
	}
	if ip.MatchesCompiledIPRules(normalized, snapshot.allowRules) {
		return nil, ErrIPBlockSuppressedByAllow
	}
	if !ip.MatchesCompiledIPRules(normalized, snapshot.blockRules) {
		return nil, infraerrors.ServiceUnavailable("IP_ACCESS_CONTROL_UNAVAILABLE", "manual IP block is not active in the runtime policy")
	}

	return &IPFailureStateBlockResult{
		Rule:                  repoResult.Rule,
		AlreadyBlocked:        repoResult.AlreadyBlocked,
		EffectivelyBlocked:    true,
		SuppressedByAllowRule: false,
		AsOf:                  time.Now().UTC(),
	}, nil
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
	now := time.Now().UTC()
	s.mu.RLock()
	emergencyAllow := s.emergencyAllow
	s.mu.RUnlock()
	for _, state := range list.Items {
		if state == nil {
			continue
		}
		state.FailureThreshold = settings.LoginFailureThreshold
		state.RuntimeEnforcementEnabled = settings.RuntimeEnforcementActive()
		state.EmergencyAllowlisted = ip.MatchesCompiledIPRules(state.NormalizedIP, emergencyAllow)
		state.EffectivelyBlocked = state.ActiveBlockRule &&
			state.RuntimeEnforcementEnabled && !state.SuppressedByAllowRule && !state.EmergencyAllowlisted
		state.AsOf = now
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
	s.mu.RLock()
	emergencyAllow := s.emergencyAllow
	s.mu.RUnlock()
	// Apply the same break-glass precedence as Evaluate. A verified client IP
	// on the deployment allowlist is outside automatic IP enforcement. When the
	// proxy chain itself is unsafe, only a matching transport peer receives the
	// recovery exemption; a trusted proxy peer must never exempt arbitrary
	// forwarded clients.
	if (identity.SafeForEnforcement && ip.MatchesCompiledIPRules(identity.EffectiveIP, emergencyAllow)) ||
		(!identity.SafeForEnforcement && ip.MatchesCompiledIPRules(identity.DirectPeerIP, emergencyAllow)) {
		return nil, nil
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
		if !activeBlockRuleMatchesIP(result.Rule, normalized, time.Now()) {
			return nil, errors.New("IP access control block result is missing its active durable rule")
		}
		s.applyCommittedRule(result.Rule)
		s.publishInvalidation(ctx)
		confirmedSettings, settingsErr := s.GetSettings(ctx)
		if settingsErr != nil {
			return nil, fmt.Errorf("confirm committed IP access control settings: %w", settingsErr)
		}
		if !confirmedSettings.RuntimeEnforcementActive() {
			return nil, errors.New("committed IP access control block is not enabled in the runtime policy")
		}
		snapshot, snapshotErr := s.activeRuleSnapshot(ctx)
		if snapshotErr != nil {
			return nil, fmt.Errorf("confirm committed IP access control block: %w", snapshotErr)
		}
		if ip.MatchesCompiledIPRules(normalized, snapshot.allowRules) ||
			!ip.MatchesCompiledIPRules(normalized, snapshot.blockRules) {
			return nil, errors.New("committed IP access control block is not effective in the runtime policy")
		}
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
	if !settings.RuntimeEnforcementActive() {
		return decision, nil
	}
	s.mu.RLock()
	emergencyAllow := s.emergencyAllow
	s.mu.RUnlock()
	// Break-glass rules normally match the verified client identity. They may
	// match the transport peer only while an operator is repairing an unsafe
	// proxy chain. They are static deployment configuration, never
	// user-controlled request headers.
	if (identity.SafeForEnforcement && ip.MatchesCompiledIPRules(identity.EffectiveIP, emergencyAllow)) ||
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

func (s *IPAccessControlService) activeRuleSnapshot(ctx context.Context) (cachedIPAccessRules, error) {
	now := time.Now()
	s.mu.RLock()
	cached := s.rulesCache
	settings := s.settingsCache
	s.mu.RUnlock()
	if !cached.valid || !settings.valid {
		if err := s.refreshSecuritySnapshot(ctx); err != nil {
			return cachedIPAccessRules{}, err
		}
		s.mu.RLock()
		cached = s.rulesCache
		settings = s.settingsCache
		s.mu.RUnlock()
	}
	if !securitySnapshotFresh(settings, cached, now) {
		s.requestSecurityRefresh()
		return cachedIPAccessRules{}, errors.New("IP access control security snapshot is too stale")
	}
	if !cached.nextExpiry.IsZero() && !now.Before(cached.nextExpiry) {
		s.mu.Lock()
		current := s.rulesCache
		if current.valid && !current.nextExpiry.IsZero() && !now.Before(current.nextExpiry) {
			compiled := compileIPAccessRules(current.rules, now)
			compiled.loadedAt = current.loadedAt
			s.rulesCache = compiled
			cached = compiled
		} else {
			cached = current
		}
		s.mu.Unlock()
	}
	return cached, nil
}

func (s *IPAccessControlService) invalidateAll() {
	if s == nil {
		return
	}
	s.requestSecurityRefresh()
}

// applyCommittedRule patches a durable local mutation into the current
// complete snapshot. It removes the previous row with the same identity and
// only retains an active, unexpired replacement. The full snapshot's loadedAt
// is deliberately preserved because one row mutation is not a full refresh.
func (s *IPAccessControlService) applyCommittedRule(rule *IPAccessRule) {
	if s == nil || rule == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	s.mutationEpoch++
	cached := s.rulesCache
	if !cached.valid {
		s.mu.Unlock()
		s.requestSecurityRefresh()
		return
	}
	rules := make([]*IPAccessRule, 0, len(cached.rules)+1)
	newerSameKindRuleExists := false
	for _, existing := range cached.rules {
		if existing == nil || existing.ID == rule.ID {
			continue
		}
		if existing.IPOrCIDR == rule.IPOrCIDR && existing.RuleKind == rule.RuleKind {
			// A released/expired result only removes its own row. A newer row
			// with the same natural key may already have been committed and
			// patched by another goroutine after this mutation left PostgreSQL.
			if rule.Status != IPAccessRuleStatusActive || existing.ID > rule.ID {
				rules = append(rules, existing)
				if existing.ID > rule.ID {
					newerSameKindRuleExists = true
				}
			}
			continue
		}
		rules = append(rules, existing)
	}
	if !newerSameKindRuleExists && rule.Status == IPAccessRuleStatusActive && (rule.ExpiresAt == nil || rule.ExpiresAt.After(now)) {
		rules = append(rules, rule)
	}
	compiled := compileIPAccessRules(rules, now)
	compiled.loadedAt = cached.loadedAt
	s.rulesCache = compiled
	s.mu.Unlock()
	s.requestSecurityRefresh()
}

func activeBlockRuleMatchesIP(rule *IPAccessRule, normalizedIP string, now time.Time) bool {
	if rule == nil || rule.Status != IPAccessRuleStatusActive ||
		(rule.RuleKind != IPAccessRuleKindManualBlock && rule.RuleKind != IPAccessRuleKindAutoBlock) ||
		(rule.ExpiresAt != nil && !rule.ExpiresAt.After(now)) {
		return false
	}
	return ip.MatchesCompiledIPRules(normalizedIP, ip.CompileIPRules([]string{rule.IPOrCIDR}))
}

func (s *IPAccessControlService) publishInvalidation(ctx context.Context) {
	if s == nil || s.invalidationBus == nil {
		return
	}
	// The durable database change has already committed. A transient fan-out
	// error must not turn that successful operation into an ambiguous failure;
	// the local mutation is already applied and periodic reconciliation is the
	// fallback for any instance that misses this notification.
	if err := s.invalidationBus.Publish(ctx, ipAccessInvalidationChannel); err != nil {
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

func compileIPAccessRules(rules []*IPAccessRule, now time.Time) cachedIPAccessRules {
	allow := make([]string, 0, len(rules))
	block := make([]string, 0, len(rules))
	active := make([]*IPAccessRule, 0, len(rules))
	var nextExpiry time.Time
	for _, rule := range rules {
		if rule == nil || rule.Status != IPAccessRuleStatusActive {
			continue
		}
		if rule.ExpiresAt != nil {
			if !rule.ExpiresAt.After(now) {
				continue
			}
			if nextExpiry.IsZero() || rule.ExpiresAt.Before(nextExpiry) {
				nextExpiry = *rule.ExpiresAt
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
		nextExpiry: nextExpiry,
		loadedAt:   now,
		valid:      true,
	}
}

func securitySnapshotFresh(settings cachedIPAccessSettings, rules cachedIPAccessRules, now time.Time) bool {
	if !settings.valid || !rules.valid || settings.loadedAt.IsZero() || rules.loadedAt.IsZero() {
		return false
	}
	return now.Sub(settings.loadedAt) <= ipAccessSecurityMaxStaleness &&
		now.Sub(rules.loadedAt) <= ipAccessSecurityMaxStaleness
}
