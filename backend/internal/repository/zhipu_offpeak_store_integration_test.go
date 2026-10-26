//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newOffPeakStoreFixture(t *testing.T) (*ZhipuOffPeakRepository, *service.Account, service.ZhipuOffPeakRecord) {
	t.Helper()
	repo, original, _ := newCNControlSnapshotFixture(t)
	credentials := map[string]any{"oauth_provider": "bigmodel", "plan_kind": service.ZhipuPlanOffPeak, "zhipu_project_kind": "personal", "zcode_jwt_token": "offpeak-owner-jwt", "oauth_access_token": "offpeak-business-login", "zhipu_project_key_id": "project-key-id"}
	payload, err := json.Marshal(credentials)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='zhipu',credentials=$2::jsonb WHERE id=$1`, original.ID, string(payload))
	require.NoError(t, err)
	owner, err := repo.GetByID(context.Background(), original.ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM zhipu_offpeak_tickets WHERE account_id=$1", owner.ID)
	})
	candidate := service.ZhipuOffPeakRecord{ID: uuid.NewString(), AccountID: owner.ID, Generation: service.CredentialStateGeneration(owner), TaskID: "offpeak-" + uuid.NewString(), KeyID: "test-key-v1", Snapshot: []byte("opaque-authenticated-ciphertext"), RetainUntil: time.Now().Add(time.Hour)}
	return &ZhipuOffPeakRepository{db: integrationDB}, owner, candidate
}

func loadOffPeakStoreRecord(t *testing.T, id string) service.ZhipuOffPeakRecord {
	t.Helper()
	record, err := scanOffPeak(integrationDB.QueryRowContext(context.Background(), "SELECT "+offPeakColumns+" FROM zhipu_offpeak_tickets WHERE id=$1", id))
	require.NoError(t, err)
	return record
}

func openOffPeakStoreTicket(t *testing.T, store *ZhipuOffPeakRepository, owner *service.Account, candidate service.ZhipuOffPeakRecord, state string) service.ZhipuOffPeakRecord {
	t.Helper()
	ctx := context.Background()
	opened, err := store.Open(ctx, owner, candidate)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	claimed.TicketID = "remote-" + uuid.NewString()
	claimed.State = state
	claimed.NextAttempt = time.Unix(1, 0)
	require.NoError(t, store.Save(ctx, claimed))
	return loadOffPeakStoreRecord(t, opened.ID)
}

func TestZhipuOffPeakStoreCrossInstanceOpenHasOneCurrentGeneration(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	const count = 8
	results := make(chan service.ZhipuOffPeakRecord, count)
	failures := make(chan error, count)
	var workers sync.WaitGroup
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			other := &ZhipuOffPeakRepository{db: integrationDB}
			proposal := candidate
			proposal.ID = uuid.NewString()
			proposal.TaskID = "offpeak-" + uuid.NewString()
			got, err := other.Open(ctx, owner, proposal)
			if err != nil {
				failures <- err
				return
			}
			results <- got
		}()
	}
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var current service.ZhipuOffPeakRecord
	seen := 0
	for result := range results {
		seen++
		if current.ID == "" {
			current = result
		}
		require.Equal(t, current.ID, result.ID)
		require.Equal(t, current.TaskID, result.TaskID)
		require.Equal(t, "new", result.State)
	}
	require.Equal(t, count, seen)
	var rows int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM zhipu_offpeak_tickets WHERE account_id=$1 AND NOT retired AND NOT closed", owner.ID).Scan(&rows))
	require.Equal(t, 1, rows)
	// Name and observation metadata are not login/route generations.
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET name='renamed-during-ticket',extra=extra || '{"probe_time":"later"}'::jsonb WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	got, err := store.Open(ctx, owner, candidate)
	require.NoError(t, err)
	require.Equal(t, current.ID, got.ID)
}

func TestZhipuOffPeakStoreExpiredOperationLeaseFencesLateSaveAndAdmission(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "queued")
	first, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	secondStore := &ZhipuOffPeakRepository{db: integrationDB}
	_, err = secondStore.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy)
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", opened.ID)
	require.NoError(t, err)
	second, err := secondStore.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	require.Greater(t, second.Fence, first.Fence)
	require.NotEqual(t, first.LeaseID, second.LeaseID)
	first.State = "ready"
	first.NextAttempt = time.Now()
	require.ErrorIs(t, store.Save(ctx, first), service.ErrOffPeakFenceLost)
	require.ErrorIs(t, store.Admit(ctx, first, owner, "stale-request", time.Minute), service.ErrOffPeakFenceLost)
	second.State = "ready"
	require.NoError(t, secondStore.Admit(ctx, second, owner, "current-request", time.Minute))
	var requests int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", opened.ID).Scan(&requests))
	require.Equal(t, 1, requests)
}

func TestZhipuOffPeakStoreBeginTakeCrashRemainsUncertainAfterRestart(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened, err := store.Open(ctx, owner, candidate)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.BeginTake(ctx, claimed))
	interrupted := loadOffPeakStoreRecord(t, opened.ID)
	require.Equal(t, "uncertain", interrupted.State)
	require.Empty(t, interrupted.TicketID)
	require.Equal(t, claimed.LeaseID, interrupted.LeaseID)
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", opened.ID)
	require.NoError(t, err)
	restarted := &ZhipuOffPeakRepository{db: integrationDB}
	recovered, err := restarted.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "uncertain", recovered.State)
	require.ErrorIs(t, restarted.BeginTake(ctx, recovered), service.ErrOffPeakFenceLost, "uncertain cannot become a fresh Take without authoritative evidence")
	recovered.NextAttempt = time.Now()
	require.NoError(t, restarted.Save(ctx, recovered))
	replacement := candidate
	replacement.ID = uuid.NewString()
	replacement.TaskID = "offpeak-new-task-must-not-win"
	got, err := restarted.Open(ctx, owner, replacement)
	require.NoError(t, err)
	require.Equal(t, opened.ID, got.ID)
	require.Equal(t, opened.TaskID, got.TaskID)
	require.Equal(t, "uncertain", got.State)
}

func TestZhipuOffPeakStoreSharedRequestLeasesBlockSettlementUntilAllFinish(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	for _, request := range []string{"request-a", "request-b"} {
		record, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
		require.NoError(t, err)
		record.State = "ready"
		require.NoError(t, store.Admit(ctx, record, owner, request, time.Minute))
	}
	_, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy)
	require.NoError(t, store.ReleaseRequest(ctx, opened.ID, "request-a"))
	require.NoError(t, store.ReleaseRequest(ctx, opened.ID, "request-a"), "release is idempotent and cannot remove another request")
	_, err = store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy)
	require.NoError(t, store.RenewRequest(ctx, opened.ID, "request-b", 2*time.Minute))
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_request_leases SET expires_at=NOW()-INTERVAL '1 second' WHERE ticket_record_id=$1 AND request_id='request-b'", opened.ID)
	require.NoError(t, err)
	require.ErrorIs(t, store.RenewRequest(ctx, opened.ID, "request-b", time.Minute), service.ErrOffPeakFenceLost, "an expired request cannot resurrect its lease")
	settlement, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.True(t, settlement.SettlementPending)
	require.Equal(t, 1, settlement.Attempts)
	_, err = store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy, "settlement intent closes new admission")
}

func TestZhipuOffPeakStoreSettlementRetryKeepsIntentUntilAcknowledged(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "active")
	first, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.True(t, first.SettlementPending)
	first.LastError = "control_failed"
	first.NextAttempt = time.Now().Add(time.Hour)
	require.NoError(t, store.Save(ctx, first))
	retained := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, retained.SettlementPending)
	require.False(t, retained.Closed)
	require.Equal(t, opened.TicketID, retained.TicketID)
	require.Equal(t, opened.Snapshot, retained.Snapshot)
	due, err := store.Due(ctx, 0, 100)
	require.NoError(t, err)
	require.NotContains(t, due, opened.ID, "backoff is durable")
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET next_attempt_at=NOW()-INTERVAL '1 second' WHERE id=$1", opened.ID)
	require.NoError(t, err)
	restarted := &ZhipuOffPeakRepository{db: integrationDB}
	ack, err := restarted.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, ack.Attempts)
	require.Equal(t, opened.TicketID, ack.TicketID)
	require.Equal(t, opened.Generation, ack.Generation)
	// Simulate receiving a successful remote ACK and crashing before Save:
	// another process retries the same ticket after the lease expires.
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_tickets SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", opened.ID)
	require.NoError(t, err)
	final, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, opened.TicketID, final.TicketID)
	require.Equal(t, 3, final.Attempts)
	ack.Closed = true
	ack.State = "settled"
	require.ErrorIs(t, restarted.Save(ctx, ack), service.ErrOffPeakFenceLost)
	final.Closed = true
	final.State = "settled"
	final.LastError = ""
	final.NextAttempt = time.Now()
	require.NoError(t, store.Save(ctx, final))
	closed := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, closed.Closed)
	require.False(t, closed.SettlementPending)
	require.Empty(t, closed.Snapshot)
	due, err = store.Due(ctx, 0, 100)
	require.NoError(t, err)
	require.NotContains(t, due, opened.ID)
	_, err = store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy, "committed ACK must never be dispatched again")
}

func TestZhipuOffPeakStoreRetiresOldOwnerAndRegistersLateTicketForOriginalGeneration(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened, err := store.Open(ctx, owner, candidate)
	require.NoError(t, err)
	old, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.BeginTake(ctx, old))
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"zcode_jwt_token":"replacement-owner"}'::jsonb WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	_, err = store.Open(ctx, owner, candidate)
	require.ErrorIs(t, err, service.ErrOffPeakFenceLost)
	accountRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	currentOwner, err := accountRepo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	newCandidate := candidate
	newCandidate.ID = uuid.NewString()
	newCandidate.TaskID = "offpeak-" + uuid.NewString()
	newCandidate.Generation = service.CredentialStateGeneration(currentOwner)
	newCandidate.Snapshot = []byte("new-owner-ciphertext")
	current, err := store.Open(ctx, currentOwner, newCandidate)
	require.NoError(t, err)
	require.NotEqual(t, opened.ID, current.ID)
	old.TicketID = "late-old-ticket"
	old.State = "ready"
	require.ErrorIs(t, store.Save(ctx, old), service.ErrOffPeakFenceLost)
	require.NoError(t, store.RegisterLateTicket(ctx, old))
	retired := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, retired.Retired)
	require.True(t, retired.SettlementPending)
	require.Equal(t, "late-old-ticket", retired.TicketID)
	require.Equal(t, candidate.Generation, retired.Generation)
	require.Equal(t, candidate.Snapshot, retired.Snapshot)
	unaffected := loadOffPeakStoreRecord(t, current.ID)
	require.False(t, unaffected.Retired)
	require.Empty(t, unaffected.TicketID)
	require.Equal(t, newCandidate.Snapshot, unaffected.Snapshot)
	_, err = store.Claim(ctx, retired.ID, false, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakFenceLost, "retirement ends this segment rather than a temporary lease wait")
	cleanup, err := store.Claim(ctx, retired.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "late-old-ticket", cleanup.TicketID)
}

func TestZhipuOffPeakStoreAdmissionRejectsDifferentCredentialOwner(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	_, otherOwner, _ := newOffPeakStoreFixture(t)
	claim, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	claim.State = "ready"
	require.ErrorIs(t, store.Admit(ctx, claim, otherOwner, "wrong-owner", time.Minute), service.ErrOffPeakFenceLost)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", opened.ID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, store.Admit(ctx, claim, owner, "right-owner", time.Minute), "failed cross-owner admission cannot consume the valid owner's lease")
}

func TestZhipuOffPeakStoreFailedAckWriteCannotErasePendingIntent(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "active")
	ack, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	ack.Closed = true
	ack.State = "invalid-ack-state"
	ack.NextAttempt = time.Now()
	require.Error(t, store.Save(ctx, ack), "real PostgreSQL constraint failure rejects the complete ACK mutation atomically")
	retained := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, retained.SettlementPending)
	require.False(t, retained.Closed)
	require.Equal(t, opened.Snapshot, retained.Snapshot)
	require.Equal(t, ack.LeaseID, retained.LeaseID)
	ack.State = "settled"
	require.NoError(t, store.Save(ctx, ack))
	closed := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, closed.Closed)
	require.False(t, closed.SettlementPending)
	require.Empty(t, closed.Snapshot)
}

func TestZhipuOffPeakStoreRetiredGenerationWaitsForOriginalInflightRequest(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	claim, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	claim.State = "ready"
	require.NoError(t, store.Admit(ctx, claim, owner, "old-owner-inflight", time.Minute))
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"zcode_jwt_token":"relinked-jwt"}'::jsonb WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	relinked, err := repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	newCandidate := candidate
	newCandidate.ID = uuid.NewString()
	newCandidate.TaskID = "offpeak-" + uuid.NewString()
	newCandidate.Generation = service.CredentialStateGeneration(relinked)
	newCandidate.Snapshot = []byte("relinked-owner-ciphertext")
	newRecord, err := store.Open(ctx, relinked, newCandidate)
	require.NoError(t, err)
	require.NotEqual(t, opened.ID, newRecord.ID)
	old := loadOffPeakStoreRecord(t, opened.ID)
	require.True(t, old.Retired)
	require.True(t, old.SettlementPending)
	_, err = store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy, "relink cannot settle the still-streaming original owner's shared ticket")
	require.NoError(t, store.ReleaseRequest(ctx, opened.ID, "old-owner-inflight"))
	settlement, err := store.Claim(ctx, opened.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, candidate.Generation, settlement.Generation)
	require.Equal(t, candidate.Snapshot, settlement.Snapshot)
}

func TestZhipuOffPeakStoreSettlementRechecksRequestLeaseAfterParentLockWait(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	claim, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	claim.State = "ready"
	require.NoError(t, store.Admit(ctx, claim, owner, "renewing-request", time.Minute))
	// A renewal/admission transaction holds the required parent lock while
	// installing an active request lease. The contender has already started
	// its settlement statement before this child-row change becomes visible.
	_, err = integrationDB.ExecContext(ctx, "UPDATE zhipu_offpeak_request_leases SET expires_at=NOW()-INTERVAL '1 second' WHERE ticket_record_id=$1", opened.ID)
	require.NoError(t, err)
	writer, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	var locked string
	require.NoError(t, writer.QueryRowContext(ctx, "SELECT id FROM zhipu_offpeak_tickets WHERE id=$1 FOR UPDATE", opened.ID).Scan(&locked))
	_, err = writer.ExecContext(ctx, "UPDATE zhipu_offpeak_request_leases SET expires_at=clock_timestamp()+INTERVAL '5 minutes' WHERE ticket_record_id=$1 AND request_id='renewing-request'", opened.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		other := &ZhipuOffPeakRepository{db: integrationDB}
		_, err := other.Claim(ctx, opened.ID, true, 0, time.Minute)
		done <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%zhipu_offpeak_tickets%' AND pid<>pg_backend_pid())`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "the settlement operation must actually reach the held PostgreSQL parent lock")
	require.NoError(t, writer.Commit())
	select {
	case err := <-done:
		require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy, "the renewed request lease committed before lock acquisition must block settlement")
	case <-ctx.Done():
		t.Fatal("blocked settlement did not finish")
	}
	record := loadOffPeakStoreRecord(t, opened.ID)
	require.False(t, record.SettlementPending)
	require.Zero(t, record.Attempts)
}

