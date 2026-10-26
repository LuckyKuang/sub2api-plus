//go:build unit || !integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderFilesModerationAllowsBinaryAndLogsSafeDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := NewContentModerationService(&contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw)}}, &contentModerationTestRepo{}, nil, nil, nil, nil, nil, nil)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{RequestID: "req-files-moderation", Endpoint: "/v1/files", Stage: "http", Protocol: ContentModerationProtocolProviderFiles, Body: []byte(`{"operation":"upload","file":{"bytes":4096,"filename":"RAW_FILENAME_CANARY","purpose":"RAW_PURPOSE_CANARY"}}`)})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	for _, value := range []string{"req-files-moderation", "/v1/files", "provider_files", "http", "incomplete_content", "unextractable_content", "body_bytes", `"file_bytes":4096`} {
		require.Contains(t, output.String(), value)
	}
	require.NotContains(t, output.String(), "RAW_FILENAME_CANARY")
	require.NotContains(t, output.String(), "RAW_PURPOSE_CANARY")
}
