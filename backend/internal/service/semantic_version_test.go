//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCodexClientVersion(t *testing.T) {
	require.Equal(t, "0.146.0", NormalizeCodexClientVersion(" 0.146.0 "))
	require.Equal(t, "0.147.0-alpha.4", NormalizeCodexClientVersion("0.147.0-alpha.4"))
	require.Equal(t, "1.2", NormalizeCodexClientVersion("1.2"))
	require.Empty(t, NormalizeCodexClientVersion(""))
	require.Empty(t, NormalizeCodexClientVersion("v0.146.0"))
	require.Empty(t, NormalizeCodexClientVersion("0.146.0 (Ubuntu)"))
	require.Empty(t, NormalizeCodexClientVersion("0.146.0\r\nX-Injected: 1"))
	require.Empty(t, NormalizeCodexClientVersion("latest"))
}

func TestNormalizeStableCodexClientVersion(t *testing.T) {
	require.Equal(t, "0.147.0", normalizeStableCodexClientVersion(" 0.147.0 "))
	require.Equal(t, "1.2", normalizeStableCodexClientVersion("1.2"))
	require.Empty(t, normalizeStableCodexClientVersion("0.147.0-alpha.4"))
	require.Empty(t, normalizeStableCodexClientVersion("latest"))
}