func TestZhipuOffPeakStoreReleaseLocksParentBeforeRequestLease(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opened := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	claim, err := store.Claim(ctx, opened.ID, false, 0, time.Minute)
	require.NoError(t, err)
	claim.State = "ready"
	require.NoError(t, store.Admit(ctx, claim, owner, "finishing-request", time.Minute))

	// Renewal and settlement take the parent lock before touching request
	// leases. A concurrent EOF/Close must use that same order; otherwise it
	// can hold the child while renewal holds the parent, forming a deadlock.
	writer, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	var parentID string
	require.NoError(t, writer.QueryRowContext(ctx, "SELECT id FROM zhipu_offpeak_tickets WHERE id=$1 FOR UPDATE", opened.ID).Scan(&parentID))
	var writerPID int
	require.NoError(t, writer.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&writerPID))

	done := make(chan error, 1)
	go func() {
		other := &ZhipuOffPeakRepository{db: integrationDB}
		done <- other.ReleaseRequest(ctx, opened.ID, "finishing-request")
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'
 AND $1=ANY(pg_blocking_pids(pid))
 AND query LIKE '%zhipu_offpeak%' AND pid<>pg_backend_pid())`, writerPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "release must actually reach the held PostgreSQL parent lock")

	var requestID string
	err = writer.QueryRowContext(ctx, `SELECT request_id FROM zhipu_offpeak_request_leases
 WHERE ticket_record_id=$1 AND request_id='finishing-request' FOR UPDATE NOWAIT`, opened.ID).Scan(&requestID)
	require.NoError(t, err, "release waiting for the parent must not hold the request lease needed by renewal")
	require.Equal(t, "finishing-request", requestID)
	require.NoError(t, writer.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("request release did not finish after the parent lock was released")
	}
	var remaining int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", opened.ID).Scan(&remaining))
	require.Zero(t, remaining, "successful EOF/Close releases the durable request lease")
}
