//go:build integration

package repository

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type offPeakTerminationClaimBarrier struct {
	service.ZhipuOffPeakStore
	once    sync.Once
	reached chan struct{}
	resume  chan struct{}
}

func (s *offPeakTerminationClaimBarrier) Claim(ctx context.Context, id string, settlement bool, idle, ttl time.Duration) (service.ZhipuOffPeakRecord, error) {
	r, err := s.ZhipuOffPeakStore.Claim(ctx, id, settlement, idle, ttl)
	if !settlement {
		s.once.Do(func() {
			close(s.reached)
			select {
			case <-s.resume:
			case <-ctx.Done():
			}
		})
	}
	return r, err
}

func TestZhipuOffPeakManagerPostgresTerminateCannotRetakeFromOldAcquire(t *testing.T) {
	for _, scenario := range []string{"waiting-operation-lease", "already-claimed-operation"} {
		t.Run(scenario, func(t *testing.T) {
			owner, route := newOffPeakManagerIntegrationOwner(t)
			seedClient := &offPeakManagerScriptClient{
				take: func(task string) (*zcode.OffPeakTicket, error) {
					return &zcode.OffPeakTicket{TicketID: "original-ticket", TaskID: task, State: zcode.TicketReady}, nil
				},
				status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
					return []zcode.OffPeakTicket{{TicketID: ids[0], State: zcode.TicketReady}}, 0, nil
				},
			}
			seed := newOffPeakManagerIntegrationManager(t, seedClient, 3*time.Second)
			ticket, err := seed.TicketForRequest(context.Background(), owner)
			require.NoError(t, err)
			ticket.Release()
			original := currentOffPeakManagerRecord(t, owner.ID)
			seed.Stop()
			store := NewZhipuOffPeakRepository(integrationDB)
			if scenario == "waiting-operation-lease" {
				_, err = store.Claim(context.Background(), original.ID, false, 0, time.Minute)
				require.NoError(t, err)
			}
			barrier := &offPeakTerminationClaimBarrier{ZhipuOffPeakStore: store, reached: make(chan struct{}), resume: make(chan struct{})}
			var resumeOnce sync.Once
			resume := func() { resumeOnce.Do(func() { close(barrier.resume) }) }
			t.Cleanup(resume)
			oldClient := &offPeakManagerScriptClient{status: seedClient.status}
			old := newOffPeakManagerIntegrationManager(t, oldClient, 3*time.Second)
			cfg := config.ZhipuOffPeakConfig{PersistenceEnabled: true, ActiveKeyID: "integration-key-v1", SnapshotKeys: `{"integration-key-v1":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("i", 32))) + `"}`, SnapshotRetentionHours: 24}
			require.NoError(t, old.WithPersistence(barrier, cfg))
			type acquisition struct {
				ticket *service.ZhipuOffPeakTicket
				err    error
			}
			finished := make(chan acquisition, 1)
			go func() {
				ticket, err := old.TicketForRequest(context.Background(), owner)
				finished <- acquisition{ticket, err}
			}()
			select {
			case <-barrier.reached:
			case <-time.After(2 * time.Second):
				t.Fatal("old acquire did not reach operation boundary")
			}
			segment, err := old.CurrentSegment(context.Background(), owner)
			require.NoError(t, err)
			require.Equal(t, original.ID, segment.RecordID)
			require.NoError(t, old.TerminateSegment(context.Background(), owner, segment.RecordID, segment.Generation))
			retired := loadOffPeakStoreRecord(t, original.ID)
			require.True(t, retired.Retired)
			require.True(t, retired.SettlementPending)
			// A separate, later business request is allowed a new segment. The
			// suspended old request must never join it or create another task.
			futureClient := &offPeakManagerScriptClient{
				take: func(task string) (*zcode.OffPeakTicket, error) {
					return &zcode.OffPeakTicket{TicketID: "replacement-ticket", TaskID: task, State: zcode.TicketReady}, nil
				},
				status: seedClient.status,
			}
			future := newOffPeakManagerIntegrationManager(t, futureClient, 3*time.Second)
			replacement, err := future.TicketForRequest(context.Background(), owner)
			require.NoError(t, err)
			require.Equal(t, "replacement-ticket", replacement.TicketID)
			replacement.Release()
			current := currentOffPeakManagerRecord(t, owner.ID)
			require.NotEqual(t, original.ID, current.ID)
			require.NotEqual(t, original.TaskID, current.TaskID)
			resume()
			select {
			case result := <-finished:
				if result.ticket != nil {
					result.ticket.Release()
				}
				require.Error(t, result.err)
				require.Nil(t, result.ticket)
			case <-time.After(4 * time.Second):
				t.Fatal("terminated acquire did not return")
			}
			calls := oldClient.snapshot()
			if scenario == "waiting-operation-lease" {
				require.Empty(t, calls)
			} else {
				require.Len(t, calls, 1)
				require.Equal(t, "status", calls[0].Operation)
				require.Equal(t, []string{"original-ticket"}, calls[0].TicketIDs)
			}
			var leases int
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", original.ID).Scan(&leases))
			require.Zero(t, leases)
			require.Equal(t, current.ID, currentOffPeakManagerRecord(t, owner.ID).ID)
			require.NoError(t, old.TerminateSegment(context.Background(), owner, original.ID, original.Generation))
			require.Equal(t, current.ID, currentOffPeakManagerRecord(t, owner.ID).ID)
			assertOffPeakManagerCapturedOwner(t, append(calls, futureClient.snapshot()...), route)
		})
	}
}
