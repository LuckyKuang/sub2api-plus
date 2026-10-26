//go:build unit || !integration

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type providerFilesReferenceReviewRepository struct {
	service.ProviderFilesRepository
	file  *service.ProviderFile
	reads []string
}

func (r *providerFilesReferenceReviewRepository) Get(_ context.Context, id string) (*service.ProviderFile, error) {
	r.reads = append(r.reads, id)
	if r.file != nil && id == r.file.PublicID {
		return r.file, nil
	}
	return nil, service.ErrProviderFileNotFound
}

func providerFilesReferenceReviewHandler(t *testing.T, platform string, store *providerFilesReferenceReviewRepository) (*OpenAIGatewayHandler, *gin.Context, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	groupID := int64(4)
	key := &service.APIKey{ID: 3, UserID: 2, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: platform}}
	h := &OpenAIGatewayHandler{providerFilesService: service.NewProviderFilesService(store, nil)}
	return h, c, key
}

func TestProviderFilesReviewHandlerLocalIDsRespectProtocolMatrix(t *testing.T) {
	const id = "file-s2a-11111111-1111-4111-8111-111111111111"
	for _, protocol := range []string{service.APIProtocolChatCompletions, service.APIProtocolAnthropic, service.APIProtocolResponses} {
		for _, encoding := range []string{"literal", "hyphens", "first-letter"} {
			for _, known := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/known=%t", protocol, encoding, known), func(t *testing.T) {
					store := &providerFilesReferenceReviewRepository{}
					if known {
						store.file = &service.ProviderFile{PublicID: id, UserID: 2, APIKeyID: 3, GroupID: 4, Platform: service.PlatformDeepseek, AccountID: 11, Principal: "review-owner", RemoteID: "remote-review", State: service.ProviderFileReady}
					}
					h, c, key := providerFilesReferenceReviewHandler(t, service.PlatformDeepseek, store)
					escaped := id
					if encoding == "hyphens" {
						escaped = strings.ReplaceAll(id, "-", `\u002d`)
					}
					if encoding == "first-letter" {
						escaped = `\u0066` + id[1:]
					}
					body := []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"%s"}}]}]}`, escaped))
					path := "messages.0.content.0.source.file_id"
					switch protocol {
					case service.APIProtocolChatCompletions:
						body = []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"%s"}}]}]}`, escaped))
						path = "messages.0.content.0.image_url.url"
					case service.APIProtocolResponses:
						body = []byte(fmt.Sprintf(`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"%s"}]}]}`, escaped))
						path = "input.0.content.0.image_url"
					}
					if encoding != "literal" {
						require.NotContains(t, string(body), service.ProviderFileIDPrefix)
					}
					rewritten, err := h.resolveProviderFilesForInference(c, key, body, protocol)
					if protocol != service.APIProtocolAnthropic {
						require.ErrorIs(t, err, service.ErrProviderFilesUnsupported, "DeepSeek local Files are only supported by the native Anthropic inference entrypoint")
						require.Nil(t, rewritten)
						require.Empty(t, store.reads, "unsupported bridges cannot read Files ACL or dispatch inference")
						return
					}
					if known {
						require.NoError(t, err)
						require.Equal(t, "remote-review", gjson.GetBytes(rewritten, path).String())
					} else {
						require.ErrorIs(t, err, service.ErrProviderFileNotFound)
						require.Nil(t, rewritten)
					}
					require.Equal(t, []string{id}, store.reads)
				})
			}
		}
	}
}

