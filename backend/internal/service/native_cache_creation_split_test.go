//go:build unit || !integration

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesTerminalCacheCreationMatchesMessagesUsage(t *testing.T) {
	const wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cache\"}}\n\n" +
		`data: {"type":"response.completed","usage":{"input_tokens":20,"output_tokens":4,"cache_creation_input_tokens":4},"response":{"id":"resp_cache","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":1}}}` + "\n\n"
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	response := &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(wire)),
	}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	result, err := (&OpenAIGatewayService{}).handleAnthropicStreamingResponse(
		response, c, account, "gpt-6.2-sol", "gpt-6.2-sol", "gpt-6.2-sol", time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheCreationInputTokens)

	for _, line := range strings.Split(rec.Body.String(), "\n") {
		payload := strings.TrimPrefix(line, "data: ")
		if gjson.Get(payload, "type").String() == "message_delta" {
			require.EqualValues(t, 9, gjson.Get(payload, "usage.input_tokens").Int())
			require.EqualValues(t, 2, gjson.Get(payload, "usage.output_tokens").Int())
			require.EqualValues(t, 1, gjson.Get(payload, "usage.cache_creation_input_tokens").Int())
			return
		}
	}
	t.Fatal("missing terminal Messages usage")
}

// An Anthropic-native upstream reached through the OpenAI gateway keeps its own
// 5m/1h write split. Flattening it into the aggregate would price every write as
// the cheaper 5m class.
func TestClaudeUsageToOpenAIUsageKeepsAnthropicSplit(t *testing.T) {
	usage := &ClaudeUsage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheCreationInputTokens: 300,
		CacheCreation5mTokens:    100,
		CacheCreation1hTokens:    200,
		CacheReadInputTokens:     4000,
	}

	got := claudeUsageToOpenAIUsage(usage)
	require.Equal(t, 300, got.CacheCreationInputTokens)
	require.Equal(t, 100, got.CacheCreation5mInputTokens)
	require.Equal(t, 200, got.CacheCreation1hInputTokens)
	require.Equal(t, 10+300+4000, got.InputTokens)
	require.Equal(t, 20, got.OutputTokens)
}

// A detail-only snapshot still yields an inclusive input total and an aggregate.
func TestClaudeUsageToOpenAIUsageDerivesAggregateFromSplit(t *testing.T) {
	usage := &ClaudeUsage{
		InputTokens:           7,
		CacheCreation5mTokens: 40,
		CacheCreation1hTokens: 60,
		CacheReadInputTokens:  90,
	}

	got := claudeUsageToOpenAIUsage(usage)
	require.Equal(t, 100, got.CacheCreationInputTokens)
	require.Equal(t, 7+100+90, got.InputTokens)
	require.Equal(t, 100, got.cacheCreationTotal())
}

func TestOpenAIUsageCacheCreationTotalCoversAnthropicSplit(t *testing.T) {
	require.Equal(t, 90, OpenAIUsage{CacheCreation5mInputTokens: 60, CacheCreation1hInputTokens: 30}.cacheCreationTotal())
	require.Equal(t, 500, OpenAIUsage{
		CacheCreationInputTokens:   500,
		CacheCreation5mInputTokens: 60,
		CacheCreation1hInputTokens: 30,
	}.cacheCreationTotal(), "a positive aggregate wins over the split")
	require.Equal(t, 120, OpenAIUsage{
		CacheCreation30mInputTokens: 100,
		CacheCreation12hInputTokens: 20,
	}.cacheCreationTotal(), "OpenAI TTL buckets stay authoritative when present")
}

// The money path: tokens projected from a native Anthropic response must bill the
// 1h class at the 1h price instead of collapsing into the 5m price.
func TestNativeAnthropicSplitReachesCacheCreationPricing(t *testing.T) {
	anthropicUsage := &ClaudeUsage{
		InputTokens:              1000,
		CacheCreationInputTokens: 300,
		CacheCreation5mTokens:    100,
		CacheCreation1hTokens:    200,
		CacheReadInputTokens:     500,
	}
	openAIUsage := claudeUsageToOpenAIUsage(anthropicUsage)

	tokens := UsageTokens{
		InputTokens:           max(openAIUsage.InputTokens-openAIUsage.CacheReadInputTokens-openAIUsage.cacheCreationTotal(), 0),
		OutputTokens:          openAIUsage.OutputTokens,
		CacheCreationTokens:   openAIUsage.cacheCreationTotal(),
		CacheCreation5mTokens: openAIUsage.CacheCreation5mInputTokens,
		CacheCreation1hTokens: openAIUsage.CacheCreation1hInputTokens,
		CacheReadTokens:       openAIUsage.CacheReadInputTokens,
	}
	require.Equal(t, 1000, tokens.InputTokens)

	pricing := &ModelPricing{SupportsCacheBreakdown: true, CacheCreation5mPrice: 2e-6, CacheCreation1hPrice: 4e-6}
	cost := (&BillingService{}).computeCacheCreationCost(pricing, tokens, 0, 1, 1)
	// 100 * 2e-6 + 200 * 4e-6
	require.InDelta(t, 0.001, cost, 1e-12)
}

// Buffered paths must not report a fabricated empty usage when the upstream did
// report cache-only usage.
func TestClaudeUsageHasReportedTokens(t *testing.T) {
	require.False(t, ClaudeUsage{}.hasReportedTokens())
	require.True(t, ClaudeUsage{CacheReadInputTokens: 5}.hasReportedTokens())
	require.True(t, ClaudeUsage{CacheCreation5mTokens: 5}.hasReportedTokens())
	require.True(t, ClaudeUsage{InputTokens: 1}.hasReportedTokens())
	require.True(t, ClaudeUsage{OutputTokens: 1}.hasReportedTokens())
}

// The buffered response carries the reconciled total rather than a zero that the
// conversion layer would report to the client.
func TestNormalizeAnthropicEventUsageForResponsesCarriesDerivedTotal(t *testing.T) {
	event := &apicompat.AnthropicStreamEvent{}
	normalizeAnthropicEventUsageForResponses(event, ClaudeUsage{
		InputTokens:           12,
		OutputTokens:          34,
		CacheReadInputTokens:  500,
		CacheCreation5mTokens: 70,
		CacheCreation1hTokens: 30,
	})
	// The helper only writes the message usage; an event without one stays empty.
	require.Nil(t, event.Usage)

	message := &apicompat.AnthropicResponse{}
	normalizeAnthropicEventUsageForResponses(&apicompat.AnthropicStreamEvent{Message: message}, ClaudeUsage{
		InputTokens:           12,
		OutputTokens:          34,
		CacheReadInputTokens:  500,
		CacheCreation5mTokens: 70,
		CacheCreation1hTokens: 30,
	})
	require.Equal(t, 12, message.Usage.InputTokens)
	require.Equal(t, 34, message.Usage.OutputTokens)
	require.Equal(t, 500, message.Usage.CacheReadInputTokens)
	require.Equal(t, 100, message.Usage.CacheCreationInputTokens, "明细推导出的聚合量必须写进响应")
}
