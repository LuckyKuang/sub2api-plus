package securityaudit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/auditcontent"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditLogAllowlistAndErrorsDoNotLeakCanarySecrets(t *testing.T) {
	const canary = "PROMPT_AUDIT_CANARY_SECRET_DO_NOT_PERSIST"
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	LogWarn(EventConfigReloadDegraded, map[string]any{
		"status":     "degraded",
		"error_code": "config_reload_failed",
		"error_kind": "Authorization: Bearer " + canary,
		"token":      canary,
		"body":       canary,
		"base_url":   "https://guard.example.test/path?api_key=" + canary,
		"raw_prompt": "prompt " + canary,
	})
	require.NotContains(t, output.String(), canary)
	require.NotContains(t, output.String(), "api_key=")
	require.Contains(t, output.String(), EventConfigReloadDegraded)

	beforeUnknown := output.Len()
	LogWarn("prompt_audit.typo_event", map[string]any{"status": "failed"})
	require.Equal(t, beforeUnknown, output.Len(), "events outside the stable dictionary must not be emitted")
	require.Len(t, knownLogEvents, 29)

	_, err := NormalizeBaseURL("https://guard.example.test/path?token=" + canary)
	require.Error(t, err)
	require.NotContains(t, err.Error(), canary)
}

func TestPromptExtractionFailureLogHasBoundedContextWithoutRawContent(t *testing.T) {
	const canary = "PROMPT_EXTRACTION_CANARY_DO_NOT_LOG"
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	typeValue := "prompt_canary_secret"
	request := Request{
		RequestID: "req-extraction", Endpoint: "/v1/responses", Protocol: "openai_responses", Stage: "subsequent_turn",
		Body: []byte(`{"input":[{"type":"` + typeValue + `","payload":"` + canary + `"}]}`),
	}
	logPromptExtractionFailure(request, promptExtractionDiagnostic{
		Failed: true, ErrorCode: "incomplete_content",
		Reasons: []auditcontent.IncompleteReason{{Kind: auditcontent.IncompleteUnsupportedItemType, ItemType: typeValue}},
	})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	require.Equal(t, EventExtractionFailed, entry["msg"])
	require.Equal(t, "req-extraction", entry["request_id"])
	require.Equal(t, "/v1/responses", entry["endpoint"])
	require.Equal(t, "openai_responses", entry["protocol"])
	require.Equal(t, "subsequent_turn", entry["stage"])
	require.EqualValues(t, len(request.Body), entry["body_bytes"])
	require.Equal(t, "incomplete_content", entry["error_code"])
	require.Contains(t, output.String(), "unknown_item_type")
	require.NotContains(t, output.String(), typeValue)
	require.NotContains(t, output.String(), canary)
}

func TestPromptValidUnrecognizedJSONProducesSafeExtractionFailureLog(t *testing.T) {
	const canary = "UNRECOGNIZED_STRUCTURE_CANARY_DO_NOT_LOG"
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	request := Request{
		RequestID: "req-unrecognized", Endpoint: "/v1/responses", Protocol: "openai_responses", Stage: "http",
		Body: []byte(`{"future_payload":{"shape":"` + canary + `"}}`),
	}
	_, diagnostic, err := extractPromptSnapshotWithDiagnostics(request, true)
	require.ErrorIs(t, err, ErrNoPromptText)
	require.True(t, diagnostic.Failed)
	require.Equal(t, "incomplete_content", diagnostic.ErrorCode)
	logPromptExtractionFailure(request, diagnostic)

	require.Contains(t, output.String(), EventExtractionFailed)
	require.Contains(t, output.String(), "unextractable_content")
	require.NotContains(t, output.String(), canary)
}

func TestPromptGuardFailureLogUsesCompleteAllowlistedContextAndNoSideEffects(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	groupID := int64(9)
	snapshot := PromptSnapshot{
		RequestID: "req-1", UserID: 2, APIKeyID: 3, GroupID: &groupID,
		Provider: "openai", Protocol: "openai_chat", Endpoint: "/v1/chat/completions",
		Model: "gpt-test", Stage: "http",
	}
	logGuardFailure(snapshot, ActiveConfig{ConfigVersion: 7}, DecisionUnavailable, ErrorCodeUnavailable, "guard-1", 25*time.Millisecond)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	for key := range snapshotLogFields(snapshot) {
		require.Contains(t, entry, key)
	}
	require.EqualValues(t, 7, entry["config_version"])
	require.Equal(t, ErrorCodeUnavailable, entry["error_code"])
	require.Equal(t, false, entry["upstream_dispatched"])
	require.Equal(t, false, entry["billing_preconsumed"])
	require.EqualValues(t, 25, entry["latency_ms"])
}

func TestPromptRuntimeFailureLogUsesSafeOperationalContext(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logPromptRuntimeFailure(EventProcessFailed, "claim_job_failed")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	require.Equal(t, EventProcessFailed, entry["msg"])
	require.Equal(t, "", entry["request_id"])
	require.Equal(t, "runtime", entry["endpoint"])
	require.Equal(t, "internal", entry["protocol"])
	require.Equal(t, "runtime", entry["stage"])
	require.EqualValues(t, 0, entry["body_bytes"])
	require.Equal(t, "claim_job_failed", entry["error_code"])
	require.Equal(t, "audit_dependency", entry["error_kind"])
	require.NotContains(t, output.String(), "error_message")
}
