//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

func unfinishedTasksUser(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `INSERT INTO users(email,password_hash) VALUES ($1,'test') RETURNING id`, fmt.Sprintf("unfinished-%d@example.test", time.Now().UnixNano())).Scan(&id))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1", id) })
	return id
}

// No worker runs during this upgrade check. Durable provider-complete phases
// must leave capacity immediately after migration, while unknown outcomes and
// reservations belonging to another owner or kind remain active.
func TestGatewayUnfinishedTasksUpgradeAdmitsBeforeWorkerRecovery(t *testing.T) {
	ctx := context.Background()
	schema := fmt.Sprintf("unfinished_capacity_upgrade_%d", time.Now().UnixNano())
	_, err := integrationDB.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := integrationDB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, cleanupErr)
	})
	dsn := integrationPostgresDSN
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn = strings.TrimSpace(dsn) + " search_path=" + schema
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.ExecContext(ctx, `
CREATE TABLE users (id BIGINT PRIMARY KEY,deleted_at TIMESTAMPTZ);
INSERT INTO users(id) VALUES (1),(2),(3);
CREATE TABLE batch_image_jobs(batch_id TEXT,user_id BIGINT,api_key_id BIGINT,status TEXT,provider_job_name TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE async_image_tasks(task_id TEXT,user_id BIGINT,api_key_id BIGINT,status TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,status,provider_job_name) VALUES
 ('provider-indexing',1,11,'indexing','provider-1'),('provider-settling',1,11,'settling','provider-2'),
 ('provider-completed',1,11,'completed','provider-3'),('provider-output-deleted',1,11,'output_deleted','provider-4'),
 ('other-key-completed',3,31,'indexing','provider-7'),
 ('unknown-created',2,21,'created',NULL),('unknown-uploading',2,21,'uploading',NULL),
 ('provider-queued',2,21,'submitted','provider-5'),('provider-running',2,21,'running','provider-6'),
 ('unknown-failed',2,21,'failed',NULL),('unknown-cancelled',2,21,'cancelled',NULL);`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("300_gateway_unfinished_tasks.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	// These reservations predate a later durable status write, or represent
	// unresolved work retained across a local failure. Different owners/kinds
	// deliberately reuse a completed task ID and must never be released by join.
	_, err = db.ExecContext(ctx, `INSERT INTO gateway_unfinished_tasks(reservation_id,user_id,api_key_id,kind,task_id,state) VALUES
 ('completed-before-worker',1,11,'batch_image','provider-completed','bound'),
 ('deleted-before-worker',1,11,'batch_image','provider-output-deleted','bound'),
 ('failed-unknown',2,21,'batch_image','unknown-failed','uncertain'),
 ('cancelled-unknown',2,21,'batch_image','unknown-cancelled','uncertain'),
 ('different-user',3,11,'batch_image','provider-indexing','uncertain'),
 ('different-key',3,32,'batch_image','other-key-completed','uncertain'),
 ('different-kind',3,31,'grok_video','other-key-completed','uncertain'),
 ('unknown-task',3,11,'batch_image','other-provider-task','uncertain');`)
	require.NoError(t, err)

	repair, err := dbmigrations.FS.ReadFile("301_release_completed_batch_task_capacity.sql")
	require.NoError(t, err)
	repaired, err := db.ExecContext(ctx, string(repair))
	require.NoError(t, err)
	count, err := repaired.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 5, count, "only exact owner/task matches with durable provider completion may release")
	for _, reservationID := range []string{"legacy:batch_image:provider-indexing", "legacy:batch_image:provider-settling", "legacy:batch_image:other-key-completed", "completed-before-worker", "deleted-before-worker"} {
		var state, reason string
		var released bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT state,reason,released_at IS NOT NULL FROM gateway_unfinished_tasks WHERE reservation_id=$1", reservationID).Scan(&state, &reason, &released))
		require.Equal(t, "released", state)
		require.Equal(t, "migration_provider_terminal", reason)
		require.True(t, released)
	}
	repeated, err := db.ExecContext(ctx, string(repair))
	require.NoError(t, err)
	count, err = repeated.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, count, "reapplying the forward repair cannot rewrite completed reservations")
	// This production Reserve call originally failed with UNFINISHED_TASK_LIMIT.
	// Upgrade must restore admission before any worker or billing retry runs.
	repo := NewGatewayUnfinishedTaskRepository(db)
	newTask := &service.GatewayTaskReservation{ID: "new-request-before-worker", Owner: service.GatewayTaskOwner{UserID: 1, APIKeyID: 12}, Kind: service.GatewayTaskKindSeedance}
	require.NoError(t, repo.Reserve(ctx, newTask, 1), "provider-complete historical work must not block a new task before any worker runs")
	for _, reservationID := range []string{"legacy:batch_image:unknown-created", "legacy:batch_image:unknown-uploading", "legacy:batch_image:provider-queued", "legacy:batch_image:provider-running", "failed-unknown", "cancelled-unknown", "different-user", "different-key", "different-kind", "unknown-task", newTask.ID} {
		var released bool
		require.NoError(t, db.QueryRowContext(ctx, "SELECT released_at IS NOT NULL FROM gateway_unfinished_tasks WHERE reservation_id=$1", reservationID).Scan(&released))
		require.False(t, released, "upgrade cannot release unresolved work or another owner: %s", reservationID)
	}
}

