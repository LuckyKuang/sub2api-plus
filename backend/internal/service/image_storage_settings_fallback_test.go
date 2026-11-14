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
	svc, _, built := newImageStorageFixture(t, fallback)
	ctx := context.Background()

	current, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Empty(t, current.SecretAccessKey, "the admin form never receives the secret")

	current.Enabled = true
	_, err = svc.Update(ctx, *current)
	require.NoError(t, err)

	_, enabled := svc.resolve()
	require.True(t, enabled)
	require.Len(t, *built, 1)
	require.Equal(t, "yaml-sk", (*built)[0].SecretAccessKey)
}
