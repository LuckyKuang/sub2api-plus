//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func contractForward(t *testing.T, platform string, inbound map[string]string) (*http.Request, []byte) {
	t.Helper()
	body := []byte(`{"model":"native","max_tokens":32,"metadata":{"user_id":"caller-claude-device"},"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	c := nativeConversationTestContext(platform, "Content-Type", "application/json", 11)
	for key, value := range inbound {
		c.Request.Header.Set(key, value)
	}
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := nativeAnthropicTestAccount()
	account.Platform = platform
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	return upstream.lastReq, upstream.lastBody
}

func TestDomesticNativeForwardUsesOfficialRequestHeaderContracts(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		t.Run(platform, func(t *testing.T) {
			req, _ := contractForward(t, platform, map[string]string{
				"X-Claude-Code-Session-Id": "foreign-session", "x-stainless-helper-method": "stream",
				"X-Stainless-Retry-Count": "97", "X-Stainless-Timeout": "99999",
				"anthropic-dangerous-direct-browser-access": "false", "Accept": "caller/type",
				"anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14",
			})
			for _, name := range []string{"X-Claude-Code-Session-Id", "x-stainless-helper-method"} {
				require.Empty(t, getHeaderRaw(req.Header, name), name)
			}
			require.Equal(t, "application/json", req.Header.Get("Accept"))
			require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), "claude-code")
			require.NotContains(t, getHeaderRaw(req.Header, "anthropic-beta"), "oauth-")
			if platform == PlatformMiniMax {
				require.Equal(t, "true", getHeaderRaw(req.Header, "anthropic-dangerous-direct-browser-access"))
			} else {
				require.Empty(t, getHeaderRaw(req.Header, "anthropic-dangerous-direct-browser-access"))
			}
			if platform == PlatformKimi || platform == PlatformMiniMax {
				require.Equal(t, "0", req.Header.Get("X-Stainless-Retry-Count"))
				timeout, err := time.ParseDuration(req.Header.Get("X-Stainless-Timeout") + "s")
				require.NoError(t, err)
				require.Positive(t, timeout)
				require.LessOrEqual(t, timeout, 900*time.Second, "declares the actual forwarding deadline, not the inbound 99999 seconds")
			} else {
				require.Empty(t, req.Header.Get("X-Stainless-Retry-Count"))
				require.Empty(t, req.Header.Get("X-Stainless-Timeout"))
			}
		})
	}
}

func TestNativeSessionWireFormatsAndPersistentOwnerBoundaries(t *testing.T) {
	for _, tc := range []struct{ platform, header, prefix string }{
		{PlatformDeepseek, "X-Deepseek-Harness-Session-Id", "session-"},
		{PlatformMiniMax, "X-Mavis-Session-Id", "mvs_"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			first, _ := contractForward(t, tc.platform, map[string]string{tc.header: "stable-conversation"})
			second, _ := contractForward(t, tc.platform, map[string]string{tc.header: "stable-conversation"})
			id := first.Header.Get(tc.header)
			require.True(t, strings.HasPrefix(id, tc.prefix))
			payload := strings.TrimPrefix(id, tc.prefix)
			parsed, err := uuid.Parse(payload)
			require.NoError(t, err)
			require.Equal(t, uuid.Version(4), parsed.Version())
			require.Equal(t, uuid.RFC4122, parsed.Variant())
			if tc.platform == PlatformMiniMax {
				require.Len(t, payload, 32)
			}
			require.Equal(t, id, second.Header.Get(tc.header))
			anonymous, _ := contractForward(t, tc.platform, nil)
			another, _ := contractForward(t, tc.platform, nil)
			require.NotEqual(t, anonymous.Header.Get(tc.header), another.Header.Get(tc.header), "identical prompts do not create a reliable conversation identifier")
		})
	}
}

func TestZCodeNativeForwardAttributionAndAnthropicDeviceMetadata(t *testing.T) {
	req, body := contractForward(t, PlatformZhipu, map[string]string{"x-session-id": "sess_original-session"})
	for _, name := range []string{"x-request-id", "x-zcode-trace-id", "x-session-id"} {
		id, err := uuid.Parse(req.Header.Get(name))
		require.NoError(t, err, name)
		require.Equal(t, uuid.Version(4), id.Version(), name)
	}
	require.Equal(t, "other", req.Header.Get("x-zcode-session-type"))
	require.Empty(t, req.Header.Get("x-query-id"))
	require.Empty(t, req.Header.Get("x-zcode-query-source"))
	require.Empty(t, req.Header.Get("X-Device-Mid"), "device belongs in Anthropic metadata, not every inference header")
	var metadata map[string]string
	require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(body, "metadata.user_id").String()), &metadata))
	require.Equal(t, "", metadata["account_uuid"])
	require.Equal(t, req.Header.Get("x-session-id"), metadata["session_id"])
	device, err := uuid.Parse(metadata["device_id"])
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), device.Version())
	control, err := http.NewRequestWithContext(req.Context(), http.MethodGet, "https://open.bigmodel.cn/api/biz/quota", nil)
	require.NoError(t, err)
	account := nativeAnthropicTestAccount()
	account.Platform = PlatformZhipu
	prepareAccountOutboundRequest(control, account)
	require.NoError(t, OutboundPreparationError(control.Context()))
	require.Equal(t, metadata["device_id"], control.Header.Get("X-Device-Mid"))
}

// ZCode's official Anthropic adapter owns metadata.user_id. These fixtures have
// no trusted session seed: verify its three-field envelope independently, then
// compare all remaining business fields to the original request.
func requireZCodeUnsessionedBusinessBody(t *testing.T, req *http.Request, body []byte, expected string) {
	t.Helper()
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	metadata, ok := decoded["metadata"].(map[string]any)
	require.True(t, ok)
	require.Len(t, metadata, 1)
	user, ok := metadata["user_id"].(string)
	require.True(t, ok)
	var attribution map[string]string
	require.NoError(t, json.Unmarshal([]byte(user), &attribution))
	require.Len(t, attribution, 3)
	id, err := uuid.Parse(attribution["device_id"])
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), id.Version())
	require.Contains(t, attribution, "account_uuid")
	require.Empty(t, attribution["account_uuid"])
	require.Contains(t, attribution, "session_id")
	require.Empty(t, attribution["session_id"])
	require.Empty(t, req.Header.Get("X-Device-Mid"))
	delete(decoded, "metadata")
	var original map[string]any
	require.NoError(t, json.Unmarshal([]byte(expected), &original))
	require.Equal(t, original, decoded)
}

func TestZCodePhysicalAttemptChangesRequestButRetainsLogicalTrace(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
			t.Run(kind+path, func(t *testing.T) {
				account := &Account{ID: 51, Platform: PlatformZhipu, Type: kind}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				newRequest := func() *http.Request {
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.bigmodel.cn"+path, bytes.NewBufferString(`{"model":"native","messages":[]}`))
					require.NoError(t, err)
					return prepareAccountOutboundRequest(req, account)
				}
				first, second := newRequest(), newRequest()
				require.NoError(t, OutboundPreparationError(first.Context()))
				require.NotEqual(t, first.Header.Get("x-request-id"), second.Header.Get("x-request-id"))
				require.Equal(t, first.Header.Get("x-zcode-trace-id"), second.Header.Get("x-zcode-trace-id"))
				if path != "/v1/messages" {
					data, err := io.ReadAll(first.Body)
					require.NoError(t, err)
					require.False(t, gjson.GetBytes(data, "metadata.user_id").Exists())
				}
			})
		}
	}
}

func TestZCodeExplicitDevicePinIsUsedInBodyAndControl(t *testing.T) {
	identity := zcode.DefaultIdentity()
	identity.ControlHeaders[zcode.HeaderDeviceMid] = "81208bcf-c7d2-4a9d-a287-a931a72468f4"
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	resolved, err := zcode.EnsureDeviceIdentity(ctx, identity)
	require.NoError(t, err)
	require.Equal(t, "81208bcf-c7d2-4a9d-a287-a931a72468f4", resolved.ControlHeaders[zcode.HeaderDeviceMid])
}

func TestDomesticCapabilitiesComeFromNativePayload(t *testing.T) {
	for _, tc := range []struct{ platform, body, expected string }{
		{PlatformDeepseek, `{"messages":[{"content":[{"type":"image","source":{"type":"file","file_id":"file_fixture"}},{"type":"tool_addition"}]}]}`, "files-api-2025-04-14,mid-conversation-tool-changes-2026-07-01"},
		{PlatformDeepseek, `{"messages":[]}`, ""},
		{PlatformDeepseek, `{"messages":[{"content":[{"type":"tool_use","input":{"type":"tool_addition","nested":{"type":"image","source":{"type":"file","file_id":"not-a-reference"}}}}]}]}`, ""},
		{PlatformDeepseek, `{"messages":[{"content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"file","file_id":"file_fixture"}}]}]}]}`, "files-api-2025-04-14"},
		{PlatformKimi, `{"messages":[]}`, "interleaved-thinking-2025-05-14"},
		{PlatformKimi, `{"thinking":{"type":"adaptive"},"context_management":{"edits":[]}}`, "context-management-2025-06-27"},
		{PlatformMiniMax, `{"messages":[],"tools":[{"name":"x"}]}`, "interleaved-thinking-2025-05-14"},
		{PlatformMiniMax, `{"thinking":{"type":"adaptive"}}`, ""},
	} {
		got := domesticAnthropicBeta(&Account{Platform: tc.platform}, []byte(tc.body), "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14")
		require.Equal(t, tc.expected, got, tc.platform+tc.body)
	}
}

func TestZCodeDeviceSourcePriorityAndProtocolCarrier(t *testing.T) {
	globalDevice := "0d632230-d675-444b-8214-76fb2bb89b23"
	accountDevice := "8bf01956-7560-42ea-ae50-1f5e2ff529b1"
	settings := emptyOutboundIdentitySettings()
	settings.Runtime["zcode"] = map[string]string{zcode.HeaderDeviceMid: globalDevice}
	_, ctx := outboundIdentityTestSettings(t, settings)
	for _, tc := range []struct{ pin, expected string }{{"", globalDevice}, {accountDevice, accountDevice}, {"invalid-device", globalDevice}} {
		account := &Account{ID: 53, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
			outboundIdentityCredential: OutboundIdentitySelection{Preset: "zcode", Version: "3.16.2", Headers: map[string]string{zcode.HeaderDeviceMid: tc.pin}},
		}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.bigmodel.cn/v1/messages", bytes.NewBufferString(`{"messages":[],"metadata":{"other":"preserved"}}`))
		require.NoError(t, err)
		prepareAccountOutboundRequest(request, account)
		require.NoError(t, OutboundPreparationError(request.Context()))
		var metadata map[string]string
		require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(domesticRequestBody(request), "metadata.user_id").String()), &metadata))
		require.Equal(t, tc.expected, metadata["device_id"])
		require.Equal(t, "preserved", gjson.GetBytes(domesticRequestBody(request), "metadata.other").String())
		require.Empty(t, request.Header.Get(zcode.HeaderDeviceMid))
		control, err := http.NewRequestWithContext(request.Context(), http.MethodGet, "https://open.bigmodel.cn/api/biz/quota", nil)
		require.NoError(t, err)
		prepareAccountOutboundRequest(control, account)
		require.NoError(t, OutboundPreparationError(control.Context()))
		require.Equal(t, tc.expected, control.Header.Get(zcode.HeaderDeviceMid))
	}
}

