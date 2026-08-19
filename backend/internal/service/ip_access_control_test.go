package service

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
)

type ipAccessSettingRepoStub struct {
	values   map[string]string
	getErr   error
	getCalls int
}

func (r *ipAccessSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (r *ipAccessSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (r *ipAccessSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *ipAccessSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.getCalls++
	if r.getErr != nil {
		return nil, r.getErr
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}
func (r *ipAccessSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
func (r *ipAccessSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *ipAccessSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type ipAccessRepoStub struct {
	rules             []*IPAccessRule
	listErr           error
	listCalls         int
	created           *IPAccessRule
	manualBlockResult *IPFailureStateBlockRepositoryResult
	manualBlockErr    error
	manualBlockCalls  int
	failureList       *IPLoginFailureStateList
	resetIPs          []string
	resetErr          error
	recordResult      *LoginFailureRecordResult
	recordErr         error
	recordCalls       int
	blockHitIPs       chan string
	blockHitErr       error
}

func (r *ipAccessRepoStub) ListIPAccessRules(context.Context, IPAccessRuleFilter) (*IPAccessRuleList, error) {
	return &IPAccessRuleList{}, nil
}
func (r *ipAccessRepoStub) ListActiveIPAccessRules(context.Context) ([]*IPAccessRule, error) {
	r.listCalls++
	return r.rules, r.listErr
}
func (r *ipAccessRepoStub) CreateManualIPAccessRule(_ context.Context, rule *IPAccessRule) (*IPAccessRule, error) {
	if rule.ID == 0 {
		rule.ID = 100
	}
	r.created = rule
	r.rules = append(r.rules, rule)
	return rule, nil
}
func (r *ipAccessRepoStub) CreateManualIPBlockForFailureState(_ context.Context, normalizedIP, reason string, actorUserID int64) (*IPFailureStateBlockRepositoryResult, error) {
	r.manualBlockCalls++
	if r.manualBlockErr != nil {
		return nil, r.manualBlockErr
	}
	result := r.manualBlockResult
	if result == nil {
		actor := actorUserID
		result = &IPFailureStateBlockRepositoryResult{Rule: &IPAccessRule{
			ID: 101, IPOrCIDR: normalizedIP, RuleKind: IPAccessRuleKindManualBlock,
			Status: IPAccessRuleStatusActive, Reason: reason,
			CreatedByUserID: &actor,
		}}
	}
	if result.Rule != nil {
		found := false
		for _, rule := range r.rules {
			if rule != nil && rule.ID == result.Rule.ID {
				found = true
			}
		}
		if !found {
			r.rules = append(r.rules, result.Rule)
		}
	}
	return result, nil
}
func (r *ipAccessRepoStub) ReleaseIPAccessRuleAndReset(context.Context, int64, int64) (*IPAccessRule, error) {
	return nil, ErrIPAccessRuleNotFound
}
func (r *ipAccessRepoStub) ListIPLoginFailureStates(context.Context, IPLoginFailureStateFilter, time.Duration) (*IPLoginFailureStateList, error) {
	if r.failureList != nil {
		return r.failureList, nil
	}
	return &IPLoginFailureStateList{}, nil
}
func (r *ipAccessRepoStub) ResetIPLoginFailureState(_ context.Context, normalizedIP string) error {
	if r.resetErr != nil {
		return r.resetErr
	}
	r.resetIPs = append(r.resetIPs, normalizedIP)
	return nil
}
func (r *ipAccessRepoStub) RecordFailedLogin(context.Context, string, int, time.Duration, time.Duration) (*LoginFailureRecordResult, error) {
	r.recordCalls++
	if r.recordResult != nil || r.recordErr != nil {
		return r.recordResult, r.recordErr
	}
	return &LoginFailureRecordResult{}, nil
}
func (r *ipAccessRepoStub) RecordIPAccessRuleHit(_ context.Context, normalizedIP string) error {
	if r.blockHitIPs != nil {
		r.blockHitIPs <- normalizedIP
	}
	return r.blockHitErr
}

type ipAccessCleanupRepoStub struct {
	ipAccessRepoStub
	cleanupBefore time.Time
	cleanupLimit  int
	cleanupCalls  int
	cleanupErr    error
}

type overlappingSnapshotRepoStub struct {
	ipAccessRepoStub
	secondReadStarted chan struct{}
	releaseSecondRead chan struct{}
}

func (r *overlappingSnapshotRepoStub) ListActiveIPAccessRules(context.Context) ([]*IPAccessRule, error) {
	r.listCalls++
	if r.listCalls == 2 {
		stale := append([]*IPAccessRule(nil), r.rules...)
		close(r.secondReadStarted)
		<-r.releaseSecondRead
		return stale, nil
	}
	return r.rules, r.listErr
}

func (r *ipAccessCleanupRepoStub) CleanupExpiredIPLoginFailureStates(_ context.Context, before time.Time, limit int) (int64, error) {
	r.cleanupBefore = before
	r.cleanupLimit = limit
	r.cleanupCalls++
	return 3, r.cleanupErr
}

func TestIPAccessControlAllowRuleTakesPrecedence(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{
		{IPOrCIDR: "203.0.113.0/24", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
		{IPOrCIDR: "203.0.113.42", RuleKind: IPAccessRuleKindAllow, Status: IPAccessRuleStatusActive},
	}}
	svc := NewIPAccessControlService(settings, repo)

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.42")
	if err != nil || blocked {
		t.Fatalf("allow rule should override matching block: blocked=%v err=%v", blocked, err)
	}
	blocked, err = svc.IsBlocked(context.Background(), "203.0.113.99")
	if err != nil || !blocked {
		t.Fatalf("block CIDR should apply outside allow rule: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlEmergencyAllowlistOverridesBlockAndRejectsGlobalRange(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{{
		IPOrCIDR: "203.0.113.0/24", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive,
	}}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.ConfigureEmergencyAllowlist([]string{"203.0.113.42", "203.0.113.42"}); err != nil {
		t.Fatalf("configure emergency allowlist: %v", err)
	}
	configured, count := svc.EmergencyAllowlistStatus()
	if !configured || count != 1 {
		t.Fatalf("unexpected emergency allowlist status: configured=%v count=%d", configured, count)
	}
	decision, err := svc.Evaluate(context.Background(), ip.ClientIdentity{
		EffectiveIP: "203.0.113.42", SafeForEnforcement: true,
	})
	if err != nil || !decision.Allowed || decision.Blocked || decision.Reason != "emergency_allowlist" {
		t.Fatalf("emergency allowlist must override matching block: decision=%#v err=%v", decision, err)
	}
	if err := svc.ConfigureEmergencyAllowlist([]string{"0.0.0.0/0"}); err == nil {
		t.Fatal("global emergency allowlist range must be rejected")
	}
}

func TestIPAccessControlEmergencyAllowlistDoesNotMatchTrustedProxyPeer(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{{
		IPOrCIDR: "203.0.113.0/24", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive,
	}}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.ConfigureEmergencyAllowlist([]string{"10.1.2.3"}); err != nil {
		t.Fatalf("configure emergency allowlist: %v", err)
	}

	decision, err := svc.Evaluate(context.Background(), ip.ClientIdentity{
		EffectiveIP: "203.0.113.42", DirectPeerIP: "10.1.2.3", SafeForEnforcement: true,
	})
	if err != nil || !decision.Blocked || decision.Allowed || decision.Reason != "blocked_rule" {
		t.Fatalf("trusted proxy peer must not bypass the verified client block: decision=%#v err=%v", decision, err)
	}
}

func TestIPAccessControlEmergencyAllowlistRecoversUnsafeProxyChainByDirectPeer(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	svc := NewIPAccessControlService(settings, &ipAccessRepoStub{})
	if err := svc.ConfigureEmergencyAllowlist([]string{"10.1.2.3"}); err != nil {
		t.Fatalf("configure emergency allowlist: %v", err)
	}
	decision, err := svc.Evaluate(context.Background(), ip.ClientIdentity{
		DirectPeerIP: "10.1.2.3", FailureReason: "unsafe_proxy_chain",
	})
	if err != nil || !decision.Allowed || decision.Reason != "emergency_allowlist" {
		t.Fatalf("emergency direct peer must recover unsafe proxy chain: decision=%#v err=%v", decision, err)
	}
}

func TestIPAccessControlEmergencyAllowlistSkipsFailureCountingWithoutExemptingForwardedClients(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "2",
		SettingKeyLoginFailureWindowMinutes:    "15",
		SettingKeyLoginFailureBlockMinutes:     "60",
	}}

	t.Run("verified emergency client", func(t *testing.T) {
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)
		if err := svc.ConfigureEmergencyAllowlist([]string{"203.0.113.8"}); err != nil {
			t.Fatalf("configure emergency allowlist: %v", err)
		}
		result, err := svc.RecordFailedLoginForIdentity(context.Background(), ip.ClientIdentity{
			EffectiveIP: "203.0.113.8", DirectPeerIP: "10.1.2.3", SafeForEnforcement: true,
		})
		if err != nil || result != nil || repo.recordCalls != 0 {
			t.Fatalf("emergency client must bypass failure counting: result=%#v calls=%d err=%v", result, repo.recordCalls, err)
		}
	})

	t.Run("unsafe recovery peer", func(t *testing.T) {
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)
		if err := svc.ConfigureEmergencyAllowlist([]string{"10.1.2.3"}); err != nil {
			t.Fatalf("configure emergency allowlist: %v", err)
		}
		result, err := svc.RecordFailedLoginForIdentity(context.Background(), ip.ClientIdentity{
			DirectPeerIP: "10.1.2.3", FailureReason: "unsafe_proxy_chain",
		})
		if err != nil || result != nil || repo.recordCalls != 0 {
			t.Fatalf("emergency recovery peer must bypass failure counting: result=%#v calls=%d err=%v", result, repo.recordCalls, err)
		}
	})

	t.Run("trusted proxy peer does not exempt forwarded client", func(t *testing.T) {
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)
		if err := svc.ConfigureEmergencyAllowlist([]string{"10.1.2.3"}); err != nil {
			t.Fatalf("configure emergency allowlist: %v", err)
		}
		_, err := svc.RecordFailedLoginForIdentity(context.Background(), ip.ClientIdentity{
			EffectiveIP: "203.0.113.8", DirectPeerIP: "10.1.2.3", SafeForEnforcement: true,
		})
		if err != nil || repo.recordCalls != 1 {
			t.Fatalf("trusted proxy peer must not exempt the verified client: calls=%d err=%v", repo.recordCalls, err)
		}
	})

	t.Run("unsafe forwarded identity cannot claim emergency client address", func(t *testing.T) {
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)
		if err := svc.ConfigureEmergencyAllowlist([]string{"203.0.113.8"}); err != nil {
			t.Fatalf("configure emergency allowlist: %v", err)
		}
		unsafeIdentity := ip.ClientIdentity{
			EffectiveIP: "203.0.113.8", DirectPeerIP: "10.1.2.4", SafeForEnforcement: false,
		}
		_, err := svc.RecordFailedLoginForIdentity(context.Background(), unsafeIdentity)
		if !errors.Is(err, ErrIPAccessIdentityUnavailable) || repo.recordCalls != 0 {
			t.Fatalf("unsafe effective IP must not claim the emergency exemption: calls=%d err=%v", repo.recordCalls, err)
		}
		decision, err := svc.Evaluate(context.Background(), unsafeIdentity)
		if !errors.Is(err, ErrIPAccessIdentityUnavailable) || decision.Allowed {
			t.Fatalf("unsafe effective IP must not bypass request enforcement: decision=%#v err=%v", decision, err)
		}
	})
}

