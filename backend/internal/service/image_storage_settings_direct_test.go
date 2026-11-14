//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func baseStorageSettings() ImageStorageSettings {
	return ImageStorageSettings{
		Enabled:     true,
		Bucket:      "my-images",
		Endpoint:    "https://acct.r2.cloudflarestorage.com",
		Region:      "auto",
		AccessKeyID: "ak",
		Prefix:      "images/",
	}
}

// The deployment explicitly accepts that changing the storage target may make
// older objects unreadable: configuration stays directly editable, with no
// migration step or reference guard.
func TestImageStorageSettingsAllowsDirectTargetChange(t *testing.T) {
	svc, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()

	first := baseStorageSettings()
	first.SecretAccessKey = "sk"
	_, err := svc.Update(ctx, first)
	require.NoError(t, err)

	moved := baseStorageSettings()
	moved.Bucket = "other-images"
	moved.Endpoint = "https://other.r2.cloudflarestorage.com"
	_, err = svc.Update(ctx, moved)
	require.NoError(t, err)

	_, enabled := svc.resolve()
	require.True(t, enabled)
}

// Prefix and credential changes never alter which key is read, and the stored
// secret is inherited when the field is left empty.
func TestImageStorageSettingsAllowsPrefixAndSecretChanges(t *testing.T) {
	svc, _, _ := newImageStorageFixture(t, config.ImageStorageConfig{})
	ctx := context.Background()

	first := baseStorageSettings()
	first.SecretAccessKey = "sk"
	_, err := svc.Update(ctx, first)
	require.NoError(t, err)

	renamed := baseStorageSettings()
	renamed.Prefix = "archive/"
	_, err = svc.Update(ctx, renamed)
	require.NoError(t, err)

	rotated := baseStorageSettings()
	rotated.SecretAccessKey = "next-sk"
	_, err = svc.Update(ctx, rotated)
	require.NoError(t, err)
}
