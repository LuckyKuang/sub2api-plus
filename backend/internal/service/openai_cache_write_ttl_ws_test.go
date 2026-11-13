//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// CHANNEL_PRICING.md requires native WS usage to reach settlement with the
// reported durations, generic fallback, and independent channel/group prices.
func TestOpenAICacheWriteTTLNativeWSSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, tc := range []struct {
			name, details string
			short, long   int
			cost          float64
		}{
			{"30m", `{"ttl_30m_tokens":1000000,"ttl_12h_tokens":0}`, 1000000, 0, 1},
			{"12h", `{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`, 0, 1000000, 2},
			{"mixed", `{"ttl_30m_tokens":500000,"ttl_12h_tokens":500000}`, 500000, 500000, 1.5},
			{"invalid", `{"ttl_30m_tokens":-1,"ttl_12h_tokens":1000001}`, 0, 0, 1},
			{"missing", `null`, 0, 0, 1},
		} {
			t.Run(accountType+"/"+tc.name, func(t *testing.T) {
				token := "fixture-key"
				if accountType == AccountTypeOAuth {
					token = "fixture-token"
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
				cfg := svc.cfg
				cfg.Gateway.OpenAIWS.Enabled = true
				cfg.Gateway.OpenAIWS.APIKeyEnabled = true
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
				cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
				svc.httpUpstream = &httpUpstreamRecorder{}
				svc.cache = &stubGatewayCache{}
				svc.openaiWSResolver = NewOpenAIWSProtocolResolver(cfg)
				dialer := &openAIWSCaptureDialer{conn: &openAIWSCaptureConn{events: [][]byte{
					[]byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_ws_ttl","model":"gpt-6-luna","status":"completed","usage":{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000,"cache_write_tokens_details":%s}}}}`, tc.details)),
				}}}
				svc.openaiWSPassthroughDialer = dialer
				account := &Account{ID: 453, Platform: PlatformOpenAI, Type: accountType, Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "fixture-key", "access_token": "fixture-token", "chatgpt_account_id": "fixture-account"},
					Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough, "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}}
				resultCh := make(chan *OpenAIForwardResult, 1)
				hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
					if err == nil && result != nil {
						resultCh <- result
					}
				}}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer func() { _ = conn.CloseNow() }()
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r
					_, first, err := conn.Read(ctx)
					if err == nil {
						_ = svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, token, first, hooks)
					}
				}))
				defer server.Close()
				conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
				require.NoError(t, err)
				defer func() { _ = conn.CloseNow() }()
				require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-luna","input":"hello"}`)))
				_, _, err = conn.Read(ctx)
				require.NoError(t, err)
				var result *OpenAIForwardResult
				select {
				case result = <-resultCh:
				case <-ctx.Done():
					t.Fatal("native WS turn did not produce usage")
				}
				_ = conn.Close(coderws.StatusNormalClosure, "done")
				require.Equal(t, 1, dialer.DialCount(), "settlement must exercise the native upstream relay")
				require.Equal(t, 1000000, result.Usage.CacheCreationInputTokens)
				require.Equal(t, tc.short, result.Usage.CacheCreation30mInputTokens)
				require.Equal(t, tc.long, result.Usage.CacheCreation12hInputTokens)
				const groupID int64 = 777
				pricing := ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{"gpt-6-luna"}, BillingMode: BillingModeToken,
					CacheWritePrice: float64Ptr(1e-6), CacheWrite12hPrice: float64Ptr(2e-6)}
				svc.channelService = newChannelServiceWithPricings(groupID, []ChannelModelPricing{pricing})
				svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
				require.NoError(t, svc.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result,
					APIKey: &APIKey{ID: 1, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 0.5}},
					User:   &User{ID: 1}, Account: account,
					ChannelUsageFields: ChannelUsageFields{OriginalModel: "gpt-6-luna", BillingModelSource: BillingModelSourceRequested}}))
				require.NotNil(t, usageRepo.lastLog)
				require.Equal(t, tc.short, usageRepo.lastLog.CacheCreation30mTokens)
				require.Equal(t, tc.long, usageRepo.lastLog.CacheCreation12hTokens)
				require.InDelta(t, tc.cost, usageRepo.lastLog.CacheCreationCost, 1e-12)
				require.InDelta(t, tc.cost*0.5, userRepo.lastAmount, 1e-12)
			})
		}
	}
}
