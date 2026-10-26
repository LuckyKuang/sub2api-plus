//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Official client.rs:1034 supports reasoning_summary_delivery; Chat's
// include_usage must not cause other Responses fields to disappear.
func TestCodexResponsesStreamOptionsCompatibility(t *testing.T) {
	for _, options := range []string{
		`{"reasoning_summary_delivery":"sequential_cutoff"}`,
		`{"reasoning_summary_delivery":"sequential_cutoff","include_usage":true,"future_option":{"value":1}}`,
		`{"include_usage":false}`, `{"include_usage":null}`, `{"future_option":true}`, `null`, `[]`,
	} {
		for _, mode := range []string{"normal", "passthrough", "websocket"} {
			t.Run(mode+"/"+options, func(t *testing.T) {
				body := []byte(`{"model":"gpt-6.1-sol","instructions":"test","stream":true,"input":"hello","stream_options":` + options + `}`)
				var normalized []byte
				var err error
				switch mode {
				case "normal":
					var payload map[string]any
					require.NoError(t, json.Unmarshal(body, &payload))
					result := applyCodexOAuthTransform(payload, true, false)
					require.NoError(t, result.Error)
					normalized, err = json.Marshal(payload)
				case "passthrough":
					normalized, _, err = normalizeOpenAIPassthroughOAuthBody(body, false)
				case "websocket":
					normalized, _, err = normalizeOpenAIResponsesWebSocketCompatibilityBody(body, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false)
				}
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(normalized, "stream_options.include_usage").Exists())
				if strings.Contains(options, "sequential_cutoff") {
					require.Equal(t, "sequential_cutoff", gjson.GetBytes(normalized, "stream_options.reasoning_summary_delivery").String())
				}
				if strings.Contains(options, "future_option") {
					require.JSONEq(t, gjson.Get(options, "future_option").Raw, gjson.GetBytes(normalized, "stream_options.future_option").Raw)
				}
				if options == `{"include_usage":false}` || options == `{"include_usage":null}` {
					require.False(t, gjson.GetBytes(normalized, "stream_options").Exists())
				}
			})
		}
	}
}

// Capture real HTTP and WS handshake headers after the production builders.
func captureCodexProtocolHeaders(t *testing.T, svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte, passthrough bool) []http.Header {
	t.Helper()
	captured := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		if websocket.IsWebSocketUpgrade(r) {
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer server.Close()
	var req *http.Request
	var err error
	if passthrough {
		req, err = svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, body, "fixture")
	} else {
		req, err = svc.buildUpstreamRequest(c.Request.Context(), c, account, body, "fixture", true, gjson.GetBytes(body, "prompt_cache_key").String(), true)
	}
	require.NoError(t, err)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	req.URL.Scheme, req.URL.Host, req.Host = target.Scheme, target.Host, target.Host
	client := &http.Client{Transport: &http.Transport{}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(prepareAccountOutboundRequest(req, account))
	require.NoError(t, err)
	_ = resp.Body.Close()
	headers, _, err := svc.buildOpenAIWSHeaders(c.Request.Context(), c, account, "fixture", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", c.Request.Header.Get(openAIWSTurnMetadataHeader), gjson.GetBytes(body, "prompt_cache_key").String(), gjson.GetBytes(body, "model").String(), "")
	require.NoError(t, err)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), headers, "")
	require.NoError(t, err)
	_ = conn.Close()
	return []http.Header{<-captured, <-captured}
}

// Official Guardian-v2 reviewer/model and classifier/backend scopes. An
// account switch or unrelated model must not inherit the reviewer role.
func TestCodexGuardianRoleWireScope(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			for _, tc := range []struct{ name, role, model, subagent, want string }{
				{"reviewer", "reviewer", "codex-auto-review", "guardian", "reviewer"},
				{"classifier", "classifier", "gpt-5.6-luna", "guardian", "classifier"},
				{"unrelated model", "reviewer", "gpt-6.1-sol", "guardian", ""},
				{"unrelated classifier", "classifier", "gpt-5.6-luna", "", ""},
				{"unknown role", "admin", "codex-auto-review", "guardian", ""},
				{"missing role", "", "codex-auto-review", "guardian", ""},
			} {
				t.Run(fmt.Sprintf("%s/%t/%s", kind, passthrough, tc.name), func(t *testing.T) {
					svc := &OpenAIGatewayService{cfg: codexTransportTestConfig()}
					account := &Account{ID: 71, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture"}}
					body := []byte(fmt.Sprintf(`{"model":%q,"stream":true,"input":"audit this","instructions":"test"}`, tc.model))
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request.Header.Set("x-codex-guardian", tc.role)
					c.Request.Header.Set("x-openai-subagent", tc.subagent)
					c.Request.Header.Set("x-codex-arbitrary", "must-not-pass")
					want := tc.want
					if kind == AccountTypeAPIKey {
						want = ""
					}
					for _, h := range captureCodexProtocolHeaders(t, svc, c, account, body, passthrough) {
						require.Equal(t, want, h.Get("x-codex-guardian"))
						require.Empty(t, h.Get("x-codex-arbitrary"))
						require.Equal(t, "Bearer fixture", h.Get("Authorization"))
					}
				})
			}
		}
	}
}

