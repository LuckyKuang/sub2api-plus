//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var openAIStrictPreflightProxyModes = []string{
	"valid-bound", "explicitly-unbound", "missing-bound-snapshot", "mismatched-bound-owner", "malformed-bound-host", "incomplete-bound-auth",
}

func setOpenAIStrictPreflightProxyMode(account *Account, mode string) {
	switch mode {
	case "explicitly-unbound":
		account.ProxyID = nil // A residual hydrated object does not authorize a proxy.
	case "missing-bound-snapshot":
		account.Proxy = nil
	case "mismatched-bound-owner":
		account.Proxy.ID++
	case "malformed-bound-host":
		account.Proxy.Host = "invalid proxy host"
	case "incomplete-bound-auth":
		account.Proxy.Password = ""
	}
}

// PAT metadata completion is a provider request before the inference helper.
// Broken bindings must stop it before whoami, metadata mutation or inference;
// a transport check reached only after whoami is already too late.
func TestAlphaSearchPATMetadataRejectsBrokenProxyBeforeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			var directValidations, proxyValidations atomic.Int64
			var wireMu sync.Mutex
			var validationHeaders http.Header
			var proxyAuth string
			respond := func(w http.ResponseWriter, r *http.Request) {
				wireMu.Lock()
				validationHeaders = r.Header.Clone()
				proxyAuth = r.Header.Get("Proxy-Authorization")
				wireMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Connection", "close")
				_, _ = io.WriteString(w, `{"email":"owner@example.test","chatgpt_user_id":"owner-user","chatgpt_account_id":"owner-account","chatgpt_plan_type":"plus","chatgpt_account_is_fedramp":false}`)
			}
			whoami := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				directValidations.Add(1)
				respond(w, r)
			}))
			t.Cleanup(whoami.Close)
			previousWhoamiURL := openAICodexPATWhoamiURL
			openAICodexPATWhoamiURL = whoami.URL
			t.Cleanup(func() { openAICodexPATWhoamiURL = previousWhoamiURL })
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyValidations.Add(1)
				respond(w, r)
			}))
			t.Cleanup(proxy.Close)
			parsed, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(parsed.Port())
			require.NoError(t, err)
			proxyID := int64(7911)
			account := &Account{
				ID: 7912, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
				Credentials: map[string]any{"access_token": "at-private-owner-token", "auth_mode": OpenAIAuthModePersonalAccessToken},
				ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: parsed.Hostname(), Port: port,
					Username: "private-proxy-user", Password: "private-proxy-password", Status: StatusActive},
			}
			setOpenAIStrictPreflightProxyMode(account, mode)
			originalCredentials, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(alphaSearchResponsesSSE("trusted search result"))),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream,
				openAITokenProvider: NewOpenAITokenProvider(nil, nil, NewOpenAIOAuthService(nil, nil))}
			body := []byte(`{"id":"strict-proxy-search","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"business requirement"}]}}`)
			inbound, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", bytes.NewReader(body)).WithContext(ctx)
			inbound.Request.Header.Set("Content-Type", "application/json")
			result, forwardErr := svc.ForwardAlphaSearch(ctx, inbound, account, body)
			if mode == "valid-bound" || mode == "explicitly-unbound" {
				require.NoError(t, forwardErr)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				require.Empty(t, account.GetChatGPTAccountID(), "metadata enrichment must not mutate the selected account; the captured sender owns it")
				require.Equal(t, "owner-account", upstream.lastReq.Header.Get("ChatGPT-Account-ID"))
				wireMu.Lock()
				headers, auth := validationHeaders.Clone(), proxyAuth
				wireMu.Unlock()
				require.Equal(t, "Bearer at-private-owner-token", headers.Get("Authorization"))
				require.Equal(t, DefaultOpenAICodexUserAgent, headers.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
				require.Empty(t, headers.Get("Version"), "official whoami is an auth-plane request")
				if mode == "valid-bound" {
					require.Equal(t, int64(1), proxyValidations.Load())
					require.Zero(t, directValidations.Load())
					require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("private-proxy-user:private-proxy-password")), auth)
					require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
				} else {
					require.Equal(t, int64(1), directValidations.Load())
					require.Zero(t, proxyValidations.Load())
					require.Empty(t, upstream.lastProxyURL)
				}
				return
			}
			require.Zero(t, directValidations.Load(), "reject the binding before the PAT metadata request can dial directly")
			require.Zero(t, proxyValidations.Load(), "a mismatched or incomplete proxy cannot receive the PAT")
			require.Empty(t, upstream.requests)
			require.Error(t, forwardErr)
			require.Nil(t, result)
			afterCredentials, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			require.JSONEq(t, string(originalCredentials), string(afterCredentials), "rejected preflight cannot mutate account metadata")
			for _, secret := range []string{"at-private-owner-token", "private-proxy-user", "private-proxy-password"} {
				require.NotContains(t, forwardErr.Error(), secret)
			}
		})
	}
}

