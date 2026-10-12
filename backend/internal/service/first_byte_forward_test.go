//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/firstbyte"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type firstByteHTTPTestUpstream struct{ client *http.Client }

func (u *firstByteHTTPTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}
func (u *firstByteHTTPTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// docs/USAGE_TIMING.md: upstream body data precedes tokens even through
// protocol adapters. Capture a real HTTP body rather than fabricating timing.
func TestFirstByteForwardAcrossChatMessagesAndResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": upstream heartbeat\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(80 * time.Millisecond)
		_, _ = io.WriteString(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	rt := firstbyte.WrapTransport(&http.Transport{})
	client := &http.Client{Transport: rt}
	defer client.CloseIdleConnections()
	for _, endpoint := range []string{"chat/completions", "messages", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.1","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}],"input":"hi"}`)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(string(body)))
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &firstByteHTTPTestUpstream{client: client}, toolCorrector: NewCodexToolCorrector()}
			account := forceChatResponsesFallbackAccount()
			account.Credentials["base_url"] = server.URL
			var result *OpenAIForwardResult
			var err error
			switch endpoint {
			case "chat/completions":
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			case "messages":
				result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			default:
				result, err = svc.Forward(context.Background(), c, account, body)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.FirstByteMs)
			require.NotNil(t, result.FirstTokenMs)
			require.GreaterOrEqual(t, *result.FirstTokenMs-*result.FirstByteMs, 40)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), "hello", "timing must preserve adapted output")
		})
	}
}
