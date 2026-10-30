//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Duplicate-key interpretation must never let the auditor inspect one input
// while the task executor receives another.
func TestCodexCLITaskRejectsAmbiguousAuditAndExecutionFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.1","input":"audited","input":"different execution"}`,
		`{"model":"gpt-5.1","model":"different-model","input":"task"}`,
		`{"model":"gpt-5.1","input":"task","reasoning":{"effort":"low","effort":"high"}}`,
		`{"model":"gpt-5.1","input":"task"} {"model":"other","input":"other"}`,
	} {
		_, err := ParseCodexCLITaskRequest([]byte(body))
		require.Error(t, err)
	}
}
