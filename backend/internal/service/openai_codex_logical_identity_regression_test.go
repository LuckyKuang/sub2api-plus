//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Permanent regressions for the Codex logical-identity findings in
// tmp/codex-full-egress-failover-audit-2026-10-14.md (A01/A02/A07). The names
// match the preserved repro log so evidence stays traceable.

func codexLogicalIdentityAuditResponse() *http.Response {
	r := newOpenAIRejectedFieldTestResponse(200, "")
	r.Header.Set("Content-Type", "text/event-stream")
	r.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))
	return r
}

// A01: one logical conversation keeps the same prompt_cache_key when the
// credential owner changes, while credential headers follow the new owner.
func TestCodexDeepAuditCacheSurvivesAccountSwitch(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, pass := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pass=%t", kind, pass), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{}
				svc := newOpenAIRejectedFieldTestService(upstream)
				body := []byte(`{"model":"gpt-6.1-sol","stream":true,"instructions":"fixture","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"prompt_cache_key":"same-logical-session"}`)
				var keys []string
				for i := 0; i < 2; i++ {
					a := newOpenAIRejectedFieldTestAccount()
					if kind == AccountTypeOAuth {
						a = newOpenAIOAuthNamespaceTestAccount()
					}
					a.ID = int64(100 + i)
					a.Credentials["chatgpt_account_id"] = fmt.Sprintf("owner-%d", i)
					a.Credentials["access_token"] = fmt.Sprintf("fixture-%d", i)
					a.Credentials["api_key"] = fmt.Sprintf("fixture-%d", i)
					a.Extra = map[string]any{"openai_passthrough": pass, "responses_supported": true}
					upstream.resp = codexLogicalIdentityAuditResponse()
					c := newOpenAIRejectedFieldTestContext(body)
					c.Set("api_key", &APIKey{ID: 42, UserID: 7})
					c.Request.Header.Set("session-id", "same-logical-session")

					_, err := svc.Forward(context.Background(), c, a, body)
					require.NoError(t, err)
					require.NotNil(t, upstream.lastReq)
					require.Equal(t, fmt.Sprintf("Bearer fixture-%d", i), upstream.lastReq.Header.Get("Authorization"))
					if kind == AccountTypeOAuth {
						require.Equal(t, fmt.Sprintf("owner-%d", i), upstream.lastReq.Header.Get("chatgpt-account-id"))
					}
					key := gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String()
					require.NotEmpty(t, key)
					keys = append(keys, key)
				}
				assert.Equal(t, keys[0], keys[1], "logical cache identity must survive credential-owner migration")
			})
		}
	}
}

// A07: the modern official body carries its base prompt in input and no
// top-level instructions; the gateway must not prepend its own base prompt,
// and must preserve official prefixed item identity.
func TestCodexDeepAuditModernOfficialBody(t *testing.T) {
	for _, pass := range []bool{false, true} {
		t.Run(fmt.Sprintf("pass=%t", pass), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
			svc := newOpenAIRejectedFieldTestService(upstream)
			a := newOpenAIOAuthNamespaceTestAccount()
			a.Extra = map[string]any{"openai_passthrough": pass}
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"store":false,"input":[{"type":"message","id":"msg_11111111-1111-5111-8111-111111111111","role":"developer","content":[{"type":"input_text","text":"official base instructions"}]},{"type":"message","id":"msg_22222222-2222-5222-8222-222222222222","role":"user","content":[{"type":"input_text","text":"hello"}]}],"tool_choice":"auto","parallel_tool_calls":true,"include":["reasoning.encrypted_content"]}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.163.0 (Ubuntu 24.04; x86_64) xterm-256color")

			_, err := svc.Forward(context.Background(), c, a, body)
			require.NoError(t, err)
			require.NotNil(t, upstream.lastReq)
			assert.False(t, gjson.GetBytes(upstream.lastBody, "instructions").Exists(), "do not prepend a second, gateway-owned base prompt")
			assert.Equal(t, "msg_11111111-1111-5111-8111-111111111111", gjson.GetBytes(upstream.lastBody, "input.0.id").String(), "preserve official prefixed item identity")
		})
	}
}

// A02: the same raw client session in two authenticated tenants must not share
// one route binding.
func TestCodexDeepAuditStickyTenantIsolation(t *testing.T) {
	svc := &OpenAIGatewayService{}
	groupID := int64(7)
	var keys []string
	for _, keyID := range []int64{42, 43} {
		c := newOpenAIPromptCacheIdentityTestContext(t, keyID)
		c.Set("api_key", &APIKey{ID: keyID, UserID: keyID, GroupID: &groupID})
		c.Request.Header.Set("session-id", "same-raw-client-session")
		keys = append(keys, svc.GenerateSessionHash(c, []byte(`{"model":"gpt-6.1-sol","input":"hello"}`)))
	}
	require.NotEmpty(t, keys[0])
	assert.NotEqual(t, keys[0], keys[1], "routing affinity must be tenant isolated before adding the same group namespace")
}