func TestIPAccessControlCleanupUsesConfiguredFailureWindow(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyLoginFailureWindowMinutes: "15",
	}}
	repo := &ipAccessCleanupRepoStub{}
	svc := NewIPAccessControlService(settings, repo)
	beforeCall := time.Now().Add(-16 * time.Minute)
	deleted, err := svc.cleanupExpiredFailureStates(context.Background(), repo)
	if err != nil || deleted != 3 || repo.cleanupCalls != 1 || repo.cleanupLimit != 1000 {
		t.Fatalf("unexpected cleanup result: deleted=%d calls=%d limit=%d err=%v", deleted, repo.cleanupCalls, repo.cleanupLimit, err)
	}
	if repo.cleanupBefore.Before(beforeCall) || repo.cleanupBefore.After(time.Now().Add(-14*time.Minute)) {
		t.Fatalf("cleanup cutoff must follow configured window, got %s", repo.cleanupBefore)
	}
}

func TestIPAccessControlRecordsBlockedRuleHitWithoutRecordingAllowedSource(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{
		blockHitIPs: make(chan string, 1),
		rules: []*IPAccessRule{
			{IPOrCIDR: "203.0.113.0/24", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
			{IPOrCIDR: "203.0.113.42", RuleKind: IPAccessRuleKindAllow, Status: IPAccessRuleStatusActive},
		},
	}
	svc := NewIPAccessControlService(settings, repo)

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.42")
	if err != nil || blocked {
		t.Fatalf("allow rule should remain unrecorded as a block hit: blocked=%v err=%v", blocked, err)
	}
	select {
	case hit := <-repo.blockHitIPs:
		t.Fatalf("allow rule unexpectedly recorded a block hit for %s", hit)
	default:
	}

	blocked, err = svc.IsBlocked(context.Background(), "203.0.113.99")
	if err != nil || !blocked {
		t.Fatalf("blocked source must be denied: blocked=%v err=%v", blocked, err)
	}
	select {
	case hit := <-repo.blockHitIPs:
		if hit != "203.0.113.99" {
			t.Fatalf("unexpected recorded block hit IP: %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asynchronous block-hit record")
	}
}

func TestIPAccessControlSettingsRejectUnsafeThresholds(t *testing.T) {
	_, err := (IPAccessControlSettings{LoginFailureThreshold: 1, LoginFailureWindowMins: 15, LoginFailureBlockMins: 60}).Validate()
	if err == nil {
		t.Fatal("threshold below 2 must be rejected")
	}
	_, err = (IPAccessControlSettings{LoginFailureThreshold: 8, LoginFailureWindowMins: 0, LoginFailureBlockMins: 60}).Validate()
	if err == nil {
		t.Fatal("zero window must be rejected")
	}
	_, err = (IPAccessControlSettings{
		LoginFailureThreshold: 8, LoginFailureWindowMins: maxLoginFailureControlMinutes,
		LoginFailureBlockMins: maxLoginFailureControlMinutes,
	}).Validate()
	if err != nil {
		t.Fatalf("one-year window and block duration must be accepted: %v", err)
	}
	_, err = (IPAccessControlSettings{
		LoginFailureThreshold: 8, LoginFailureWindowMins: maxLoginFailureControlMinutes + 1,
		LoginFailureBlockMins: 60,
	}).Validate()
	if err == nil {
		t.Fatal("window above one year must be rejected")
	}
}

func TestIPAccessControlUsesStaleSecurityCacheDuringDatabaseFailure(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{
		{IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
	}}
	svc := NewIPAccessControlService(settings, repo)

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("initial block lookup failed: blocked=%v err=%v", blocked, err)
	}

	settings.getErr = errors.New("settings database unavailable")
	repo.listErr = errors.New("rules database unavailable")
	if err := svc.refreshSecuritySnapshot(context.Background()); err == nil {
		t.Fatal("background refresh should report the database failure")
	}

	blocked, err = svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("known block must survive transient database failure: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlRefreshRetriesWhenLocalMutationCommitsDuringRead(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &overlappingSnapshotRepoStub{
		secondReadStarted: make(chan struct{}),
		releaseSecondRead: make(chan struct{}),
	}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	refreshDone := make(chan error, 1)
	go func() { refreshDone <- svc.refreshSecuritySnapshot(context.Background()) }()
	<-repo.secondReadStarted

	rule := &IPAccessRule{
		ID: 42, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindAutoBlock,
		Status: IPAccessRuleStatusActive,
	}
	// The repository update represents the already committed database state;
	// applyCommittedRule is the service patch that must not be lost when the
	// older in-flight read is released.
	repo.rules = []*IPAccessRule{rule}
	svc.applyCommittedRule(rule)
	close(repo.releaseSecondRead)
	if err := <-refreshDone; err != nil {
		t.Fatalf("refresh after overlapping mutation: %v", err)
	}
	if repo.listCalls != 3 {
		t.Fatalf("overlapping refresh must retry from the new mutation epoch, calls=%d", repo.listCalls)
	}

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("stale refresh must not overwrite the committed block: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlLateReleasePatchDoesNotRemoveNewerActiveRule(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	newer := &IPAccessRule{
		ID: 102, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock,
		Status: IPAccessRuleStatusActive,
	}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{newer}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	svc.applyCommittedRule(&IPAccessRule{
		ID: 101, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock,
		Status: IPAccessRuleStatusReleased,
	})
	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("late release result must not remove a newer active row: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlFailsClosedAfterSnapshotMaxStaleness(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{{
		IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive,
	}}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	svc.mu.Lock()
	staleAt := time.Now().Add(-ipAccessSecurityMaxStaleness - time.Second)
	svc.settingsCache.loadedAt = staleAt
	svc.rulesCache.loadedAt = staleAt
	svc.mu.Unlock()

	if svc.SecuritySnapshotReady() {
		t.Fatal("over-stale security snapshot must fail readiness")
	}
	if _, err := svc.IsBlocked(context.Background(), "203.0.113.8"); err == nil {
		t.Fatal("over-stale security snapshot must fail closed")
	}
}