func TestGatewayUnfinishedTasksSerializesAllKeysKindsAndInstances(t *testing.T) {
	ctx := context.Background()
	userID := unfinishedTasksUser(t)
	repositories := []service.GatewayUnfinishedTaskRepository{NewGatewayUnfinishedTaskRepository(integrationDB), NewGatewayUnfinishedTaskRepository(integrationDB)}
	kinds := []string{service.GatewayTaskKindBatchImage, service.GatewayTaskKindAsyncImage, service.GatewayTaskKindGrokVideo, service.GatewayTaskKindSeedance}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r := &service.GatewayTaskReservation{ID: fmt.Sprintf("parallel:%d:%d", userID, i), Owner: service.GatewayTaskOwner{UserID: userID, APIKeyID: int64(i + 1)}, Kind: kinds[i%4]}
			results <- repositories[i%2].Reserve(ctx, r, 3)
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, service.ErrGatewayUnfinishedTaskLimit)
		}
	}
	require.Equal(t, 3, accepted, "one shared cap covers every independent agent task, API key, supplier and instance")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM gateway_unfinished_tasks WHERE user_id=$1 AND released_at IS NULL", userID).Scan(&count))
	require.Equal(t, 3, count)
}

func TestGatewayUnfinishedTasksUnknownCreateNeverExpiresOrReleasesOnLocalFailure(t *testing.T) {
	ctx := context.Background()
	userID := unfinishedTasksUser(t)
	owner := service.GatewayTaskOwner{UserID: userID, APIKeyID: 11}
	repo := NewGatewayUnfinishedTaskRepository(integrationDB)
	r := &service.GatewayTaskReservation{ID: fmt.Sprintf("unknown:%d", userID), Owner: owner, Kind: service.GatewayTaskKindBatchImage}
	require.NoError(t, repo.Reserve(ctx, r, 1))
	require.NoError(t, repo.Bind(ctx, r, "batch-accepted"))
	require.NoError(t, repo.MarkUncertain(ctx, r, "provider_create_inflight"))
	_, err := integrationDB.ExecContext(ctx, "UPDATE gateway_unfinished_tasks SET created_at=NOW()-INTERVAL '1 year',updated_at=NOW()-INTERVAL '1 year' WHERE reservation_id=$1", r.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ReleaseTask(ctx, owner, r.Kind, "batch-accepted", "local_terminal"))
	other := &service.GatewayTaskReservation{ID: fmt.Sprintf("next:%d", userID), Owner: service.GatewayTaskOwner{UserID: userID, APIKeyID: 22}, Kind: service.GatewayTaskKindSeedance}
	require.ErrorIs(t, repo.Reserve(ctx, other, 1), service.ErrGatewayUnfinishedTaskLimit)
	require.NoError(t, repo.ReleaseTask(ctx, other.Owner, r.Kind, "batch-accepted", "provider_terminal"))
	require.ErrorIs(t, repo.Reserve(ctx, other, 1), service.ErrGatewayUnfinishedTaskLimit, "another key cannot release an owner's reservation")
	require.NoError(t, repo.ReleaseTask(ctx, owner, r.Kind, "batch-accepted", "provider_terminal"))
	require.NoError(t, repo.ReleaseTask(ctx, owner, r.Kind, "batch-accepted", "provider_terminal"))
	require.NoError(t, repo.Reserve(ctx, other, 1))
	var state string
	var released bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT state,released_at IS NOT NULL FROM gateway_unfinished_tasks WHERE reservation_id=$1", r.ID).Scan(&state, &released))
	require.Equal(t, "released", state)
	require.True(t, released)
}

