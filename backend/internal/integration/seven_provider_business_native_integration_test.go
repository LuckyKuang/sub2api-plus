//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sevenNativeTrust struct {
	certificate, key string
}

// The CA exists only in a test subprocess's trust store. All native requests
// keep their official HTTPS host, normal certificate verification and saved
// proxy; neither base-URL overrides nor InsecureSkipVerify authorize a grant.
func newSevenNativeTrust(t *testing.T) sevenNativeTrust {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"chatgpt.com", "cli-chat-proxy.grok.com", "api.deepseek.com", "api.kimi.com", "agent.minimax.cn", "api.stepfun.com", "api.stepfun.ai", "open.bigmodel.cn", "bigmodel.cn", "api.z.ai", "zcode.z.ai"},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	require.NoError(t, err)
	key, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	directory := t.TempDir()
	trust := sevenNativeTrust{certificate: filepath.Join(directory, "native-ca.pem"), key: filepath.Join(directory, "native-key.pem")}
	require.NoError(t, os.WriteFile(trust.certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	require.NoError(t, os.WriteFile(trust.key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
	return trust
}

type sevenNativeReply struct {
	status int
	body   string
	events []string
}

type sevenNativeWire struct {
	connectHost, proxyAuthorization string
	request                         *http.Request
	body                            []byte
	slots                           sevenBusinessSlots
}

type sevenNativeProxy struct {
	server  *httptest.Server
	mu      sync.Mutex
	wire    []sevenNativeWire
	errors  []error
	observe func() sevenBusinessSlots
	reply   func(*http.Request, []byte) sevenNativeReply
}

func newSevenNativeProxy(t *testing.T, trust sevenNativeTrust, reply func(*http.Request, []byte) sevenNativeReply) *sevenNativeProxy {
	t.Helper()
	certificate, err := tls.LoadX509KeyPair(trust.certificate, trust.key)
	require.NoError(t, err)
	proxy := &sevenNativeProxy{reply: reply}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			proxy.recordError(fmt.Errorf("native request bypassed CONNECT: %s %s", request.Method, request.Host))
			http.Error(w, "CONNECT required", http.StatusBadRequest)
			return
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			proxy.recordError(err)
			return
		}
		defer connection.Close()
		if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
			proxy.recordError(err)
			return
		}
		if _, err := io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			proxy.recordError(err)
			return
		}
		tlsConnection := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		defer tlsConnection.Close()
		upstream, err := http.ReadRequest(bufio.NewReader(tlsConnection))
		if err != nil {
			proxy.recordError(err)
			return
		}
		defer upstream.Body.Close()
		body, err := io.ReadAll(upstream.Body)
		if err != nil {
			proxy.recordError(err)
			return
		}
		if upstream.Header.Get("Content-Encoding") == "zstd" {
			decoder, err := zstd.NewReader(bytes.NewReader(body))
			if err != nil {
				proxy.recordError(err)
				return
			}
			body, err = io.ReadAll(decoder)
			decoder.Close()
			if err != nil {
				proxy.recordError(err)
				return
			}
		}
		proxy.mu.Lock()
		observe := proxy.observe
		proxy.mu.Unlock()
		var slots sevenBusinessSlots
		if observe != nil {
			slots = observe()
		}
		proxy.mu.Lock()
		proxy.wire = append(proxy.wire, sevenNativeWire{connectHost: request.Host, proxyAuthorization: request.Header.Get("Proxy-Authorization"), request: upstream, body: body, slots: slots})
		proxy.mu.Unlock()
		response := proxy.reply(upstream, body)
		if response.status == 0 {
			response.status = http.StatusOK
		}
		if len(response.events) == 0 {
			err = (&http.Response{StatusCode: response.status, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(response.body)), ContentLength: int64(len(response.body)), Close: true}).Write(tlsConnection)
		} else {
			_, err = fmt.Fprintf(tlsConnection, "HTTP/1.1 %d %s\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n", response.status, http.StatusText(response.status))
			for _, event := range response.events {
				if err != nil {
					break
				}
				_, err = fmt.Fprintf(tlsConnection, "%x\r\n%s\r\n", len(event), event)
			}
			if err == nil {
				_, err = io.WriteString(tlsConnection, "0\r\n\r\n")
			}
		}
		if err != nil {
			proxy.recordError(err)
		}
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (p *sevenNativeProxy) recordError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errors = append(p.errors, err)
}