// Official client.rs:646-668: non-root agents can share a cache while their
// Responses session stays distinct. Plus must preserve tenant/owner isolation.
func TestCodexNonRootSessionCacheWireIsolation(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", kind, passthrough), func(t *testing.T) {
				svc := &OpenAIGatewayService{cfg: codexTransportTestConfig()}
				account := &Account{ID: 71, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "owner-71"}}
				capture := func(user int64, session, subagent string) (string, string) {
					c := newOpenAIPromptCacheIdentityTestContext(t, user)
					c.Request.Header.Set("session-id", session)
					c.Request.Header.Set("x-openai-subagent", subagent)
					body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello","prompt_cache_key":"guardian:parent"}`)
					body, _, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(c, account, body, "", "gpt-6.1-sol")
					require.NoError(t, err)
					cache := gjson.GetBytes(body, "prompt_cache_key").String()
					headers := captureCodexProtocolHeaders(t, svc, c, account, body, passthrough)
					require.Equal(t, headers[0].Get("session-id"), headers[1].Get("session-id"), "HTTP/WS must preserve the same isolated child session")
					for _, h := range headers {
						require.NotEmpty(t, h.Get("session-id"))
						require.NotEqual(t, session, h.Get("session-id"))
						require.Empty(t, h.Get("session_id"))
						require.Empty(t, h.Get("conversation_id"))
					}
					return cache, headers[0].Get("session-id")
				}
				cacheA, sessionA := capture(42, "child-a", "guardian")
				cacheB, sessionB := capture(42, "child-b", "guardian")
				require.Equal(t, cacheA, cacheB)
				require.NotEqual(t, sessionA, sessionB)
				require.NotEqual(t, cacheA, sessionA)
				repeatCache, repeatSession := capture(42, "child-a", "guardian")
				require.Equal(t, cacheA, repeatCache)
				require.Equal(t, sessionA, repeatSession)
				otherCache, otherSession := capture(43, "child-a", "guardian")
				require.NotEqual(t, cacheA, otherCache)
				require.NotEqual(t, sessionA, otherSession)
				rootCache, rootSession := capture(42, "root-session", "")
				require.NotEqual(t, rootCache, rootSession)
				if kind == AccountTypeOAuth {
					account.ID = 72
					account.Credentials["chatgpt_account_id"] = "owner-72"
					failoverCache, failoverSession := capture(42, "child-a", "guardian")
					// A01: failover rebuilds credentials and connections, but the
					// logical cache survives while execution identity follows the owner.
					require.Equal(t, cacheA, failoverCache, "logical cache key must survive credential-owner failover")
					require.NotEqual(t, sessionA, failoverSession, "execution session changes with credential owner")
				}
			})
		}
	}
}

// Official to_api_provider inserts managed residency in its common header map,
// including models. Native API keys retain residency even with stale foreign
// selections; ignoring that candidate also preserves the catalog cache identity.
func TestCodexModelsResidencyWireAndCache(t *testing.T) {
	captured := make(chan http.Header, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[{"slug":"gpt-6.1-sol"}]}`)
	}))
	defer server.Close()
	originalURL := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL + "/models"
	defer func() { chatgptCodexModelsURL = originalURL }()
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		svc := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		}})
		svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
		account := newCodexModelsTestAccount()
		if kind == AccountTypeAPIKey {
			account = newCodexModelsAPIKeyTestAccount(server.URL)
		}
		for _, value := range []string{"off", "us"} {
			svc.settingService = &SettingService{settingRepo: &settingRepoStub{values: map[string]string{SettingKeyOpenAICodexResidency: value}}}
			require.Equal(t, value, svc.settingService.GetOpenAICodexResidency(context.Background()))
			_, err := svc.FetchCodexModelsManifest(context.Background(), account, "", "")
			require.NoError(t, err)
			select {
			case headers := <-captured:
				want := value
				if want == "off" {
					want = ""
				}
				require.Equal(t, want, headers.Get("x-openai-internal-codex-residency"))
				require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
			default:
				t.Fatalf("%s/%s: residency change must not reuse an incompatible models cache", kind, value)
			}
		}
		if kind == AccountTypeAPIKey {
			account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude"}
			_, err := svc.FetchCodexModelsManifest(context.Background(), account, "", "")
			require.NoError(t, err)
			select {
			case <-captured:
				t.Fatal("ignored foreign candidate must not split the native Codex cache identity")
			default:
			}
		}
	}
}
