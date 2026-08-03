package service

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestCodexSnapshotBaseTime(t *testing.T) {
	fallback := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)

	t.Run("nil snapshot uses fallback", func(t *testing.T) {
		got := codexSnapshotBaseTime(nil, fallback)
		if !got.Equal(fallback) {
			t.Fatalf("got %v, want fallback %v", got, fallback)
		}
	})

	t.Run("empty updatedAt uses fallback", func(t *testing.T) {
		got := codexSnapshotBaseTime(&OpenAICodexUsageSnapshot{}, fallback)
		if !got.Equal(fallback) {
			t.Fatalf("got %v, want fallback %v", got, fallback)
		}
	})

	t.Run("valid updatedAt wins", func(t *testing.T) {
		got := codexSnapshotBaseTime(&OpenAICodexUsageSnapshot{UpdatedAt: "2026-02-16T10:00:00Z"}, fallback)
		want := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("invalid updatedAt uses fallback", func(t *testing.T) {
		got := codexSnapshotBaseTime(&OpenAICodexUsageSnapshot{UpdatedAt: "invalid"}, fallback)
		if !got.Equal(fallback) {
			t.Fatalf("got %v, want fallback %v", got, fallback)
		}
	})
}

func TestCodexResetAtRFC3339(t *testing.T) {
	base := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)

	t.Run("nil reset returns nil", func(t *testing.T) {
		if got := codexResetAtRFC3339(base, nil); got != nil {
			t.Fatalf("expected nil, got %v", *got)
		}
	})

	t.Run("positive seconds", func(t *testing.T) {
		sec := 90
		got := codexResetAtRFC3339(base, &sec)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if *got != "2026-02-16T10:01:30Z" {
			t.Fatalf("got %s, want %s", *got, "2026-02-16T10:01:30Z")
		}
	})

	t.Run("negative seconds clamp to base", func(t *testing.T) {
		sec := -3
		got := codexResetAtRFC3339(base, &sec)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if *got != "2026-02-16T10:00:00Z" {
			t.Fatalf("got %s, want %s", *got, "2026-02-16T10:00:00Z")
		}
	})

	t.Run("duration overflow returns nil", func(t *testing.T) {
		if strconv.IntSize < 64 {
			t.Skip("test requires a 64-bit int")
		}
		sec := int(maxCodexResetDurationSeconds + 1)
		if got := codexResetAtRFC3339(base, &sec); got != nil {
			t.Fatalf("expected nil, got %v", *got)
		}
	})
}

func TestParseCodexRateLimitHeadersResetAtCompatibility(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	resetIn90Seconds := strconv.FormatInt(now.Add(90*time.Second).Unix(), 10)
	pastReset := strconv.FormatInt(now.Add(-10*time.Second).Unix(), 10)

	tests := []struct {
		name    string
		resetAt string
		legacy  string
		want    int
	}{
		{name: "absolute timestamp only", resetAt: resetIn90Seconds, want: 90},
		{name: "legacy relative seconds only", legacy: "45", want: 45},
		{name: "absolute timestamp wins conflicts", resetAt: resetIn90Seconds, legacy: "45", want: 90},
		{name: "malformed absolute falls back", resetAt: "not-a-timestamp", legacy: "45", want: 45},
		{name: "past absolute timestamp becomes zero", resetAt: pastReset, legacy: "45", want: 0},
		{name: "overflowing absolute falls back", resetAt: "9223372036854775808", legacy: "45", want: 45},
		{name: "duration-overflowing absolute falls back", resetAt: "9223372036854775807", legacy: "45", want: 45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("x-codex-primary-used-percent", "12")
			if tt.resetAt != "" {
				headers.Set("x-codex-primary-reset-at", tt.resetAt)
			}
			if tt.legacy != "" {
				headers.Set("x-codex-primary-reset-after-seconds", tt.legacy)
			}

			snapshot := parseCodexRateLimitHeadersAt(headers, now)
			if snapshot == nil || snapshot.PrimaryResetAfterSeconds == nil {
				t.Fatal("expected primary reset data")
			}
			if got := *snapshot.PrimaryResetAfterSeconds; got != tt.want {
				t.Fatalf("reset seconds = %d, want %d", got, tt.want)
			}
			if snapshot.UpdatedAt != now.Format(time.RFC3339) {
				t.Fatalf("updated_at = %s, want %s", snapshot.UpdatedAt, now.Format(time.RFC3339))
			}
		})
	}
}

