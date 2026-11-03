package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ForwardAsResponses shares Gemini's credential-owner transport, retries and
// usage collection with Chat, rendering its existing Responses events directly.
func (s *GeminiMessagesCompatService) ForwardAsResponses(ctx context.Context, c *gin.Context, account *Account, body []byte) (forwardResult *ForwardResult, firstByteResultErr error) {
	start := time.Now()
	ctx, byteTiming := beginFirstByteTiming(ctx, start)
	defer func() {
		if forwardResult != nil && forwardResult.FirstByteMs == nil {
			forwardResult.FirstByteMs = byteTiming.Milliseconds()
		}
	}()
	if err := CheckCompositeNativeForward(ctx, c, account, APIProtocolResponses, gjson.GetBytes(body, "model").String()); err != nil {
		return nil, err
	}
	if err := CheckCompositeResponsesContinuation(ctx, c, account, body); err != nil {
		return nil, err
	}
	var request apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	anthropic, err := apicompat.ResponsesToAnthropicRequest(&request)
	if err != nil {
		return nil, s.writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	anthropic.Stream = request.Stream
	converted, err := json.Marshal(anthropic)
	if err != nil {
		return nil, fmt.Errorf("marshal Gemini Responses request: %w", err)
	}
	return s.forwardClaudeBodyAsOpenAICompat(ctx, c, account, converted, request.Model, request.Stream, true, start, true)
}

func writeGeminiOpenAICompatResponse(c *gin.Context, response *apicompat.ChatCompletionsResponse, model string, responses bool) {
	if responses {
		c.JSON(http.StatusOK, apicompat.ChatCompletionsResponseToResponses(response, model, nil, nil, false, nil))
		return
	}
	c.JSON(http.StatusOK, response)
}
