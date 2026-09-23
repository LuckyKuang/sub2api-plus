//go:build unit || !integration

package repository

import "testing"

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_CompactCapabilityKeysAreRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_compact_supported":  true,
		"openai_compact_checked_at": "2026-04-10T10:00:00Z",
	}

	if !shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected compact capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_OpenAIResponsesCapabilityKeysAreRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_responses_mode":      "force_chat_completions",
		"openai_responses_supported": false,
	}

	if !shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected responses capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_CodexCreditsSnapshotKeysAreNeutral(t *testing.T) {
	updates := map[string]any{
		"codex_credits_has_credits": true,
		"codex_credits_unlimited":   false,
		"codex_credits_balance":     "12.75",
		"codex_limit_name":          "gpt-5.2-codex-sonic",
		"codex_rate_limit_families": []any{},
		"codex_usage_updated_at":    "2026-09-22T08:00:00Z",
	}
	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("credits/limit/family snapshot keys must stay scheduler-neutral")
	}
}
