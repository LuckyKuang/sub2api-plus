//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A task's independent route scope must also ignore an inherited attempt: the
// text conversation's acceptance deadline must not cancel a CLI execution.
// The handler suite independently proves that the public CLI wrapper applies
// this scope before selection, including when caller session headers are set.
func TestCodexCLITaskIgnoresInheritedConversationAcceptanceDeadline(t *testing.T) {
	store := newFakeOpenAISessionRouteStore()
	route, err := store.CreateIfAbsent(context.Background(), OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: "conversation", OwnerAccountID: 101})
	require.NoError(t, err)
	conversation := &openAISessionRouteAttempt{store: store, route: route, accountID: 102}
	inherited := context.WithValue(context.Background(), openAISessionRouteAttemptKey{}, conversation)
	taskContext := WithoutOpenAISessionRoute(WithCodexCLITask(inherited))
	ctx, finish, err := beginOpenAISessionRouteForward(taskContext, &Account{ID: 102}, 5*time.Millisecond)
	require.NoError(t, err)
	defer finish()
	select {
	case <-ctx.Done():
		t.Fatalf("independent CLI task inherited a conversation acceptance deadline: %v", context.Cause(ctx))
	case <-time.After(25 * time.Millisecond):
	}
	current, err := store.Get(context.Background(), "group:0", "conversation")
	require.NoError(t, err)
	require.EqualValues(t, 101, current.OwnerAccountID)
	require.Empty(t, current.LeaseID, "independent tasks cannot acquire a conversation migration lease")
	require.False(t, conversation.forwardStarted)
}
