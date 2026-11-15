//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Upgrade behaviour: with no administrator value the deployment default wins, so
// moving the switch out of config.yaml never changes the effective state.
func TestBatchImageSettingsFallsBackToDeploymentDefault(t *testing.T) {
	repo := newStubSettingRepo()
	svc := NewBatchImageSettingsService(repo, true)

	require.True(t, svc.Enabled(context.Background()))
	require.Equal(t, BatchImageAvailability{Enabled: true}, svc.Availability(context.Background()))

	disabled := NewBatchImageSettingsService(repo, false)
	require.False(t, disabled.Enabled(context.Background()))
	require.Equal(t, BatchImageAvailability{Enabled: false, Reason: "disabled"}, disabled.Availability(context.Background()))
}

func TestBatchImageSettingsStoredValueOverridesDefault(t *testing.T) {
	repo := newStubSettingRepo()
	svc := NewBatchImageSettingsService(repo, false)
	ctx := context.Background()

	require.NoError(t, svc.SetEnabled(ctx, true))
	require.True(t, svc.Enabled(ctx))

	// A second instance in the same deployment sees the stored value immediately.
	other := NewBatchImageSettingsService(repo, false)
	require.True(t, other.Enabled(ctx))

	require.NoError(t, svc.SetEnabled(ctx, false))
	require.False(t, other.Enabled(ctx))
}

type failingImageSettingRepo struct {
	SettingRepository
	readErr error
}

func (r *failingImageSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	return r.SettingRepository.GetValue(ctx, key)
}

func TestBatchImageSettingsReadFailureCannotReenableDisabledMode(t *testing.T) {
	ctx := context.Background()
	repo := &failingImageSettingRepo{SettingRepository: newStubSettingRepo()}
	svc := NewBatchImageSettingsService(repo, true)
	require.NoError(t, svc.SetEnabled(ctx, false))
	repo.readErr = errors.New("database unavailable")
	require.False(t, svc.Enabled(ctx))
	require.Equal(t, BatchImageAvailability{Reason: "dependency_unavailable"}, svc.Availability(ctx))
	repo.readErr = nil
	require.Equal(t, BatchImageAvailability{Reason: "disabled"}, svc.Availability(ctx))
}

func TestBatchImageSettingsMissingRowUsesDeploymentDefault(t *testing.T) {
	repo := &failingImageSettingRepo{readErr: fmt.Errorf("wrapped: %w", ErrSettingNotFound)}
	svc := NewBatchImageSettingsService(repo, true)
	require.True(t, svc.Enabled(context.Background()))
}

func TestBatchImageSettingsWorkerReadinessDoesNotChangeAdminSwitch(t *testing.T) {
	svc := NewBatchImageSettingsService(newStubSettingRepo(), true)
	svc.SetWorkerEnabled(false)
	require.True(t, svc.Enabled(context.Background()))
	require.Equal(t, BatchImageAvailability{Enabled: true, Reason: "worker_disabled"}, svc.Availability(context.Background()))
}
