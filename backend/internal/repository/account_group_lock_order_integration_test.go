//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func newMembershipTargetGroup(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name, platform) VALUES ($1, 'openai') RETURNING id`, fmt.Sprintf("membership-lock-%d", time.Now().UnixNano())).Scan(&id))
	t.Cleanup(func() { _, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=$1`, id) })
	return id
}

func TestMembershipWritesLockAccountsBeforeWeeklyResetGroups(t *testing.T) {
	for _, method := range []string{"BindGroups", "AddToGroup", "BindAccountsToGroup", "CreateShadowWithGroups"} {
		t.Run(method, func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			targetGroupID := f.groupID
			if method == "AddToGroup" {
				targetGroupID = newMembershipTargetGroup(t)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			blocker, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback() }()
			_, err = blocker.ExecContext(ctx, `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, f.groupID)
			require.NoError(t, err)
			observationDone := make(chan error, 1)
			go func() {
				_, err := observeWeeklyResetAt(f.repo, ctx, f.accountID, f.baseline.Add(7*24*time.Hour), f.baseline)
				observationDone <- err
			}()
			waitForLock := func(query string) {
				t.Helper()
				require.Eventually(t, func() bool {
					var waiting bool
					err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
						SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
						AND wait_event_type='Lock' AND query LIKE $1
					)`, query).Scan(&waiting)
					return err == nil && waiting
				}, 3*time.Second, 10*time.Millisecond, "expected blocked query: %s", query)
			}
			waitForLock("%FROM groups g%") // The observer now owns the account row.
			bindingDone := make(chan error, 1)
			bindingAccountID := f.accountID
			shadow := &service.Account{Name: fmt.Sprintf("lock-shadow-%d", f.accountID), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive,
				ParentAccountID: &f.accountID, QuotaDimension: service.QuotaDimensionSpark}
			t.Cleanup(func() {
				if shadow.ID != 0 {
					_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, shadow.ID)
				}
			})
			go func() {
				accounts := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
				var err error
				switch method {
				case "BindGroups":
					err = accounts.BindGroups(ctx, f.accountID, []int64{targetGroupID})
				case "AddToGroup":
					err = accounts.AddToGroup(ctx, f.accountID, targetGroupID, 50)
				case "BindAccountsToGroup":
					err = NewGroupRepository(integrationEntClient, integrationDB).BindAccountsToGroup(ctx, targetGroupID, []int64{f.accountID})
				case "CreateShadowWithGroups":
					err = accounts.CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: targetGroupID, Priority: 50}})
					bindingAccountID = shadow.ID
				}
				bindingDone <- err
			}()
			// With reversed order BindGroups waits on the group here and can race
			// the observer for it, then deadlock on the membership FK account lock.
			waitForLock("%/* account_group_account_lock */%")
			require.NoError(t, blocker.Commit())
			for _, done := range []chan error{observationDone, bindingDone} {
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal("concurrent repository operation did not finish")
				}
			}
			var members, events int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, bindingAccountID, targetGroupID).Scan(&members))
			require.Equal(t, 1, members)
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM group_quota_follow_reset_events WHERE group_id=$1`, f.groupID).Scan(&events))
			require.Equal(t, 1, events)
		})
	}
}

func TestMembershipAccountLocksRespectCallerTransactionRollback(t *testing.T) {
	for _, method := range []string{"BindGroups", "AddToGroup", "BindAccountsToGroup", "CreateShadowWithGroups"} {
		t.Run(method, func(t *testing.T) {
			f := newQuotaFollowFixture(t)
			target := newMembershipTargetGroup(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, err := integrationEntClient.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			ctx = dbent.NewTxContext(ctx, tx)
			accounts := newAccountRepositoryWithSQL(tx.Client(), tx.Client(), nil)
			bindingAccountID := f.accountID
			switch method {
			case "BindGroups":
				err = accounts.BindGroups(ctx, f.accountID, []int64{target})
			case "AddToGroup":
				err = accounts.AddToGroup(ctx, f.accountID, target, 50)
			case "BindAccountsToGroup":
				err = NewGroupRepository(integrationEntClient, integrationDB).BindAccountsToGroup(ctx, target, []int64{f.accountID})
			case "CreateShadowWithGroups":
				shadow := &service.Account{Name: fmt.Sprintf("lock-shadow-%d", f.accountID), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive,
					ParentAccountID: &f.accountID, QuotaDimension: service.QuotaDimensionSpark}
				err = accounts.CreateWithAccountGroups(ctx, shadow, []service.AccountGroup{{GroupID: target, Priority: 50}})
				bindingAccountID = shadow.ID
			}
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())
			var members int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, bindingAccountID, target).Scan(&members))
			require.Zero(t, members)
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, f.accountID, f.groupID).Scan(&members))
			require.Equal(t, 1, members)
		})
	}
}
