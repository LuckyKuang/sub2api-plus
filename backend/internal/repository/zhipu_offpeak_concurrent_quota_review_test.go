//go:build integration

package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Observe a real PostgreSQL refusal without changing the record or replacing
// its authority. The waiting manager must already share the in-flight Take's
// segment before that Take is allowed to publish the quota rejection.
type offPeakConcurrentQuotaClaimObserver struct {
	service.ZhipuOffPeakStore
	once sync.Once
	busy chan string
}

func (s *offPeakConcurrentQuotaClaimObserver) Claim(ctx context.Context, id string, settlement bool, idle, ttl time.Duration) (service.ZhipuOffPeakRecord, error) {
	r, err := s.ZhipuOffPeakStore.Claim(ctx, id, settlement, idle, ttl)
	if !settlement && errors.Is(err, service.ErrOffPeakLeaseBusy) {
		s.once.Do(func() { s.busy <- id })
	}
	return r, err
}

func TestZhipuOffPeakManagerPostgresConcurrentQuotaRejectionPreservesResetTime(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	takeStarted := make(chan struct{})
	finishTake := make(chan struct{})
	var finishOnce sync.Once
	releaseTake := func() { finishOnce.Do(func() { close(finishTake) }) }
	t.Cleanup(releaseTake)
	nextTake := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	firstClient := &offPeakManagerScriptClient{take: func(string) (*zcode.OffPeakTicket, error) {
		close(takeStarted)
		<-finishTake
		return nil, &zcode.OffPeakError{HTTPStatus: http.StatusTooManyRequests, Code: zcode.OffPeakCodeQuotaExhausted, NextTakeAt: nextTake}
	}}
	first := newOffPeakManagerIntegrationManager(t, firstClient, 3*time.Second)
	secondClient := &offPeakManagerScriptClient{}
	second := newOffPeakManagerIntegrationManager(t, secondClient, 2*time.Second)
	observer := &offPeakConcurrentQuotaClaimObserver{ZhipuOffPeakStore: NewZhipuOffPeakRepository(integrationDB), busy: make(chan string, 1)}
	cfg := config.ZhipuOffPeakConfig{PersistenceEnabled: true, ActiveKeyID: "integration-key-v1", SnapshotKeys: `{"integration-key-v1":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("i", 32))) + `"}`, SnapshotRetentionHours: 24}
	require.NoError(t, second.WithPersistence(observer, cfg))
	type acquisition struct {
		ticket *service.ZhipuOffPeakTicket
		err    error
	}
	acquire := func(manager *service.ZhipuOffPeakTicketManager) <-chan acquisition {
		result := make(chan acquisition, 1)
		go func() {
			ticket, err := manager.TicketForRequest(context.Background(), owner)
			result <- acquisition{ticket, err}
		}()
		return result
	}
	firstResult := acquire(first)
	select {
	case <-takeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first manager did not persist and send Take")
	}
	original := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, "uncertain", original.State)
	secondResult := acquire(second)
	select {
	case id := <-observer.busy:
		require.Equal(t, original.ID, id, "both requests must already share the same in-flight Take")
	case <-time.After(time.Second):
		t.Fatal("second manager did not wait for the same segment's operation lease")
	}
	releaseTake()
	for _, result := range []<-chan acquisition{firstResult, secondResult} {
		select {
		case outcome := <-result:
			if outcome.ticket != nil {
				outcome.ticket.Release()
			}
			require.Nil(t, outcome.ticket)
			require.Equal(t, http.StatusTooManyRequests, infraerrors.Code(outcome.err))
			require.Equal(t, "ZHIPU_OFFPEAK_QUOTA_EXHAUSTED", infraerrors.Reason(outcome.err), "a concurrent waiter must return the committed quota rejection, not a fabricated queued position")
			require.Contains(t, infraerrors.Message(outcome.err), "2030-01-02T03:04:05Z")
		case <-time.After(4 * time.Second):
			t.Fatal("quota rejection did not complete acquisition")
		}
	}
	retained := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, original.ID, retained.ID)
	require.Equal(t, original.TaskID, retained.TaskID)
	require.Equal(t, "new", retained.State)
	require.Equal(t, "quota_exhausted", retained.LastError)
	require.WithinDuration(t, nextTake, retained.NextAttempt, time.Millisecond)
	require.Empty(t, retained.TicketID)
	require.Empty(t, retained.LeaseID)
	require.False(t, retained.Closed)
	firstCalls := firstClient.snapshot()
	require.Len(t, firstCalls, 1)
	require.Equal(t, "take", firstCalls[0].Operation)
	require.Empty(t, secondClient.snapshot(), "the waiter must not spend another Take allowance")
	assertOffPeakManagerCapturedOwner(t, firstCalls, route)
	var leases int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", original.ID).Scan(&leases))
	require.Zero(t, leases, "quota exhaustion grants no physical model admission")
}