func TestIPAccessControlFailureStateSeparatesRuleAndRuntimeStatus(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "false",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureIPThreshold:      "2",
	}}
	repo := &ipAccessRepoStub{failureList: &IPLoginFailureStateList{Items: []*IPLoginFailureState{{
		NormalizedIP: "203.0.113.8", FailureCount: 2, ActiveBlockRule: true,
	}}}}
	svc := NewIPAccessControlService(settings, repo)

	list, err := svc.ListFailureStates(context.Background(), IPLoginFailureStateFilter{})
	if err != nil {
		t.Fatalf("list failure states: %v", err)
	}
	state := list.Items[0]
	if !state.ActiveBlockRule || state.RuntimeEnforcementEnabled || state.EffectivelyBlocked {
		t.Fatalf("durable rule must remain visible while runtime enforcement is off: %#v", state)
	}
	if state.FailureThreshold != 2 || state.AsOf.IsZero() {
		t.Fatalf("failure threshold and as-of must be populated: %#v", state)
	}
}

func TestIPAccessControlFailureStateReportsEmergencyAllowlistOverride(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureIPThreshold:      "2",
	}}
	repo := &ipAccessRepoStub{failureList: &IPLoginFailureStateList{Items: []*IPLoginFailureState{{
		NormalizedIP: "203.0.113.8", FailureCount: 2, ActiveBlockRule: true,
	}}}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.ConfigureEmergencyAllowlist([]string{"203.0.113.0/24"}); err != nil {
		t.Fatalf("configure emergency allowlist: %v", err)
	}

	list, err := svc.ListFailureStates(context.Background(), IPLoginFailureStateFilter{})
	if err != nil {
		t.Fatalf("list failure states: %v", err)
	}
	state := list.Items[0]
	if !state.ActiveBlockRule || !state.RuntimeEnforcementEnabled || !state.EmergencyAllowlisted || state.EffectivelyBlocked {
		t.Fatalf("emergency allowlist must be visible and override the effective block state: %#v", state)
	}
}

