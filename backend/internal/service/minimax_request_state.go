package service

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

type miniMaxRequestState struct{ session, offset string }

// MiniMax Code sets these for managed and BYOK model calls. The gateway owns
// the turn/session; never trust caller-supplied agent/session attribution.
func prepareMiniMaxRequestState(req *http.Request, account *Account) {
	if account == nil || account.Platform != PlatformMiniMax || outboundidentity.RequestProtocol(req) != APIProtocolAnthropic {
		return
	}
	identity, ok := outboundidentity.FromContext(req.Context())
	if !ok || identity.Preset != minimax.Preset && identity.Preset != minimax.APIKeyPreset {
		return
	}
	scope := outboundIdentityScopeFromContext(req.Context())
	key := outboundIdentityOwnerKey(account)
	value, ok := scope.minimaxTurns.Load(key)
	if !ok {
		_, offset := time.Now().Zone()
		value, _ = scope.minimaxTurns.LoadOrStore(key, miniMaxRequestState{session: uuid.NewString(), offset: strconv.Itoa(offset)})
	}
	state, ok := value.(miniMaxRequestState)
	if !ok {
		return
	}
	req.Header.Set("X-Mavis-Session-Id", state.session)
	req.Header.Set("X-Mavis-Agent-Id", "main")
	req.Header.Set("X-Mavis-Timezone-Offset", state.offset)
}