func TestDomesticSDKTimeoutUsesActualAttemptDeadline(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://native.invalid/v1/messages", bytes.NewBufferString(`{"messages":[]}`))
		require.NoError(t, err)
		request.Header["x-stainless-timeout"] = []string{"99999"}
		prepareDomesticRequestHeaders(request, &Account{Platform: platform})
		duration, err := time.ParseDuration(request.Header.Get("X-Stainless-Timeout") + "s")
		require.NoError(t, err)
		require.Positive(t, duration)
		require.LessOrEqual(t, duration, 30*time.Second)
		cancel()
	}
}

func TestZCodeNewHTTPLogicalRequestStartsNewTraceAndQuery(t *testing.T) {
	first, _ := contractForward(t, PlatformZhipu, map[string]string{"x-session-id": "sess_shared-conversation", "x-query-id": "query_first"})
	second, _ := contractForward(t, PlatformZhipu, map[string]string{"x-session-id": "shared-conversation", "x-query-id": "query_second"})
	require.Equal(t, first.Header.Get("x-session-id"), second.Header.Get("x-session-id"))
	require.NotEqual(t, first.Header.Get("x-zcode-trace-id"), second.Header.Get("x-zcode-trace-id"))
	require.NotEqual(t, first.Header.Get("x-query-id"), second.Header.Get("x-query-id"))
	for _, request := range []*http.Request{first, second} {
		parsed, err := uuid.Parse(request.Header.Get("x-query-id"))
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), parsed.Version())
	}
}