func TestIPAccessControlBlockFailureStateCreatesEffectiveManualBlockWithoutReset(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureBlockMinutes:     "60",
		SettingKeyLoginFailureIPThreshold:      "2",
		SettingKeyLoginFailureWindowMinutes:    "15",
		SettingKeyLoginFailureAutoBlockEnabled: "false",
	}}
	repo := &ipAccessRepoStub{}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	result, err := svc.BlockFailureState(context.Background(), " 203.0.113.8 ", "", 7)
	if err != nil {
		t.Fatalf("block failure state: %v", err)
	}
	if result == nil || result.Rule == nil || result.Rule.RuleKind != IPAccessRuleKindManualBlock ||
		result.Rule.IPOrCIDR != "203.0.113.8" || result.AlreadyBlocked || !result.EffectivelyBlocked ||
		result.SuppressedByAllowRule || result.AsOf.IsZero() {
		t.Fatalf("unexpected manual block result: %#v", result)
	}
	if result.Rule.Reason != defaultManualFailureBlockReason {
		t.Fatalf("unexpected default reason: %q", result.Rule.Reason)
	}
	if result.Rule.ExpiresAt != nil {
		t.Fatalf("failure-state manual block must be permanent: %#v", result.Rule.ExpiresAt)
	}
	if len(repo.resetIPs) != 0 {
		t.Fatalf("manual block must preserve failure state: %#v", repo.resetIPs)
	}
}

