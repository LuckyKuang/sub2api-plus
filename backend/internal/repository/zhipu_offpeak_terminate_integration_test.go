//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestZhipuOffPeakStoreTerminateFencesOldSegmentAndIsIdempotent(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	old := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	claimed, err := store.Claim(ctx, old.ID, false, 0, time.Minute)
	require.NoError(t, err)
	retired, err := store.Terminate(ctx, owner.ID, old.ID, old.Generation)
	require.NoError(t, err)
	require.True(t, retired.Retired)
	require.True(t, retired.SettlementPending)
	require.False(t, retired.Closed)
	require.Equal(t, old.TicketID, retired.TicketID)
	require.Equal(t, old.Snapshot, retired.Snapshot)
	require.Greater(t, retired.Fence, claimed.Fence)
	require.Empty(t, retired.LeaseID)
	require.ErrorIs(t, store.Save(ctx, claimed), service.ErrOffPeakFenceLost)
	require.ErrorIs(t, store.Admit(ctx, claimed, owner, "late-model", time.Minute), service.ErrOffPeakFenceLost)
	_, err = store.Claim(ctx, old.ID, false, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakFenceLost)

	proposal := candidate
	proposal.ID, proposal.TaskID = uuid.NewString(), "offpeak-"+uuid.NewString()
	next, err := store.Open(ctx, owner, proposal)
	require.NoError(t, err)
	require.NotEqual(t, old.ID, next.ID)
	require.Equal(t, old.Generation, next.Generation, "a later business request may begin a new segment under the same credential owner")
	_, err = store.Terminate(ctx, owner.ID, old.ID, old.Generation)
	require.NoError(t, err, "repeated termination of the original pair is idempotent")
	unchanged, err := store.Current(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, next.ID, unchanged.ID)
	require.Equal(t, next.Fence, unchanged.Fence)
	require.False(t, unchanged.Retired, "a delayed old termination cannot end a replacement run segment")
}

func TestZhipuOffPeakStoreTerminateWaitsForSharedInflightRequests(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	old := openOffPeakStoreTicket(t, store, owner, candidate, "active")
	for _, id := range []string{"physical-one", "physical-two"} {
		claimed, err := store.Claim(ctx, old.ID, false, 0, time.Minute)
		require.NoError(t, err)
		require.NoError(t, store.Admit(ctx, claimed, owner, id, time.Minute))
	}
	_, err := store.Terminate(ctx, owner.ID, old.ID, old.Generation)
	require.NoError(t, err)
	_, err = store.Claim(ctx, old.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy)
	require.NoError(t, store.RenewRequest(ctx, old.ID, "physical-one", time.Minute), "termination permits already admitted requests to drain")
	require.NoError(t, store.ReleaseRequest(ctx, old.ID, "physical-one"))
	_, err = store.Claim(ctx, old.ID, true, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrOffPeakLeaseBusy)
	require.NoError(t, store.ReleaseRequest(ctx, old.ID, "physical-two"))
	settlement, err := store.Claim(ctx, old.ID, true, 0, time.Minute)
	require.NoError(t, err)
	require.Equal(t, old.TicketID, settlement.TicketID)
	require.Equal(t, old.Snapshot, settlement.Snapshot)
	settlement.State, settlement.Closed = "settled", true
	require.NoError(t, store.Save(ctx, settlement))
	due, err := store.Due(ctx, 0, 50)
	require.NoError(t, err)
	require.NotContains(t, due, old.ID, "a durably acknowledged termination is never dispatched again")
	closed := loadOffPeakStoreRecord(t, old.ID)
	require.Empty(t, closed.Snapshot)
	_, err = store.Terminate(ctx, owner.ID, old.ID, old.Generation)
	require.NoError(t, err)
	require.Equal(t, closed.Fence, loadOffPeakStoreRecord(t, old.ID).Fence)
}

