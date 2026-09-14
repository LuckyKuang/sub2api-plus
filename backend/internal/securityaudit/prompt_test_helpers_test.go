//go:build !e2e

package securityaudit

import (
	"context"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
)

func promptAuditUpdateRequest(version int64, workerCount int, token string) UpdateConfigRequest {
	return UpdateConfigRequest{
		ExpectedConfigVersion: version, Enabled: true, BlockingEnabled: false, StorePassEvents: false,
		Strategy: "priority", WorkerCount: workerCount, QueueCapacity: 64, Scanners: []string{"pii", "jailbreak"},
		AllGroups: true, Endpoints: []UpdateEndpoint{{
			ID: "guard-one", Name: "Guard One", Protocol: "openai_compatible",
			BaseURL: "http://127.0.0.1:18080", Model: "", Token: token,
			TimeoutMS: 1000, InputLimit: 1024, Enabled: true,
		}},
	}
}

func integrationResult(decision EventDecision) *NormalizedResult {
	result := &NormalizedResult{
		Decision: decision, RiskLevel: RiskLow, Action: ActionAllow, Safety: "Safe",
		Categories: []string{}, MatchedScanners: []string{}, ScannerScores: map[string]float64{},
		ScannerEvidence: map[string]string{}, ScannerBackend: "qwen3guard-openai",
		ScannerVersion: "test", GuardEndpointID: "guard-1", PolicyID: "priority",
		PolicyVersion: 1, ChunkTotal: 1, InputLimit: 100000, LatencyMS: 2,
	}
	if decision != EventPass {
		result.RiskLevel = RiskCritical
		result.Action = ActionBlock
		result.Safety = "Unsafe"
		result.Categories = []string{"pii"}
		result.MatchedScanners = []string{"pii"}
		result.ScannerScores["pii"] = 1
		result.ScannerEvidence["pii"] = "redacted evidence"
		result.MatchedChunkIndex = 1
	}
	return result
}

// testTotpKeyConfig mirrors a deployment with a fixed TOTP_ENCRYPTION_KEY so
// tests may persist endpoint tokens.
func testTotpKeyConfig() *config.Config {
	return &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true}}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeConfigStore struct {
	cfg      ActiveConfig
	active   bool
	degraded bool
}

func (s *fakeConfigStore) Start(context.Context) error    { return nil }
func (s *fakeConfigStore) Shutdown(context.Context) error { return nil }
func (s *fakeConfigStore) Active() (ActiveConfig, bool)   { return cloneActiveConfig(s.cfg), s.active }
func (s *fakeConfigStore) EffectiveMode() Mode {
	if s.BlockingActivationDegraded() {
		return ModeBlocking
	}
	if !s.active {
		return ModeOff
	}
	return s.cfg.EffectiveMode()
}
func (s *fakeConfigStore) BlockingActivationDegraded() bool { return s.degraded }
func (s *fakeConfigStore) Public() (PublicConfig, error)    { return PublicConfig{}, nil }
func (s *fakeConfigStore) Save(context.Context, UpdateConfigRequest, int64) (PublicConfig, error) {
	return PublicConfig{}, nil
}
func (s *fakeConfigStore) RuntimeState() (int64, int64, *time.Time, string) {
	return s.cfg.ConfigVersion, s.cfg.ConfigVersion, nil, ""
}
func (s *fakeConfigStore) Encrypt(value string) (string, error) { return value, nil }
func (s *fakeConfigStore) Decrypt(value string) (string, error) { return value, nil }
