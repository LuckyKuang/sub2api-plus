//go:build integration

package repository

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func seedOffPeakManagerRecoveryTicket(t *testing.T, owner *service.Account, ticketID string) service.ZhipuOffPeakRecord {
	t.Helper()
	client := &offPeakManagerScriptClient{
		take: func(task string) (*zcode.OffPeakTicket, error) {
			return &zcode.OffPeakTicket{TicketID: ticketID, TaskID: task, State: zcode.TicketReady}, nil
		},
		status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
			return []zcode.OffPeakTicket{{TicketID: ticketID, State: zcode.TicketReady}}, 0, nil
		},
	}
	manager := newOffPeakManagerIntegrationManager(t, client, 3*time.Second)
	ticket, err := manager.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ticketID, ticket.TicketID)
	ticket.Release()
	manager.Stop()
	return currentOffPeakManagerRecord(t, owner.ID)
}

func assertOffPeakManagerRecoveryRetainsSameRecord(t *testing.T, original service.ZhipuOffPeakRecord) service.ZhipuOffPeakRecord {
	t.Helper()
	retained := currentOffPeakManagerRecord(t, original.AccountID)
	require.Equal(t, original.ID, retained.ID)
	require.Equal(t, original.Generation, retained.Generation)
	require.Equal(t, original.TaskID, retained.TaskID)
	require.Equal(t, original.TicketID, retained.TicketID)
	require.Equal(t, original.Snapshot, retained.Snapshot)
	require.Equal(t, original.State, retained.State)
	require.False(t, retained.Retired)
	require.False(t, retained.Closed)
	var leases int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", original.ID).Scan(&leases))
	require.Zero(t, leases)
	return retained
}

func TestZhipuOffPeakManagerPostgresKnownStatusFailureRecoversSameTicketAfterRestart(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	original := seedOffPeakManagerRecoveryTicket(t, owner, "known-status-recovery-ticket")
	failedClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		require.Equal(t, []string{original.TicketID}, ids)
		return nil, 0, &net.OpError{Op: "read", Net: "tcp", Err: io.ErrUnexpectedEOF}
	}}
	failed := newOffPeakManagerIntegrationManager(t, failedClient, 3*time.Second)
	ticket, err := failed.TicketForRequest(context.Background(), owner)
	require.Error(t, err)
	require.Nil(t, ticket)
	retained := assertOffPeakManagerRecoveryRetainsSameRecord(t, original)
	require.Empty(t, retained.LeaseID, "a failed known-ticket Status preserves the ticket while releasing its control lease")
	failed.Stop()
	recoveryClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		require.Equal(t, []string{original.TicketID}, ids)
		return []zcode.OffPeakTicket{{TicketID: original.TicketID, State: zcode.TicketReady}}, 0, nil
	}}
	restarted := newOffPeakManagerIntegrationManager(t, recoveryClient, 3*time.Second)
	recovered, err := restarted.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, original.TicketID, recovered.TicketID)
	recovered.Release()
	assertOffPeakManagerRecoveryRetainsSameRecord(t, original)
	for _, client := range []*offPeakManagerScriptClient{failedClient, recoveryClient} {
		calls := client.snapshot()
		require.Len(t, calls, 1, "known-ticket failure/restart must perform Status without Take")
		require.Equal(t, "status", calls[0].Operation)
		require.Equal(t, []string{original.TicketID}, calls[0].TicketIDs)
		assertOffPeakManagerCapturedOwner(t, calls, route)
	}
}

func TestZhipuOffPeakManagerPostgresAdmissionFailureRecoversSameTicketAfterRestart(t *testing.T) {
	for _, scenario := range []string{"request-lease-insert-failure", "operation-lease-expired"} {
		t.Run(scenario, func(t *testing.T) {
			owner, route := newOffPeakManagerIntegrationOwner(t)
			original := seedOffPeakManagerRecoveryTicket(t, owner, "known-admission-recovery-ticket")
			ctx := context.Background()
			triggerName := "offpeak_admit_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if scenario == "request-lease-insert-failure" {
				_, err := integrationDB.ExecContext(ctx, `CREATE FUNCTION `+triggerName+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected offpeak request lease write failure'; END $$`)
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON zhipu_offpeak_request_leases`)
					_, _ = integrationDB.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS `+triggerName+`()`)
				})
				_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER `+triggerName+` BEFORE INSERT ON zhipu_offpeak_request_leases FOR EACH ROW WHEN (NEW.ticket_record_id='`+original.ID+`') EXECUTE FUNCTION `+triggerName+`()`)
				require.NoError(t, err)
			}
			failedClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
				require.Equal(t, []string{original.TicketID}, ids)
				if scenario == "operation-lease-expired" {
					_, err := integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", original.ID)
					require.NoError(t, err)
				}
				return []zcode.OffPeakTicket{{TicketID: original.TicketID, State: zcode.TicketReady}}, 0, nil
			}}
			failed := newOffPeakManagerIntegrationManager(t, failedClient, 3*time.Second)
			ticket, err := failed.TicketForRequest(ctx, owner)
			require.Error(t, err)
			require.Nil(t, ticket)
			retained := assertOffPeakManagerRecoveryRetainsSameRecord(t, original)
			require.NotEmpty(t, retained.LeaseID, "failed admission cannot invent a committed request lease or clear its fencing owner")
			var leaseExpired bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT lease_expires_at<=clock_timestamp() FROM zhipu_offpeak_tickets WHERE id=$1", original.ID).Scan(&leaseExpired))
			if scenario == "operation-lease-expired" {
				require.True(t, leaseExpired)
			} else {
				require.False(t, leaseExpired)
				_, err = integrationDB.ExecContext(ctx, `DROP TRIGGER `+triggerName+` ON zhipu_offpeak_request_leases`)
				require.NoError(t, err)
			}
			failed.Stop()
			// A process crash cannot release an operation lease. Expire that
			// original lease before a different manager resumes durable Status.
			_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", original.ID)
			require.NoError(t, err)
			recoveryClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
				require.Equal(t, []string{original.TicketID}, ids)
				return []zcode.OffPeakTicket{{TicketID: original.TicketID, State: zcode.TicketReady}}, 0, nil
			}}
			restarted := newOffPeakManagerIntegrationManager(t, recoveryClient, 3*time.Second)
			recovered, err := restarted.TicketForRequest(ctx, owner)
			require.NoError(t, err)
			require.Equal(t, original.TicketID, recovered.TicketID)
			recovered.Release()
			after := assertOffPeakManagerRecoveryRetainsSameRecord(t, original)
			require.Greater(t, after.Fence, retained.Fence)
			for _, client := range []*offPeakManagerScriptClient{failedClient, recoveryClient} {
				calls := client.snapshot()
				require.Len(t, calls, 1, "both failed admission and restart must Status the known ticket without Take")
				require.Equal(t, "status", calls[0].Operation)
				require.Equal(t, []string{original.TicketID}, calls[0].TicketIDs)
				assertOffPeakManagerCapturedOwner(t, calls, route)
			}
		})
	}
}