func TestGatewayUnfinishedTasksBindingCannotChangeOwnerOrTask(t *testing.T) {
	ctx := context.Background()
	userID := unfinishedTasksUser(t)
	repo := NewGatewayUnfinishedTaskRepository(integrationDB)
	r := &service.GatewayTaskReservation{ID: fmt.Sprintf("binding:%d", userID), Owner: service.GatewayTaskOwner{UserID: userID, APIKeyID: 11}, Kind: service.GatewayTaskKindGrokVideo}
	require.NoError(t, repo.Reserve(ctx, r, 1))
	require.NoError(t, repo.Bind(ctx, r, "video-1"))
	require.NoError(t, repo.Bind(ctx, r, "video-1"))
	require.ErrorIs(t, repo.Bind(ctx, r, "video-2"), service.ErrGatewayTaskReservationConflict)
	wrong := *r
	wrong.Owner.APIKeyID++
	require.ErrorIs(t, repo.Release(ctx, &wrong, "not_started"), service.ErrGatewayTaskReservationConflict)
	require.ErrorIs(t, repo.MarkUncertain(ctx, &wrong, "unknown"), service.ErrGatewayTaskReservationConflict)
	require.NoError(t, repo.Release(ctx, r, "not_started"))
	require.NoError(t, repo.Release(ctx, r, "not_started"))
	require.ErrorIs(t, repo.Bind(ctx, r, "video-1"), service.ErrGatewayTaskReservationConflict)
}

// A real PostgreSQL row-lock timeout after provider creation leaves no task
// binding. Polling a known task must not guess which uncertain reservation to
// free; only a correlated, owner-scoped reconciliation may free that slot.
func TestGatewayUnfinishedTasksBindTimeoutRequiresExactReservationReconciliation(t *testing.T) {
	for _, kind := range []string{service.GatewayTaskKindGrokVideo, service.GatewayTaskKindSeedance} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			userID := unfinishedTasksUser(t)
			owner := service.GatewayTaskOwner{UserID: userID, APIKeyID: 11}
			repo := NewGatewayUnfinishedTaskRepository(integrationDB)
			reservation := &service.GatewayTaskReservation{ID: fmt.Sprintf("bind-timeout:%d", userID), Owner: owner, Kind: kind}
			require.NoError(t, repo.Reserve(ctx, reservation, 1))
			require.NoError(t, repo.MarkUncertain(ctx, reservation, "provider_create_inflight"))
			blocker, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = blocker.Rollback() })
			_, err = blocker.ExecContext(ctx, "UPDATE gateway_unfinished_tasks SET updated_at=updated_at WHERE reservation_id=$1", reservation.ID)
			require.NoError(t, err)
			bindCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
			err = repo.Bind(bindCtx, reservation, "known-provider-task")
			require.Error(t, err, "a real blocked SQL binding must fail before the lock is released")
			require.ErrorIs(t, bindCtx.Err(), context.DeadlineExceeded)
			cancel()
			require.NoError(t, blocker.Rollback())
			// A later provider-confirmed terminal poll names this known task,
			// but cannot safely identify an unbound reservation by task ID.
			require.NoError(t, repo.ReleaseTask(ctx, owner, kind, "known-provider-task", "provider_terminal"))
			var taskID sql.NullString
			var state string
			var released bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT task_id,state,released_at IS NOT NULL FROM gateway_unfinished_tasks WHERE reservation_id=$1", reservation.ID).Scan(&taskID, &state, &released))
			require.False(t, taskID.Valid)
			require.Equal(t, "uncertain", state)
			require.False(t, released)
			next := &service.GatewayTaskReservation{ID: fmt.Sprintf("after-bind-timeout:%d", userID), Owner: service.GatewayTaskOwner{UserID: userID, APIKeyID: 12}, Kind: kind}
			require.ErrorIs(t, repo.Reserve(ctx, next, 1), service.ErrGatewayUnfinishedTaskLimit)
			// Only after exact reservation/owner/task correlation and terminal
			// evidence may the documented operator action release this row.
			require.NoError(t, repo.Release(ctx, reservation, "operator_confirmed_terminal"))
			require.NoError(t, repo.Reserve(ctx, next, 1))
			require.NoError(t, repo.Bind(ctx, next, "new-provider-task"))
			require.NoError(t, repo.ReleaseTask(ctx, owner, kind, "known-provider-task", "provider_terminal"))
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT released_at IS NOT NULL FROM gateway_unfinished_tasks WHERE reservation_id=$1", next.ID).Scan(&released))
			require.False(t, released, "old task confirmation cannot free the new key's reservation")
		})
	}
}