func TestIPAccessControlBlockFailureStateReturnsPermanentManualBlockWhenAlreadyBlocked(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	expiresAt := time.Now().Add(time.Hour)
	existing := &IPAccessRule{
		ID: 88, IPOrCIDR: "203.0.113.0/24", RuleKind: IPAccessRuleKindAutoBlock,
		Status: IPAccessRuleStatusActive, ExpiresAt: &expiresAt,
	}
	permanent := &IPAccessRule{
		ID: 89, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock,
		Status: IPAccessRuleStatusActive,
	}
	repo := &ipAccessRepoStub{
		rules: []*IPAccessRule{existing},
		manualBlockResult: &IPFailureStateBlockRepositoryResult{
			Rule: permanent, AlreadyBlocked: true,
		},
	}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	result, err := svc.BlockFailureState(context.Background(), "203.0.113.8", "", 7)
	if err != nil {
		t.Fatalf("return existing block: %v", err)
	}
	if result == nil || !result.AlreadyBlocked || result.Rule == nil || result.Rule.ID != permanent.ID ||
		result.Rule.ExpiresAt != nil || !result.EffectivelyBlocked {
		t.Fatalf("unexpected idempotent result: %#v", result)
	}
	if len(repo.rules) != 2 {
		t.Fatalf("quick block must add the permanent exact manual rule: %#v", repo.rules)
	}
}