func TestParseCodexRateLimitHeadersRejectsDurationOverflowingLegacyReset(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)

	for _, tt := range []struct {
		name    string
		resetAt string
		legacy  string
	}{
		{name: "legacy only", legacy: "9223372037"},
		{name: "malformed absolute and legacy", resetAt: "not-a-timestamp", legacy: "9223372037"},
		{name: "duration-overflowing absolute and legacy", resetAt: "9223372036854775807", legacy: "9223372037"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("x-codex-primary-used-percent", "12")
			if tt.resetAt != "" {
				headers.Set("x-codex-primary-reset-at", tt.resetAt)
			}
			headers.Set("x-codex-primary-reset-after-seconds", tt.legacy)

			snapshot := parseCodexRateLimitHeadersAt(headers, now)
			if snapshot == nil {
				t.Fatal("expected usage data from the used-percent header")
			}
			if snapshot.PrimaryResetAfterSeconds != nil {
				t.Fatalf("expected overflowing legacy reset to be ignored, got %d", *snapshot.PrimaryResetAfterSeconds)
			}
		})
	}
}

func TestParseCodexRateLimitHeadersResetAtNormalizesReversedWindows(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-window-minutes", "300")
	headers.Set("x-codex-primary-reset-at", strconv.FormatInt(now.Add(time.Hour).Unix(), 10))
	headers.Set("x-codex-secondary-used-percent", "50")
	headers.Set("x-codex-secondary-window-minutes", "10080")
	headers.Set("x-codex-secondary-reset-at", strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10))

	snapshot := parseCodexRateLimitHeadersAt(headers, now)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	normalized := snapshot.Normalize()
	if normalized == nil || normalized.Reset5hSeconds == nil || normalized.Reset7dSeconds == nil {
		t.Fatal("expected normalized reset windows")
	}
	if got := *normalized.Reset5hSeconds; got != 3600 {
		t.Fatalf("5h reset seconds = %d, want 3600", got)
	}
	if got := *normalized.Reset7dSeconds; got != 86400 {
		t.Fatalf("7d reset seconds = %d, want 86400", got)
	}
}

func TestBuildCodexUsageExtraUpdates_UsesSnapshotUpdatedAt(t *testing.T) {
	primaryUsed := 88.0
	primaryReset := 86400
	primaryWindow := 10080
	secondaryUsed := 12.0
	secondaryReset := 3600
	secondaryWindow := 300

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:         &primaryUsed,
		PrimaryResetAfterSeconds:   &primaryReset,
		PrimaryWindowMinutes:       &primaryWindow,
		SecondaryUsedPercent:       &secondaryUsed,
		SecondaryResetAfterSeconds: &secondaryReset,
		SecondaryWindowMinutes:     &secondaryWindow,
		UpdatedAt:                  "2026-02-16T10:00:00Z",
	}

	updates := buildCodexUsageExtraUpdates(snapshot, time.Date(2026, 2, 20, 8, 0, 0, 0, time.UTC))
	if updates == nil {
		t.Fatal("expected non-nil updates")
	}

	if got := updates["codex_usage_updated_at"]; got != "2026-02-16T10:00:00Z" {
		t.Fatalf("codex_usage_updated_at = %v, want %s", got, "2026-02-16T10:00:00Z")
	}
	if got := updates["codex_5h_reset_at"]; got != "2026-02-16T11:00:00Z" {
		t.Fatalf("codex_5h_reset_at = %v, want %s", got, "2026-02-16T11:00:00Z")
	}
	if got := updates["codex_7d_reset_at"]; got != "2026-02-17T10:00:00Z" {
		t.Fatalf("codex_7d_reset_at = %v, want %s", got, "2026-02-17T10:00:00Z")
	}
}

// TestBuildCodexUsageExtraUpdates_FreshAccountUsedPercentNotInverted_Issue2994 locks in the
// canonical "used %" semantics for the 5h window. A fresh account reports a tiny
// secondary-used-percent (~1%); the stored codex_5h_used_percent must equal that value
// directly and must NOT be inverted to ~99%. Regression guard for issue #2994 / the reverted
// commit b65dde63 (PR #2918), which applied `100 - used` and made fresh accounts look
// exhausted, tripping auto-pause and excluding them from scheduling.
func TestBuildCodexUsageExtraUpdates_FreshAccountUsedPercentNotInverted_Issue2994(t *testing.T) {
	secondaryUsed := 1.0 // 5h window: barely used
	secondaryWindow := 300
	primaryUsed := 2.0 // 7d window: barely used
	primaryWindow := 10080

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:     &primaryUsed,
		PrimaryWindowMinutes:   &primaryWindow,
		SecondaryUsedPercent:   &secondaryUsed,
		SecondaryWindowMinutes: &secondaryWindow,
		UpdatedAt:              "2026-02-16T10:00:00Z",
	}

	updates := buildCodexUsageExtraUpdates(snapshot, time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC))
	if updates == nil {
		t.Fatal("expected non-nil updates")
	}

	if got := updates["codex_5h_used_percent"]; got != 1.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 1.0 (direct used%%, NOT inverted to 99)", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 2.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 2.0 (direct used%%, NOT inverted to 98)", got)
	}
}

