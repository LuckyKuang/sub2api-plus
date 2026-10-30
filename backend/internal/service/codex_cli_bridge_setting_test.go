//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexCLIBridgeSettingRepo struct {
	SettingRepository
	values            map[string]string
	readErr, writeErr error
	writes            int
}

func (r *codexCLIBridgeSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *codexCLIBridgeSettingRepo) Set(_ context.Context, key, value string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.writes++
	r.values[key] = value
	return nil
}

// Requirement: installation is not opt-in; the persisted global permission
// defaults off, and disabling must be observed across service instances without
// changing any account credentials or other settings.
func TestCodexCLIBridgeSettingDefaultsOffAndPersistsAcrossServices(t *testing.T) {
	ctx := context.Background()
	repo := &codexCLIBridgeSettingRepo{values: map[string]string{"unrelated": "preserve"}}
	first := NewSettingService(repo, nil)
	enabled, err := first.GetCodexCLIBridgeEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)
	require.Zero(t, repo.writes)
	invalidations := 0
	first.onUpdate = func() { invalidations++ }
	require.NoError(t, first.SetCodexCLIBridgeEnabled(ctx, true))
	require.Equal(t, "true", repo.values[SettingKeyCodexCLIBridgeEnabled])
	second := NewSettingService(repo, nil)
	enabled, err = second.GetCodexCLIBridgeEnabled(ctx)
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, first.SetCodexCLIBridgeEnabled(ctx, false))
	enabled, err = second.GetCodexCLIBridgeEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)
	require.Equal(t, "preserve", repo.values["unrelated"])
	require.Equal(t, 2, invalidations)
}

func TestCodexCLIBridgeSettingStorageFailuresAreSanitizedAndNeverEnable(t *testing.T) {
	ctx := context.Background()
	private := errors.New("postgres://private-password@private-host/secrets")
	for _, value := range []string{"TRUE", "1", "invalid"} {
		repo := &codexCLIBridgeSettingRepo{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: value}}
		enabled, err := NewSettingService(repo, nil).GetCodexCLIBridgeEnabled(ctx)
		require.False(t, enabled)
		require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
	}
	repo := &codexCLIBridgeSettingRepo{values: map[string]string{SettingKeyCodexCLIBridgeEnabled: "false"}, readErr: private, writeErr: private}
	svc := NewSettingService(repo, nil)
	enabled, err := svc.GetCodexCLIBridgeEnabled(ctx)
	require.False(t, enabled)
	require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
	require.NotContains(t, err.Error(), "private")
	require.ErrorIs(t, svc.SetCodexCLIBridgeEnabled(ctx, true), ErrCodexCLIBridgeSettingUnavailable)
	require.Equal(t, "false", repo.values[SettingKeyCodexCLIBridgeEnabled])
	require.Zero(t, repo.writes)
	var unavailable *SettingService
	enabled, err = unavailable.GetCodexCLIBridgeEnabled(ctx)
	require.False(t, enabled)
	require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
}

type codexCLIBridgeBlockingSettingRepo struct {
	SettingRepository
	get func(context.Context, string) (string, error)
}

func (r *codexCLIBridgeBlockingSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	return r.get(ctx, key)
}

// Requirement: a database outage must not strand WS protocol selection on its
// background context. Reads have a two-second cap, preserve caller cancellation
// and earlier deadlines, and never interpret timeout as bridge permission.
func TestCodexCLIBridgeSettingBackgroundReadHasBoundedDeadline(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		repo := &codexCLIBridgeBlockingSettingRepo{get: func(dbCtx context.Context, key string) (string, error) {
			require.Equal(t, SettingKeyCodexCLIBridgeEnabled, key)
			deadline, ok := dbCtx.Deadline()
			require.True(t, ok, "background selection must receive a database deadline")
			remaining := time.Until(deadline)
			require.Positive(t, remaining)
			require.LessOrEqual(t, remaining, 2*time.Second)
			return "", context.DeadlineExceeded
		}}
		enabled, err := NewSettingService(repo, nil).GetCodexCLIBridgeEnabled(ctx)
		require.False(t, enabled)
		require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
	}
}

func TestCodexCLIBridgeSettingReadPropagatesEarlierDeadlineAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if cancelled {
			cancel()
		}
		parentDeadline, ok := ctx.Deadline()
		require.True(t, ok)
		repo := &codexCLIBridgeBlockingSettingRepo{get: func(dbCtx context.Context, _ string) (string, error) {
			deadline, exists := dbCtx.Deadline()
			require.True(t, exists)
			require.Equal(t, parentDeadline, deadline)
			select {
			case <-dbCtx.Done():
				if cancelled {
					require.ErrorIs(t, dbCtx.Err(), context.Canceled)
				} else {
					require.ErrorIs(t, dbCtx.Err(), context.DeadlineExceeded)
				}
				return "", dbCtx.Err()
			case <-time.After(time.Second):
				t.Fatal("database read lost the caller deadline or cancellation")
				return "", nil
			}
		}}
		enabled, err := NewSettingService(repo, nil).GetCodexCLIBridgeEnabled(ctx)
		cancel()
		require.False(t, enabled)
		require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
	}
}
