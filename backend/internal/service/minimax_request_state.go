package service

import (
	"net/http"
	"strconv"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

type miniMaxRequestState struct{ session, offset string }

// MiniMax Code sets these for managed and BYOK model calls. The gateway owns
// the turn/session; never trust caller-supplied agent/session attribution.
func prepareMiniMaxRequestState(req *http.Request, account *Account) error {
	if req == nil || account == nil || account.Platform != PlatformMiniMax || outboundidentity.RequestProtocol(req) != APIProtocolAnthropic {
		return nil
	}
	identity, ok := outboundidentity.FromContext(req.Context())
	if !ok || identity.Preset != minimax.Preset && identity.Preset != minimax.APIKeyPreset {
		return nil
	}
	scope := outboundIdentityScopeFromContext(req.Context())
	if scope == nil {
		return nil
	}
	key := domesticCredentialOwnerScope(req.Context(), account)
	value, ok := scope.minimaxTurns.Load(key)
	if !ok {
		offset := identity.TimezoneOffset(time.Now())
		session, err := nativeConversationOwnerSession(req.Context(), account)
		if err != nil {
			return err
		}
		value, _ = scope.minimaxTurns.LoadOrStore(key, miniMaxRequestState{session: session, offset: strconv.Itoa(offset)})
	}
	state, ok := value.(miniMaxRequestState)
	if !ok {
		return nil
	}
	req.Header.Set("X-Mavis-Session-Id", state.session)
	req.Header.Set("X-Mavis-Agent-Id", "main")
	req.Header.Set("X-Mavis-Timezone-Offset", state.offset)
	return nil
}