func TestIPAccessControlBlockFailureStateRejectsInactiveEnforcementAndAllowCoverage(t *testing.T) {
	t.Run("runtime enforcement disabled", func(t *testing.T) {
		settings := &ipAccessSettingRepoStub{values: map[string]string{
			SettingKeyGlobalIPAccessControlEnabled: "true",
			SettingKeyIPAccessControlEnabled:       "false",
		}}
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)
		_, err := svc.BlockFailureState(context.Background(), "203.0.113.8", "", 7)
		if infraerrors.Reason(err) != "IP_ACCESS_ENFORCEMENT_DISABLED" {
			t.Fatalf("unexpected disabled error: %v", err)
		}
		if repo.manualBlockCalls != 0 {
			t.Fatal("disabled enforcement must be rejected before rule creation")
		}
	})

	t.Run("allow rule coverage", func(t *testing.T) {
		settings := &ipAccessSettingRepoStub{values: map[string]string{
			SettingKeyGlobalIPAccessControlEnabled: "true",
			SettingKeyIPAccessControlEnabled:       "true",
		}}
		repo := &ipAccessRepoStub{manualBlockErr: ErrIPBlockSuppressedByAllow}
		svc := NewIPAccessControlService(settings, repo)
		_, err := svc.BlockFailureState(context.Background(), "203.0.113.8", "", 7)
		if infraerrors.Reason(err) != "IP_BLOCK_SUPPRESSED_BY_ALLOW" {
			t.Fatalf("unexpected allow conflict: %v", err)
		}
	})
}

func TestIPAccessControlBlockFailureStateFailsClosedWhenPostCommitRefreshFails(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	repo.listErr = errors.New("rules database unavailable")

	_, err := svc.BlockFailureState(context.Background(), "203.0.113.8", "", 7)
	if infraerrors.Reason(err) != "IP_ACCESS_CONTROL_UNAVAILABLE" {
		t.Fatalf("unconfirmed post-commit state must fail closed: %v", err)
	}
}

func TestIPAccessControlBlockFailureStateRejectsInvalidRepositoryRule(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)
	tests := []struct {
		name string
		rule *IPAccessRule
	}{
		{
			name: "temporary manual rule",
			rule: &IPAccessRule{ID: 1, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive, ExpiresAt: &expiresAt},
		},
		{
			name: "automatic rule",
			rule: &IPAccessRule{ID: 2, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindAutoBlock, Status: IPAccessRuleStatusActive},
		},
		{
			name: "different exact IP",
			rule: &IPAccessRule{ID: 3, IPOrCIDR: "203.0.113.9", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
		},
		{
			name: "inactive manual rule",
			rule: &IPAccessRule{ID: 4, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusReleased},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := &ipAccessSettingRepoStub{values: map[string]string{
				SettingKeyGlobalIPAccessControlEnabled: "true",
				SettingKeyIPAccessControlEnabled:       "true",
			}}
			repo := &ipAccessRepoStub{manualBlockResult: &IPFailureStateBlockRepositoryResult{Rule: test.rule}}
			svc := NewIPAccessControlService(settings, repo)

			_, err := svc.BlockFailureState(context.Background(), "203.0.113.8", "", 7)
			if infraerrors.Reason(err) != "IP_ACCESS_CONTROL_UNAVAILABLE" {
				t.Fatalf("invalid repository rule must fail closed: %v", err)
			}
		})
	}
}

func TestIPAccessControlRequiresActiveRuleBeforeReportingBlocked(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "2",
	}}
	repo := &ipAccessRepoStub{recordResult: &LoginFailureRecordResult{FailureCount: 2, Blocked: true}}
	svc := NewIPAccessControlService(settings, repo)

	if _, err := svc.RecordFailedLogin(context.Background(), "203.0.113.8"); err == nil {
		t.Fatal("blocked result without an active durable rule must fail closed")
	}
}

func TestIPAccessControlRequiresCommittedBlockToBeEffectiveBeforeReportingBlocked(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "2",
	}}
	allow := &IPAccessRule{
		ID: 1, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindAllow,
		Status: IPAccessRuleStatusActive,
	}
	block := &IPAccessRule{
		ID: 2, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindAutoBlock,
		Status: IPAccessRuleStatusActive,
	}
	repo := &ipAccessRepoStub{
		rules: []*IPAccessRule{allow},
		recordResult: &LoginFailureRecordResult{
			FailureCount: 2, Blocked: true, Rule: block,
		},
	}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	if _, err := svc.RecordFailedLogin(context.Background(), "203.0.113.8"); err == nil {
		t.Fatal("a locally allow-suppressed block must not be reported as an effective ban")
	}
}

func TestIPAccessControlRequestPathUsesWarmSnapshotWithoutDatabaseReload(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{{
		ID: 1, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive,
	}}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	for range 20 {
		blocked, err := svc.IsBlocked(context.Background(), "203.0.113.8")
		if err != nil || !blocked {
			t.Fatalf("warm snapshot decision failed: blocked=%v err=%v", blocked, err)
		}
	}
	if settings.getCalls != 1 || repo.listCalls != 1 {
		t.Fatalf("request path reloaded PostgreSQL: settings=%d rules=%d", settings.getCalls, repo.listCalls)
	}
}

