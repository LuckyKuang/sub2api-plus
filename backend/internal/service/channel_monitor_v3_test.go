//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV3ConfigAndHealth(t *testing.T) {
	cfg := DefaultChannelMonitorV3Config()
	require.NoError(t, cfg.Validate())
	for _, mutate := range []func(*ChannelMonitorV3Config){
		func(c *ChannelMonitorV3Config) { c.MinimumSamples = 0 }, func(c *ChannelMonitorV3Config) { c.WarningError = math.NaN() },
		func(c *ChannelMonitorV3Config) { c.OutageError = .01 }, func(c *ChannelMonitorV3Config) { c.RecoveryWindows = 0 },
	} {
		bad := cfg
		mutate(&bad)
		require.ErrorIs(t, bad.Validate(), ErrChannelMonitorV3Config)
	}
	f := ChannelMonitorV3Fact{}
	require.Equal(t, "unknown", ChannelMonitorV3Health(f, cfg))
	f.Success = 4
	require.Equal(t, "insufficient", ChannelMonitorV3Health(f, cfg))
	f.Success = 100
	f.Latency[7] = 100
	require.Equal(t, "normal", ChannelMonitorV3Health(f, cfg))
	require.Equal(t, int64(5000), *f.P50())
	f.Latency[7] = 0
	f.Latency[8] = 100
	require.Equal(t, "degraded", ChannelMonitorV3Health(f, cfg))
	f.Failures = 10
	require.Equal(t, "partial", ChannelMonitorV3Health(f, cfg))
	f.Success = 0
	require.Equal(t, "outage", ChannelMonitorV3Health(f, cfg))
	for _, window := range []string{"24h", "7d", "30d", ""} {
		_, ok := ChannelMonitorV3Window(window)
		require.True(t, ok)
	}
	_, ok := ChannelMonitorV3Window("90m")
	require.False(t, ok)
}

func TestChannelMonitorV3EligibleErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in       ChannelMonitorV2ErrorInput
		eligible bool
	}{
		{"provider auth", ChannelMonitorV2ErrorInput{StatusCode: 502, UpstreamStatusCode: 401, ErrorOwner: "provider"}, true},
		{"user auth", ChannelMonitorV2ErrorInput{StatusCode: 401, ErrorOwner: "client"}, false},
		{"user quota", ChannelMonitorV2ErrorInput{StatusCode: 429, Message: "insufficient quota"}, false},
		{"upstream capacity", ChannelMonitorV2ErrorInput{StatusCode: 429, UpstreamStatusCode: 429}, true},
		{"policy", ChannelMonitorV2ErrorInput{StatusCode: 502, Message: "content_policy"}, false},
		{"bad input", ChannelMonitorV2ErrorInput{StatusCode: 502, Message: "invalid_request"}, false},
		{"context", ChannelMonitorV2ErrorInput{StatusCode: 400, Message: "context length"}, false},
		{"unsupported", ChannelMonitorV2ErrorInput{StatusCode: 503, Message: "model not supported"}, false},
		{"cancel beats transport", ChannelMonitorV2ErrorInput{StatusCode: 499, Message: "stream_read_error"}, false},
		{"cancel beats timeout", ChannelMonitorV2ErrorInput{StatusCode: 504, Message: "context canceled timeout"}, false},
		{"pool", ChannelMonitorV2ErrorInput{StatusCode: 503, Message: "no available accounts"}, true},
		{"timeout", ChannelMonitorV2ErrorInput{StatusCode: 504}, true},
		{"upstream 500", ChannelMonitorV2ErrorInput{StatusCode: 502, UpstreamStatusCode: 500}, true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.eligible, ChannelMonitorV3EligibleError(tc.in)) })
	}
}