func TestGatewayUnfinishedTasksMigrationBackfillsOnlyActiveOwnedTasks(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	schema := fmt.Sprintf("unfinished_migration_%d", time.Now().UnixNano())
	_, err = tx.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, "SET LOCAL search_path TO "+schema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
CREATE TABLE users (id BIGINT PRIMARY KEY);
INSERT INTO users(id) VALUES (1);
CREATE TABLE batch_image_jobs(batch_id TEXT,user_id BIGINT,api_key_id BIGINT,status TEXT,provider_job_name TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE async_image_tasks(task_id TEXT,user_id BIGINT,api_key_id BIGINT,status TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
INSERT INTO batch_image_jobs(batch_id,user_id,api_key_id,status,provider_job_name) VALUES
 ('created',1,11,'created',NULL),('uploading',1,11,'uploading',NULL),
 ('submitted',1,12,'submitted','upstream-1'),('running',1,12,'running','upstream-2'),
 ('indexing',1,13,'indexing','upstream-3'),('settling',1,13,'settling','upstream-4'),
 ('completed',1,11,'completed','finished'),('failed',1,11,'failed',NULL),
 ('cancelled',1,11,'cancelled',NULL),('output_deleted',1,11,'output_deleted','finished'),
 ('orphan',999,11,'running','missing-user'),('missing-key',1,NULL,'running','unknown-owner');
INSERT INTO async_image_tasks(task_id,user_id,api_key_id,status) VALUES
 ('image-active',1,14,'processing'),('image-done',1,14,'completed'),('image-failed',1,14,'failed'),('image-orphan',999,14,'processing');`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("300_gateway_unfinished_tasks.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var active, uncertain int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(*) FILTER (WHERE state='uncertain') FROM gateway_unfinished_tasks WHERE released_at IS NULL").Scan(&active, &uncertain))
	require.Equal(t, 7, active, "six unfinished batch phases and one processing image, independent of stored monetary amounts")
	require.Equal(t, 3, uncertain, "unknown upstream creates and processing images require confirmation")
	var terminal int
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM gateway_unfinished_tasks WHERE task_id IN ('completed','failed','cancelled','output_deleted','image-done','image-failed')").Scan(&terminal))
	require.Zero(t, terminal)
	repair, err := dbmigrations.FS.ReadFile("301_release_completed_batch_task_capacity.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(repair))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(*) FILTER (WHERE state='uncertain') FROM gateway_unfinished_tasks WHERE released_at IS NULL").Scan(&active, &uncertain))
	require.Equal(t, 5, active, "created/uploading/queued/running Batch work and processing image remain unfinished after the forward repair")
	require.Equal(t, 3, uncertain, "the repair cannot infer termination for unknown outcomes")
}
