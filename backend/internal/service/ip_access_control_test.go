package service

import (
	"context"
	"errors"
	"testing"
	"time"

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
	rules       []*IPAccessRule
	listErr     error
	listCalls   int
	created     *IPAccessRule
	resetIPs    []string
	resetErr    error
	blockHitIPs chan string
	blockHitErr error
}

func (r *ipAccessRepoStub) ListIPAccessRules(context.Context, IPAccessRuleFilter) (*IPAccessRuleList, error) {
	return &IPAccessRuleList{}, nil
}
func (r *ipAccessRepoStub) ListActiveIPAccessRules(context.Context) ([]*IPAccessRule, error) {
	r.listCalls++
	return r.rules, r.listErr
}
func (r *ipAccessRepoStub) CreateManualIPAccessRule(_ context.Context, rule *IPAccessRule) (*IPAccessRule, error) {
	r.created = rule
	r.rules = append(r.rules, rule)
	return rule, nil
}
func (r *ipAccessRepoStub) ReleaseIPAccessRuleAndReset(context.Context, int64, int64) (*IPAccessRule, error) {
	return nil, ErrIPAccessRuleNotFound
}
func (r *ipAccessRepoStub) ListIPLoginFailureStates(context.Context, IPLoginFailureStateFilter, time.Duration) (*IPLoginFailureStateList, error) {
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

func TestIPAccessControlClearsSuccessfulLocalLoginOnlyWhenAutoBlockingIsActive(t *testing.T) {
	t.Run("automatic blocking active", func(t *testing.T) {
		settings := &ipAccessSettingRepoStub{values: map[string]string{
			SettingKeyGlobalIPAccessControlEnabled: "true",
			SettingKeyIPAccessControlEnabled:       "true",
			SettingKeyLoginFailureAutoBlockEnabled: "true",
		}}
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)

		if err := svc.ClearSuccessfulLocalLogin(context.Background(), "203.0.113.8"); err != nil {
			t.Fatalf("clear successful local login: %v", err)
		}
		if len(repo.resetIPs) != 1 || repo.resetIPs[0] != "203.0.113.8" {
			t.Fatalf("successful local login did not reset its IP state: %#v", repo.resetIPs)
		}
	})

	t.Run("automatic blocking disabled", func(t *testing.T) {
		settings := &ipAccessSettingRepoStub{values: map[string]string{
			SettingKeyGlobalIPAccessControlEnabled: "true",
			SettingKeyIPAccessControlEnabled:       "true",
			SettingKeyLoginFailureAutoBlockEnabled: "false",
		}}
		repo := &ipAccessRepoStub{}
		svc := NewIPAccessControlService(settings, repo)

		if err := svc.ClearSuccessfulLocalLogin(context.Background(), "203.0.113.8"); err != nil {
			t.Fatalf("disabled automatic blocking must be a no-op: %v", err)
		}
		if len(repo.resetIPs) != 0 {
			t.Fatalf("disabled automatic blocking unexpectedly wrote login state: %#v", repo.resetIPs)
		}
	})
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

	svc.mu.Lock()
	svc.settingsCache.expiresAt = time.Now().Add(-time.Second)
	svc.rulesCache.expiresAt = time.Now().Add(-time.Second)
	svc.mu.Unlock()
	settings.getErr = errors.New("settings database unavailable")
	repo.listErr = errors.New("rules database unavailable")

	blocked, err = svc.IsBlocked(context.Background(), "203.0.113.8")
	if err != nil || !blocked {
		t.Fatalf("known block must survive transient database failure: blocked=%v err=%v", blocked, err)
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