func TestChannelMonitorV3IncidentRequiresFreshEvidence(t *testing.T) {
	cfg := DefaultChannelMonitorV3Config()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	f := ChannelMonitorV3Fact{Platform: "openai", GroupID: 1, Model: "gpt", Failures: 5, LastRequest: now.Add(-time.Second)}
	state, event := AdvanceChannelMonitorV3State(ChannelMonitorV3State{}, f, now, cfg)
	require.Nil(t, event)
	state, event = AdvanceChannelMonitorV3State(state, f, now.Add(time.Minute), cfg)
	require.Nil(t, event)
	require.Equal(t, 1, state.Streak)
	f.Success = 5
	f.Failures = 1
	f.LastRequest = now.Add(time.Minute)
	state, event = AdvanceChannelMonitorV3State(state, f, now.Add(2*time.Minute), cfg)
	require.NotNil(t, event)
	require.Equal(t, "detected", event.Phase)
	require.Equal(t, "partial", event.Severity)
	id := event.ID
	previous := state
	quiet := ChannelMonitorV3Fact{}
	state, event = AdvanceChannelMonitorV3State(state, quiet, now.Add(3*time.Minute), cfg)
	require.Equal(t, "awaiting_data", event.Phase)
	require.Nil(t, event.ResolvedAt)
	require.Equal(t, "detected", previous.Incident.Phase, "previous state must remain immutable")
	f.Success = 5
	f.Failures = 0
	for i := 4; i <= 6; i++ {
		f.LastRequest = now.Add(time.Duration(i)*time.Minute - time.Second)
		state, event = AdvanceChannelMonitorV3State(state, f, now.Add(time.Duration(i)*time.Minute), cfg)
		if i == 4 {
			require.Equal(t, "recovering", event.Phase)
		}
		if i == 5 {
			require.Nil(t, event)
		}
	}
	require.Equal(t, id, event.ID)
	require.Equal(t, "resolved", event.Phase)
	require.NotNil(t, event.ResolvedAt)
	require.Nil(t, state.Incident)
}

func TestChannelMonitorV3RecoveryInterruptedAndIdempotent(t *testing.T) {
	cfg := DefaultChannelMonitorV3Config()
	cfg.AbnormalWindows = 1
	now := time.Now().UTC()
	f := ChannelMonitorV3Fact{Platform: "openai", GroupID: 1, Model: "gpt", Failures: 5, LastRequest: now.Add(-time.Second)}
	state, _ := AdvanceChannelMonitorV3State(ChannelMonitorV3State{}, f, now, cfg)
	f.Success = 5
	f.Failures = 0
	f.LastRequest = now.Add(time.Minute - time.Second)
	state, _ = AdvanceChannelMonitorV3State(state, f, now.Add(time.Minute), cfg)
	same, event := AdvanceChannelMonitorV3State(state, f, now.Add(time.Minute), cfg)
	require.Nil(t, event)
	require.Equal(t, state, same)
	f.Success = 0
	f.Failures = 5
	f.LastRequest = now.Add(2*time.Minute - time.Second)
	state, event = AdvanceChannelMonitorV3State(state, f, now.Add(2*time.Minute), cfg)
	require.Equal(t, "ongoing", event.Phase)
	require.Nil(t, event.ResolvedAt)
	// Waiting beyond the rolling window preserves the active incident.
	state, event = AdvanceChannelMonitorV3State(state, f, now.Add(8*time.Minute), cfg)
	require.Equal(t, "awaiting_data", event.Phase)
	require.NotNil(t, state.Incident)
}

func TestChannelMonitorV3SnapshotScopeStalenessAndSeverity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	cfg := DefaultChannelMonitorV3Config()
	data := &ChannelMonitorV3Data{DataThrough: &now, Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1, GroupName: "Visible"}}, States: map[string]ChannelMonitorV3State{}, Current: []ChannelMonitorV3Fact{
		{Platform: "openai", GroupID: 1, GroupName: "Visible", Model: "healthy", Success: 1000, LastRequest: now.Add(-time.Second), Latency: [16]int64{8: 1000}},
		{Platform: "openai", GroupID: 1, GroupName: "Visible", Model: "bad", Failures: 5, LastRequest: now.Add(-time.Second)},
		{Platform: "openai", GroupID: 999, GroupName: "Hidden", Model: "secret", Failures: 10000, LastRequest: now},
	}, KnownModels: []ChannelMonitorV3Fact{{Platform: "openai", GroupID: 1, GroupName: "Visible", Model: "old"}}, Incidents: []ChannelMonitorV3Incident{{Platform: "openai", GroupID: 999, Model: "secret"}, {Platform: "openai", GroupID: 1, Model: "bad"}}}
	result := BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Equal(t, "partial", result.Platforms[0].Status, "small model outage must override pooled degraded latency")
	require.Equal(t, 1, result.Summary.ActiveEvents)
	require.Len(t, result.Incidents, 1)
	require.Len(t, result.Platforms[0].Models, 3)
	require.Len(t, result.Platforms[0].Timeline, 48)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	for _, secret := range []string{"secret", "Hidden", "user_id", "account_id", "actual_cost", "request_count", "error_message"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Empty(t, BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "gemini").Platforms)
	through := now.Add(-4 * time.Minute)
	data.DataThrough = &through
	result = BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Equal(t, "unknown", result.Platforms[0].Status)
	for _, model := range result.Platforms[0].Models {
		require.Equal(t, "unknown", model.Status)
	}
	data.DataThrough = &now
	data.Current = nil
	result = BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Equal(t, "unknown", result.Summary.Status)
	require.Equal(t, 1, result.Summary.ActiveEvents)
}

