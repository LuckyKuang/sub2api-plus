//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func TestZhipuOffPeakDurablePartialTakeRetainsEvidenceBeforeRestartAdmission(t *testing.T) {
	for _, scenario := range []struct{ name, state string }{
		{"ready-with-error", zcode.TicketReady},
		{"queued-with-error", zcode.TicketQueued},
		{"unknown-with-error", "future_state"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store := &offPeakMemoryStore{}
			client := &offPeakPartialTakeReviewClient{state: scenario.state, err: errors.New("partial Take response must not grant admission")}
			owner := offPeakDurableAccount()
			first := newOffPeakDurableTestManager(t, client, store)
			ticket, err := first.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
			if ticket != nil {
				ticket.Release()
			}
			require.Error(t, err, "even a partial ready result cannot authorize the current acquisition")
			require.Nil(t, ticket)
			require.Zero(t, store.admissions)
			require.Empty(t, store.requests)
			require.Equal(t, "known-memory-partial-ticket", store.record.TicketID)
			require.Equal(t, "uncertain", store.record.State)
			require.False(t, store.record.Retired)
			require.False(t, store.record.Closed)
			require.NotEmpty(t, store.record.Snapshot)
			require.Empty(t, store.record.LeaseID)
			require.Equal(t, 1, store.beginCount)
			original := store.record
			original.Snapshot = append([]byte(nil), store.record.Snapshot...)
			client.mu.Lock()
			takes, statuses := client.takes, client.statuses
			tasks := append([]string(nil), client.tasks...)
			client.mu.Unlock()
			require.Equal(t, 1, takes)
			require.Zero(t, statuses, "the partial Take must end this acquisition before Status or admission")
			require.Equal(t, []string{original.TaskID}, tasks)
			first.Stop()

			restarted := newOffPeakDurableTestManager(t, client, store)
			recovered, err := restarted.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
			require.NoError(t, err)
			require.NotNil(t, recovered)
			require.Equal(t, "known-memory-partial-ticket", recovered.TicketID)
			require.Equal(t, zcode.TicketReady, recovered.State)
			require.Equal(t, 1, store.admissions, "a successful original-ticket Status is required before obtaining a physical request lease")
			require.Len(t, store.requests, 1)
			require.Equal(t, original.ID, store.record.ID)
			require.Equal(t, original.Generation, store.record.Generation)
			require.Equal(t, original.TaskID, store.record.TaskID)
			require.Equal(t, original.Snapshot, store.record.Snapshot)
			recovered.Release()
			recovered.Release()
			require.Empty(t, store.requests)
			require.Equal(t, 1, store.releases)
			client.mu.Lock()
			defer client.mu.Unlock()
			require.Equal(t, 1, client.takes, "restart must not spend another Take for known partial evidence")
			require.Equal(t, 1, client.statuses)
			require.Equal(t, [][]string{{"known-memory-partial-ticket"}}, client.statusIDs)
			require.Equal(t, []string{original.TaskID}, client.tasks)
		})
	}
}
