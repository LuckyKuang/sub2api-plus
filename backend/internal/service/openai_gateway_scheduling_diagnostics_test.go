//go:build unit

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsRoutingDiagnosticsFromSelectionErrorUsesTypedData(t *testing.T) {
	err := noAvailableOpenAISelectionErrorWithStats(
		"gpt-5.2",
		false,
		openAISelectionFilterStats{
			pool:    4,
			reasons: map[string]int{"runtime_blocked": 2, "model_not_supported": 1},
		},
		"",
	)

	diagnostics := OpsRoutingDiagnosticsFromSelectionError(err)
	require.NotNil(t, diagnostics)
	require.Equal(t, "no_available_account", diagnostics.SelectionDecision)
	require.Equal(t, "load_balance", diagnostics.SelectionLayer)
	require.Equal(t, 4, diagnostics.CandidatePool)
	require.Equal(t, map[string]int{"runtime_blocked": 2, "model_not_supported": 1}, diagnostics.FilteredCandidates)

	diagnostics.FilteredCandidates["runtime_blocked"] = 99
	again := OpsRoutingDiagnosticsFromSelectionError(err)
	require.Equal(t, 2, again.FilteredCandidates["runtime_blocked"], "returned diagnostics must not mutate the typed scheduler error")
}

func TestOpsRoutingDiagnosticsFromSelectionErrorIgnoresUntypedNoAvailableError(t *testing.T) {
	require.Nil(t, OpsRoutingDiagnosticsFromSelectionError(errors.New("no available OpenAI accounts (pool=secret)")))
}