func TestZhipuOffPeakStoreTerminateRejectsUnmatchedOwnerAndGeneration(t *testing.T) {
	for _, scenario := range []string{"other-account", "unknown-record", "wrong-generation"} {
		t.Run(scenario, func(t *testing.T) {
			store, owner, candidate := newOffPeakStoreFixture(t)
			ctx := context.Background()
			old := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
			id, generation, accountID := old.ID, old.Generation, owner.ID
			switch scenario {
			case "other-account":
				accountID++
			case "unknown-record":
				id = uuid.NewString()
			case "wrong-generation":
				generation = "unrelated-owner"
			}
			_, err := store.Terminate(ctx, accountID, id, generation)
			require.Error(t, err)
			require.Equal(t, old, loadOffPeakStoreRecord(t, old.ID), "a rejected termination changes no admission, lease, intent or ciphertext")
		})
	}
}

func TestZhipuOffPeakStoreTerminateDatabaseFailureKeepsOriginalIntent(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	original := openOffPeakStoreTicket(t, store, owner, candidate, "ready")
	_, err := store.Claim(ctx, original.ID, false, 0, time.Minute)
	require.NoError(t, err)
	original = loadOffPeakStoreRecord(t, original.ID)
	require.NotEmpty(t, original.LeaseID)
	name := "offpeak_terminate_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = integrationDB.ExecContext(ctx, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected offpeak termination write failure'; END $$`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name+` ON zhipu_offpeak_tickets`)
		_, _ = integrationDB.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS `+name+`()`)
	})
	_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER `+name+` BEFORE UPDATE ON zhipu_offpeak_tickets FOR EACH ROW WHEN (NEW.id='`+original.ID+`' AND NEW.retired) EXECUTE FUNCTION `+name+`()`)
	require.NoError(t, err)
	_, err = store.Terminate(ctx, owner.ID, original.ID, original.Generation)
	require.Error(t, err)
	require.Equal(t, original, loadOffPeakStoreRecord(t, original.ID), "a database failure must not acknowledge or partially commit retirement")
	_, err = integrationDB.ExecContext(ctx, `DROP TRIGGER `+name+` ON zhipu_offpeak_tickets`)
	require.NoError(t, err)
	retired, err := store.Terminate(ctx, owner.ID, original.ID, original.Generation)
	require.NoError(t, err)
	require.True(t, retired.Retired)
	require.True(t, retired.SettlementPending)
	require.Equal(t, original.Snapshot, retired.Snapshot)
}

func TestZhipuOffPeakStoreTerminateRetainsLateTakeOnlyForCleanup(t *testing.T) {
	store, owner, candidate := newOffPeakStoreFixture(t)
	ctx := context.Background()
	original, err := store.Open(ctx, owner, candidate)
	require.NoError(t, err)
	claim, err := store.Claim(ctx, original.ID, false, 0, time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.BeginTake(ctx, claim))
	retired, err := store.Terminate(ctx, owner.ID, original.ID, original.Generation)
	require.NoError(t, err)
	require.False(t, retired.SettlementPending, "unknown upstream ticket cannot be fabricated")
	proposal := candidate
	proposal.ID, proposal.TaskID = uuid.NewString(), "offpeak-"+uuid.NewString()
	replacement, err := store.Open(ctx, owner, proposal)
	require.NoError(t, err)
	claim.TicketID, claim.State = "late-original-ticket", "ready"
	require.ErrorIs(t, store.Save(ctx, claim), service.ErrOffPeakFenceLost)
	require.NoError(t, store.RegisterLateTicket(ctx, claim))
	late := loadOffPeakStoreRecord(t, original.ID)
	require.True(t, late.Retired)
	require.True(t, late.SettlementPending)
	require.Equal(t, "late-original-ticket", late.TicketID)
	require.Equal(t, original.Snapshot, late.Snapshot)
	require.ErrorIs(t, store.Admit(ctx, claim, owner, "late-physical-request", time.Minute), service.ErrOffPeakFenceLost)
	current, err := store.Current(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, replacement.ID, current.ID)
	require.Equal(t, replacement.Fence, current.Fence)
}
