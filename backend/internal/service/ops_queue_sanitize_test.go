package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeOpsUpstreamErrorsForQueueBoundsAndRedacts(t *testing.T) {
	entry := &OpsInsertErrorLogInput{}
	for i := 0; i < 20; i++ {
		entry.UpstreamErrors = append(entry.UpstreamErrors, &OpsUpstreamErrorEvent{
			Platform:             strings.Repeat("p", 100),
			AccountName:          strings.Repeat("a", 300),
			UpstreamStatusCode:   500,
			UpstreamURL:          strings.Repeat("u", 3000),
			UpstreamResponseBody: `{"authorization":"Bearer secret","message":"` + strings.Repeat("x", 10_000) + `"}`,
			Message:              strings.Repeat("m", 3000),
			Detail:               `{"api_key":"secret","detail":"` + strings.Repeat("y", 10_000) + `"}`,
		})
	}

	if err := SanitizeOpsUpstreamErrorsForQueue(entry); err != nil {
		t.Fatal(err)
	}
	if entry.UpstreamErrors != nil {
		t.Fatal("raw upstream event slice must be released before queueing")
	}
	if entry.UpstreamErrorsJSON == nil {
		t.Fatal("sanitized upstream event JSON is missing")
	}
	events, err := ParseOpsUpstreamErrors(*entry.UpstreamErrorsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 16 {
		t.Fatalf("event count = %d, want 16", len(events))
	}
	for _, event := range events {
		if len(event.Platform) > 32 || len(event.AccountName) > 128 || len(event.UpstreamURL) > 2048 || len(event.Message) > 2048 {
			t.Fatalf("event fields were not bounded: %+v", event)
		}
		if len(event.UpstreamResponseBody) > OpsErrorLogQueueBodyMaxBytes || len(event.Detail) > OpsErrorLogQueueBodyMaxBytes {
			t.Fatal("event body/detail exceeded queue limit")
		}
		if strings.Contains(event.UpstreamResponseBody, "Bearer secret") || strings.Contains(event.Detail, `"secret"`) {
			t.Fatal("credential material was not redacted")
		}
	}
}

func TestSanitizeOpsRoutingDiagnosticsUsesOnlyBoundedVocabulary(t *testing.T) {
	entry := &OpsInsertErrorLogInput{
		RoutingDiagnostics: &OpsRoutingDiagnostics{
			SelectionDecision:      "no_available_account",
			SelectionLayer:         "load_balance",
			CandidatePool:          7,
			FilteredCandidates:     map[string]int{"runtime_blocked": 2},
			TransportFailure:       "connection_reset",
			TimeoutPhase:           "first_semantic_output_timeout",
			OutboundIdentitySource: "account",
		},
	}

	if err := SanitizeOpsUpstreamErrorsForQueue(entry); err != nil {
		t.Fatal(err)
	}
	require.NotNil(t, entry.RoutingDiagnosticsJSON)
	require.Nil(t, entry.RoutingDiagnostics)
	require.JSONEq(t, `{
  "selection_decision":"no_available_account",
  "selection_layer":"load_balance",
  "candidate_pool":7,
  "filtered_candidates":{"runtime_blocked":2},
  "transport_failure":"connection_reset",
  "timeout_phase":"first_semantic_output_timeout",
  "outbound_identity_source":"account"
}`, *entry.RoutingDiagnosticsJSON)
}

func TestSanitizeOpsRoutingDiagnosticsDropsArbitraryValues(t *testing.T) {
	entry := &OpsInsertErrorLogInput{
		RoutingDiagnostics: &OpsRoutingDiagnostics{
			SelectionDecision:      "https://user:secret@example.test/very/not-safe",
			SelectionLayer:         "too many words for a fixed diagnostic token",
			CandidatePool:          -1,
			FilteredCandidates:     map[string]int{"bad value with spaces": -3},
			TransportFailure:       "Bearer secret",
			TimeoutPhase:           "timeout phase with spaces",
			OutboundIdentitySource: "Codex CLI 0.1",
		},
	}

	if err := SanitizeOpsUpstreamErrorsForQueue(entry); err != nil {
		t.Fatal(err)
	}
	require.Nil(t, entry.RoutingDiagnosticsJSON)
	require.Nil(t, entry.RoutingDiagnostics)
}

func TestSanitizeOpsRoutingDiagnosticsKeepsValidReasonsAfterInvalidFlood(t *testing.T) {
	filtered := map[string]int{"runtime_blocked": 2}
	for i := 0; i < 32; i++ {
		filtered["untrusted_reason_"+string(rune('a'+i))] = 1
	}
	entry := &OpsInsertErrorLogInput{
		RoutingDiagnostics: &OpsRoutingDiagnostics{FilteredCandidates: filtered},
	}

	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	require.NotNil(t, entry.RoutingDiagnosticsJSON)
	require.JSONEq(t, `{"filtered_candidates":{"runtime_blocked":2}}`, *entry.RoutingDiagnosticsJSON)
}
