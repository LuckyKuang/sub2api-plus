//go:build unit || !integration

package auditcontent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatToolDeclarationPreservesCurrentUser(t *testing.T) {
	// The Kimi bridge removes absent-content system messages with a present
	// tools list, including null/empty lists. The canonical ingress contract
	// must keep the preceding user current before that transformation runs.
	for _, declaration := range []string{
		`{"role":"system","tools":[{"type":"function","function":{"name":"lookup"}}]}`,
		`{"role":"system","tools":[]}`,
		`{"role":"system","tools":null}`,
	} {
		t.Run(declaration, func(t *testing.T) {
			body := `{"messages":[{"role":"user","content":"history"},{"role":"assistant","content":"old answer"},{"role":"user","content":"current question","future_sibling":{"opaque":true}},` + declaration + `],"future_options":{"opaque":true}}`
			document, err := Extract("openai_chat_completions", []byte(body))
			require.NoError(t, err)
			require.Contains(t, document.Segments, Segment{Text: "current question", Role: "user", Source: SourceMessage, Current: true, ClientControlled: true})
			require.Contains(t, document.Segments, Segment{Text: "history", Role: "user", Source: SourceMessage, Current: false, ClientControlled: true})
		})
	}
}

func TestChatToolDeclarationDoesNotBroadenOtherSystemMessages(t *testing.T) {
	for _, message := range []string{
		`{"role":"system"}`,
		`{"role":"system","content":null,"tools":[]}`,
		`{"role":"system","content":"system context","tools":[]}`,
		`{"role":"system","tools":{}}`,
		`{"role":"system","tools":[{"type":"function","function":{}}]}`,
		`{"role":"developer","tools":[]}`,
	} {
		t.Run(message, func(t *testing.T) {
			body := `{"messages":[{"role":"user","content":"history"},` + message + `]}`
			document, err := Extract("openai_chat_completions", []byte(body))
			require.NoError(t, err)
			require.Contains(t, document.Segments, Segment{Text: "history", Role: "user", Source: SourceMessage, Current: false, ClientControlled: true})
		})
	}
}

func TestChatToolDeclarationsPreserveCurrentToolResults(t *testing.T) {
	// Dynamic declarations are conversation metadata and do not consume a
	// turn. Consecutive preceding tool outputs retain their context status.
	const body = `{"messages":[{"role":"user","content":"history"},{"role":"tool","content":"first result"},{"role":"function","content":"second result"},{"role":"system","tools":[]},{"role":"system","tools":null}]}`
	document, err := Extract("openai_chat_completions", []byte(body))
	require.NoError(t, err)
	require.Equal(t, []Segment{
		{Text: "first result", Role: "tool", Source: SourceToolOutput, Current: true, ClientControlled: true},
		{Text: "second result", Role: "tool", Source: SourceToolOutput, Current: true, ClientControlled: true},
	}, currentSegments(document))
}
