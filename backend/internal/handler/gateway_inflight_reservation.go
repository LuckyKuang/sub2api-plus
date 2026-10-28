package handler

import (
	"context"
	"strings"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// inflightReservationEstimator 估算单请求在途预留金额（USD）；false 表示无法定价。
type inflightReservationEstimator interface {
	EstimateInflightReservation(ctx context.Context, apiKey *service.APIKey, req service.InflightEstimateRequest) (float64, bool)
}

// requestMaxOutputTokens 从请求体中提取输出 token 上限（兼容 Anthropic / OpenAI Chat / Responses / Gemini）。
func requestMaxOutputTokens(protocol string, body []byte) int {
	var paths []string
	switch protocol {
	case service.ContentModerationProtocolAnthropicMessages:
		paths = []string{"max_tokens"}
	case service.ContentModerationProtocolOpenAIChat:
		paths = []string{"max_completion_tokens", "max_tokens"}
	case service.ContentModerationProtocolOpenAIResponses:
		paths = []string{"max_output_tokens"}
	case service.ContentModerationProtocolGemini:
		paths = []string{"generationConfig.maxOutputTokens", "generation_config.max_output_tokens"}
	}
	for _, path := range paths {
		if v := gjson.GetBytes(body, path); v.Exists() && v.Type == gjson.Number && v.Int() > 0 {
			return int(v.Int())
		}
	}
	return 0
}

// tokenInflightEstimate 文本类请求的估算输入。
func tokenInflightEstimate(protocol, model string, body []byte) service.InflightEstimateRequest {
	return (service.InflightEstimateRequest{
		Protocol:  protocol,
		Model:     model,
		BodyBytes: len(body),
		MaxTokens: requestMaxOutputTokens(protocol, body),
		Kind:      service.InflightEstimateToken,
	}).WithRequestPricingMetadata(body)
}

func inflightNoop() {}

// reserveInflightBalance 在 CheckBillingEligibility 之后为余额模式请求登记在途预留。
//
// 成功时把预留句柄挂到 c.Request 的 context 上：之后通过 submit*UsageRecordTask 提交的
// 计费任务会接管一个引用，直到余额缓存实际扣减后才释放。返回的 done 必须 defer 调用
// （停止续期并归还 handler 引用；没有提交计费任务时即刻释放）。
// 开关关闭、订阅模式、Redis 故障时返回 no-op（fail-open）；无法定价时默认 fail-open，
// 配置 fail_closed_on_unpriced=true 时返回 ErrInsufficientBalance。
func reserveInflightBalance(
	c *gin.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (func(), error) {
	if c == nil || c.Request == nil {
		return inflightNoop, nil
	}
	req.RequireCompact = strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/responses/compact")
	if req.Protocol == service.ContentModerationProtocolAnthropicMessages {
		for _, token := range strings.Split(c.GetHeader("anthropic-beta"), ",") {
			if strings.TrimSpace(token) == claude.BetaFastMode {
				req.AnthropicFast = true
				break
			}
		}
	}
	ctx, done, err := reserveInflightBalanceCtx(c.Request.Context(), billing, estimator, apiKey, subscription, req)
	if err != nil {
		return inflightNoop, err
	}
	c.Request = c.Request.WithContext(ctx)
	return done, nil
}

// reserveInflightBalanceCtx 同 reserveInflightBalance，但返回携带预留句柄的新 context
// （供 WebSocket 等自管 context 的路径）。
func reserveInflightBalanceCtx(
	ctx context.Context,
	billing *service.BillingCacheService,
	estimator inflightReservationEstimator,
	apiKey *service.APIKey,
	subscription *service.UserSubscription,
	req service.InflightEstimateRequest,
) (context.Context, func(), error) {
	// This is a new admission owner, including fallback-group admissions and
	// later WS turns. Inactive/free admissions cannot inherit a closed plan or
	// a released handle from their parent request context.
	ctx = context.WithValue(service.WithoutInflightReservation(ctx), inflightAccountReservationPlanKey{}, (*inflightAccountReservationPlan)(nil))
	if billing == nil || estimator == nil || apiKey == nil || apiKey.User == nil || !billing.InflightReservationEnabled() {
		return ctx, inflightNoop, nil
	}
	if apiKey.Group != nil && apiKey.Group.IsSubscriptionType() && subscription != nil {
		return ctx, inflightNoop, nil
	}
	estimate, priced := estimator.EstimateInflightReservation(ctx, apiKey, req)
	if !priced && billing.InflightReservationFailClosedOnUnpriced() {
		return ctx, inflightNoop, service.ErrInsufficientBalance
	}
	plan := &inflightAccountReservationPlan{billing: billing, estimator: estimator, key: apiKey, subscription: subscription, request: req}
	if estimate > 0 {
		res, err := billing.ReserveInflight(ctx, apiKey.User, apiKey.Group, subscription, estimate)
		if err != nil {
			return ctx, inflightNoop, err
		}
		plan.reservation = res
	}
	ctx = context.WithValue(ctx, inflightAccountReservationPlanKey{}, plan)
	ctx = service.WithInflightReservation(ctx, plan.reservation)
	return ctx, plan.done, nil
}

// Keep metadata even when the preliminary quote was free or unavailable: a
// selected account may introduce a paid concrete model. No request body is kept.
type inflightAccountReservationPlanKey struct{}

type inflightAccountReservationPlan struct {
	mu           sync.Mutex
	billing      *service.BillingCacheService
	estimator    inflightReservationEstimator
	key          *service.APIKey
	subscription *service.UserSubscription
	request      service.InflightEstimateRequest
	reservation  *service.InflightReservation
	closed       bool
}

func (p *inflightAccountReservationPlan) done() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	res := p.reservation
	p.mu.Unlock()
	res.HandlerDone()
}

type inflightAccountReservationEstimator interface {
	EstimateInflightReservationForAccount(context.Context, *service.APIKey, service.InflightEstimateRequest, *service.Account) (float64, bool)
}

// Reconcile before binding or sending. Increasing the existing owner atomically
// avoids both a release/reserve gap and counting this request twice.
func ensureAccountInflightReservationCtx(ctx context.Context, account *service.Account) (context.Context, error) {
	plan, _ := ctx.Value(inflightAccountReservationPlanKey{}).(*inflightAccountReservationPlan)
	if plan == nil || account == nil {
		return ctx, nil
	}
	estimator, ok := plan.estimator.(inflightAccountReservationEstimator)
	if !ok {
		return ctx, nil
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	if plan.closed {
		return ctx, service.ErrInsufficientBalance
	}
	estimate, priced := estimator.EstimateInflightReservationForAccount(ctx, plan.key, plan.request, account)
	if !priced {
		if plan.billing.InflightReservationFailClosedOnUnpriced() {
			return ctx, service.ErrInsufficientBalance
		}
		// Unknown selected pricing retains fail-open, but an existing owner
		// must still be alive before another upstream attempt can use it.
		if plan.reservation != nil {
			estimate = plan.reservation.Amount()
		} else {
			return ctx, nil
		}
	}
	if plan.reservation != nil {
		if err := plan.billing.IncreaseInflightReservation(ctx, plan.reservation, estimate); err != nil {
			return ctx, err
		}
	} else if estimate > 0 {
		res, err := plan.billing.ReserveInflight(ctx, plan.key.User, plan.key.Group, plan.subscription, estimate)
		if err != nil {
			return ctx, err
		}
		plan.reservation = res
	}
	return service.WithInflightReservation(ctx, plan.reservation), nil
}

func ensureAccountInflightReservation(c *gin.Context, account *service.Account) error {
	if c == nil || c.Request == nil {
		return nil
	}
	ctx, err := ensureAccountInflightReservationCtx(c.Request.Context(), account)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
	}
	return err
}

// grokMediaInflightEstimate 媒体生成请求的估算输入；状态/内容查询返回空模型（不预留：
// 查询会为已生成的媒体计费，不能因余额预留而拦截用户取回已付费结果）。
func grokMediaInflightEstimate(endpoint service.GrokMediaEndpoint, model string, info service.GrokMediaRequestInfo, body []byte) service.InflightEstimateRequest {
	if !endpoint.IsGenerationRequest() {
		return service.InflightEstimateRequest{}
	}
	switch endpoint {
	case service.GrokMediaEndpointImagesGenerations, service.GrokMediaEndpointImagesEdits:
		return service.InflightEstimateRequest{Model: model, BodyBytes: len(body), Kind: service.InflightEstimateImage, Units: info.N}
	default:
		return service.InflightEstimateRequest{
			Model:                model,
			BodyBytes:            len(body),
			Kind:                 service.InflightEstimateVideo,
			Units:                1,
			VideoResolution:      info.Resolution,
			VideoDurationSeconds: info.DurationSeconds,
		}
	}
}

// grokVoiceSTTBytesPerSecond STT 时长粗估（~128kbps 压缩音频）。
const grokVoiceSTTBytesPerSecond = 16000

// grokVoiceInflightEstimate 语音 HTTP 接口估算：TTS 按输入字符数（百万字符），STT 按音频字节粗估时长（小时）。
// 其他接口（custom-voices）无音频计量，返回的估算为 0（不预留）。
func grokVoiceInflightEstimate(endpoint string, body []byte) service.InflightEstimateRequest {
	req := service.InflightEstimateRequest{Model: endpoint, Kind: service.InflightEstimateAudio, AudioMode: endpoint}
	switch endpoint {
	case "tts":
		req.AudioUnits = float64(len([]rune(extractGrokTTSInputText(body)))) / 1e6
	case "stt":
		req.AudioUnits = float64(len(body)) / grokVoiceSTTBytesPerSecond / 3600
	}
	return req
}
