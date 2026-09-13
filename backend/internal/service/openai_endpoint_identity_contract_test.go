//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIIdentityContractAlphaSearchStripsResponsesHeaderVariants(t *testing.T) {
	headers := http.Header{"User-Agent": {"codex_cli_rs/0.200.1"}, "Originator": {"codex_cli_rs"}, "Version": {"0.200.1"}, "X-Codex-Turn-Metadata": {`{"turn_id":"retained"}`}}
	want := headers.Clone()
	for _, name := range []string{"OpenAI-Beta", "Session_ID", "Conversation_ID", "X-Codex-Beta-Features", "X-Codex-Turn-State", responsesLiteHeaderKey} {
		for _, variant := range []string{name, strings.ToLower(name), strings.ToUpper(name)} {
			headers[variant] = []string{"responses-only"}
		}
	}
	stripOpenAIAlphaSearchResponsesHeaders(headers)
	require.Equal(t, want, headers)
}

// Exercise the public service entries and capture the final upstream request.
// Updating settings after a failed send models both handler retries and hot reload.
func TestOpenAIIdentityContractEndpointMatrix(t *testing.T) {
	const searchMetadata = `{"turn_id":"turn-1","mcp_request_meta":"{\"openai/search_context\":\"{\\\"telemetry_attributes\\\":{\\\"model_id\\\":\\\"search-model\\\"}}\"}","future_field":"keep"}`
	endpoints := []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "/v1/images/generations", "/v1/alpha/search"}
	for _, endpoint := range endpoints {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, "pat"} {
			if accountType == "pat" && endpoint != "/v1/alpha/search" {
				continue
			}
			for _, passthrough := range []bool{false, true} {
				for _, source := range []string{"account", "global", "default", "claude", "gemini", "grok", "antigravity"} {
					compatible := source != "account" && source != "global" && source != "default"
					if compatible && accountType != AccountTypeAPIKey {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/passthrough=%t/%s", endpoint, accountType, passthrough, source), func(t *testing.T) {
						account := newOpenAIOAuthNamespaceTestAccount()
						wantAuth := "Bearer oauth-token"
						if accountType == AccountTypeAPIKey {
							account = newOpenAIRejectedFieldTestAccount()
							wantAuth = "Bearer sk-test"
						}
						if accountType == "pat" {
							account.Credentials["auth_mode"] = OpenAIAuthModePersonalAccessToken
						}
						if account.Extra == nil {
							account.Extra = map[string]any{}
						}
						account.Extra["openai_passthrough"] = passthrough
						repo := &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexClientVersion: "0.200.1"}}
						family, fingerprint := "codex-tui", " (Ubuntu 24.04; x86_64) xterm-256color"
						switch source {
						case "account":
							account.Credentials["user_agent"] = "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal"
							repo.values[SettingKeyOpenAICodexUserAgent] = "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode"
							family, fingerprint = "codex_cli_rs", " (Ubuntu 22.4.0; x86_64) terminal"
						case "global":
							account.Credentials["user_agent"] = "curl/8.0"
							repo.values[SettingKeyOpenAICodexUserAgent] = "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode"
							family, fingerprint = "codex_vscode", " (Ubuntu 24.04; aarch64) vscode"
						case "default":
							account.Credentials["user_agent"] = "invalid"
							repo.values[SettingKeyOpenAICodexUserAgent] = "invalid"
						default:
							account.Credentials[outboundIdentityCredential] = map[string]any{"preset": source}
						}
						account.Credentials[credKeyHeaderOverrideEnabled] = true
						overrides := map[string]any{}
						for _, name := range managedIdentityOverrideTestNames {
							overrides[strings.ToLower(name)] = "override/999.0.0"
						}
						account.Credentials[credKeyHeaderOverrides] = overrides
						settings := &SettingService{settingRepo: repo}
						svc := newOpenAIRejectedFieldTestService(nil)
						svc.settingService = settings
						var requests []*http.Request
						svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
							requests = append(requests, req.Clone(req.Context()))
							if len(requests) == 1 {
								repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
								settings.InvalidateOpenAICodexClientVersionCache()
								return newOpenAIRejectedFieldTestResponse(http.StatusBadGateway, `{"error":{"type":"server_error","message":"temporary upstream error"}}`), nil
							}
							return openAIEndpointIdentitySuccessResponse(endpoint, accountType), nil
						}}
						body := openAIEndpointIdentityBody(endpoint)
						newContext := func() *gin.Context {
							c := newOpenAIIdentityContractContaminatedContext(body)
							c.Request.URL.Path = endpoint
							c.Request.Header.Set("Originator", "chatgpt_cca")
							c.Request.Header.Set("X-Codex-Turn-Metadata", searchMetadata)
							c.Set("api_key", &APIKey{ID: 99})
							return c
						}
						c := newContext()
						_, err := forwardOpenAIEndpointIdentity(svc, c, account, endpoint, body)
						require.Error(t, err)
						require.Len(t, requests, 1)
						_, err = forwardOpenAIEndpointIdentity(svc, c, account, endpoint, body)
						require.NoError(t, err)
						require.Len(t, requests, 2)
						_, err = forwardOpenAIEndpointIdentity(svc, newContext(), account, endpoint, body)
						require.NoError(t, err)
						require.Len(t, requests, 3)
						for index, req := range requests {
							version := "0.200.1"
							if index == 2 {
								version = "0.200.2"
							}
							want := http.Header{"User-Agent": {family + "/" + version + fingerprint}}
							if accountType != AccountTypeAPIKey {
								want.Set("Originator", family)
								want.Set("Version", version)
							}
							if compatible {
								want = http.Header{}
								builtInOutboundIdentity(source).Apply(want)
							}
							requireOpenAIIdentityContractHeaders(t, req.Header, want, wantAuth)
							if endpoint == "/v1/alpha/search" {
								metadata := req.Header.Get("X-Codex-Turn-Metadata")
								require.True(t, gjson.Valid(metadata), "search metadata must survive every auth/preset path")
								require.Equal(t, gjson.Get(searchMetadata, "mcp_request_meta").String(), gjson.Get(metadata, "mcp_request_meta").String())
								require.Equal(t, "keep", gjson.Get(metadata, "future_field").String())
								wantTurn := "turn-1"
								if accountType != AccountTypeAPIKey {
									wantTurn = scopeCodexAccountIdentityValue(account, 99, "turn", "turn-1")
								}
								require.Equal(t, wantTurn, gjson.Get(metadata, "turn_id").String())
								require.Empty(t, req.Header.Get("X-OpenAI-Actor-Authorization"))
								if accountType == "pat" {
									require.Equal(t, chatgptCodexURL, req.URL.String())
									require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
								} else {
									require.True(t, strings.HasSuffix(req.URL.Path, "/alpha/search"))
									require.Equal(t, "application/json", req.Header.Get("Accept"))
									for _, name := range []string{"OpenAI-Beta", "Session_ID", "Conversation_ID", "X-Codex-Beta-Features", "X-Codex-Turn-State", responsesLiteHeaderKey} {
										require.Empty(t, req.Header.Get(name))
									}
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestOpenAIIdentityContractChatResponsesFallbackSnapshot(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			account := newOpenAIRejectedFieldTestAccount()
			account.Extra = map[string]any{"openai_passthrough": passthrough}
			account.Credentials["user_agent"] = "codex_cli_rs/0.180.0 (Ubuntu 22.4.0; x86_64) terminal"
			repo := &openAIIdentitySettingRepoStub{values: map[string]string{SettingKeyOpenAICodexClientVersion: "0.200.1"}}
			settings := &SettingService{settingRepo: repo}
			svc := newOpenAIRejectedFieldTestService(nil)
			svc.settingService = settings
			var requests []*http.Request
			svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				requests = append(requests, req.Clone(req.Context()))
				if len(requests) == 1 {
					repo.values[SettingKeyOpenAICodexClientVersion] = "0.200.2"
					settings.InvalidateOpenAICodexClientVersionCache()
					return newOpenAIRejectedFieldTestResponse(http.StatusNotFound, `{"error":{"message":"responses endpoint unsupported"}}`), nil
				}
				return newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"chatcmpl-review","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
			}}
			body := openAIEndpointIdentityBody("/v1/chat/completions")
			c := newOpenAIIdentityContractContaminatedContext(body)
			c.Request.URL.Path = "/v1/chat/completions"
			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Len(t, requests, 2)
			require.Equal(t, "/v1/responses", requests[0].URL.Path)
			require.Equal(t, "/v1/chat/completions", requests[1].URL.Path)
			for _, req := range requests {
				requireOpenAIIdentityContractHeaders(t, req.Header, http.Header{"User-Agent": {"codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) terminal"}}, "Bearer sk-test")
			}
		})
	}
}