func TestIPAccessControlAppliesCommittedAutoBlockBeforePublishingRefresh(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "2",
	}}
	rule := &IPAccessRule{
		ID: 42, IPOrCIDR: "203.0.113.8", RuleKind: IPAccessRuleKindAutoBlock,
		Status: IPAccessRuleStatusActive,
	}
	repo := &ipAccessRepoStub{recordResult: &LoginFailureRecordResult{
		FailureCount: 2, Blocked: true, Rule: rule,
	}}
	svc := NewIPAccessControlService(settings, repo)
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if _, err := svc.RecordFailedLogin(context.Background(), "203.0.113.8"); err != nil {
		t.Fatalf("record failed login: %v", err)
	}
	repo.listErr = errors.New("rules database unavailable")

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("committed auto block must be immediately available locally: blocked=%v err=%v", blocked, err)
	}
	if repo.listCalls != 1 {
		t.Fatalf("request path reloaded rules after local commit: %d", repo.listCalls)
	}
}

func TestIPAccessControlWarmupRequiresCompleteSnapshot(t *testing.T) {
	settings := &ipAccessSettingRepoStub{
		values: map[string]string{
			SettingKeyGlobalIPAccessControlEnabled: "true",
			SettingKeyIPAccessControlEnabled:       "true",
		},
	}
	repo := &ipAccessRepoStub{}
	svc := NewIPAccessControlService(settings, repo)

	settings.getErr = errors.New("settings database unavailable")
	if err := svc.Warmup(context.Background()); err == nil {
		t.Fatal("cold warmup must fail when settings cannot be loaded")
	}
	if svc.SecuritySnapshotReady() {
		t.Fatal("partial or missing cold snapshot must not report ready")
	}

	settings.getErr = nil
	repo.listErr = errors.New("rules database unavailable")
	if err := svc.Warmup(context.Background()); err == nil {
		t.Fatal("cold warmup must fail when rules cannot be loaded")
	}
	if svc.SecuritySnapshotReady() {
		t.Fatal("settings-only snapshot must not report ready")
	}

	repo.listErr = nil
	if err := svc.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup should recover: %v", err)
	}
	if !svc.SecuritySnapshotReady() {
		t.Fatal("complete snapshot must report ready")
	}
}

func TestIPAccessControlDropsExpiredRulesFromStaleCache(t *testing.T) {
	expiresAt := time.Now().Add(10 * time.Millisecond)
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{
		{
			IPOrCIDR: "203.0.113.9", RuleKind: IPAccessRuleKindManualBlock,
			Status: IPAccessRuleStatusActive, ExpiresAt: &expiresAt,
		},
	}}
	svc := NewIPAccessControlService(settings, repo)

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.9")
	if err != nil || !blocked {
		t.Fatalf("rule should initially block: blocked=%v err=%v", blocked, err)
	}
	time.Sleep(15 * time.Millisecond)
	repo.listErr = errors.New("rules database unavailable")

	blocked, err = svc.IsBlocked(context.Background(), "203.0.113.9")
	if err != nil || blocked {
		t.Fatalf("expired cached rule must not continue blocking: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlCanonicalizesCIDRRules(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{}}
	repo := &ipAccessRepoStub{}
	svc := NewIPAccessControlService(settings, repo)

	_, err := svc.AddManualRule(
		context.Background(),
		"192.0.2.9/24",
		IPAccessRuleKindManualBlock,
		"test",
		nil,
		1,
	)
	if err != nil {
		t.Fatalf("add rule failed: %v", err)
	}
	if repo.created == nil || repo.created.IPOrCIDR != "192.0.2.0/24" {
		t.Fatalf("CIDR was not canonicalized: %#v", repo.created)
	}
}

func TestIPAccessControlRejectsGlobalManualRule(t *testing.T) {
	svc := NewIPAccessControlService(&ipAccessSettingRepoStub{values: map[string]string{}}, &ipAccessRepoStub{})
	_, err := svc.AddManualRule(context.Background(), "0.0.0.0/0", IPAccessRuleKindManualBlock, "", nil, 1)
	if err == nil {
		t.Fatal("global manual block must be rejected")
	}
	_, err = svc.AddManualRule(context.Background(), "::/0", IPAccessRuleKindAllow, "", nil, 1)
	if err == nil {
		t.Fatal("global allow rule must be rejected")
	}
}

func TestIPAccessControlInvalidPersistedLimitsUseSafeDefaults(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "1",
		SettingKeyLoginFailureWindowMinutes:    "999999",
		SettingKeyLoginFailureBlockMinutes:     "invalid",
	}}
	svc := NewIPAccessControlService(settings, &ipAccessRepoStub{})

	got, err := svc.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("invalid persisted limits should fall back safely: %v", err)
	}
	want := DefaultIPAccessControlSettings()
	if !got.EnforcementEnabled || !got.LoginFailureAutoBlock {
		t.Fatalf("valid security switches must be preserved: %#v", got)
	}
	if got.LoginFailureThreshold != want.LoginFailureThreshold ||
		got.LoginFailureWindowMins != want.LoginFailureWindowMins ||
		got.LoginFailureBlockMins != want.LoginFailureBlockMins {
		t.Fatalf("invalid limits did not use defaults: %#v", got)
	}
}

