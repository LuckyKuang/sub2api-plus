//go:build unit || !integration

package securityaudit

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderFilesPromptAuditAllowsBinaryAndLogsSafeDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	metrics := NewAtomicMetrics()
	svc := &PromptService{config: &fakeConfigStore{active: true, cfg: ActiveConfig{RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true, Scanners: AllScannerIDs, Endpoints: []ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}}}}, metrics: metrics,
		evaluator: newGuardEvaluator(PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
			t.Fatal("binary file metadata must never be sent to the prompt scanner")
			return nil, nil
		}), nil, metrics, 1, 1)}
	decision, err := svc.Evaluate(context.Background(), Request{RequestID: "req-files-prompt", Endpoint: "/v1/files", Stage: "http", Protocol: "provider_files", Body: []byte(`{"operation":"upload","file":{"bytes":4096,"filename":"RAW_FILENAME_CANARY","purpose":"RAW_PURPOSE_CANARY"}}`)})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, decision.Kind)
	require.True(t, decision.AllowNextStage)
	for _, value := range []string{"req-files-prompt", "/v1/files", "provider_files", "http", "incomplete_content", "unextractable_content", "body_bytes", `"file_bytes":4096`} {
		require.Contains(t, output.String(), value)
	}
	require.NotContains(t, output.String(), "RAW_FILENAME_CANARY")
	require.NotContains(t, output.String(), "RAW_PURPOSE_CANARY")
	require.Equal(t, AuditMetricsSnapshot{ExtractionAttempted: 1, ExtractionFailed: 1}, metrics.AuditSnapshot())
}
