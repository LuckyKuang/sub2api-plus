//go:build unit || !integration

package service

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// TestParseCodexRateLimitHeadersFamiliesAndCredits verifies the official-aligned
// additions: the credits header family, the default limit name, and the
// prefix-scanned non-default rate-limit families.
func TestParseCodexRateLimitHeadersFamiliesAndCredits(t *testing.T) {
	now := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "12.5")
	headers.Set("x-codex-limit-name", "gpt-5.2-codex-sonic")
	headers.Set("x-codex-credits-has-credits", "true")
	headers.Set("x-codex-credits-unlimited", "false")
	headers.Set("x-codex-credits-balance", "12.75")
	// codex_secondary family (its primary window headers must NOT be mistaken
	// for the default family's secondary window) plus one unknown family.
	headers.Set("x-codex-secondary-primary-used-percent", "80")
	headers.Set("x-codex-secondary-primary-window-minutes", "1440")
	headers.Set("x-codex-secondary-primary-reset-at", "1790000000")
	headers.Set("x-codex-bengalfox-primary-used-percent", "42")
	headers.Set("x-codex-bengalfox-limit-name", "gpt-5.2-codex-bengalfox")

	snapshot := parseCodexRateLimitHeadersAt(headers, now)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.LimitName != "gpt-5.2-codex-sonic" {
		t.Fatalf("limit name = %q", snapshot.LimitName)
	}
	if snapshot.CreditsHasCredits == nil || !*snapshot.CreditsHasCredits {
		t.Fatalf("credits has_credits = %v", snapshot.CreditsHasCredits)
	}
	if snapshot.CreditsUnlimited == nil || *snapshot.CreditsUnlimited {
		t.Fatalf("credits unlimited = %v", snapshot.CreditsUnlimited)
	}
	if snapshot.CreditsBalance != "12.75" {
		t.Fatalf("credits balance = %q", snapshot.CreditsBalance)
	}
	// The default family's secondary window stays untouched by the
	// codex-secondary family headers.
	if snapshot.SecondaryUsedPercent != nil {
		t.Fatalf("default secondary used percent unexpectedly set: %v", *snapshot.SecondaryUsedPercent)
	}
	if len(snapshot.Families) != 2 {
		t.Fatalf("families = %+v, want 2", snapshot.Families)
	}
	// Families are sorted by limit id: codex-bengalfox precedes codex-secondary.
	bengalfox := snapshot.Families[0]
	if bengalfox.LimitID != "codex_bengalfox" {
		t.Fatalf("first family id = %q, want codex_bengalfox", bengalfox.LimitID)
	}
	if bengalfox.LimitName != "gpt-5.2-codex-bengalfox" {
		t.Fatalf("bengalfox limit name = %q", bengalfox.LimitName)
	}
	secondary := snapshot.Families[1]
	if secondary.LimitID != "codex_secondary" {
		t.Fatalf("second family id = %q, want codex_secondary", secondary.LimitID)
	}
	if secondary.PrimaryUsedPercent == nil || *secondary.PrimaryUsedPercent != 80 {
		t.Fatalf("codex_secondary primary used = %v", secondary.PrimaryUsedPercent)
	}
	if secondary.PrimaryWindowMinutes == nil || *secondary.PrimaryWindowMinutes != 1440 {
		t.Fatalf("codex_secondary primary window = %v", secondary.PrimaryWindowMinutes)
	}
	if secondary.PrimaryResetAtUnix == nil || *secondary.PrimaryResetAtUnix != 1790000000 {
		t.Fatalf("codex_secondary primary reset-at = %v", secondary.PrimaryResetAtUnix)
	}
}

// Credits headers alone must produce a snapshot (official has_rate_limit_data
// counts credits), matching the WHAM pull as a realtime supplement.
func TestParseCodexRateLimitHeadersCreditsOnly(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-credits-has-credits", "false")
	headers.Set("x-codex-credits-unlimited", "true")
	snapshot := parseCodexRateLimitHeadersAt(headers, time.Now())
	if snapshot == nil {
		t.Fatal("expected snapshot from credits-only headers")
	}
	if snapshot.CreditsHasCredits == nil || *snapshot.CreditsHasCredits {
		t.Fatalf("has_credits = %v", snapshot.CreditsHasCredits)
	}
	if snapshot.CreditsUnlimited == nil || !*snapshot.CreditsUnlimited {
		t.Fatalf("unlimited = %v", snapshot.CreditsUnlimited)
	}
}

// Official parse_credits_snapshot requires both has-credits and unlimited, so a
// lone half-family must not raise a credits snapshot (and must not on its own
// turn an otherwise empty response into a snapshot).
func TestParseCodexRateLimitHeaders_CreditsRequiresBothFlags(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-credits-has-credits", "true")
	if snapshot := parseCodexRateLimitHeadersAt(headers, time.Now()); snapshot != nil {
		t.Fatalf("a lone has-credits header must not produce a snapshot, got %+v", snapshot)
	}

	headers.Set("x-codex-credits-unlimited", "false")
	snapshot := parseCodexRateLimitHeadersAt(headers, time.Now())
	if snapshot == nil {
		t.Fatal("expected snapshot once both credits flags are present")
	}
	if snapshot.CreditsHasCredits == nil || !*snapshot.CreditsHasCredits {
		t.Fatalf("has_credits = %v", snapshot.CreditsHasCredits)
	}
	if snapshot.CreditsBalance != "" {
		t.Fatalf("balance = %q, want empty when the header is absent", snapshot.CreditsBalance)
	}
}

