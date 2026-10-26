//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type liveUsageReaderStub struct {
	rows []UsageLog
	keys []LiveUsageKey
}

func (r *liveUsageReaderStub) ReadLiveUsage(_ context.Context, keys []LiveUsageKey) ([]UsageLog, error) {
	r.keys = keys
	return r.rows, nil
}

// Approved request-list contract: canonical billing keys only; no guessed fees,
// cross-owner data, duplicate turn rows, or inference of success from HTTP 200.
func TestLiveUsageJoinsCanonicalOwnerKeyAndPreservesFailure(t *testing.T) {
	s := liveTestService(t)
	now := time.Now().UTC()
	r := LiveRequest{ID: "http", UserID: 7, APIKeyID: 9, Version: 1, Status: "finished", StartedAt: now, UpdatedAt: now, FinishedAt: &now}
	require.NoError(t, s.store(context.Background(), r))
	ctx := WithLiveUsageBinding(context.Background(), s, r.ID, 7, 9)
	ctx = CopyLiveUsageBinding(ctx, context.Background()) // asynchronous billing worker
	u := UsageLog{ID: 123, RequestID: "client:canonical", UserID: 7, APIKeyID: 9, ActualCost: 0.000000001, InputTokens: 120, OutputTokens: 40, CacheReadTokens: 80, TimingVersion: 1, NativeCompactionV2: true, CompletionStatus: UsageCompletionCompleted}
	reader := &liveUsageReaderStub{rows: []UsageLog{u, {RequestID: "client:canonical", UserID: 8, APIKeyID: 10, ActualCost: 999}}}
	s.SetUsageReader(reader)
	bindLiveUsage(ctx, &u)
	require.Eventually(t, func() bool { return liveTestBinding(t, s, "http") != "" }, time.Second, time.Millisecond)
	data, err := s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, data.Items, 1)
	require.Equal(t, "completed", data.Items[0].Status)
	require.Equal(t, 0.000000001, *data.Items[0].ActualCost)
	require.Equal(t, 40, *data.Items[0].OutputTokens)
	require.True(t, data.Items[0].NativeCompactionV2)
	require.Equal(t, []LiveUsageKey{{"client:canonical", 7, 9}}, reader.keys)
	r.Version++
	r.Status = "failed"
	require.NoError(t, s.store(context.Background(), r))
	data, err = s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "failed", data.Items[0].Status, "stream error must survive a persisted complete usage row")
}

func TestLiveUsageMissingAndMultipleMoneyEventsStayUnknown(t *testing.T) {
	s := liveTestService(t)
	now := time.Now().UTC()
	r := LiveRequest{ID: "turn", UserID: 7, APIKeyID: 9, Version: 1, Status: "finished", StartedAt: now, UpdatedAt: now}
	require.NoError(t, s.store(context.Background(), r))
	s.SetUsageReader(&liveUsageReaderStub{})
	ctx := WithLiveUsageBinding(context.Background(), s, "turn", 7, 9)
	u := UsageLog{RequestID: "first", UserID: 7, APIKeyID: 9}
	bindLiveUsage(ctx, &u)
	require.Eventually(t, func() bool { return liveTestBinding(t, s, "turn") != "" }, time.Second, time.Millisecond)
	data, err := s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Nil(t, data.Items[0].ActualCost, "missing settlement is not zero/free")
	require.Equal(t, "finished", data.Items[0].Status, "handler return is not success")
	u.RequestID = "second"
	bindLiveUsage(ctx, &u)
	require.Eventually(t, func() bool { return liveTestBinding(t, s, "turn") == "ambiguous" }, time.Second, time.Millisecond)
	data, err = s.List(context.Background(), 7)
	require.NoError(t, err)
	require.Nil(t, data.Items[0].ActualCost, "session-level events must not be assigned to every turn")
	wrong := UsageLog{RequestID: "wrong", UserID: 8, APIKeyID: 9}
	bindLiveUsage(ctx, &wrong)
	require.Equal(t, "ambiguous", liveTestBinding(t, s, "turn"))
}
