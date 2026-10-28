//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/adminaccess"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type scopedRefreshIntegrationExecutor struct {
	calls atomic.Int32
	issue func(context.Context) (map[string]any, error)
}

func (*scopedRefreshIntegrationExecutor) CanRefresh(*service.Account) bool { return true }
func (*scopedRefreshIntegrationExecutor) NeedsRefresh(*service.Account, time.Duration) bool {
	return true
}
func (*scopedRefreshIntegrationExecutor) CacheKey(a *service.Account) string {
	return fmt.Sprintf("scoped-refresh-integration:%d", a.ID)
}
func (e *scopedRefreshIntegrationExecutor) Refresh(ctx context.Context, _ *service.Account) (map[string]any, error) {
	e.calls.Add(1)
	return e.issue(ctx)
}

// The scoped authorization transaction must release its real account row lock
// before the in-flight issuer returns. Otherwise the refresh mutex and row lock
// invert, preventing an already consumed refresh grant from being persisted.
func TestScopedAdminRefreshBusyRollsBackRowLockBeforeOrdinaryRotationPersists(t *testing.T) {
	repo, account, _ := newCNControlSnapshotFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	group, err := integrationEntClient.Group.Create().SetName(fmt.Sprintf("refresh-lock-%d", account.ID)).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
	})
	_, err = integrationDB.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)", account.ID, group.ID)
	require.NoError(t, err)
	account, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	adminRepo := NewAdminAccountRepository(integrationEntClient, integrationDB, nil)
	groups := NewAdminGroupRepository(integrationEntClient, integrationDB)
	admin := service.NewAdminService(nil, nil, groups, adminRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, integrationEntClient, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	scoped := admin.(service.ScopedAdminAccounts)
	api := service.NewOAuthRefreshAPI(repo, nil)
	issuerStarted, releaseIssuer := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseIssuer) }) }
	defer release()
	ordinary := &scopedRefreshIntegrationExecutor{issue: func(issueCtx context.Context) (map[string]any, error) {
		close(issuerStarted)
		select {
		case <-releaseIssuer:
			return map[string]any{"oauth_provider": "kimi", "access_token": "rotated-access", "refresh_token": "rotated-refresh", "_token_version": 8}, nil
		case <-issueCtx.Done():
			return nil, issueCtx.Err()
		}
	}}
	type outcome struct {
		result *service.OAuthRefreshResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, refreshErr := api.RefreshIfNeeded(ctx, account, ordinary, time.Hour)
		done <- outcome{result, refreshErr}
	}()
	ordinaryCompleted := false
	defer func() {
		release()
		cancel()
		if !ordinaryCompleted {
			<-done
		}
	}()
	select {
	case <-issuerStarted:
	case <-ctx.Done():
		t.Fatal("ordinary refresh did not reach issuer")
	}
	contender := &scopedRefreshIntegrationExecutor{issue: func(context.Context) (map[string]any, error) {
		return nil, errors.New("scoped contender must not contact issuer")
	}}
	scopedCtx := adminaccess.WithPrincipal(ctx, &adminaccess.Principal{Policy: adminaccess.Policy{Rules: []adminaccess.Rule{{Permissions: []string{"accounts.refresh"}, GroupIDs: []int64{group.ID}, AccountTypes: []string{service.AccountTypeOAuth}}}}})
	scopedCtx, scopedCancel := context.WithTimeout(scopedCtx, 2*time.Second)
	defer scopedCancel()
	busy := errors.New("refresh busy")
	_, err = scoped.ScopedAdminWrite(scopedCtx, account.ID, "accounts.refresh", nil, func(txctx context.Context, current *service.Account) (*service.Account, error) {
		// A distinct connection proves the callback actually owns the row lock.
		probe, probeErr := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, probeErr)
		_, probeErr = probe.ExecContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT", account.ID)
		require.NoError(t, probe.Rollback())
		var pgErr *pq.Error
		require.ErrorAs(t, probeErr, &pgErr)
		require.Equal(t, pq.ErrorCode("55P03"), pgErr.Code)
		result, refreshErr := api.RefreshIfNeeded(txctx, current, contender, time.Hour)
		if refreshErr != nil {
			return nil, refreshErr
		}
		if result != nil && result.LockHeld {
			return nil, busy
		}
		return nil, errors.New("scoped refresh did not report lock contention")
	})
	require.ErrorIs(t, err, busy, "busy must precede the scoped deadline and issuer return")
	require.NoError(t, scopedCtx.Err())
	require.Zero(t, contender.calls.Load())
	require.Zero(t, cnControlOutboxCount(t, account.ID))
	// Busy must have rolled back, rather than retaining the authorization lock.
	probe, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = probe.Rollback() }()
	_, err = probe.ExecContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT", account.ID)
	require.NoError(t, err)
	require.NoError(t, probe.Rollback())
	release()
	completed := <-done
	ordinaryCompleted = true
	require.NoError(t, completed.err)
	require.NotNil(t, completed.result)
	require.True(t, completed.result.Refreshed)
	require.EqualValues(t, 1, ordinary.calls.Load())
	require.Zero(t, contender.calls.Load())
	stored, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, "rotated-access", stored.GetCredential("access_token"))
	require.Equal(t, "rotated-refresh", stored.GetCredential("refresh_token"))
	require.True(t, stored.IsDomesticOAuth())
	require.Equal(t, 1, cnControlOutboxCount(t, account.ID), "only the ordinary rotation may commit an outbox event")
}