func TestIPAccessControlPersistedMaximumLimitsArePreserved(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
		SettingKeyLoginFailureIPThreshold:      "2",
		SettingKeyLoginFailureWindowMinutes:    "525600",
		SettingKeyLoginFailureBlockMinutes:     "525600",
	}}
	svc := NewIPAccessControlService(settings, &ipAccessRepoStub{})

	got, err := svc.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("maximum persisted limits should load: %v", err)
	}
	if got.LoginFailureWindowMins != maxLoginFailureControlMinutes ||
		got.LoginFailureBlockMins != maxLoginFailureControlMinutes {
		t.Fatalf("maximum persisted limits were not preserved: %#v", got)
	}
}

func TestIPAccessControlDisablingEnforcementClearsHiddenAutoBlockFlag(t *testing.T) {
	got, err := (IPAccessControlSettings{
		EnforcementEnabled:     false,
		LoginFailureAutoBlock:  true,
		LoginFailureThreshold:  8,
		LoginFailureWindowMins: 15,
		LoginFailureBlockMins:  60,
	}).Validate()
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if got.LoginFailureAutoBlock {
		t.Fatal("auto-block must not remain secretly enabled while enforcement is disabled")
	}
}

func TestIPAccessControlMasterSwitchDisablesRuntimeEnforcement(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "false",
		SettingKeyIPAccessControlEnabled:       "true",
		SettingKeyLoginFailureAutoBlockEnabled: "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{
		{IPOrCIDR: "203.0.113.99", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
	}}
	svc := NewIPAccessControlService(settings, repo)

	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.99")
	if err != nil || blocked {
		t.Fatalf("master switch off must not enforce stored blocks: blocked=%v err=%v", blocked, err)
	}
	if _, err := svc.RecordFailedLogin(context.Background(), "203.0.113.99"); err != nil {
		t.Fatalf("master switch off must not record failures: %v", err)
	}
}

func TestIPAccessControlMissingMasterSwitchDoesNotEnableLegacyEnforcement(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyIPAccessControlEnabled: "true",
	}}
	repo := &ipAccessRepoStub{rules: []*IPAccessRule{
		{IPOrCIDR: "203.0.113.99", RuleKind: IPAccessRuleKindManualBlock, Status: IPAccessRuleStatusActive},
	}}
	svc := NewIPAccessControlService(settings, repo)

	got, err := svc.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.FeatureEnabled || got.RuntimeEnforcementActive() {
		t.Fatalf("missing master switch must stay off even if legacy enforcement is on: %#v", got)
	}
	if _, exists := settings.values[SettingKeyGlobalIPAccessControlEnabled]; exists {
		t.Fatalf("missing master switch must not be written during read, got %q", settings.values[SettingKeyGlobalIPAccessControlEnabled])
	}
	blocked, err := svc.IsBlocked(context.Background(), "203.0.113.99")
	if err != nil || blocked {
		t.Fatalf("legacy enforcement alone must not block: blocked=%v err=%v", blocked, err)
	}
}

func TestIPAccessControlUpdateSettingsPreservesMasterSwitch(t *testing.T) {
	settings := &ipAccessSettingRepoStub{values: map[string]string{
		SettingKeyGlobalIPAccessControlEnabled: "true",
		SettingKeyIPAccessControlEnabled:       "true",
	}}
	svc := NewIPAccessControlService(settings, &ipAccessRepoStub{})
	if _, err := svc.GetSettings(context.Background()); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	got, err := svc.UpdateSettings(context.Background(), IPAccessControlSettings{
		EnforcementEnabled:     true,
		LoginFailureAutoBlock:  false,
		LoginFailureThreshold:  8,
		LoginFailureWindowMins: 15,
		LoginFailureBlockMins:  60,
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if !got.FeatureEnabled {
		t.Fatal("IP page save must not clear the system-settings master switch")
	}
	if settings.values[SettingKeyGlobalIPAccessControlEnabled] != "true" {
		t.Fatalf("master switch must stay persisted, got %q", settings.values[SettingKeyGlobalIPAccessControlEnabled])
	}
	cached, err := svc.GetSettings(context.Background())
	if err != nil || !cached.FeatureEnabled || !cached.RuntimeEnforcementActive() {
		t.Fatalf("cached snapshot lost the master switch: %#v err=%v", cached, err)
	}
}
