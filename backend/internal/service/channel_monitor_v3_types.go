package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
)

var ErrChannelMonitorV3Config = errors.New("invalid service status configuration")
var ErrChannelMonitorV3Conflict = errors.New("service status configuration changed")

var ChannelMonitorV3LatencyBounds = [...]int64{50, 100, 250, 500, 1000, 2000, 3000, 5000, 8000, 10000, 15000, 30000, 60000, 120000, 300000, 2147483647}

type ChannelMonitorV3Config struct {
	Version           int      `json:"version"`
	MinimumSamples    int64    `json:"minimum_samples"`
	WarningError      float64  `json:"warning_error_rate"`
	OutageError       float64  `json:"outage_error_rate"`
	WarningTTFTMs     int64    `json:"warning_ttft_ms"`
	AbnormalWindows   int      `json:"abnormal_windows"`
	RecoveryWindows   int      `json:"recovery_windows"`
	DisabledPlatforms []string `json:"disabled_platforms"`
	// ObservationStarts is server-owned persisted metadata, never an API field.
	ObservationStarts map[string]time.Time `json:"-"`
}

func DefaultChannelMonitorV3Config() ChannelMonitorV3Config {
	return ChannelMonitorV3Config{Version: 1, MinimumSamples: 5, WarningError: .05, OutageError: .9, WarningTTFTMs: 5000, AbnormalWindows: 2, RecoveryWindows: 3, DisabledPlatforms: []string{}}
}

func (c ChannelMonitorV3Config) Validate() error {
	if c.MinimumSamples < 1 || c.MinimumSamples > 10000 || math.IsNaN(c.WarningError) || math.IsNaN(c.OutageError) || c.WarningError <= 0 || c.WarningError >= 1 || c.OutageError < c.WarningError || c.OutageError > 1 || c.WarningTTFTMs < 100 || c.WarningTTFTMs > 300000 || c.AbnormalWindows < 1 || c.AbnormalWindows > 10 || c.RecoveryWindows < 1 || c.RecoveryWindows > 10 {
		return ErrChannelMonitorV3Config
	}
	seen := map[string]bool{}
	for _, platform := range c.DisabledPlatforms {
		if seen[platform] || !containsChannelMonitorV3Platform(platform) {
			return ErrChannelMonitorV3Config
		}
		seen[platform] = true
	}
	return nil
}

func containsChannelMonitorV3Platform(platform string) bool {
	for _, name := range domain.ConcretePlatforms() {
		if name == platform {
			return true
		}
	}
	return false
}

func (c ChannelMonitorV3Config) PlatformEnabled(platform string) bool {
	if !containsChannelMonitorV3Platform(platform) {
		return false
	}
	for _, disabled := range c.DisabledPlatforms {
		if disabled == platform {
			return false
		}
	}
	return true
}

func (c ChannelMonitorV3Config) EnabledPlatforms() []string {
	result := []string{}
	for _, platform := range domain.ConcretePlatforms() {
		if c.PlatformEnabled(platform) {
			result = append(result, platform)
		}
	}
	return result
}

// Facts are internal aggregates. They are never serialized in viewer responses.
type ChannelMonitorV3Fact struct {
	Bucket      time.Time
	Platform    string
	GroupID     int64
	GroupName   string
	Model       string
	Success     int64
	Failures    int64
	LastRequest time.Time
	Latency     [16]int64
}

func (f *ChannelMonitorV3Fact) Merge(other ChannelMonitorV3Fact) {
	f.Success += other.Success
	f.Failures += other.Failures
	if other.LastRequest.After(f.LastRequest) {
		f.LastRequest = other.LastRequest
	}
	for i := range f.Latency {
		f.Latency[i] += other.Latency[i]
	}
}

func (f ChannelMonitorV3Fact) P50() *int64 {
	var total, cumulative int64
	for _, n := range f.Latency {
		total += n
	}
	if total == 0 {
		return nil
	}
	for i, n := range f.Latency {
		cumulative += n
		if cumulative >= (total+1)/2 {
			v := ChannelMonitorV3LatencyBounds[i]
			return &v
		}
	}
	return nil
}

func ChannelMonitorV3Health(f ChannelMonitorV3Fact, cfg ChannelMonitorV3Config) string {
	n := f.Success + f.Failures
	if n == 0 {
		return "unknown"
	}
	if n < cfg.MinimumSamples {
		return "insufficient"
	}
	rate := float64(f.Failures) / float64(n)
	if rate >= cfg.OutageError {
		return "outage"
	}
	if rate >= cfg.WarningError {
		return "partial"
	}
	if p50 := f.P50(); p50 != nil && *p50 > cfg.WarningTTFTMs {
		return "degraded"
	}
	return "normal"
}

