//go:build unit

package service

import (
	"context"
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