func (p *sevenNativeProxy) snapshot() ([]sevenNativeWire, []error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sevenNativeWire(nil), p.wire...), append([]error(nil), p.errors...)
}

type sevenNativeScenario struct {
	trust                         sevenNativeTrust
	protocol, ingress, host, path string
	model, authorization, query   string
	accountKind, region, mode     string
	nonStreaming                  bool
}

// Requirements and independently specified wire fixtures are from the pinned
// official clients listed in SEVEN_PROVIDER_TEST_MATRIX.md, DOMESTIC_OAUTH.md,
// STEPFUN.md, GROK.md, ZHIPU.md and OUTBOUND_IDENTITY.md. Native OAuth keeps the
// official destination, authentication, SSE tool semantics and exact usage while
// using the real gateway, two authenticated CONNECT proxies, Redis and SQL.
func TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const child = "SUB2API_SEVEN_NATIVE_TEST_CA"
	if os.Getenv(child) == "" {
		trust := newSevenNativeTrust(t)
		for _, platform := range []string{"openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu"} {
			t.Run(platform, func(t *testing.T) {
				command := exec.Command(os.Args[0], "-test.run=^TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits$/^"+platform+"$", "-test.v", "-test.timeout=1m")
				command.Env = append(os.Environ(), child+"="+trust.certificate, "SUB2API_SEVEN_NATIVE_TEST_KEY="+trust.key,
					"SSL_CERT_FILE="+trust.certificate)
				output, err := command.CombinedOutput()
				t.Log(string(output))
				require.NoError(t, err, "isolated native TLS subprocess must execute its complete business scenario")
				require.Contains(t, string(output), "--- PASS: TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits/"+platform)
			})
		}
		return
	}
	trust := sevenNativeTrust{certificate: os.Getenv(child), key: os.Getenv("SUB2API_SEVEN_NATIVE_TEST_KEY")}
	services := newSevenBusinessServices(t)
	for _, test := range []struct {
		provider sevenBusinessProvider
		native   sevenNativeScenario
	}{
		{sevenBusinessProvider{"openai", "codex", "0.165.0", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", map[string]string{"Originator": "codex_cli_rs", "Version": "0.165.0"}},
			sevenNativeScenario{protocol: "responses", ingress: "/v1/responses", host: "chatgpt.com", path: "/backend-api/codex/responses", model: "gpt-5.4", authorization: "Authorization"}},
		{sevenBusinessProvider{"grok", "grok", "1.0.45", "grok-shell/1.0.45 (linux; x86_64)", map[string]string{"X-Grok-Client-Identifier": "grok-shell", "X-Grok-Client-Version": "1.0.45", "X-Xai-Token-Auth": "xai-grok-cli"}},
			sevenNativeScenario{protocol: "responses", ingress: "/v1/responses", host: "cli-chat-proxy.grok.com", path: "/v1/responses", model: "grok-4.5", authorization: "Authorization"}},
		{sevenBusinessProvider{"deepseek", "deepseek", "0.2.1-rc.3", "deepseek-harness/0.2.1-rc.3 (+https://github.com/deepseek-ai/deepseek-harness)", nil},
			sevenNativeScenario{protocol: "anthropic", ingress: "/v1/messages", host: "api.deepseek.com", path: "/anthropic/v1/messages", model: "deepseek-v4-pro", authorization: "X-Dsh-Auth-Token"}},
		{sevenBusinessProvider{"kimi", "kimi", "2.3.1", "kimi-code-cli/2.3.1", map[string]string{"X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": "2.3.1", "X-Stainless-Package-Version": "0.95.2"}},
			sevenNativeScenario{protocol: "anthropic", ingress: "/v1/messages", host: "api.kimi.com", path: "/coding/v1/messages", model: "kimi-for-coding", authorization: "Authorization", query: "beta=true"}},
		{sevenBusinessProvider{"minimax", "minimax", "", "MiniMaxAgent", map[string]string{"X-Stainless-Package-Version": "0.91.1"}},
			sevenNativeScenario{protocol: "anthropic", ingress: "/v1/messages", host: "agent.minimax.cn", path: "/mavis/api/v1/llm/v1/messages", model: "MiniMax-M3.1", authorization: "Authorization"}},
		{sevenBusinessProvider{"stepfun", "stepfun", "", "stepcode (linux 6.8.0-31-generic; x64)", map[string]string{"X-Step-Client": "stepcode", "X-Stainless-Package-Version": "6.40.0"}},
			sevenNativeScenario{protocol: "chat_completions", ingress: "/v1/chat/completions", host: "api.stepfun.com", path: "/step_plan/v1/chat/completions", model: "step-3.5-flash", authorization: "Authorization"}},
		{sevenBusinessProvider{"zhipu", "zcode", "3.16.2", "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22", map[string]string{"X-ZCode-App-Version": "3.16.2"}},
			sevenNativeScenario{protocol: "anthropic", ingress: "/v1/messages", host: "zcode.z.ai", path: "/api/v1/zcode-plan/anthropic/v1/messages", model: "glm-5.2", authorization: "Authorization"}},
	} {
		t.Run(test.provider.platform, func(t *testing.T) {
			test.native.trust = trust
			runSevenBusinessFlow(t, services, test.provider, http.StatusServiceUnavailable, &test.native)
		})
	}
}

