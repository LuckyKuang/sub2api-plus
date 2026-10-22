//go:build unit || !integration

package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func requireGrokIndependentConversation(t *testing.T, req *http.Request, cacheKey string) {
	t.Helper()
	conversation := req.Header.Get(grokConversationIDHeader)
	require.NotEqual(t, cacheKey, conversation, "conversation ownership is independent of model cache routing")
	id, err := uuid.Parse(conversation)
	require.NoError(t, err)
	require.Contains(t, []uuid.Version{4, 7}, id.Version(), "official sessions are UUIDv7 or resumed UUIDv4")
	require.Equal(t, conversation, req.Header.Get("X-Grok-Session-Id"), "these fixtures have no auxiliary conversation")
}
