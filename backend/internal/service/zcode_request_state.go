package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/google/uuid"
	"github.com/tidwall/sjson"
)

type zcodeRequestState struct{ trace, session, query, device string }

// Each physical attempt receives a fresh request ID; the owner-scoped logical
// request keeps its trace and canonical session. No inbound attribution chooses
// a client family, device identity or agent role.
func prepareZCodeRequestState(req *http.Request, account *Account) error {
	if req == nil || account == nil || account.Platform != PlatformZhipu {
		return nil
	}
	identity, ok := outboundidentity.FromContext(req.Context())
	if !ok || identity.Preset != zcode.Preset {
		return nil
	}
	identity, err := zcode.EnsureDeviceIdentity(req.Context(), identity)
	if err != nil {
		return err
	}
	protocol := outboundidentity.RequestProtocol(req)
	if protocol == "" {
		*req = *req.WithContext(outboundidentity.WithIdentity(req.Context(), zcode.ControlIdentity(identity)))
		outboundidentity.ApplyContext(req)
		deleteHeaderAllForms(req.Header, "x-request-id")
		req.Header.Set("x-request-id", uuid.NewString())
		return nil
	}
	identity = zcode.ModelIdentity(identity)
	*req = *req.WithContext(outboundidentity.WithIdentity(req.Context(), identity))
	outboundidentity.ApplyContext(req)
	for _, name := range []string{"x-request-id", "x-zcode-trace-id", "x-zcode-session-type", "x-session-id", "x-query-id", "x-zcode-query-source"} {
		deleteHeaderAllForms(req.Header, name)
	}
	state := zcodeRequestState{trace: uuid.NewString(), device: identity.ControlHeaders[zcode.HeaderDeviceMid]}
	scope := outboundIdentityScopeFromContext(req.Context())
	if scope != nil {
		key := domesticCredentialOwnerScope(req.Context(), account)
		if existing, found := scope.zcodeTurns.Load(key); found {
			state, ok = existing.(zcodeRequestState)
			if !ok {
				return fmt.Errorf("ZCode request state is invalid")
			}
		} else {
			if scope.conversation != "" {
				state.session, err = nativeConversationOwnerSession(req.Context(), account)
				if err != nil {
					return err
				}
			}
			if scope.zcodeQuery != "" {
				state.query, err = resolveOutboundMappedID(req.Context(), "zcode-query", key+"\x00"+scope.zcodeQuery, 4)
				if err != nil {
					return err
				}
			}
			actual, _ := scope.zcodeTurns.LoadOrStore(key, state)
			state, ok = actual.(zcodeRequestState)
			if !ok {
				return fmt.Errorf("ZCode request state is invalid")
			}
		}
	}
	req.Header.Set("x-request-id", uuid.NewString())
	req.Header.Set("x-zcode-trace-id", state.trace)
	req.Header.Set("x-zcode-session-type", "other")
	if state.session != "" {
		req.Header.Set("x-session-id", state.session)
	}
	if state.query != "" {
		req.Header.Set("x-query-id", state.query)
	}
	if protocol != APIProtocolAnthropic {
		return nil
	}
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	body := domesticRequestBody(req)
	if len(body) == 0 || !json.Valid(body) {
		return fmt.Errorf("ZCode Anthropic request body cannot be prepared")
	}
	metadata, err := json.Marshal(struct {
		Device  string `json:"device_id"`
		Account string `json:"account_uuid"`
		Session string `json:"session_id"`
	}{state.device, "", state.session})
	if err != nil {
		return err
	}
	body, err = sjson.SetBytes(body, "metadata.user_id", string(metadata))
	if err != nil {
		return err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	deleteHeaderAllForms(req.Header, "Content-Length")
	return nil
}