func newSevenBusinessNativeProxy(t *testing.T, native *sevenNativeScenario, status int) *sevenBusinessProxy {
	t.Helper()
	proxy := &sevenBusinessProxy{status: status}
	proxy.native = newSevenNativeProxy(t, native.trust, func(_ *http.Request, _ []byte) sevenNativeReply {
		proxy.mu.Lock()
		status := proxy.status
		proxy.mu.Unlock()
		if status != http.StatusOK {
			return sevenNativeReply{status: status, body: `{"error":{"type":"upstream_unavailable","message":"temporarily unavailable"}}`}
		}
		if native.nonStreaming {
			return sevenNativeReply{body: native.nonStreamingReply()}
		}
		return sevenNativeReply{events: native.events()}
	})
	proxy.server = proxy.native.server
	return proxy
}

func (n *sevenNativeScenario) credentials(t *testing.T, provider sevenBusinessProvider, owner string) map[string]any {
	t.Helper()
	credentials := sevenBusinessCredentials(provider, owner, "https://untrusted-override.invalid")
	delete(credentials, "api_key")
	credentials["access_token"] = "business-native-" + owner
	credentials["refresh_token"] = "business-refresh-" + owner
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	credentials["model_mapping"] = map[string]any{"business-chat": n.model}
	credentials["api_protocol"] = n.protocol
	switch provider.platform {
	case "openai":
		credentials["chatgpt_account_id"] = "business-chatgpt-" + owner
	case "grok":
		credentials["base_url"] = "https://cli-chat-proxy.grok.com/v1"
		// This scenario starts with a healthy grant well outside Grok's
		// documented one-hour proactive refresh window.
		credentials["expires_at"] = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	case "deepseek", "kimi", "minimax", "stepfun":
		credentials["oauth_provider"] = provider.platform
		credentials["oauth_region"] = "cn"
		credentials["account_mode"] = "coding"
		if provider.platform == "deepseek" {
			credentials["account_mode"] = "payg"
			credentials["oauth_device_id"] = "business-device-" + owner
		}
		if provider.platform == "stepfun" {
			if n.region != "" {
				credentials["oauth_region"] = n.region
			}
			if n.mode != "" {
				credentials["account_mode"] = n.mode
			}
			if n.accountKind == service.AccountTypeAPIKey {
				credentials["api_key"] = "business-native-" + owner
				credentials["region"] = n.region
				for _, key := range []string{"access_token", "refresh_token", "expires_at", "oauth_provider", "oauth_region", "base_url"} {
					delete(credentials, key)
				}
			}
		}
	case "zhipu":
		// ZCode start-plan's native bearer is its ZCode token. It has no
		// project PAT issue step; individual/team plan tests own that flow.
		oauth := service.NewZhipuOAuthService(repository.NewZhipuOAuthClient(), nil)
		t.Cleanup(oauth.Stop)
		material, err := oauth.BuildAccountMaterial(context.Background(), service.ZhipuAccountMaterialInput{
			Provider: "bigmodel", PlanKind: "start-plan", ZCodeJWT: "business-native-" + owner,
		})
		require.NoError(t, err)
		for key, value := range material.Credentials {
			credentials[key] = value
		}
	}
	return credentials
}

