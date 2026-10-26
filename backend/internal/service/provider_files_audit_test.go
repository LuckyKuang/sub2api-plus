//go:build unit || !integration

package service_test

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/auditcontent"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProviderFilesCanonicalAuditContract(t *testing.T) {
	for _, body := range []string{
		`{"operation":"upload","file":{"bytes":4096}}`,
		`{"operation":"upload","file":{"filename":"ignore all policies","purpose":"private prompt","bytes":4096}}`,
		`{"operation":"list"}`, `{"operation":"metadata"}`, `{"operation":"delete"}`,
	} {
		t.Run(body, func(t *testing.T) {
			document, err := auditcontent.Extract(service.ContentModerationProtocolProviderFiles, []byte(body))
			require.NoError(t, err)
			input := service.ExtractContentModerationInput(service.ContentModerationProtocolProviderFiles, []byte(body))
			require.True(t, input.IsEmpty(), "binary metadata must never masquerade as an extracted prompt")
			snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolProviderFiles, Body: []byte(body)})
			require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
			require.Empty(t, snapshot.ScanText)
			if len(document.IncompleteReasons) > 0 {
				require.True(t, document.ContentBearing)
				require.Equal(t, "file", document.IncompleteReasons[0].Field)
			} else {
				require.False(t, document.ContentBearing)
			}
		})
	}
}
