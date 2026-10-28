//go:build unit || !integration

package auditcontent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexNamedToolOutputCanonicalContent(t *testing.T) {
	// Real producer: openai/codex 6d8f311b00e823052622eac2b463919ad86a666d,
	// context-fragments/src/additional_context.rs. Labels remain metadata.
	for _, protocol := range []string{"openai_responses", "responses_websocket", "openai_live"} {
		for _, namespace := range []string{"agent", "slack"} {
			t.Run(protocol+"/"+namespace, func(t *testing.T) {
				item := `{"type":"function_call_output","name":"notifications","namespace":"` + namespace + `","output":"Alice mentioned you."}`
				body := `{"input":[` + item + `,{"type":"message","role":"user","content":"audit this user text"}]}`
				if protocol == "openai_live" {
					body = `{"type":"conversation.item.create","item":` + item + `}`
				}
				doc, err := Extract(protocol, []byte(body))
				require.NoError(t, err)
				require.False(t, doc.Incomplete, "recognized source labels are metadata")
				require.Contains(t, doc.Segments, Segment{Text: "Alice mentioned you.", Role: "tool", Source: SourceToolOutput, Current: protocol == "openai_live", ClientControlled: true})
				if protocol != "openai_live" {
					require.Contains(t, doc.Segments, Segment{Text: "audit this user text", Role: "user", Source: SourceMessage, Current: true, ClientControlled: true})
				}
			})
		}
	}
}