func (n *sevenNativeScenario) request(turn int) string {
	body := n.streamingRequest(turn)
	if n.nonStreaming {
		body = strings.ReplaceAll(body, `"stream":true`, `"stream":false`)
	}
	return body
}

func (n *sevenNativeScenario) clientProtocol() string {
	switch n.ingress {
	case "/v1/responses":
		return "responses"
	case "/v1/messages":
		return "anthropic"
	default:
		return "chat_completions"
	}
}

func (n *sevenNativeScenario) streamingRequest(turn int) string {
	// A subsequent native turn supplies the tool result under the same session;
	// the new gateway must retain B and preserve the protocol's tool history.
	switch n.clientProtocol() {
	case "responses":
		history := `{"type":"message","role":"user","content":[{"type":"input_text","text":"lookup status"}]}`
		if turn == 2 {
			history += `,{"type":"function_call","call_id":"call_business","name":"lookup","arguments":"{\"query\":\"status\"}"},{"type":"function_call_output","call_id":"call_business","output":"ready"}`
		}
		return strings.ReplaceAll(`{"model":"business-chat","stream":true,"input":[`+history+`],"tools":[{"type":"function","name":"lookup","description":"lookup status","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`, "call_business", n.callID())
	case "anthropic":
		history := `{"role":"user","content":[{"type":"text","text":"lookup status"}]}`
		if turn == 2 {
			history += `,{"role":"assistant","content":[{"type":"tool_use","id":"call_business","name":"lookup","input":{"query":"status"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_business","content":"ready"}]}`
		}
		return `{"model":"business-chat","max_tokens":64,"stream":true,"messages":[` + history + `],"tools":[{"name":"lookup","description":"lookup status","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`
	default:
		history := `{"role":"user","content":"lookup status"}`
		if turn == 2 {
			history += `,{"role":"assistant","content":null,"tool_calls":[{"id":"call_business","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"status\"}"}}]},{"role":"tool","tool_call_id":"call_business","content":"ready"}`
		}
		return `{"model":"business-chat","stream":true,"messages":[` + history + `],"tools":[{"type":"function","function":{"name":"lookup","description":"lookup status","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}}]} `
	}
}

