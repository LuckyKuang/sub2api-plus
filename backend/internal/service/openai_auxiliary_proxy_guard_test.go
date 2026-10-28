//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchMissingProxyStopsBeforeWhoamiAndInference(t *testing.T) {
	var whoami atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		whoami.Add(1)
		_, _ = io.WriteString(w, `{"chatgpt_account_id":"discovered-owner","chatgpt_user_id":"discovered-user"}`)
	}))
	defer server.Close()
	previous := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	defer func() { openAICodexPATWhoamiURL = previous }()
	upstream := &httpUpstreamRecorder{}
	s := newOpenAIRejectedFieldTestService(upstream)
	s.openAITokenProvider = NewOpenAITokenProvider(nil, nil, NewOpenAIOAuthService(nil, nil))
	id := int64(19)
	account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id,
		Credentials: map[string]any{"access_token": "at-proxy-regression", "auth_mode": OpenAIAuthModePersonalAccessToken}}
	body := []byte(`{"model":"gpt-5.6-sol","commands":{"search_query":[{"q":"test"}]}}`)
	c := newOpenAIRejectedFieldTestContext(body)
	_, err := s.ForwardAlphaSearch(context.Background(), c, account, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy")
	require.Zero(t, whoami.Load(), "PAT metadata discovery must not leak onto direct egress before inference")
	require.Nil(t, upstream.lastReq)
}

func TestLiveSidebandMissingProxyNeverReachesDialer(t *testing.T) {
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-live-proxy-regression"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"Location": []string{"/backend-api/codex/call_strict_proxy"}}, Body: io.NopCloser(strings.NewReader("sdp"))}}
	s := newOpenAIRejectedFieldTestService(upstream)
	s.liveAttestationCipher = newLiveAttestationCipher(cfg)
	s.cfg.Gateway.OpenAIWS.Enabled, s.cfg.Gateway.OpenAIWS.OAuthEnabled = true, true
	account := turnStateAccount(71, "principal-live-proxy")
	created, err := s.createUpstreamLiveCall(context.Background(), account, &LiveCallRequest{SDP: "offer", Session: json.RawMessage(`{"type":"realtime"}`)}, "proof")
	require.NoError(t, err)
	cipher, err := s.liveAttestationCipher.Encrypt("proof")
	require.NoError(t, err)
	record := &LiveCallRecord{AccountID: account.ID, CallID: created.CallID, OutboundSnapshot: created.OutboundSnapshot, AttestationCiphertext: cipher}
	id := int64(19)
	account.ProxyID = &id
	headers, route, err := s.liveSidebandHeaders(context.Background(), account, record)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy")
	require.Nil(t, headers, "a sideband dial cannot receive credentials with an empty/direct route")
	require.Empty(t, route)
}
