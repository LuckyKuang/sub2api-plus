//go:build unit

package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Source: openai/codex c9b4267c37, core/src/context/world_state/top_level_tools.rs
// and core/tests/suite/scenarios_exec_tools.rs. History is ordered: restored
// functions.exec declarations may differ from an earlier catalog. The separate
// workspace namespace exercises the gateway's supported top-level migration.
func officialIncrementalExecFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../testdata/official_agents/codex_incremental_exec.json")
	require.NoError(t, err)
	return body
}

func assertOfficialIncrementalExecHistory(t *testing.T, body []byte) {
	t.Helper()
	require.Equal(t, "Initial exec catalog.", gjson.GetBytes(body, "input.0.tools.0.tools.0.description").String())
	require.Equal(t, "functions", gjson.GetBytes(body, "input.4.tools.0.name").String())
	require.Equal(t, "Restored exec catalog with current definitions.", gjson.GetBytes(body, "input.4.tools.0.tools.0.description").String())
	require.Equal(t, "## Namespace: functions\n### `lookup`\nUpdated tool definition.\nexec tool declaration:\ndeclare const tools: { lookup(args: { query: string }): Promise<string>; };", gjson.GetBytes(body, "input.1.content.0.text").String())
	require.Equal(t, "- Tool: tools.lookup\n- Namespace instructions: search", gjson.GetBytes(body, "input.2.content.0.text").String())
	require.Equal(t, "developer", gjson.GetBytes(body, "input.1.role").String())
	require.Equal(t, "developer", gjson.GetBytes(body, "input.2.role").String())
	require.Equal(t, "audit current request", gjson.GetBytes(body, "input.5.content.0.text").String())
	require.True(t, gjson.GetBytes(body, "input.5.content.1.opaque").Bool())
	require.True(t, gjson.GetBytes(body, "future_options.opaque").Bool())
	require.Equal(t, int64(6), gjson.GetBytes(body, "input.#").Int(), "do not collapse or reorder the tool history")
}

func TestResponsesLiteOfficialIncrementalExecHistory(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, websocket := range []bool{false, true} {
			t.Run(kind+"/websocket="+strconvBool(websocket), func(t *testing.T) {
				body := officialIncrementalExecFixture(t)
				if websocket {
					var err error
					body, err = sjson.SetBytes(body, "type", "response.create")
					require.NoError(t, err)
				}
				updated, _, err := normalizeOpenAIResponsesLitePayloadForAccount(body, &Account{Platform: PlatformOpenAI, Type: kind})
				require.NoError(t, err, "separate historical tool definitions are valid chronological updates")
				assertOfficialIncrementalExecHistory(t, updated)
				if kind == AccountTypeOAuth {
					require.False(t, gjson.GetBytes(updated, "tools").Exists())
					require.Equal(t, "workspace", gjson.GetBytes(updated, "input.0.tools.1.name").String())
				} else {
					require.Equal(t, "workspace", gjson.GetBytes(updated, "tools.0.name").String())
				}
				if websocket {
					require.Equal(t, "response.create", gjson.GetBytes(updated, "type").String())
				}
			})
		}
	}
}

func TestResponsesLiteOfficialIncrementalExecForward(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, passthrough := range []bool{false, true} {
			t.Run(kind+"/passthrough="+strconvBool(passthrough), func(t *testing.T) {
				body := officialIncrementalExecFixture(t)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set(responsesLiteHeader, "true")
				c.Request.Header.Set("User-Agent", "untrusted-client/9.0")
				c.Request.Header.Set("Originator", "untrusted-client")
				c.Request.Header.Set("Version", "9.0")
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_incremental\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n")),
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{
					ID: 501, Platform: PlatformOpenAI, Type: kind, Concurrency: 1,
					Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1),
					Credentials: map[string]any{"access_token": "fixture-oauth", "chatgpt_account_id": "fixture-workspace", "api_key": "fixture-api-key"},
					Extra:       map[string]any{"openai_passthrough": passthrough},
				}
				result, err := svc.Forward(t.Context(), c, account, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.NotNil(t, upstream.lastReq)
				assertOfficialIncrementalExecHistory(t, upstream.lastBody)
				require.Equal(t, "true", upstream.lastReq.Header.Get(responsesLiteHeader))
				require.Equal(t, "codex_cli_rs/0.167.0 (Ubuntu 24.04; x86_64) xterm-256color", upstream.lastReq.Header.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", upstream.lastReq.Header.Get("Originator"))
				require.Equal(t, "0.167.0", upstream.lastReq.Header.Get("Version"))
				require.Contains(t, rec.Body.String(), "resp_incremental")
			})
		}
	}
}

func TestResponsesLiteMigratedToolsCompareLatestDeclaration(t *testing.T) {
	for _, latest := range []bool{false, true} {
		t.Run("latest="+strconvBool(latest), func(t *testing.T) {
			body := officialIncrementalExecFixture(t)
			declaration := `[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Initial exec catalog."}]}]`
			if latest {
				declaration = `[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Restored exec catalog with current definitions."}]}]`
			}
			body, err := sjson.SetRawBytes(body, "tools", []byte(declaration))
			require.NoError(t, err)
			updated, changed, err := normalizeOpenAIResponsesLiteToolsPayload(body)
			if latest {
				require.NoError(t, err)
				require.True(t, changed)
				assertOfficialIncrementalExecHistory(t, updated)
				require.Equal(t, int64(1), gjson.GetBytes(updated, "input.0.tools.#").Int(), "do not duplicate the latest declaration into the initial catalog")
				require.False(t, gjson.GetBytes(updated, "tools").Exists())
			} else {
				require.ErrorContains(t, err, `conflicts with migrated tool type "namespace" name "functions"`)
				require.False(t, changed)
				require.Equal(t, body, updated, "a stale migration cannot overwrite a later official declaration")
			}
		})
	}
}

func TestCodexOfficialAnswerDisplayMetadata(t *testing.T) {
	// Source: Codex 202b590e84, protocol/src/items.rs. Display actions are
	// opaque presentation instructions for the producing client, including
	// future actions; they cannot be removed with the independent Plus notice.
	for _, action := range []string{"answerHelpline", "futureDisplayAction"} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			for _, passthrough := range []bool{false, true} {
				t.Run(action+"/"+kind+"/passthrough="+strconvBool(passthrough), func(t *testing.T) {
					svc, _ := safetyBufferingTestService(t)
					saveSafetyBuffering(t, svc.settingService, true)
					c, rec := newSafetyBufferingContext(t)
					metadata := `{"type":"response.metadata","safety_buffering":{},"metadata":{"openai_chatgpt_moderation_metadata":{"final_answer_moderation":{"item_id":"msg-42","moderated":true,"display":{"action":"` + action + `","blockType":"example","cyberTrustedAccess":false,"checkTrustedContacts":true}}}}}`
					wire := "data: " + metadata + "\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_display\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"
					resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
					account := &Account{ID: 7, Platform: PlatformOpenAI, Type: kind}
					if passthrough {
						_, err := svc.handleStreamingResponsePassthrough(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NoError(t, err)
					} else {
						_, err := svc.handleStreamingResponse(t.Context(), resp, c, account, time.Now(), "fixture", "fixture")
						require.NoError(t, err)
					}
					require.Contains(t, rec.Body.String(), `"display":{"action":"`+action+`","blockType":"example","cyberTrustedAccess":false,"checkTrustedContacts":true}`)
					require.NotContains(t, rec.Body.String(), "safety_buffering")
					require.Contains(t, rec.Body.String(), "resp_display")
				})
			}
		}
	}
}