type channelMonitorV3RepoStub struct {
	cfg       ChannelMonitorV3Config
	refreshes atomic.Int32
	data      *ChannelMonitorV3Data
}

func (s *channelMonitorV3RepoStub) GetConfig(context.Context) (*ChannelMonitorV3Config, error) {
	c := s.cfg
	return &c, nil
}
func (s *channelMonitorV3RepoStub) UpdateConfig(_ context.Context, c ChannelMonitorV3Config) (*ChannelMonitorV3Config, error) {
	s.cfg = c
	return &c, nil
}
func (s *channelMonitorV3RepoStub) Refresh(context.Context, time.Time, ChannelMonitorV3Config) error {
	s.refreshes.Add(1)
	return nil
}
func (s *channelMonitorV3RepoStub) Read(_ context.Context, _ time.Time, _ time.Duration) (*ChannelMonitorV3Data, error) {
	return s.data, nil
}
func TestChannelMonitorV3WorkerModesAndStop(t *testing.T) {
	for _, rt := range []ChannelMonitorRuntime{{Enabled: false, Mode: ChannelMonitorModeV3}, {Enabled: true, Mode: ChannelMonitorModeV1}, {Enabled: true, Mode: ChannelMonitorModeV2}, {Enabled: true, Mode: ChannelMonitorModeV3}} {
		repo := &channelMonitorV3RepoStub{cfg: DefaultChannelMonitorV3Config()}
		svc := NewChannelMonitorV3Service(repo, channelMonitorV2RuntimeStub{rt: rt})
		svc.Start()
		svc.Start()
		if rt.Enabled && rt.Mode == ChannelMonitorModeV3 {
			require.Eventually(t, func() bool { return repo.refreshes.Load() == 1 }, time.Second, 5*time.Millisecond)
		}
		svc.Stop()
		require.Equal(t, int32(map[bool]int{true: 1, false: 0}[rt.Enabled && rt.Mode == ChannelMonitorModeV3]), repo.refreshes.Load())
	}
}

func TestChannelMonitorV3IdleIncidentCannotLookRecovered(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	key := ChannelMonitorV3Scope("openai", 1, "idle")
	data := &ChannelMonitorV3Data{
		DataThrough: &now,
		Catalog:     []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}},
		KnownModels: []ChannelMonitorV3Fact{{Platform: "openai", GroupID: 1, Model: "idle"}},
		Current:     []ChannelMonitorV3Fact{{Platform: "openai", GroupID: 1, Model: "active", Success: 20, LastRequest: now.Add(-time.Second)}},
		States:      map[string]ChannelMonitorV3State{key: {Incident: &ChannelMonitorV3Incident{Phase: "awaiting_data"}}},
	}
	result := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	require.Equal(t, "unknown", result.Platforms[0].Status)
}

func TestChannelMonitorV3DegradedModelOutranksAnotherModelRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	key := ChannelMonitorV3Scope("openai", 1, "healthy")
	data := &ChannelMonitorV3Data{
		DataThrough: &now,
		Catalog:     []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}},
		Current: []ChannelMonitorV3Fact{
			{Platform: "openai", GroupID: 1, Model: "slow", Success: 5, LastRequest: now.Add(-time.Second), Latency: [16]int64{8: 5}},
			{Platform: "openai", GroupID: 1, Model: "healthy", Success: 100, LastRequest: now.Add(-time.Second), Latency: [16]int64{0: 100}},
		},
		States: map[string]ChannelMonitorV3State{key: {Incident: &ChannelMonitorV3Incident{Phase: "recovering"}}},
	}
	// Map iteration cannot make a recovering model mask an ongoing slow model.
	for i := 0; i < 32; i++ {
		result := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
		require.Equal(t, "degraded", result.Platforms[0].Status)
		require.Equal(t, "degraded", result.Summary.Status)
	}
}

func TestChannelMonitorV3RollingWindowCannotManufactureRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	last := now.Add(-time.Minute)
	key := ChannelMonitorV3Scope("openai", 1, "gpt")
	for _, phase := range []string{"detected", "ongoing", "awaiting_data"} {
		data := &ChannelMonitorV3Data{
			DataThrough: &now,
			Catalog:     []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}},
			Current:     []ChannelMonitorV3Fact{{Platform: "openai", GroupID: 1, Model: "gpt", Success: 5, LastRequest: last}},
			States:      map[string]ChannelMonitorV3State{key: {LastRequest: last, Incident: &ChannelMonitorV3Incident{Phase: phase}}},
		}
		result := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
		require.Equal(t, "unknown", result.Platforms[0].Status, phase)
		require.Equal(t, "unknown", result.Platforms[0].Models[0].Status, phase)
	}
}