func forwardOpenAIEndpointIdentity(svc *OpenAIGatewayService, c *gin.Context, account *Account, endpoint string, body []byte) (*OpenAIForwardResult, error) {
	ctx := context.Background()
	switch endpoint {
	case "/v1/messages":
		return svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
	case "/v1/chat/completions":
		return svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
	case "/v1/images/generations":
		parsed, err := svc.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			return nil, err
		}
		return svc.ForwardImages(ctx, c, account, body, parsed, "")
	case "/v1/alpha/search":
		return svc.ForwardAlphaSearch(ctx, c, account, body)
	default:
		return svc.Forward(ctx, c, account, body)
	}
}

func openAIEndpointIdentityBody(endpoint string) []byte {
	switch endpoint {
	case "/v1/messages", "/v1/chat/completions":
		return []byte(`{"model":"gpt-5.4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	case "/v1/images/generations":
		return []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"1024x1024","n":1,"response_format":"b64_json"}`)
	case "/v1/alpha/search":
		return []byte(`{"id":"search-session","model":"gpt-5.4","commands":{"search_query":[{"q":"test"}]}}`)
	default:
		return []byte(`{"model":"gpt-5.4","stream":true,"instructions":"test","input":"hello"}`)
	}
}

func openAIEndpointIdentitySuccessResponse(endpoint, accountType string) *http.Response {
	if endpoint == "/v1/alpha/search" && accountType != "pat" {
		return newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"encrypted_output":"ciphertext","output":"search result"}`)
	}
	if endpoint == "/v1/images/generations" && accountType == AccountTypeAPIKey {
		return newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"created":1710000007,"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)
	}
	response := openAIIdentityContractSuccessResponse()
	if endpoint == "/v1/images/generations" {
		response = newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: "+`{"type":"response.completed","response":{"id":"resp_image","output":[{"type":"image_generation_call","result":"aW1hZ2U=","output_format":"png","size":"1024x1024"}],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n\ndata: [DONE]\n\n")
	} else if accountType == "pat" {
		response = newOpenAIRejectedFieldTestResponse(http.StatusOK, alphaSearchResponsesSSE("search result"))
	}
	response.Header.Set("Content-Type", "text/event-stream")
	return response
}
