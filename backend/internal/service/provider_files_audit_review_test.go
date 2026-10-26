//go:build unit || !integration

package service_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/auditcontent"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProviderFilesReviewInferenceAuditKeepsTextAndOpaqueReferencesSeparate(t *testing.T) {
	const localID = "file-s2a-11111111-1111-4111-8111-111111111111"
	const userText = "Inspect the attached image."
	const unknownText = "opaque sibling must never become a prompt"
	for _, protocol := range []struct {
		name, audit, body, textBlock string
	}{
		{"chat", service.ContentModerationProtocolOpenAIChat,
			`{"messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"%s"}},{"type":"text","text":"Inspect the attached image."}]}],"extension":{"text":"opaque sibling must never become a prompt"}}`,
			`,{"type":"text","text":"Inspect the attached image."}`},
		{"responses", service.ContentModerationProtocolOpenAIResponses,
			`{"input":[{"type":"message","role":"user","content":[{"type":"input_file","file_id":"%s"},{"type":"input_text","text":"Inspect the attached image."}]}],"extension":{"text":"opaque sibling must never become a prompt"}}`,
			`,{"type":"input_text","text":"Inspect the attached image."}`},
		{"messages", service.ContentModerationProtocolAnthropicMessages,
			`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"%s"}},{"type":"text","text":"Inspect the attached image."}]}],"extension":{"text":"opaque sibling must never become a prompt"}}`,
			`,{"type":"text","text":"Inspect the attached image."}`},
	} {
		for _, encoding := range []string{"literal", "escaped"} {
			t.Run(protocol.name+"/"+encoding, func(t *testing.T) {
				wireID := localID
				if encoding == "escaped" {
					wireID = strings.ReplaceAll(localID, "-", `\u002d`)
				}
				body := []byte(fmt.Sprintf(protocol.body, wireID))
				_, err := auditcontent.Extract(protocol.audit, body)
				require.NoError(t, err, "a valid opaque reference is incomplete content rather than an extraction failure")
				moderation := service.ExtractContentModerationInput(protocol.audit, body)
				require.Equal(t, userText, moderation.Text, "recognized current-user sibling text remains auditable")
				require.Empty(t, moderation.Images, "an unresolved local ID is not fetched or scanned as an image")
				snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: protocol.audit, Body: body})
				require.NoError(t, err, "opaque sibling content cannot turn the recognized user text into an audit-derived block")
				require.Equal(t, userText, snapshot.ScanText)
				require.Equal(t, 1, snapshot.MessageCount)
				require.NotContains(t, snapshot.ScanText, localID)
				require.NotContains(t, snapshot.ScanText, unknownText)

				// With only the opaque reference and extension left, both engines
				// have no auditable prompt. Incompleteness cannot invent text or
				// become a policy violation; the canonical result remains empty.
				opaqueBody := []byte(strings.Replace(string(body), protocol.textBlock, "", 1))
				_, err = auditcontent.Extract(protocol.audit, opaqueBody)
				require.NoError(t, err)
				require.True(t, service.ExtractContentModerationInput(protocol.audit, opaqueBody).IsEmpty())
				opaque, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: protocol.audit, Body: opaqueBody})
				require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
				require.Empty(t, opaque.ScanText)
			})
		}
	}
}
