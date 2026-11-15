//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// The admin form never receives the environment secret. Toggling the switch on an
// unchanged deployment target must therefore reuse it instead of rejecting the
// save as incomplete.
func TestImageStorageSettingsEnableUsesEnvironmentSecretWhenTargetUnchanged(t *testing.T) {
	fallback := config.ImageStorageConfig{
		Enabled: false, Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "yaml-bucket", AccessKeyID: "yaml-ak", SecretAccessKey: "yaml-sk",
		Prefix: "images/",
	}
	svc, repo, built := newImageStorageFixture(t, fallback)
	ctx := context.Background()

	current, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Empty(t, current.SecretAccessKey, "the admin form never receives the secret")

	current.Enabled = true
	_, err = svc.Update(ctx, *current)
	require.NoError(t, err)
	raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.NoError(t, err)
	require.Contains(t, raw, `"secret_access_key":"enc:yaml-sk"`)
	require.NotContains(t, raw, `"secret_access_key":"yaml-sk"`)

	_, enabled := svc.resolve()
	require.True(t, enabled)
	require.Len(t, *built, 1)
	require.Equal(t, "yaml-sk", (*built)[0].SecretAccessKey)
}

func TestImageStorageSettingsEnvironmentSecretRequiresPersistentEncryptionKey(t *testing.T) {
	fallback := config.ImageStorageConfig{
		Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "yaml-bucket", AccessKeyID: "yaml-ak", SecretAccessKey: "yaml-sk",
	}
	svc, repo, _ := newImageStorageFixtureWithKey(t, fallback, false)
	ctx := context.Background()
	current, err := svc.Get(ctx)
	require.NoError(t, err)
	current.Enabled = true
	_, err = svc.Update(ctx, *current)
	require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)
	raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
	require.NoError(t, err)
	require.Empty(t, raw)
}

func TestImageStorageSettingsAWSEnvironmentTargetAllowsDefaultEndpoint(t *testing.T) {
	for _, persistentKey := range []bool{true, false} {
		t.Run(map[bool]string{true: "persistent encryption key", false: "ephemeral encryption key"}[persistentKey], func(t *testing.T) {
			fallback := config.ImageStorageConfig{
				Region: "us-east-1", Bucket: "aws-images", AccessKeyID: "aws-ak", SecretAccessKey: "aws-sk",
			}
			svc, repo, built := newImageStorageFixtureWithKey(t, fallback, persistentKey)
			ctx := context.Background()
			current, err := svc.Get(ctx)
			require.NoError(t, err)
			current.Enabled = true
			_, err = svc.Update(ctx, *current)
			if !persistentKey {
				require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)
				raw, readErr := repo.GetValue(ctx, settingKeyImageStorageConfig)
				require.NoError(t, readErr)
				require.Empty(t, raw)
				return
			}
			require.NoError(t, err)
			raw, err := repo.GetValue(ctx, settingKeyImageStorageConfig)
			require.NoError(t, err)
			require.Contains(t, raw, `"secret_access_key":"enc:aws-sk"`)
			_, enabled := svc.resolve()
			require.True(t, enabled)
			require.Len(t, *built, 1)
			require.Empty(t, (*built)[0].Endpoint)
			require.Equal(t, "us-east-1", (*built)[0].Region)
			require.Equal(t, "aws-sk", (*built)[0].SecretAccessKey)
		})
	}
}
