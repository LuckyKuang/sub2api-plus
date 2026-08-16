package repository

import (
	"testing"

	appTimezone "github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func useGroupUsageRepositoryTestTimezone(t *testing.T, name string) {
	t.Helper()

	previousName := appTimezone.Name()
	require.NoError(t, appTimezone.Init(name))
	t.Cleanup(func() { require.NoError(t, appTimezone.Init(previousName)) })
}