type liveStrictProxyDialer struct {
	conn    *liveTestFrameConn
	calls   int
	url     string
	proxy   string
	headers http.Header
}

func (d *liveStrictProxyDialer) Dial(_ context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.calls++
	d.url, d.proxy, d.headers = target, proxy, headers.Clone()
	return d.conn, http.StatusSwitchingProtocols, nil, nil
}

// A saved Live call reuses the account and encrypted attestation. Invalid route
// hydration must fail before dialing, without changing the call/lease record.
func TestLiveSidebandRejectsBrokenProxyBeforeDial(t *testing.T) {
	for _, mode := range openAIStrictPreflightProxyModes {
		t.Run(mode, func(t *testing.T) {
			proxyID := int64(7921)
			account := &Account{ID: 7922, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 2,
				Credentials: map[string]any{"access_token": "private-live-access-token", "chatgpt_account_id": "live-owner-account"},
				ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "owner.proxy.example", Port: 8080,
					Username: "private-proxy-user", Password: "private-proxy-password", Status: StatusActive}}
			setOpenAIStrictPreflightProxyMode(account, mode)
			cipher := newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "strict-live-sideband-test-secret"}})
			attestation := `{"v":1,"s":0,"t":"v1.strict-proxy"}`
			ciphertext, err := cipher.Encrypt(attestation)
			require.NoError(t, err)
			record := &LiveCallRecord{CallID: "call_strict_proxy", CallHash: hashLiveCallID("call_strict_proxy"), AccountID: account.ID,
				APIKeyID: 7923, UserID: 7924, LeaseID: "original-live-lease", Controller: LiveControllerPending,
				AttestationCiphertext: ciphertext, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
				OutboundSnapshot: &LiveOutboundSnapshot{Owner: openAICodexStateOwner(context.Background(), account),
					SessionID: "01912345-6789-7abc-8def-0123456789ab", ThreadID: "01912345-7000-7fed-9234-123456789abc",
					UserAgent: DefaultOpenAICodexUserAgent, Originator: "codex_cli_rs", Version: codexCLIVersion}}
			originalRecord := *record
			originalSnapshot := *record.OutboundSnapshot
			originalRecord.OutboundSnapshot = &originalSnapshot
			connection := newLiveTestFrameConn()
			t.Cleanup(func() { _ = connection.Close() })
			dialer := &liveStrictProxyDialer{conn: connection}
			svc := &OpenAIGatewayService{accountRepo: &liveTestAccountRepo{account: account}, liveAttestationCipher: cipher,
				openaiWSPassthroughDialer: dialer}
			conn, dialErr := svc.dialLiveSideband(context.Background(), record)
			require.Equal(t, originalRecord, *record, "route validation must not end the call, release its lease or replace attestation")
			if mode == "valid-bound" || mode == "explicitly-unbound" {
				require.NoError(t, dialErr)
				require.Same(t, connection, conn)
				require.Equal(t, 1, dialer.calls)
				require.Equal(t, chatGPTLiveSidebandBaseURL+"/call_strict_proxy", dialer.url)
				require.Equal(t, "Bearer private-live-access-token", dialer.headers.Get("Authorization"))
				require.Equal(t, "live-owner-account", dialer.headers.Get("ChatGPT-Account-ID"))
				require.Equal(t, attestation, dialer.headers.Get(liveAttestationHeader))
				require.Equal(t, DefaultOpenAICodexUserAgent, dialer.headers.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", dialer.headers.Get("Originator"))
				require.Equal(t, codexCLIVersion, dialer.headers.Get("Version"))
				require.NotEmpty(t, dialer.headers.Get("session-id"))
				require.NotEmpty(t, dialer.headers.Get("thread-id"))
				require.Empty(t, dialer.headers.Get("OpenAI-Beta"))
				if mode == "valid-bound" {
					require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", dialer.proxy)
				} else {
					require.Empty(t, dialer.proxy)
				}
				return
			}
			require.Zero(t, dialer.calls, "reject the saved broken route before any sideband dial")
			require.Error(t, dialErr)
			require.Nil(t, conn)
			for _, secret := range []string{"private-live-access-token", "private-proxy-user", "private-proxy-password"} {
				require.NotContains(t, dialErr.Error(), secret)
			}
		})
	}
}
