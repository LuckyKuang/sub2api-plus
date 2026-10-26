//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLiveCreateSidebandAndReconnectReuseCallIdentity(t *testing.T) {
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-only-live-cipher-key"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 201, Header: http.Header{"Location": []string{"https://chatgpt.com/backend-api/codex/call_123"}}, Body: io.NopCloser(strings.NewReader("sdp-answer"))}}
	svc := newOpenAIRejectedFieldTestService(upstream)
	svc.liveAttestationCipher = newLiveAttestationCipher(cfg)
	account := turnStateAccount(12, "principal-a")
	account.Credentials["user_agent"] = "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color"
	created, err := svc.createUpstreamLiveCall(context.Background(), account, &LiveCallRequest{SDP: "offer", Session: json.RawMessage(`{"type":"realtime"}`)}, "proof")
	require.NoError(t, err)
	require.NotNil(t, created.OutboundSnapshot)
	original := upstream.lastReq.Header.Clone()
	require.Equal(t, uuid.Version(7), uuid.MustParse(original.Get("session-id")).Version())
	require.Equal(t, uuid.Version(7), uuid.MustParse(original.Get("thread-id")).Version())
	ciphertext, err := svc.liveAttestationCipher.Encrypt("proof")
	require.NoError(t, err)
	record := &LiveCallRecord{AccountID: 12, CallID: created.CallID, OutboundSnapshot: created.OutboundSnapshot, AttestationCiphertext: ciphertext}
	// Controller can move to a different replica and account settings can change.
	second := &OpenAIGatewayService{liveAttestationCipher: newLiveAttestationCipher(cfg)}
	refreshed := turnStateAccount(12, "principal-a")
	refreshed.Credentials["access_token"] = "rotated-token"
	refreshed.Credentials["user_agent"] = "codex_vscode/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color"
	for i := 0; i < 2; i++ {
		headers, _, err := second.liveSidebandHeaders(context.Background(), refreshed, record)
		require.NoError(t, err)
		for _, name := range []string{"User-Agent", "Originator", "Version", "session-id", "thread-id", "OpenAI-Alpha"} {
			require.Equal(t, original.Get(name), headers.Get(name), name)
		}
		require.Equal(t, "Bearer rotated-token", headers.Get("Authorization"))
	}
	_, _, err = second.liveSidebandHeaders(context.Background(), turnStateAccount(12, "principal-b"), record)
	require.ErrorIs(t, err, ErrLiveIdentityMismatch)
	record.OutboundSnapshot = nil
	_, _, err = second.liveSidebandHeaders(context.Background(), refreshed, record)
	require.ErrorIs(t, err, ErrLiveIdentityMismatch, "old call cannot reconstruct its creation headers")
}

type liveAcquiredOwnerTokenCache struct {
	OpenAITokenCache
	acquire func(context.Context) string
}

func (c *liveAcquiredOwnerTokenCache) GetAccessToken(ctx context.Context, _ string) (string, error) {
	return c.acquire(ctx), nil
}

type liveAcquiredOwnerAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *liveAcquiredOwnerAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], nil
}

type liveAcquiredOwnerHTTPUpstream struct {
	accountID int64
}

func (u *liveAcquiredOwnerHTTPUpstream) Do(request *http.Request, proxyURL string, accountID int64, _ int) (*http.Response, error) {
	u.accountID = accountID
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	return (&http.Client{Transport: transport}).Do(request)
}

func (u *liveAcquiredOwnerHTTPUpstream) DoWithTLS(request *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxyURL, accountID, concurrency)
}

