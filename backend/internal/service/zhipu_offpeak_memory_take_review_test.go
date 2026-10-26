//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type offPeakPartialTakeReviewClient struct {
	ZhipuOffPeakClient
	mu              sync.Mutex
	state           string
	err             error
	statusErr       error
	nilTicket, noID bool
	takes, statuses int
	tasks           []string
	statusIDs       [][]string
}

func (c *offPeakPartialTakeReviewClient) TakeTicket(_ context.Context, task string, _ zcode.OffPeakAuth, _ string) (*zcode.OffPeakTicket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.takes++
	c.tasks = append(c.tasks, task)
	if c.nilTicket {
		return nil, c.err
	}
	if c.noID {
		return &zcode.OffPeakTicket{State: c.state}, c.err
	}
	return &zcode.OffPeakTicket{TicketID: "known-memory-partial-ticket", State: c.state}, c.err
}

func TestZhipuOffPeakMemoryTakeWithoutTicketEvidenceCannotAdmit(t *testing.T) {
	for _, scenario := range []string{"nil-ticket", "missing-ticket-id"} {
		t.Run(scenario, func(t *testing.T) {
			client := &offPeakPartialTakeReviewClient{state: zcode.TicketReady, nilTicket: scenario == "nil-ticket", noID: scenario == "missing-ticket-id"}
			manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(time.Second)
			owner := offPeakDurableAccount()
			ticket, err := manager.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
			if ticket != nil {
				ticket.Release()
			}
			require.Error(t, err)
			require.Nil(t, ticket)
			holder := manager.holderFor(owner.ID)
			require.Empty(t, holder.ticketID)
			holder.leaseMu.Lock()
			active := holder.active
			holder.leaseMu.Unlock()
			require.Zero(t, active)
			client.mu.Lock()
			defer client.mu.Unlock()
			require.Equal(t, 1, client.takes)
			require.Zero(t, client.statuses, "Status requires known remote ticket evidence")
		})
	}
}

func (c *offPeakPartialTakeReviewClient) TicketStatus(_ context.Context, ids []string, _ zcode.OffPeakAuth, _ string) ([]zcode.OffPeakTicket, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statuses++
	c.statusIDs = append(c.statusIDs, append([]string(nil), ids...))
	if c.statusErr != nil {
		return nil, 0, c.statusErr
	}
	return []zcode.OffPeakTicket{{TicketID: "known-memory-partial-ticket", State: zcode.TicketReady}}, 0, nil
}

func TestZhipuOffPeakMemoryUncertainStatusFailureKeepsBoundedPollWait(t *testing.T) {
	client := &offPeakPartialTakeReviewClient{err: errors.New("invalid off-peak ticket response"), statusErr: errors.New("temporary status failure")}
	manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(50 * time.Millisecond)
	owner := offPeakDurableAccount()
	ticket, err := manager.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, ticket)
	holder := manager.holderFor(owner.ID)
	originalTask := holder.taskID
	require.Equal(t, "known-memory-partial-ticket", holder.ticketID)
	require.Equal(t, "uncertain", holder.state)

	started := time.Now()
	ticket, err = manager.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Less(t, time.Since(started), time.Second, "the request deadline must interrupt the poll floor")
	require.Equal(t, "known-memory-partial-ticket", holder.ticketID)
	require.Equal(t, "uncertain", holder.state)
	require.Equal(t, originalTask, holder.taskID)
	client.mu.Lock()
	defer client.mu.Unlock()
	require.Equal(t, 1, client.takes)
	require.Equal(t, 1, client.statuses, "a failing Status must wait at the poll floor instead of immediately retrying an uncertain ticket")
	require.Equal(t, [][]string{{"known-memory-partial-ticket"}}, client.statusIDs)
	holder.leaseMu.Lock()
	defer holder.leaseMu.Unlock()
	require.Zero(t, holder.active)
}

func TestZhipuOffPeakMemoryPartialTakeRetainsKnownIDWithoutAdmissionOrRetake(t *testing.T) {
	for _, scenario := range []struct {
		name, state string
		err         error
	}{
		{name: "unknown-state", state: "future_state"},
		{name: "missing-or-empty-state", err: errors.New("invalid off-peak ticket response")},
		{name: "partial-error-with-ready-state", state: zcode.TicketReady, err: errors.New("invalid off-peak ticket response")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := &offPeakPartialTakeReviewClient{state: scenario.state, err: scenario.err}
			manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(time.Second)
			owner := offPeakDurableAccount()
			initial, err := manager.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
			if initial != nil {
				initial.Release()
			}
			require.Error(t, err, "a partial or unknown-state Take cannot grant model admission")
			require.Nil(t, initial)
			holder := manager.holderFor(owner.ID)
			require.Equal(t, "known-memory-partial-ticket", holder.ticketID, "known remote evidence must survive a failed admission")
			require.Equal(t, "uncertain", holder.state)
			holder.leaseMu.Lock()
			active := holder.active
			holder.leaseMu.Unlock()
			require.Zero(t, active)
			require.NotEmpty(t, holder.taskID)
			originalTask := holder.taskID
			client.mu.Lock()
			takes, statuses := client.takes, client.statuses
			client.mu.Unlock()
			require.Equal(t, 1, takes)
			require.Zero(t, statuses, "current acquisition must fail; later requests revalidate the partial ticket")

			recovered, err := manager.Acquire(context.Background(), owner, zhipuOffPeakTestAuth(), "")
			require.NoError(t, err)
			require.NotNil(t, recovered)
			require.Equal(t, "known-memory-partial-ticket", recovered.TicketID)
			recovered.Release()
			require.Equal(t, originalTask, holder.taskID)
			client.mu.Lock()
			defer client.mu.Unlock()
			require.Equal(t, 1, client.takes, "a later request must not issue another Take for known partial evidence")
			require.Equal(t, 1, client.statuses)
			require.Equal(t, [][]string{{"known-memory-partial-ticket"}}, client.statusIDs)
			require.Equal(t, []string{originalTask}, client.tasks)
		})
	}
}
