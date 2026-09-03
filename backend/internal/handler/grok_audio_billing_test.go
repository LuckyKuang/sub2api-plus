//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestIsExpectedGrokRealtimeClose(t *testing.T) {
	for _, status := range []coderws.StatusCode{
		coderws.StatusNormalClosure,
		coderws.StatusGoingAway,
		coderws.StatusNoStatusRcvd,
		coderws.StatusAbnormalClosure,
	} {
		if !isExpectedGrokRealtimeClose(coderws.CloseError{Code: status}) {
			t.Fatalf("status %v should be treated as an expected session close", status)
		}
	}
	if isExpectedGrokRealtimeClose(coderws.CloseError{Code: coderws.StatusPolicyViolation}) {
		t.Fatal("policy violations must not be treated as billable normal closes")
	}
}

func TestGrokRealtimeTurnTrackerMatchesByResponseIDAndResetsOnCompletion(t *testing.T) {
	repo := &clientDisconnectRiskHandlerRepoStub{}
	risk := service.NewClientDisconnectRiskService(repo, nil, nil)
	tracker := newGrokRealtimeTurnTracker(risk, 7, 11, service.RoleUser, "server-request")
	observer := tracker.observer(context.Background())

	observer.Accepted("resp-a")
	observer.Accepted("resp-b")
	observer.Completed("resp-b")
	tracker.disconnectOutstanding(context.Background())
	observer.Accepted("resp-c")
	tracker.disconnectOutstanding(context.Background())

	require.Len(t, repo.begins, 3)
	require.Len(t, repo.finalizes, 3)
	require.Equal(t, "server-request:grok-turn:1", repo.begins[0].RequestID)
	require.Equal(t, "server-request:grok-turn:2", repo.begins[1].RequestID)
	require.Equal(t, "server-request:grok-turn:3", repo.begins[2].RequestID)
	require.Equal(t, service.ClientDisconnectOutcomeCompleted, repo.finalizes[0].Outcome)
	require.Equal(t, service.ClientDisconnectOutcomeDisconnected, repo.finalizes[1].Outcome)
	require.Equal(t, service.ClientDisconnectOutcomeDisconnected, repo.finalizes[2].Outcome)
}

func TestGrokRealtimeTurnTrackerSkipsWhenRoleMissing(t *testing.T) {
	repo := &clientDisconnectRiskHandlerRepoStub{}
	risk := service.NewClientDisconnectRiskService(repo, nil, nil)
	tracker := newGrokRealtimeTurnTracker(risk, 7, 11, "", "server-request")
	observer := tracker.observer(context.Background())

	observer.Accepted("resp-a")
	tracker.disconnectOutstanding(context.Background())

	require.Empty(t, repo.begins)
	require.Empty(t, repo.finalizes)
}

func TestGrokRealtimeTurnTrackerDeduplicatesPendingResponseAndAuditsAdmin(t *testing.T) {
	repo := &clientDisconnectRiskHandlerRepoStub{}
	risk := service.NewClientDisconnectRiskService(repo, nil, nil)
	tracker := newGrokRealtimeTurnTracker(risk, 7, 11, service.RoleAdmin, "server-request")
	observer := tracker.observer(context.Background())

	observer.Accepted("resp-a")
	observer.Accepted("resp-a")
	tracker.disconnectOutstanding(context.Background())

	require.Len(t, repo.begins, 1)
	require.Len(t, repo.finalizes, 1)
	require.Equal(t, service.ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.False(t, repo.finalizes[0].Enforce)
}

func TestGrokRealtimeBillingResultRequiresObservedAudio(t *testing.T) {
	if grokRealtimeBillingResult("grok-voice-latest", time.Second, false) != nil {
		t.Fatal("a session without observed audio must not be billed")
	}
	if grokRealtimeBillingResult("grok-voice-latest", 0, true) != nil {
		t.Fatal("zero-duration sessions must not be billed")
	}
}

func TestGrokRealtimeBillingResultUsesForcedUniqueID(t *testing.T) {
	first := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	second := grokRealtimeBillingResult("grok-voice-latest", 90*time.Second, true)
	if first == nil || second == nil {
		t.Fatal("observed audio sessions should be billable")
	}
	if first.RequestID == "" {
		t.Fatalf("unexpected billing request ID %q", first.RequestID)
	}
	if first.RequestID == second.RequestID {
		t.Fatal("independent realtime connections must not share a billing request ID")
	}
	if first.AudioUsage == nil || first.AudioUsage.Mode != "realtime" || first.AudioUsage.DurationOrUnits != 1.5 {
		t.Fatalf("unexpected audio usage: %#v", first.AudioUsage)
	}
}