func TestBuildCodexUsageExtraUpdates_FallbackToNowWhenUpdatedAtInvalid(t *testing.T) {
	primaryUsed := 15.0
	primaryReset := 30
	primaryWindow := 300

	fallbackNow := time.Date(2026, 2, 20, 8, 30, 0, 0, time.UTC)
	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:       &primaryUsed,
		PrimaryResetAfterSeconds: &primaryReset,
		PrimaryWindowMinutes:     &primaryWindow,
		UpdatedAt:                "invalid-time",
	}

	updates := buildCodexUsageExtraUpdates(snapshot, fallbackNow)
	if updates == nil {
		t.Fatal("expected non-nil updates")
	}

	if got := updates["codex_usage_updated_at"]; got != "2026-02-20T08:30:00Z" {
		t.Fatalf("codex_usage_updated_at = %v, want %s", got, "2026-02-20T08:30:00Z")
	}
	if got := updates["codex_5h_reset_at"]; got != "2026-02-20T08:30:30Z" {
		t.Fatalf("codex_5h_reset_at = %v, want %s", got, "2026-02-20T08:30:30Z")
	}
}

func TestBuildCodexUsageExtraUpdates_ClampNegativeResetSeconds(t *testing.T) {
	primaryUsed := 90.0
	primaryReset := 7200
	primaryWindow := 10080
	secondaryUsed := 100.0
	secondaryReset := -15
	secondaryWindow := 300

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:         &primaryUsed,
		PrimaryResetAfterSeconds:   &primaryReset,
		PrimaryWindowMinutes:       &primaryWindow,
		SecondaryUsedPercent:       &secondaryUsed,
		SecondaryResetAfterSeconds: &secondaryReset,
		SecondaryWindowMinutes:     &secondaryWindow,
		UpdatedAt:                  "2026-02-16T10:00:00Z",
	}

	updates := buildCodexUsageExtraUpdates(snapshot, time.Time{})
	if updates == nil {
		t.Fatal("expected non-nil updates")
	}

	if got := updates["codex_5h_reset_after_seconds"]; got != -15 {
		t.Fatalf("codex_5h_reset_after_seconds = %v, want %d", got, -15)
	}
	if got := updates["codex_5h_reset_at"]; got != "2026-02-16T10:00:00Z" {
		t.Fatalf("codex_5h_reset_at = %v, want %s", got, "2026-02-16T10:00:00Z")
	}
}

func TestBuildCodexUsageExtraUpdates_NilSnapshot(t *testing.T) {
	if got := buildCodexUsageExtraUpdates(nil, time.Now()); got != nil {
		t.Fatalf("expected nil updates, got %v", got)
	}
}

func TestBuildCodexUsageExtraUpdates_WithoutNormalizedWindowFields(t *testing.T) {
	primaryUsed := 42.0
	fallbackNow := time.Date(2026, 2, 20, 9, 15, 0, 0, time.UTC)
	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent: &primaryUsed,
		UpdatedAt:          "",
	}

	updates := buildCodexUsageExtraUpdates(snapshot, fallbackNow)
	if updates == nil {
		t.Fatal("expected non-nil updates")
	}

	if got := updates["codex_usage_updated_at"]; got != "2026-02-20T09:15:00Z" {
		t.Fatalf("codex_usage_updated_at = %v, want %s", got, "2026-02-20T09:15:00Z")
	}
	if _, ok := updates["codex_5h_reset_at"]; ok {
		t.Fatalf("did not expect codex_5h_reset_at in updates: %v", updates["codex_5h_reset_at"])
	}
	if _, ok := updates["codex_7d_reset_at"]; ok {
		t.Fatalf("did not expect codex_7d_reset_at in updates: %v", updates["codex_7d_reset_at"])
	}
}
