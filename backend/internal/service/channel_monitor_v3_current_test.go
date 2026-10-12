//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type v3CurrentRepoStub struct {
	channelMonitorV3RepoStub
	reads int
}

func (r *v3CurrentRepoStub) Read(_ context.Context, now time.Time, _ time.Duration, _ string) (*ChannelMonitorV3Data, error) {
	r.reads++
	return &ChannelMonitorV3Data{Config: r.cfg, CurrentThrough: &now}, nil
}

// docs/CHANNEL_MONITOR.md: cache is shared for at most ten seconds, config
// updates invalidate it, and snapshot reads must not advance incident state.
func TestChannelMonitorV3CurrentSnapshotCacheDoesNotAdvanceIncidents(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 17, 0, time.UTC)
	repo := &v3CurrentRepoStub{channelMonitorV3RepoStub: channelMonitorV3RepoStub{cfg: DefaultChannelMonitorV3Config()}}
	svc := NewChannelMonitorV3Service(repo, nil)
	svc.now = func() time.Time { return now }
	ctx := context.Background()
	first, err := svc.Snapshot(ctx, 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, now, first.CurrentWindowEnd)
	require.Equal(t, now.Add(-5*time.Minute), first.CurrentWindowStart)
	now = now.Add(9 * time.Second)
	cached, err := svc.Snapshot(ctx, 24*time.Hour, "")
	require.NoError(t, err)
	require.Same(t, first, cached)
	require.Equal(t, 1, repo.reads)
	now = now.Add(time.Second)
	fresh, err := svc.Snapshot(ctx, 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, now, fresh.CurrentWindowEnd)
	require.Equal(t, 2, repo.reads)
	_, err = svc.UpdateConfig(ctx, repo.cfg)
	require.NoError(t, err)
	_, err = svc.Snapshot(ctx, 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, 3, repo.reads)
	require.Zero(t, repo.refreshes.Load())
}

func TestChannelMonitorV3CurrentConfidenceUsesIndependentObservationWatermark(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 17, 0, time.UTC)
	last := now.Add(-time.Second)
	data := &ChannelMonitorV3Data{CurrentThrough: &now, Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}}, Current: []ChannelMonitorV3Fact{{Platform: "openai", GroupID: 1, Model: "model", Success: 4, LastRequest: last}}}
	snapshot := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	model := snapshot.Platforms[0].Models[0]
	require.Equal(t, "insufficient", model.Status)
	require.Equal(t, "insufficient", model.ObservationReason)
	require.Equal(t, last, *model.LastObserved)
	data.Current[0].Success = 5
	snapshot = BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	require.Equal(t, "normal", snapshot.Platforms[0].Models[0].Status, "fresh current observations do not wait for a history watermark")
	stale := now.Add(-31 * time.Second)
	data.CurrentThrough = &stale
	snapshot = BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	require.Equal(t, "unknown", snapshot.Platforms[0].Models[0].Status)
	require.Equal(t, "stale", snapshot.Platforms[0].Models[0].ObservationReason)
}

// docs/CHANNEL_MONITOR.md: persisted model identity must not suppress the
// latest probe or contribute historical samples to current request health.
func TestChannelMonitorV3CurrentNeverBorrowsKnownModelCountersOrStatus(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 17, 0, time.UTC)
	last := now.Add(-time.Second)
	data := &ChannelMonitorV3Data{
		CurrentThrough: &now,
		Catalog:        []ChannelMonitorV3Catalog{{Platform: "openai", GroupID: 1}, {Platform: "openai", SourceKind: "probe", MonitorID: 2}},
		KnownModels: []ChannelMonitorV3Fact{
			{Platform: "openai", GroupID: 1, Model: "model", Success: 1000, LastRequest: last},
			{Platform: "openai", SourceKind: "probe", MonitorID: 2, Model: "model", Success: 1000, ProbeStatus: "operational", LastRequest: last},
		},
		Current: []ChannelMonitorV3Fact{
			{Platform: "openai", GroupID: 1, Model: "model", Success: 4, LastRequest: last},
			{Platform: "openai", SourceKind: "probe", MonitorID: 2, Model: "model", Failures: 1, ProbeStatus: "failed", LastRequest: last},
		},
	}
	snapshot := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	require.Len(t, snapshot.Platforms[0].Models, 2)
	for _, model := range snapshot.Platforms[0].Models {
		if model.SourceKind == "probe" {
			require.Equal(t, "outage", model.Status)
		} else {
			require.Equal(t, "insufficient", model.Status)
		}
	}
	data.Current = nil
	snapshot = BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
	for _, model := range snapshot.Platforms[0].Models {
		require.Equal(t, "unknown", model.Status)
		require.Equal(t, "no_observations", model.ObservationReason)
		require.Equal(t, last, *model.LastObserved)
	}
}

// docs/CHANNEL_MONITOR.md: a configured probe without current samples still
// shows its actual last observation, without using old status or counters.
func TestChannelMonitorV3CurrentProbePlaceholderPreservesLastObservation(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 17, 0, time.UTC)
	last := now.Add(-6 * time.Minute)
	for _, reverse := range []bool{false, true} {
		known := []ChannelMonitorV3Fact{
			{Platform: "openai", SourceKind: "probe", MonitorID: 2, Model: "model"},
			{Platform: "openai", SourceKind: "probe", MonitorID: 2, Model: "model", LastRequest: last, Success: 100, ProbeStatus: "operational"},
		}
		if reverse {
			known[0], known[1] = known[1], known[0]
		}
		data := &ChannelMonitorV3Data{CurrentThrough: &now,
			Catalog: []ChannelMonitorV3Catalog{{Platform: "openai", SourceKind: "probe", MonitorID: 2}}, KnownModels: known}
		snapshot := BuildChannelMonitorV3Snapshot(data, DefaultChannelMonitorV3Config(), now, 24*time.Hour, "")
		model := snapshot.Platforms[0].Models[0]
		require.Equal(t, "unknown", model.Status)
		require.Equal(t, "no_observations", model.ObservationReason)
		require.NotNil(t, model.LastObserved)
		require.Equal(t, last, *model.LastObserved)
	}
}