func TestProviderFilesReviewMiniMaxBridgesIgnoreNonReferenceMentions(t *testing.T) {
	for _, protocol := range []string{service.APIProtocolAnthropic, service.APIProtocolResponses} {
		for name, body := range map[string]string{
			"plain-text":                            `{"messages":[{"role":"user","content":"Explain the file-s2a- namespace"}]}`,
			"text-block":                            `{"messages":[{"role":"user","content":[{"type":"text","text":"Explain file-s2a- IDs"}]}]}`,
			"tool-input":                            `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"echo","input":{"value":"file-s2a-123"}}]}]}`,
			"unknown-sibling":                       `{"messages":[{"role":"user","content":"hello"}],"extension":{"file_id":"file-s2a-123"}}`,
			"native-reference-with-unknown-sibling": `{"messages":[{"role":"user","content":[{"type":"video","source":{"type":"url","url":"mm_file://native-123"}}]}],"extension":{"file_id":"file-s2a-123"}}`,
		} {
			t.Run(protocol+"/"+name, func(t *testing.T) {
				store := &providerFilesReferenceReviewRepository{}
				h, c, key := providerFilesReferenceReviewHandler(t, service.PlatformMiniMax, store)
				got, err := h.resolveProviderFilesForInference(c, key, []byte(body), protocol)
				require.NoError(t, err, "ordinary text, tool arguments and unknown fields do not request a local Files bridge")
				require.Equal(t, []byte(body), got, "unrelated content and native references keep their compatibility path")
				require.Empty(t, store.reads, "non-reference mentions cannot perform Files ACL reads")
			})
		}
	}
}

func TestProviderFilesReviewMiniMaxBridgesRejectActualLocalReferences(t *testing.T) {
	for _, protocol := range []string{service.APIProtocolAnthropic, service.APIProtocolResponses} {
		for _, encoding := range []string{"literal", "escaped"} {
			t.Run(protocol+"/"+encoding, func(t *testing.T) {
				id := "file-s2a-11111111-1111-4111-8111-111111111111"
				if encoding == "escaped" {
					id = strings.ReplaceAll(id, "-", `\u002d`)
				}
				body := []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"video","source":{"type":"url","url":"mm_file://%s"}}]}]}`, id))
				store := &providerFilesReferenceReviewRepository{}
				h, c, key := providerFilesReferenceReviewHandler(t, service.PlatformMiniMax, store)
				got, err := h.resolveProviderFilesForInference(c, key, body, protocol)
				require.ErrorIs(t, err, service.ErrProviderFilesUnsupported, "a real local MiniMax reference cannot gain bridge support through JSON spelling")
				require.Nil(t, got)
				require.Empty(t, store.reads, "unsupported bridges must be rejected before remote or local Files lookup")
			})
		}
	}
}

func TestProviderFilesReviewDeepSeekBridgesKeepNativeAndInlineCompatibility(t *testing.T) {
	for _, protocol := range []string{service.APIProtocolChatCompletions, service.APIProtocolResponses} {
		for _, shape := range []string{"native-reference", "inline-image"} {
			t.Run(protocol+"/"+shape, func(t *testing.T) {
				part := `{"type":"file","file":{"file_id":"file-api-native-external"}}`
				if shape == "inline-image" {
					part = `{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}`
				}
				body := []byte(`{"messages":[{"role":"user","content":[` + part + `]}],"extension":{"file_id":"file-s2a-only-an-unknown-sibling"}}`)
				if protocol == service.APIProtocolResponses {
					part = `{"type":"input_file","file_id":"file-api-native-external"}`
					if shape == "inline-image" {
						part = `{"type":"input_image","image_url":"data:image/png;base64,AA=="}`
					}
					body = []byte(`{"input":[{"role":"user","content":[` + part + `]}],"extension":{"file_id":"file-s2a-only-an-unknown-sibling"}}`)
				}
				store := &providerFilesReferenceReviewRepository{}
				h, c, key := providerFilesReferenceReviewHandler(t, service.PlatformDeepseek, store)
				got, err := h.resolveProviderFilesForInference(c, key, body, protocol)
				require.NoError(t, err, "the local Files bridge restriction must not change native references, inline media or unknown sibling semantics")
				require.Equal(t, body, got)
				require.Empty(t, store.reads)
			})
		}
	}
}

type providerFilesReferenceReviewUpstream struct {
	bodies [][]byte
	paths  []string
}

func (u *providerFilesReferenceReviewUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	u.paths = append(u.paths, r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/files") {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"remote-review-image","type":"file"}`))}, nil
	}
	if !gjson.GetBytes(body, "stream").Bool() {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg-review","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`))}, nil
	}
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg-review","type":"message","role":"assistant","model":"deepseek-flash","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}

`
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
}

func (u *providerFilesReferenceReviewUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func (r *providerFilesReferenceReviewRepository) Reserve(_ context.Context, f *service.ProviderFile, _ *service.Account) (*service.ProviderFile, bool, error) {
	r.file = f
	return f, true, nil
}
func (r *providerFilesReferenceReviewRepository) Publish(_ context.Context, f *service.ProviderFile, _ *service.Account) error {
	r.file = f
	return nil
}

func TestProviderFilesReviewDeepSeekBridgesNeverDiscardLocalImages(t *testing.T) {
	for _, shape := range []string{"native-image", "chat-file", "chat-image-url", "responses-file", "responses-image"} {
		t.Run(shape, func(t *testing.T) {
			store := &providerFilesReferenceReviewRepository{}
			upstream := &providerFilesReferenceReviewUpstream{}
			gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			files := service.NewProviderFilesService(store, gateway)
			h, c, key := providerFilesReferenceReviewHandler(t, service.PlatformDeepseek, store)
			h.providerFilesService = files
			account := &service.Account{ID: 11, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Concurrency: 1, Credentials: map[string]any{"api_key": "review-file-owner", "api_protocol": service.APIProtocolAnthropic}}
			input, err := service.ValidateProviderFileUpload(service.PlatformDeepseek, "review.png", "image/png", "", 0, bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)))
			require.NoError(t, err)
			file, err := files.Upload(context.Background(), key, account, input, "review-image-reuse")
			require.NoError(t, err)
			require.Len(t, upstream.bodies, 1)
			protocol, path := service.APIProtocolAnthropic, "/v1/messages"
			body := []byte(fmt.Sprintf(`{"model":"deepseek-flash","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"inspect this image"},{"type":"image","source":{"type":"file","file_id":"%s"}}]}]}`, file.PublicID))
			switch shape {
			case "chat-file":
				protocol, path = service.APIProtocolChatCompletions, "/v1/chat/completions"
				body = []byte(fmt.Sprintf(`{"model":"deepseek-flash","messages":[{"role":"user","content":[{"type":"text","text":"inspect this image"},{"type":"file","file":{"file_id":"%s"}}]}]}`, file.PublicID))
			case "chat-image-url":
				protocol, path = service.APIProtocolChatCompletions, "/v1/chat/completions"
				body = []byte(fmt.Sprintf(`{"model":"deepseek-flash","messages":[{"role":"user","content":[{"type":"text","text":"inspect this image"},{"type":"image_url","image_url":{"url":"%s"}}]}]}`, file.PublicID))
			case "responses-file":
				protocol, path = service.APIProtocolResponses, "/v1/responses"
				body = []byte(fmt.Sprintf(`{"model":"deepseek-flash","input":[{"role":"user","content":[{"type":"input_text","text":"inspect this image"},{"type":"input_file","file_id":"%s"}]}]}`, file.PublicID))
			case "responses-image":
				protocol, path = service.APIProtocolResponses, "/v1/responses"
				body = []byte(fmt.Sprintf(`{"model":"deepseek-flash","input":[{"role":"user","content":[{"type":"input_text","text":"inspect this image"},{"type":"input_image","image_url":"%s"}]}]}`, file.PublicID))
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			rewritten, err := h.resolveProviderFilesForInference(c, key, body, protocol)
			if shape != "native-image" {
				require.ErrorIs(t, err, service.ErrProviderFilesUnsupported)
				require.Nil(t, rewritten)
				require.Empty(t, store.reads, "unsupported bridges are rejected before Files ACL reads")
				require.Len(t, upstream.bodies, 1, "an unsupported local-reference bridge must fail before inference and usage")
				return
			}
			require.NoError(t, err)
			result, err := gateway.ForwardAsAnthropic(c.Request.Context(), c, account, rewritten, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 2)
			require.Equal(t, "/anthropic/v1/messages", upstream.paths[1])
			var found bool
			for _, message := range gjson.GetBytes(upstream.bodies[1], "messages").Array() {
				for _, block := range message.Get("content").Array() {
					if block.Get("type").String() == "image" && block.Get("source.type").String() == "file" && block.Get("source.file_id").String() == "remote-review-image" {
						found = true
					}
				}
			}
			require.True(t, found, "an accepted local image reference must reach the actual Anthropic request; successful text-only inference would silently discard media")
		})
	}
}
