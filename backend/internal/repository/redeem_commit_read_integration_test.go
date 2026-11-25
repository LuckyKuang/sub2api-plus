//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// redeemReadFailureRepo injects one read failure into the transactional
// re-read that builds the redemption response.
type redeemReadFailureRepo struct {
	service.RedeemCodeRepository
	failNextGetByID bool
}

func (r *redeemReadFailureRepo) GetByID(ctx context.Context, id int64) (*service.RedeemCode, error) {
	if r.failNextGetByID {
		r.failNextGetByID = false
		return nil, errors.New("injected redeem code read failure")
	}
	return r.RedeemCodeRepository.GetByID(ctx, id)
}

// TestRedeemCommitReadFailureRollsBackAndRetrySucceeds covers the reported
// defect where a failed response left the redemption committed. A read failure
// before commit must roll back both the code consumption and the balance
// credit; the same code must then succeed with a response that matches the
// committed row, and a third attempt must stay rejected.
func TestRedeemCommitReadFailureRollsBackAndRetrySucceeds(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "commit-read-" + uuid.NewString()[:16]

	user, err := client.User.Create().
		SetEmail(prefix + "@example.com").
		SetPasswordHash("test").
		Save(ctx)
	require.NoError(t, err)

	codes := NewRedeemCodeRepository(client)
	users := NewUserRepository(client, integrationDB)
	code := &service.RedeemCode{
		Code:   prefix,
		Type:   service.RedeemTypeBalance,
		Value:  10,
		Status: service.StatusUnused,
	}
	require.NoError(t, codes.Create(ctx, code))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM redeem_codes WHERE id=$1`, code.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
	})

	repo := &redeemReadFailureRepo{RedeemCodeRepository: codes, failNextGetByID: true}
	redeem := service.NewRedeemService(repo, users, nil, nil, nil, client, nil, nil)

	// First attempt: the response read fails, so the whole redemption rolls back.
	got, err := redeem.Redeem(ctx, user.ID, code.Code)
	require.Nil(t, got)
	require.ErrorContains(t, err, "injected redeem code read failure")

	stored, err := codes.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, stored.Status)
	require.Nil(t, stored.UsedBy)
	require.Nil(t, stored.UsedAt)
	account, err := users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Zero(t, account.Balance)

	// Retry: the same code succeeds and the response matches the committed row.
	got, err = redeem.Redeem(ctx, user.ID, code.Code)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, service.StatusUsed, got.Status)
	require.NotNil(t, got.UsedBy)
	require.Equal(t, user.ID, *got.UsedBy)
	require.NotNil(t, got.UsedAt)

	stored, err = codes.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, stored.Status, got.Status)
	require.Equal(t, *stored.UsedBy, *got.UsedBy)
	require.WithinDuration(t, *stored.UsedAt, *got.UsedAt, time.Microsecond)
	account, err = users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, 10.0, account.Balance)

	// Third attempt: already used, with no further balance change.
	got, err = redeem.Redeem(ctx, user.ID, code.Code)
	require.Nil(t, got)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	stored, err = codes.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusUsed, stored.Status)
	account, err = users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, 10.0, account.Balance)
}