// Real HTTP and WS handshakes must use the acquired grant's proxy, principal
// and client family. A token-cache callback relinks the selected parent after
// recording acquisition, reproducing the interleaving without a data race.
func TestLiveAcquiredOwnerIdentityAndProxySurviveSelectedRowRelink(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		t.Run(map[bool]string{false: "account", true: "shadow"}[shadow], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			captured := make(chan http.Header, 3)
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				if r.URL.Path == "/calls" {
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Location", "/backend-api/codex/call_acquired_owner")
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, "sdp-answer")
					return
				}
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, _, _ = conn.Read(ctx)
			}))
			defer proxyServer.Close()
			var replacementRequests atomic.Int32
			replacementProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				replacementRequests.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer replacementProxy.Close()
			makeProxy := func(raw string, id int64) *Proxy {
				parsed, err := url.Parse(raw)
				require.NoError(t, err)
				port, err := strconv.Atoi(parsed.Port())
				require.NoError(t, err)
				return &Proxy{ID: id, Protocol: "http", Host: parsed.Hostname(), Port: port}
			}
			oldProxy, newProxy := makeProxy(proxyServer.URL, 1), makeProxy(replacementProxy.URL, 2)
			newOwner := func(token, principal string, proxy *Proxy) *Account {
				return &Account{ID: 81, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					ProxyID: &proxy.ID, Proxy: proxy,
					Credentials: map[string]any{"access_token": token, "chatgpt_account_id": principal, "chatgpt_user_id": "same-user",
						"user_agent": "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color"}}
			}
			oldCallsURL, oldSidebandURL := chatGPTLiveCallsURL, chatGPTLiveSidebandBaseURL
			chatGPTLiveCallsURL, chatGPTLiveSidebandBaseURL = "http://live-wire.invalid/calls", "ws://live-wire.invalid/sideband"
			defer func() { chatGPTLiveCallsURL, chatGPTLiveSidebandBaseURL = oldCallsURL, oldSidebandURL }()
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.OAuthEnabled = true, true
			cfg.JWT.Secret = "unit-only-live-owner-cipher-key"
			upstream := &liveAcquiredOwnerHTTPUpstream{}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, liveAttestationCipher: newLiveAttestationCipher(cfg)}
			setAcquisition := func(token string, relink bool) *Account {
				owner := newOwner(token, "old-principal", oldProxy)
				if !relink {
					owner = newOwner(token, "replacement-principal", newProxy)
				}
				selected := owner
				if shadow {
					selected = &Account{ID: 82, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID,
						ProxyID: &newProxy.ID, Proxy: newProxy}
				}
				svc.accountRepo = &liveAcquiredOwnerAccountRepo{accounts: map[int64]*Account{owner.ID: owner, selected.ID: selected}}
				svc.openAITokenProvider = NewOpenAITokenProvider(nil, &liveAcquiredOwnerTokenCache{acquire: func(ctx context.Context) string {
					recordRequestCredentialOwner(ctx, owner, token)
					if relink {
						replacement := newOwner("replacement-token", "replacement-principal", newProxy)
						replacement.Credentials["user_agent"] = "codex_vscode/0.164.0 (Windows 11; aarch64) WindowsTerminal"
						owner.Credentials, owner.ProxyID, owner.Proxy = replacement.Credentials, replacement.ProxyID, replacement.Proxy
					}
					return token
				}}, nil)
				return selected
			}
			selected := setAcquisition("creation-token", true)
			created, err := svc.createUpstreamLiveCall(ctx, selected, &LiveCallRequest{SDP: "offer", Session: json.RawMessage(`{"type":"realtime"}`)}, "proof")
			require.NoError(t, err)
			require.Equal(t, selected.ID, upstream.accountID, "resource accounting retains the selected row")
			creation := <-captured
			require.Equal(t, "Bearer creation-token", creation.Get("Authorization"))
			require.Equal(t, "old-principal", creation.Get("ChatGPT-Account-Id"))
			require.Equal(t, "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color", creation.Get("User-Agent"))
			require.Equal(t, "codex_cli_rs", creation.Get("Originator"))
			require.Equal(t, "0.164.0", creation.Get("Version"))
			encrypted, err := svc.liveAttestationCipher.Encrypt("proof")
			require.NoError(t, err)
			record := &LiveCallRecord{CallID: created.CallID, AccountID: selected.ID, OutboundSnapshot: created.OutboundSnapshot, AttestationCiphertext: encrypted}
			for _, token := range []string{"refreshed-token-1", "refreshed-token-2"} {
				setAcquisition(token, true)
				conn, err := svc.dialLiveSideband(ctx, record)
				require.NoError(t, err)
				require.NoError(t, conn.Close())
				reconnected := <-captured
				require.Equal(t, "Bearer "+token, reconnected.Get("Authorization"))
				for _, name := range []string{"ChatGPT-Account-Id", "User-Agent", "Originator", "Version", "session-id", "thread-id"} {
					require.Equal(t, creation.Get(name), reconnected.Get(name), name)
				}
			}
			setAcquisition("replacement-token", false)
			_, err = svc.dialLiveSideband(ctx, record)
			require.ErrorIs(t, err, ErrLiveIdentityMismatch)
			require.Zero(t, replacementRequests.Load(), "a relink cannot transmit the old call through the replacement proxy")
			require.Empty(t, captured, "a changed principal fails before any sideband handshake")
		})
	}
}
