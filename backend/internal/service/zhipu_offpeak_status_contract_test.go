//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func TestZhipuOffPeakMissingStatusPreservesExistingTicket(t *testing.T) {
	const originalID = "original-admission-ticket"
	for _, initialState := range []string{zcode.TicketReady, zcode.TicketQueued} {
		t.Run(initialState, func(t *testing.T) {
			for _, scenario := range []struct {
				name     string
				statuses []zcode.OffPeakTicket
			}{
				{name: "empty"},
				{name: "unmatched", statuses: []zcode.OffPeakTicket{{TicketID: "another-ticket", State: zcode.TicketNotFound}}},
				{name: "unknown-state", statuses: []zcode.OffPeakTicket{{TicketID: originalID, State: "future_unknown_state"}}},
				{name: "conflicting-duplicate", statuses: []zcode.OffPeakTicket{{TicketID: originalID, State: zcode.TicketReady}, {TicketID: originalID, State: zcode.TicketExpired}}},
				{name: "duplicate-ready", statuses: []zcode.OffPeakTicket{{TicketID: originalID, State: zcode.TicketReady}, {TicketID: originalID, State: zcode.TicketReady}}},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					client := &zhipuOffPeakStub{
						takes:    []*zcode.OffPeakTicket{{TicketID: originalID, State: initialState}},
						statuses: [][]zcode.OffPeakTicket{{{TicketID: originalID, State: initialState}}},
					}
					manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(50 * time.Millisecond)
					account := zhipuOffPeakTestAccount()
					initial, err := manager.Acquire(context.Background(), account, zhipuOffPeakTestAuth(), "")
					if initialState == zcode.TicketReady {
						require.NoError(t, err)
						require.NotNil(t, initial)
						initial.Release()
					} else {
						require.Error(t, err, "bounded queue wait keeps a known ticket for a later request")
						require.Nil(t, initial)
					}
					holder := manager.holderFor(account.ID)
					originalTask := holder.taskID
					require.NotEmpty(t, originalTask)
					require.Equal(t, originalID, holder.ticketID)
					require.Equal(t, 1, client.takeCalls)

					client.mu.Lock()
					client.statuses = [][]zcode.OffPeakTicket{scenario.statuses}
					client.takes = []*zcode.OffPeakTicket{{TicketID: "unintended-replacement", State: zcode.TicketReady}}
					beforeStatuses := client.statusCalls
					client.mu.Unlock()
					ticket, err := manager.Acquire(context.Background(), account, zhipuOffPeakTestAuth(), "")
					if ticket != nil {
						defer ticket.Release()
					}
					require.Error(t, err, "only one explicit matching legal Status can change admission or establish a terminal ticket")
					require.Nil(t, ticket, "an incomplete or ambiguous Status cannot grant model admission")
					require.Equal(t, originalID, holder.ticketID, "absence cannot be converted to the platform's explicit not_found state")
					require.Equal(t, originalTask, holder.taskID)
					require.Equal(t, initialState, holder.state, "invalid control data cannot replace the last valid observation")
					client.mu.Lock()
					takes, statuses := client.takeCalls, client.statusCalls
					client.mu.Unlock()
					require.Equal(t, 1, takes, "failed Status must not spend additional Take quota")
					require.Greater(t, statuses, beforeStatuses, "the assertion must exercise the returned invalid Status")
					require.Empty(t, client.settledTickets())
				})
			}
		})
	}
}
