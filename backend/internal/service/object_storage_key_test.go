//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildObjectStorageKey(t *testing.T) {
	at := time.Date(2026, time.August, 17, 23, 59, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	tests := []struct {
		name           string
		prefix         string
		fallback       string
		appendDatePath bool
		want           string
	}{
		{name: "dated", prefix: "images/", fallback: "images", appendDatePath: true, want: "images/2026/08/17/task.png"},
		{name: "plain", prefix: "images", fallback: "images", appendDatePath: false, want: "images/task.png"},
		{name: "trailing slashes", prefix: "images///", fallback: "images", appendDatePath: true, want: "images/2026/08/17/task.png"},
		{name: "fallback", prefix: " ", fallback: "backups", appendDatePath: false, want: "backups/task.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, buildObjectStorageKey(tt.prefix, tt.fallback, tt.appendDatePath, at, "/task.png"))
		})
	}
}

func TestBuildObjectStorageKeyUsesProvidedTimezoneDate(t *testing.T) {
	instant := time.Date(2026, time.August, 17, 16, 30, 0, 0, time.UTC)
	taipei := instant.In(time.FixedZone("UTC+8", 8*60*60))
	require.Equal(t, "images/2026/08/18/task.png", buildObjectStorageKey("images/", "images", true, taipei, "task.png"))
}
