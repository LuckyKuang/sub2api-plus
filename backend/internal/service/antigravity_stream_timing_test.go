package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAnthropicSSEBatchTimingIncludesTokensAfterSignature(t *testing.T) {
	processor := antigravity.NewStreamingProcessor("claude-test")
	processor.ProcessLine(`data: {"response":{"candidates":[{"content":{"parts":[{"thought":true,"thoughtSignature":"signature"}]}}]}}`)
	batch := processor.ProcessLine(`data: {"response":{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}}`)
	require.Contains(t, string(batch), "signature_delta")
	require.Contains(t, string(batch), "text_delta")
	observation := observeAnthropicSSEOutput(batch)
	require.True(t, observation.TokenLikeDelta, "the signature must not hide text later in the same converted batch")
	require.Equal(t, "reasoning", string(observation.Kind))
}

func TestAntigravityClaudeTimingUsesNativeOutput(t *testing.T) {
	for _, tc := range []struct {
		name, frames, kind string
		wantToken          bool
	}{
		{"image markdown", `data: {"response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":"STOP"}]}}` + "\n\n", "image", false},
		{"signature then text", `data: {"response":{"candidates":[{"content":{"parts":[{"thought":true,"thoughtSignature":"signature"}]}}]}}` + "\n\n" +
			`data: {"response":{"candidates":[{"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP"}]}}` + "\n\n", "reasoning", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			svc := &AntigravityGatewayService{settingService: &SettingService{}}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(tc.frames))}
			result, err := svc.handleClaudeStreamingResponse(c, resp, time.Now().Add(-250*time.Millisecond), "claude-test")
			require.NoError(t, err)
			require.Equal(t, tc.kind, result.firstOutputKind)
			if tc.wantToken {
				require.NotNil(t, result.firstTokenMs)
				require.GreaterOrEqual(t, *result.firstTokenMs, 250)
				require.NotNil(t, result.lastTokenMs)
			} else {
				require.Nil(t, result.firstTokenMs, "converted image markdown is not generated text")
				require.Nil(t, result.lastTokenMs)
			}
		})
	}
}
