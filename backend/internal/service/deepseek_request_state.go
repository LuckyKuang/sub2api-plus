package service

import (
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
)

const deepSeekAnonymousUserCredential = "deepseek_harness_user_id"

type deepSeekRequestState struct{ user, session string }

// Harness anonymous-user-id/src/index.ts persists an anonymous UUID rather
// than sending the upstream account ID, caller identity or credential material.
func normalizeDeepSeekAnonymousUser(platform string, credentials map[string]any) {
	if platform != PlatformDeepseek || credentials == nil {
		return
	}
	raw, _ := credentials[deepSeekAnonymousUserCredential].(string)
	if id, err := uuid.Parse(raw); err != nil || id.Version() != 4 || id.String() != raw {
		credentials[deepSeekAnonymousUserCredential] = uuid.NewString()
	}
}

// Harness llm-deepseek/adapter.ts owns these per-request declarations for both
// account and API-key providers. They never participate in identity selection.
func prepareDeepSeekRequestState(req *http.Request, account *Account) error {
	if req == nil || account == nil || account.Platform != PlatformDeepseek {
		return nil
	}
	for _, name := range []string{"X-Deepseek-Harness-User-Id", "X-Deepseek-Harness-Session-Id", "X-Deepseek-Harness-Compact"} {
		deleteHeaderAllForms(req.Header, name)
	}
	if outboundidentity.RequestProtocol(req) != APIProtocolAnthropic {
		return nil
	}
	scope := outboundIdentityScopeFromContext(req.Context())
	if scope == nil {
		return nil
	}
	key := domesticCredentialOwnerScope(req.Context(), account)
	value, ok := scope.deepseekTurns.Load(key)
	if !ok {
		user := domesticCredentialOwnerAccount(req.Context(), account).GetCredential(deepSeekAnonymousUserCredential)
		if id, err := uuid.Parse(user); err != nil || id.Version() != 4 || id.String() != user {
			user = uuid.NewString()
		}
		session, err := nativeConversationOwnerSession(req.Context(), account)
		if err != nil {
			return err
		}
		value, _ = scope.deepseekTurns.LoadOrStore(key, deepSeekRequestState{user: user, session: session})
	}
	state, valid := value.(deepSeekRequestState)
	if !valid {
		return nil
	}
	req.Header.Set("X-Deepseek-Harness-User-Id", state.user)
	req.Header.Set("X-Deepseek-Harness-Session-Id", state.session)
	// No native compaction operation exists in this gateway. A caller cannot
	// label an ordinary request as compaction by supplying the optional header.
	return nil
}
