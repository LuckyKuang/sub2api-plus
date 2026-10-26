//go:build unit || !integration

package usagestats

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseSnapshotMaxID(t *testing.T) {
	for _, invalid := range []string{"-1", "invalid", "1.5", "9223372036854775808"} {
		_, err := ParseSnapshotMaxID(invalid)
		require.Error(t, err)
	}
	for raw, want := range map[string]int64{"new": -1, "0": 0, "42": 42} {
		value, err := ParseSnapshotMaxID(raw)
		require.NoError(t, err)
		require.Equal(t, want, *value)
	}
	value, err := ParseSnapshotMaxID("")
	require.NoError(t, err)
	require.Nil(t, value)
}
