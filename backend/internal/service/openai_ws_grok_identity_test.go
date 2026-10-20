//go:build unit

package service

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func grokWSIdentityFixture() outboundidentity.Identity {
	return outboundidentity.Identity{
		Preset: "grok", AccountID: 91, Source: "account",
		UserAgent: "grok-shell/3.9.1 (linux; x86_64)", Originator: "grok-shell", Version: "3.9.1",
		Headers: map[string]string{"x-grok-client-identifier": "grok-shell", "x-grok-client-version": "3.9.1", "x-grok-client-mode": "headless"},
	}
}

func TestCoderWebSocketGrokIdentityRendersEveryDestination(t *testing.T) {
	for _, tc := range []struct {
		name, host, mode string
		redirect         bool
	}{
		{name: "native-proxy", host: "cli-chat-proxy.grok.com", mode: "headless"},
		{name: "native-auth", host: "auth.x.ai", mode: "headless"},
		{name: "native-accounts", host: "accounts.x.ai", mode: "headless"},
		{name: "public-api", host: "api.x.ai"},
		{name: "regional-api", host: "eu-west-1.api.x.ai"},
		{name: "compatible", host: "supplier.example"},
		{name: "lookalike", host: "cli-chat-proxy.grok.com.supplier.example"},
		{name: "redirect-from-native", host: "cli-chat-proxy.grok.com", redirect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := make(chan http.Header, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- r.Header.Clone()
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "http://supplier.example/v1/responses", http.StatusTemporaryRedirect)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			// Map the destination socket to loopback while preserving its URL
			// and Host, so the real handshake exercises destination policy.
			const proxy = "http://identity-test-proxy.invalid"
			dialer := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
			dialer.proxyClients[proxy] = &openAIWSProxyClientEntry{client: &http.Client{Transport: transport}}
			identity := grokWSIdentityFixture()
			ctx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), identity), 3*time.Second)
			defer cancel()
			headers := make(http.Header)
			identity.ForProtocol("responses").Apply(headers)
			headers.Set("Authorization", "Bearer test-key")
			path := "/v1/responses"
			if tc.redirect {
				path = "/redirect"
			}
			conn, _, _, err := dialer.Dial(ctx, "ws://"+tc.host+path, headers, proxy)
			require.NoError(t, err)
			defer func() { _ = conn.Close() }()
			if tc.redirect {
				initial := <-observed
				require.Equal(t, "headless", initial.Get("X-Grok-Client-Mode"), "the initial native destination receives its mode")
				require.Equal(t, "grok-shell/3.9.1 (linux; x86_64)", initial.Get("User-Agent"))
			}
			sent := <-observed
			require.Equal(t, tc.mode, sent.Get("X-Grok-Client-Mode"))
			require.Equal(t, "grok-shell/3.9.1 (linux; x86_64)", sent.Get("User-Agent"))
			require.Equal(t, "grok-shell", sent.Get("X-Grok-Client-Identifier"))
			require.Equal(t, "3.9.1", sent.Get("X-Grok-Client-Version"))
			require.Equal(t, "headless", headers.Get("X-Grok-Client-Mode"), "destination rendering must not mutate caller headers")
			snapshot, ok := outboundidentity.FromContext(ctx)
			require.True(t, ok)
			require.Equal(t, "headless", snapshot.Headers["x-grok-client-mode"])
			require.Equal(t, "account", snapshot.Source)
			require.Equal(t, int64(91), snapshot.AccountID)
		})
	}
}

func TestOpenAIWSConnPoolGrokPrewarmRetainsOwnerSnapshot(t *testing.T) {
	observed := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	pool := newOpenAIWSConnPool(cfg)
	defer pool.Close()
	identity := grokWSIdentityFixture()
	ctx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), identity), 3*time.Second)
	defer cancel()
	headers := make(http.Header)
	identity.Apply(headers)
	account := &Account{ID: 91, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"},
	}}
	lease, err := pool.Acquire(ctx, openAIWSAcquireRequest{Account: account, WSURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses", Headers: headers})
	require.NoError(t, err)
	defer lease.Release()
	ap, ok := pool.getAccountPool(account.ID)
	require.True(t, ok)
	ap.mu.Lock()
	remembered := cloneOpenAIWSAcquireRequest(*ap.lastAcquire)
	ap.creating++
	generation := ap.generation
	ap.mu.Unlock()
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.2"}
	identity.Headers["x-grok-client-version"] = "3.9.2"
	// prewarmConns creates its own background context after the request ends.
	cancel()
	pool.prewarmConns(account.ID, remembered, 1, generation)
	require.Len(t, observed, 2, "both request and background prewarm must complete a real handshake")
	for range 2 {
		sent := <-observed
		require.Empty(t, sent.Get("X-Grok-Client-Mode"))
		require.Equal(t, "grok-shell/3.9.1 (linux; x86_64)", sent.Get("User-Agent"))
		require.Equal(t, "grok-shell", sent.Get("X-Grok-Client-Identifier"))
		require.Equal(t, "3.9.1", sent.Get("X-Grok-Client-Version"))
	}
}

func TestCoderWebSocketGrokRendererPreservesOtherSignedDeclarations(t *testing.T) {
	observed := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), outboundidentity.Identity{
		Preset: "codex", UserAgent: "codex_cli_rs/0.163.0", Originator: "codex_cli_rs", Version: "0.163.0",
	}), 3*time.Second)
	defer cancel()
	headers := http.Header{"User-Agent": {"signed-client/4.2.0"}, "Authorization": {`Signature keyId="test",headers="user-agent",signature="test-signature"`}}
	conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", headers, "")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	sent := <-observed
	require.Equal(t, "signed-client/4.2.0", sent.Get("User-Agent"))
	require.Equal(t, headers.Get("Authorization"), sent.Get("Authorization"))
}