// Reuse the established taxonomy while distinguishing upstream credentials and
// capacity from user authentication, quota, policy, cancellation and bad input.
func ChannelMonitorV3EligibleError(input ChannelMonitorV2ErrorInput) bool {
	input.ErrorOwner = strings.ToLower(strings.TrimSpace(input.ErrorOwner))
	if input.ErrorOwner == "client" || input.StatusCode == 499 || channelMonitorV2ContainsAny(strings.ToLower(input.Message+" "+input.ErrorType), "client cancelled", "client canceled", "context canceled") {
		return false
	}
	category := ClassifyChannelMonitorV2Error(input)
	switch category {
	case "content_policy", "client_cancelled", "invalid_request", "context_limit", "group_access", "model_unsupported", "not_found":
		return false
	case "authentication", "quota_or_balance", "rate_or_capacity", "upstream_forbidden":
		return input.ErrorOwner == "provider" || input.UpstreamStatusCode > 0
	case "account_pool_unavailable", "timeout", "transport_or_stream", "upstream_5xx", "internal":
		return input.ErrorOwner != "client"
	default:
		return input.ErrorOwner != "client" && (input.StatusCode >= 500 || input.UpstreamStatusCode >= 500)
	}
}

type ChannelMonitorV3Update struct {
	Phase    string    `json:"phase"`
	Severity string    `json:"severity"`
	At       time.Time `json:"at"`
}

type ChannelMonitorV3Incident struct {
	ID         string                   `json:"id"`
	Platform   string                   `json:"platform"`
	GroupID    int64                    `json:"group_id"`
	GroupName  string                   `json:"group_name"`
	Model      string                   `json:"model"`
	Severity   string                   `json:"severity"`
	Phase      string                   `json:"phase"`
	StartedAt  time.Time                `json:"started_at"`
	UpdatedAt  time.Time                `json:"updated_at"`
	ResolvedAt *time.Time               `json:"resolved_at"`
	Updates    []ChannelMonitorV3Update `json:"updates"`
}

type ChannelMonitorV3State struct {
	LastEvaluated time.Time                 `json:"last_evaluated"`
	LastRequest   time.Time                 `json:"last_request"`
	Pending       string                    `json:"pending"`
	Streak        int                       `json:"streak"`
	PendingSince  time.Time                 `json:"pending_since"`
	Incident      *ChannelMonitorV3Incident `json:"incident,omitempty"`
}

type ChannelMonitorV3Catalog struct {
	Platform  string
	GroupID   int64
	GroupName string
}
type ChannelMonitorV3Data struct {
	Config      ChannelMonitorV3Config
	Catalog     []ChannelMonitorV3Catalog
	Current     []ChannelMonitorV3Fact
	KnownModels []ChannelMonitorV3Fact
	History     []ChannelMonitorV3Fact
	Totals      []ChannelMonitorV3Fact
	States      map[string]ChannelMonitorV3State
	Incidents   []ChannelMonitorV3Incident
	DataThrough *time.Time
}

type ChannelMonitorV3Repository interface {
	GetConfig(context.Context) (*ChannelMonitorV3Config, error)
	UpdateConfig(context.Context, ChannelMonitorV3Config) (*ChannelMonitorV3Config, error)
	Refresh(context.Context, time.Time) error
	Read(context.Context, time.Time, time.Duration, string) (*ChannelMonitorV3Data, error)
}

type ChannelMonitorV3Timeline struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
}
type ChannelMonitorV3Model struct {
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	Model     string `json:"model"`
	Status    string `json:"status"`
}
type ChannelMonitorV3Platform struct {
	Platform    string                     `json:"platform"`
	Status      string                     `json:"status"`
	SuccessRate *float64                   `json:"success_rate"`
	TTFTP50Ms   *int64                     `json:"ttft_p50_ms"`
	LastRequest *time.Time                 `json:"last_request_at"`
	Timeline    []ChannelMonitorV3Timeline `json:"timeline"`
	Models      []ChannelMonitorV3Model    `json:"models"`
}
type ChannelMonitorV3Summary struct {
	Status       string `json:"status"`
	Normal       int    `json:"normal"`
	Affected     int    `json:"affected"`
	Unknown      int    `json:"unknown"`
	Recovering   int    `json:"recovering"`
	ActiveEvents int    `json:"active_events"`
}
type ChannelMonitorV3Snapshot struct {
	MonitoringEnabled bool                       `json:"monitoring_enabled"`
	ComputedAt        time.Time                  `json:"computed_at"`
	DataThrough       *time.Time                 `json:"data_through"`
	Platforms         []ChannelMonitorV3Platform `json:"platforms"`
	Incidents         []ChannelMonitorV3Incident `json:"incidents"`
	Summary           ChannelMonitorV3Summary    `json:"summary"`
}