func (n *sevenNativeScenario) nonStreamingReply() string {
	// Step's only upstream inference protocol is Chat Completions; the two
	// client bridges must consume this same wire contract in buffered mode.
	return fmt.Sprintf(`{"id":"chat_business","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_business","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"status\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, n.model)
}

func (n *sevenNativeScenario) assertResult(t *testing.T, body []byte) {
	t.Helper()
	if !n.nonStreaming {
		sevenNativeAssertToolResult(t, n.clientProtocol(), n.callID(), body)
		return
	}
	require.True(t, json.Valid(body))
	root := gjson.ParseBytes(body)
	var id, name, arguments string
	switch n.clientProtocol() {
	case "responses":
		require.Equal(t, "completed", root.Get("status").String())
		require.Len(t, root.Get("output").Array(), 1)
		require.Equal(t, "function_call", root.Get("output.0.type").String())
		id, name, arguments = root.Get("output.0.call_id").String(), root.Get("output.0.name").String(), root.Get("output.0.arguments").String()
		require.Equal(t, int64(10), root.Get("usage.input_tokens").Int())
		require.Equal(t, int64(5), root.Get("usage.output_tokens").Int())
	case "anthropic":
		require.Equal(t, "tool_use", root.Get("stop_reason").String())
		require.Len(t, root.Get("content").Array(), 1)
		require.Equal(t, "tool_use", root.Get("content.0.type").String())
		id, name, arguments = root.Get("content.0.id").String(), root.Get("content.0.name").String(), root.Get("content.0.input").Raw
		require.Equal(t, int64(10), root.Get("usage.input_tokens").Int())
		require.Equal(t, int64(5), root.Get("usage.output_tokens").Int())
	default:
		require.Equal(t, "tool_calls", root.Get("choices.0.finish_reason").String())
		require.Len(t, root.Get("choices.0.message.tool_calls").Array(), 1)
		id, name, arguments = root.Get("choices.0.message.tool_calls.0.id").String(), root.Get("choices.0.message.tool_calls.0.function.name").String(), root.Get("choices.0.message.tool_calls.0.function.arguments").String()
		require.Equal(t, int64(10), root.Get("usage.prompt_tokens").Int())
		require.Equal(t, int64(5), root.Get("usage.completion_tokens").Int())
	}
	require.Equal(t, n.callID(), id)
	require.Equal(t, "lookup", name)
	require.JSONEq(t, `{"query":"status"}`, arguments)
}

func (n *sevenNativeScenario) events() []string {
	sse := func(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }
	switch n.protocol {
	case "responses":
		events := []string{
			sse("response.created", `{"type":"response.created","response":{"id":"resp_business","object":"response","status":"in_progress","output":[]}}`),
			sse("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"fc_business","type":"function_call","call_id":"call_business","name":"lookup","arguments":"","status":"in_progress"}}`),
			sse("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_business","output_index":0,"delta":"{\"query\":"}`),
			sse("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_business","output_index":0,"delta":"\"status\"}"}`),
			sse("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"fc_business","type":"function_call","call_id":"call_business","name":"lookup","arguments":"{\"query\":\"status\"}","status":"completed"}}`),
			sse("response.completed", fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_business","object":"response","model":%q,"status":"completed","output":[{"id":"fc_business","type":"function_call","call_id":"call_business","name":"lookup","arguments":"{\"query\":\"status\"}","status":"completed"}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`, n.model)),
			"data: [DONE]\n\n",
		}
		for index := range events {
			events[index] = strings.ReplaceAll(events[index], "call_business", n.callID())
		}
		return events
	case "anthropic":
		return []string{
			sse("message_start", fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_business","type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`, n.model)),
			sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_business","name":"lookup","input":{}}}`),
			sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`),
			sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"status\"}"}}`),
			sse("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sse("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`),
			sse("message_stop", `{"type":"message_stop"}`),
		}
	default:
		return []string{
			"data: " + fmt.Sprintf(`{"id":"chat_business","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_business","type":"function","function":{"name":"lookup","arguments":"{\"query\":"}}]},"finish_reason":null}]}`, n.model) + "\n\n",
			"data: " + `{"id":"chat_business","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"status\"}"}}]},"finish_reason":null}]}` + "\n\n",
			"data: " + `{"id":"chat_business","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n",
			"data: " + `{"id":"chat_business","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			"data: [DONE]\n\n",
		}
	}
}

func (n *sevenNativeScenario) callID() string {
	if n.host == "chatgpt.com" {
		// Native Codex tool calls use the fc_ family; the compatibility
		// normalizer for foreign call_ IDs has separate focused regressions.
		return "fc_business"
	}
	return "call_business"
}

func (n *sevenNativeScenario) assertWire(t *testing.T, owner string, turn int, wire sevenBusinessWire) {
	t.Helper()
	require.Equal(t, n.host+":443", wire.connectHost)
	require.Equal(t, n.host, wire.host)
	require.Equal(t, n.path, wire.path)
	require.Equal(t, n.query, wire.query)
	auth := "Bearer business-native-" + owner
	if n.authorization == "X-Dsh-Auth-Token" {
		auth = "business-native-" + owner
		require.Empty(t, wire.headers.Get("Authorization"))
	}
	require.Equal(t, auth, wire.headers.Get(n.authorization))
	require.Empty(t, wire.headers.Get("X-Api-Key"))
	require.Empty(t, wire.headers.Get("Cookie"))
	if n.host == "chatgpt.com" {
		require.Equal(t, "business-chatgpt-"+owner, wire.headers.Get("ChatGPT-Account-ID"))
		require.Equal(t, "zstd", wire.headers.Get("Content-Encoding"), "native Codex HTTP keeps its official compressed request contract")
	} else {
		require.Empty(t, wire.headers.Get("Content-Encoding"))
	}
	require.Equal(t, n.model, gjson.GetBytes(wire.body, "model").String())
	require.Equal(t, !n.nonStreaming, gjson.GetBytes(wire.body, "stream").Bool())
	name, schema, history := "tools.0.name", "tools.0.input_schema", "messages"
	switch n.protocol {
	case "responses":
		schema, history = "tools.0.parameters", "input"
	case "chat_completions":
		name, schema = "tools.0.function.name", "tools.0.function.parameters"
	}
	require.Equal(t, "lookup", gjson.GetBytes(wire.body, name).String())
	require.JSONEq(t, `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`, gjson.GetBytes(wire.body, schema).Raw)
	items := gjson.GetBytes(wire.body, history).Array()
	if turn == 1 {
		require.Len(t, items, 1)
	} else {
		require.Len(t, items, 3, "the tool-result turn must preserve complete call/output pairing")
		switch n.protocol {
		case "responses":
			require.Equal(t, "lookup", items[1].Get("name").String())
			require.JSONEq(t, `{"query":"status"}`, items[1].Get("arguments").String())
			require.Equal(t, "function_call_output", items[2].Get("type").String())
			require.Equal(t, n.callID(), items[1].Get("call_id").String())
			require.Equal(t, n.callID(), items[2].Get("call_id").String())
			require.Equal(t, "ready", items[2].Get("output").String())
		case "anthropic":
			require.Equal(t, "lookup", items[1].Get("content.0.name").String())
			require.JSONEq(t, `{"query":"status"}`, items[1].Get("content.0.input").Raw)
			require.Equal(t, "tool_result", items[2].Get("content.0.type").String())
			require.Equal(t, "call_business", items[2].Get("content.0.tool_use_id").String())
			require.Equal(t, "ready", items[2].Get("content.0.content").String())
		default:
			require.Equal(t, "lookup", items[1].Get("tool_calls.0.function.name").String())
			require.JSONEq(t, `{"query":"status"}`, items[1].Get("tool_calls.0.function.arguments").String())
			require.Equal(t, "tool", items[2].Get("role").String())
			require.Equal(t, "call_business", items[2].Get("tool_call_id").String())
			require.Equal(t, "ready", items[2].Get("content").String())
		}
	}
}

func sevenNativeAssertToolResult(t *testing.T, protocol, expectedCallID string, body []byte) {
	t.Helper()
	var name, id, arguments string
	var terminal bool
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		require.True(t, json.Valid([]byte(data)), "client-facing SSE data must be valid JSON")
		event := gjson.Parse(data)
		switch protocol {
		case "responses":
			switch event.Get("type").String() {
			case "response.output_item.added":
				name, id = event.Get("item.name").String(), event.Get("item.call_id").String()
			case "response.function_call_arguments.delta":
				arguments += event.Get("delta").String()
			case "response.completed":
				terminal = event.Get("response.status").String() == "completed"
				require.Equal(t, "lookup", event.Get("response.output.0.name").String())
				require.JSONEq(t, `{"query":"status"}`, event.Get("response.output.0.arguments").String())
			}
		case "anthropic":
			switch event.Get("type").String() {
			case "content_block_start":
				name, id = event.Get("content_block.name").String(), event.Get("content_block.id").String()
			case "content_block_delta":
				arguments += event.Get("delta.partial_json").String()
			case "message_delta":
				require.Equal(t, "tool_use", event.Get("delta.stop_reason").String())
			case "message_stop":
				terminal = true
			}
		default:
			for _, choice := range event.Get("choices").Array() {
				for _, tool := range choice.Get("delta.tool_calls").Array() {
					if tool.Get("id").String() != "" {
						name, id = tool.Get("function.name").String(), tool.Get("id").String()
					}
					arguments += tool.Get("function.arguments").String()
				}
				terminal = terminal || choice.Get("finish_reason").String() == "tool_calls"
			}
		}
	}
	require.Equal(t, "lookup", name)
	require.Equal(t, expectedCallID, id)
	require.JSONEq(t, `{"query":"status"}`, arguments)
	require.True(t, terminal, "tool stream must reach its protocol's terminal event")
}