// The openai-model response header joins the response-model observer before
// body events: it supplies the model when the body never declares one and
// raises the conflict flag when the terminal body event disagrees. The official
// x-openai-model spelling is accepted as well.
func TestObserveOpenAICodexServerResponseHeaders(t *testing.T) {
	observer := beginUpstreamResponseModelObservation(nil)
	observeOpenAICodexServerResponseHeaders(observer, http.Header{"Openai-Model": []string{"gpt-5.6-sol"}})
	if got := observer.Model(); got != "gpt-5.6-sol" {
		t.Fatalf("model = %q, want gpt-5.6-sol", got)
	}
	if observer.Conflict() {
		t.Fatal("single declaration must not conflict")
	}

	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.6"}}`), "response.completed")
	if got := observer.Model(); got != "gpt-5.6" {
		t.Fatalf("terminal body model = %q, want gpt-5.6 (terminal wins)", got)
	}
	if !observer.Conflict() {
		t.Fatal("header/body disagreement must raise the conflict flag")
	}

	alternate := beginUpstreamResponseModelObservation(nil)
	observeOpenAICodexServerResponseHeaders(alternate, http.Header{"X-Openai-Model": []string{"gpt-5.6-sol"}})
	if got := alternate.Model(); got != "gpt-5.6-sol" {
		t.Fatalf("x-openai-model spelling = %q, want gpt-5.6-sol", got)
	}

	absent := beginUpstreamResponseModelObservation(nil)
	observeOpenAICodexServerResponseHeaders(absent, http.Header{"X-Models-Etag": []string{"etag-1"}})
	if got := absent.Model(); got != "" {
		t.Fatalf("model = %q, want empty without a model header", got)
	}
}

// The official codex_rollout_budget_units usage declaration is parsed as a
// reserved billing dimension; absent or non-numeric values stay nil.
func TestOpenAIUsageParsesCodexRolloutBudgetUnits(t *testing.T) {
	usage, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2,"codex_rollout_budget_units":2.5}}}`))
	if !ok {
		t.Fatal("expected usage")
	}
	if usage.CodexRolloutBudgetUnits == nil || *usage.CodexRolloutBudgetUnits != 2.5 {
		t.Fatalf("rollout budget units = %v, want 2.5", usage.CodexRolloutBudgetUnits)
	}

	absent, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":{"input_tokens":5,"output_tokens":2}}`))
	if !ok {
		t.Fatal("expected usage")
	}
	if absent.CodexRolloutBudgetUnits != nil {
		t.Fatalf("rollout budget units = %v, want nil when unreported", absent.CodexRolloutBudgetUnits)
	}

	invalid, ok := extractOpenAIUsageFromJSONBytes([]byte(`{"usage":{"input_tokens":5,"output_tokens":2,"codex_rollout_budget_units":"oops"}}`))
	if !ok {
		t.Fatal("expected usage")
	}
	if invalid.CodexRolloutBudgetUnits != nil {
		t.Fatalf("rollout budget units = %v, want nil for non-numeric value", invalid.CodexRolloutBudgetUnits)
	}
}

// 官方 response_model()（sse/responses.rs:203-217）优先读事件体内嵌套的
// response.headers.openai-model / x-openai-model：上游做流中重路由时只写那里。
// 只看 response.model 会漏掉这次改名，计费校正与 model_mismatch 审计都会记错。
func TestObserveOpenAIInBandServerModelHeaderSource(t *testing.T) {
	for _, spelling := range []string{"openai-model", "x-openai-model", "Openai-Model", "X-OpenAI-Model"} {
		t.Run(spelling, func(t *testing.T) {
			observer := beginUpstreamResponseModelObservation(nil)
			observer.ObserveOpenAI([]byte(`{"type":"response.output_text.delta","response":{"headers":{"`+spelling+`":"gpt-5.6-rerouted"},"model":"gpt-5.6-original"}}`), "response.output_text.delta")
			require.Equal(t, "gpt-5.6-rerouted", observer.Model(),
				"the in-band header must win over response.model")
		})
	}

	// Without the in-band header the body model is still used.
	observer := beginUpstreamResponseModelObservation(nil)
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.6-original"}}`), "response.completed")
	require.Equal(t, "gpt-5.6-original", observer.Model())

	// A terminal body model still wins over a non-terminal header declaration.
	observer = beginUpstreamResponseModelObservation(nil)
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"headers":{"openai-model":"gpt-5.6-first"},"model":"gpt-5.6-first"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"headers":{"openai-model":"gpt-5.6-second"},"model":"gpt-5.6-second"}}`), "response.completed")
	require.Equal(t, "gpt-5.6-second", observer.Model())
}
