package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

const gatewayStreamHeartbeatBytesKey = "gateway_stream_heartbeat_bytes"

func recordGatewayStreamHeartbeat(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	total, _ := c.Get(gatewayStreamHeartbeatBytesKey)
	bytes, _ := total.(int)
	c.Set(gatewayStreamHeartbeatBytesKey, bytes+written)
}

func gatewayStreamHasOnlyHeartbeats(c *gin.Context) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	value, ok := c.Get(gatewayStreamHeartbeatBytesKey)
	if !ok {
		return false
	}
	heartbeatBytes, _ := value.(int)
	return heartbeatBytes > 0 && c.Writer.Size() == heartbeatBytes
}

// claudeCodeValidator is a singleton validator for Claude Code client detection
var claudeCodeValidator = service.NewClaudeCodeValidator()

// SetClaudeCodeClientContext 检查请求是否来自 Claude Code 客户端，并设置到 context 中
// 返回更新后的 context
func SetClaudeCodeClientContext(c *gin.Context, body []byte, parsedReq *service.ParsedRequest) {
	if c == nil || c.Request == nil {
		return
	}
	ua := c.GetHeader("User-Agent")
	// Fast path：非 Claude CLI UA 直接判定 false，避免热路径二次 JSON 反序列化。
	if !claudeCodeValidator.ValidateUserAgent(ua) {
		ctx := service.SetClaudeCodeClient(c.Request.Context(), false)
		c.Request = c.Request.WithContext(ctx)
		return
	}

	isClaudeCode := false
	if !strings.Contains(c.Request.URL.Path, "messages") {
		// 与 Validate 行为一致：非 messages 路径 UA 命中即可视为 Claude Code 客户端。
		isClaudeCode = true
	} else {
		// 仅在确认为 Claude CLI 且 messages 路径时再做 body 解析。
		bodyMap := claudeCodeBodyMapFromParsedRequest(parsedReq)
		if bodyMap == nil && len(body) > 0 {
			_ = json.Unmarshal(body, &bodyMap)
		}
		isClaudeCode = claudeCodeValidator.Validate(c.Request, bodyMap)
	}

	// 更新 request context
	ctx := service.SetClaudeCodeClient(c.Request.Context(), isClaudeCode)

	// 仅在确认为 Claude Code 客户端时提取版本号写入 context
	if isClaudeCode {
		if version := claudeCodeValidator.ExtractVersion(ua); version != "" {
			ctx = service.SetClaudeCodeVersion(ctx, version)
		}
	}

	c.Request = c.Request.WithContext(ctx)
}

func claudeCodeBodyMapFromParsedRequest(parsedReq *service.ParsedRequest) map[string]any {
	if parsedReq == nil {
		return nil
	}
	bodyMap := map[string]any{
		"model": parsedReq.Model,
	}
	// 探测识别（max_tokens=1）需要看到该字段，复用已解析请求时一并带上。
	if parsedReq.MaxTokens > 0 {
		bodyMap["max_tokens"] = parsedReq.MaxTokens
	}
	if parsedReq.HasSystem {
		if system, ok := parsedReq.SystemValue(); ok {
			bodyMap["system"] = system
		} else {
			bodyMap["system"] = nil
		}
	}
	if parsedReq.MetadataUserID != "" {
		bodyMap["metadata"] = map[string]any{"user_id": parsedReq.MetadataUserID}
	}
	return bodyMap
}

// 并发槽位等待相关常量
//
// 性能优化说明：
// 原实现使用固定间隔（100ms）轮询并发槽位，存在以下问题：
// 1. 高并发时频繁轮询增加 Redis 压力
// 2. 固定间隔可能导致多个请求同时重试（惊群效应）
//
// 新实现使用指数退避 + 抖动算法：
// 1. 初始退避 100ms，每次乘以 1.5，最大 2s
// 2. 添加 ±20% 的随机抖动，分散重试时间点
// 3. 减少 Redis 压力，避免惊群效应
const (
	// maxConcurrencyWait 等待并发槽位的最大时间
	maxConcurrencyWait = service.WorkerUserConcurrencyWaitTimeout
	// defaultPingInterval 流式响应等待时发送 ping 的默认间隔
	defaultPingInterval = 10 * time.Second
	// initialBackoff 初始退避时间
	initialBackoff = 100 * time.Millisecond
	// backoffMultiplier 退避时间乘数（指数退避）
	backoffMultiplier = 1.5
	// maxBackoff 最大退避时间
	maxBackoff = 2 * time.Second
)

// SSEPingFormat defines the format of SSE ping events for different platforms
type SSEPingFormat string

const (
	// SSEPingFormatClaude is the Claude/Anthropic SSE ping format
	SSEPingFormatClaude SSEPingFormat = "data: {\"type\": \"ping\"}\n\n"
	// SSEPingFormatNone indicates no ping should be sent (e.g., OpenAI has no ping spec)
	SSEPingFormatNone SSEPingFormat = ""
	// SSEPingFormatComment is an SSE comment ping for OpenAI/Codex CLI clients
	SSEPingFormatComment SSEPingFormat = ":\n\n"
)

// ConcurrencyError represents a concurrency limit error with context
type ConcurrencyError struct {
	SlotType  string
	IsTimeout bool
}

func (e *ConcurrencyError) Error() string {
	if e.IsTimeout {
		return fmt.Sprintf("timeout waiting for %s concurrency slot", e.SlotType)
	}
	return fmt.Sprintf("%s concurrency limit reached", e.SlotType)
}

type WaitQueueFullError struct {
	SlotType string
}

func (e *WaitQueueFullError) Error() string {
	return "Too many pending requests, please retry later"
}

// ConcurrencyHelper provides common concurrency slot management for gateway handlers
type ConcurrencyHelper struct {
	concurrencyService  *service.ConcurrencyService
	billingCacheService *service.BillingCacheService
	pingFormat          SSEPingFormat
	pingInterval        time.Duration
}

// WithRPMAdmission binds the gateway's existing user/group policy to all Gin
// admission paths, including content-free controls. Internal rechecks share the
// process-owned request scope; no API-key RPM counter is introduced.
func (h *ConcurrencyHelper) WithRPMAdmission(billing *service.BillingCacheService) *ConcurrencyHelper {
	h.billingCacheService = billing
	return h
}

// NewConcurrencyHelper creates a new ConcurrencyHelper
func NewConcurrencyHelper(concurrencyService *service.ConcurrencyService, pingFormat SSEPingFormat, pingInterval time.Duration) *ConcurrencyHelper {
	if pingInterval <= 0 {
		pingInterval = defaultPingInterval
	}
	return &ConcurrencyHelper{
		concurrencyService: concurrencyService,
		pingFormat:         pingFormat,
		pingInterval:       pingInterval,
	}
}

// wrapReleaseOnDone ensures release runs at most once and still triggers on context cancellation.
// 用于避免客户端断开或上游超时导致的并发槽位泄漏。
// 优化：基于 context.AfterFunc 注册回调，避免每请求额外守护 goroutine。
func wrapReleaseOnDone(ctx context.Context, releaseFunc func()) func() {
	if releaseFunc == nil {
		return nil
	}
	var once sync.Once
	releaseOnce := func() {
		once.Do(releaseFunc)
	}
	// Detached upstream draining can continue after a client disconnect. Keep
	// counting that execution until Forward returns, unless capacity itself is
	// lost; its separate cancellation domain also stops the upstream operation.
	stop := context.AfterFunc(service.ConcurrencyCapacityContext(ctx), releaseOnce)

	return func() {
		_ = stop()
		releaseOnce()
	}
}

// IncrementWaitCount increments the wait count for a user
func (h *ConcurrencyHelper) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	return h.concurrencyService.IncrementWaitCount(ctx, userID, maxWait)
}

// DecrementWaitCount decrements the wait count for a user
func (h *ConcurrencyHelper) DecrementWaitCount(ctx context.Context, userID int64) {
	h.concurrencyService.DecrementWaitCount(ctx, userID)
}

// IncrementAccountWaitCount increments the wait count for an account
func (h *ConcurrencyHelper) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return h.concurrencyService.IncrementAccountWaitCount(ctx, accountID, maxWait)
}

// DecrementAccountWaitCount decrements the wait count for an account
func (h *ConcurrencyHelper) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
	h.concurrencyService.DecrementAccountWaitCount(ctx, accountID)
}

// TryAcquireUserSlot 尝试立即获取用户并发槽位。
// Callers without Gin must install WithConcurrencyLeaseContext on execution
// before calling; this API returns only release/capacity, not a replacement ctx.
// 返回值: (releaseFunc, acquired, error)
func (h *ConcurrencyHelper) TryAcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int) (func(), bool, error) {
	if h == nil || h.concurrencyService == nil {
		return nil, false, fmt.Errorf("concurrency service is unavailable")
	}
	result, err := h.concurrencyService.AcquireUserSlot(ctx, userID, maxConcurrency)
	if err != nil {
		return nil, false, err
	}
	if !result.Acquired {
		return nil, false, nil
	}
	return result.ReleaseFunc, true, nil
}

func (h *ConcurrencyHelper) TryAcquireUserSlotForAPIKey(ctx context.Context, userID int64, maxConcurrency int, apiKeyID int64) (func(), bool, error) {
	releaseFunc, acquired, err := h.TryAcquireUserSlot(ctx, userID, maxConcurrency)
	if err != nil || !acquired {
		return releaseFunc, acquired, err
	}
	return h.withAPIKeySlot(ctx, apiKeyID, releaseFunc), true, nil
}

// AcquireOpenAIWSIngressLease bounds the audited client WebSocket lifecycle,
// independently from per-turn user and account slots.
func (h *ConcurrencyHelper) AcquireOpenAIWSIngressLease(ctx context.Context, apiKeyID int64, maxConnections int) (*service.OpenAIWSIngressLease, bool, error) {
	if h == nil || h.concurrencyService == nil {
		return nil, false, fmt.Errorf("concurrency service is unavailable")
	}
	return h.concurrencyService.AcquireOpenAIWSIngressLease(ctx, apiKeyID, maxConnections)
}

// TryAcquireAccountSlot 尝试立即获取账号并发槽位。
// 返回值: (releaseFunc, acquired, error)
func (h *ConcurrencyHelper) TryAcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int) (func(), bool, error) {
	if h == nil || h.concurrencyService == nil {
		return nil, false, fmt.Errorf("concurrency service is unavailable")
	}
	result, err := h.concurrencyService.AcquireAccountSlot(ctx, accountID, maxConcurrency)
	if err != nil {
		return nil, false, err
	}
	if !result.Acquired {
		return nil, false, nil
	}
	return result.ReleaseFunc, true, nil
}

// installConcurrencyExecutionContext shares one lease cancellation domain with
// every Gin user/account acquisition and the upstream operation that follows.
func installConcurrencyExecutionContext(c *gin.Context) (context.Context, error) {
	if c == nil || c.Request == nil {
		return nil, fmt.Errorf("request context is unavailable")
	}
	ctx, _ := service.WithConcurrencyLeaseContext(service.WithRPMAdmissionScope(c.Request.Context()))
	c.Request = c.Request.WithContext(ctx)
	return ctx, nil
}

// AcquireUserSlot admits one external execution immediately. A full user is
// rejected before queue accounting, account selection, or streaming writes.
// Durable workers use their separate bounded wait during ownership handoff.
func (h *ConcurrencyHelper) AcquireUserSlot(c *gin.Context, userID int64, maxConcurrency int) (func(), error) {
	ctx, contextErr := installConcurrencyExecutionContext(c)
	if contextErr != nil {
		return nil, contextErr
	}

	// Try to acquire immediately
	releaseFunc, acquired, err := h.TryAcquireUserSlot(ctx, userID, maxConcurrency)
	if err != nil {
		return nil, err
	}

	if acquired {
		if key, ok := middleware2.GetAPIKeyFromContext(c); ok && key != nil {
			if err := h.billingCacheService.CheckRequestRPM(ctx, key.User, key.Group); err != nil {
				if releaseFunc != nil {
					releaseFunc()
				}
				setRPMRetryAfter(c, err)
				return nil, err
			}
		}
		return h.withAPIKeySlotFromGin(c, releaseFunc), nil
	}

	return nil, &ConcurrencyError{SlotType: "user"}
}

func setRPMRetryAfter(c *gin.Context, err error) {
	if errors.Is(err, service.ErrUserRPMExceeded) || errors.Is(err, service.ErrGroupRPMExceeded) {
		_, _, _, retry := billingErrorDetails(err)
		c.Header("Retry-After", strconv.Itoa(retry))
	}
}

func (h *ConcurrencyHelper) withAPIKeySlotFromGin(c *gin.Context, releaseFunc func()) func() {
	if c == nil {
		return releaseFunc
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		return releaseFunc
	}
	return h.withAPIKeySlot(c.Request.Context(), apiKey.ID, releaseFunc)
}

func (h *ConcurrencyHelper) withAPIKeySlot(ctx context.Context, apiKeyID int64, releaseFunc func()) func() {
	if h == nil || h.concurrencyService == nil || apiKeyID <= 0 {
		return releaseFunc
	}
	apiKeyReleaseFunc := h.concurrencyService.TrackAPIKeySlot(ctx, apiKeyID)
	var once sync.Once
	return func() {
		once.Do(func() {
			if releaseFunc != nil {
				releaseFunc()
			}
			if apiKeyReleaseFunc != nil {
				apiKeyReleaseFunc()
			}
		})
	}
}

// waitForSlotWithPingTimeout waits for a concurrency slot with a custom timeout.
func (h *ConcurrencyHelper) waitForSlotWithPingTimeout(c *gin.Context, slotType string, id int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error) {
	if h == nil || h.concurrencyService == nil {
		return nil, fmt.Errorf("concurrency service is unavailable")
	}
	executionCtx, contextErr := installConcurrencyExecutionContext(c)
	if contextErr != nil {
		return nil, contextErr
	}
	// The wait deadline bounds queueing only. Renewal and upstream execution must
	// outlive this function's deferred cancel after a queued slot is acquired.
	ctx, cancel := context.WithTimeout(executionCtx, timeout)
	defer cancel()

	acquireSlot := func() (*service.AcquireResult, error) {
		return h.concurrencyService.AcquireAccountSlot(executionCtx, id, maxConcurrency)
	}

	// Determine if ping is needed (streaming + ping format defined)
	needPing := isStream && h.pingFormat != ""

	var flusher http.Flusher
	if needPing {
		var ok bool
		flusher, ok = c.Writer.(http.Flusher)
		if !ok {
			return nil, fmt.Errorf("streaming not supported")
		}
	}

	// Only create ping ticker if ping is needed
	var pingCh <-chan time.Time
	if needPing {
		pingTicker := time.NewTicker(h.pingInterval)
		defer pingTicker.Stop()
		pingCh = pingTicker.C
	}

	backoff := initialBackoff
	timer := time.NewTimer(backoff)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			if parentErr := c.Request.Context().Err(); parentErr != nil {
				return nil, parentErr
			}
			return nil, &ConcurrencyError{
				SlotType:  slotType,
				IsTimeout: true,
			}

		case <-pingCh:
			// Send ping to keep connection alive
			if !*streamStarted {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				c.Header("X-Accel-Buffering", "no")
				*streamStarted = true
			}
			written, err := fmt.Fprint(c.Writer, string(h.pingFormat))
			if err != nil {
				return nil, err
			}
			recordGatewayStreamHeartbeat(c, written)
			flusher.Flush()

		case <-timer.C:
			if err := ctx.Err(); err != nil {
				if parentErr := executionCtx.Err(); parentErr != nil {
					return nil, parentErr
				}
				return nil, &ConcurrencyError{SlotType: slotType, IsTimeout: true}
			}
			// Try to acquire slot
			result, err := acquireSlot()
			if err != nil {
				return nil, err
			}

			if result.Acquired {
				// Acquisition has its own bounded transport operation. A success
				// arriving after the queue deadline cannot authorize execution.
				// Return that exact owner without binding renewal to the queue ctx.
				if ctx.Err() != nil {
					result.ReleaseFunc()
					if parentErr := executionCtx.Err(); parentErr != nil {
						return nil, parentErr
					}
					return nil, &ConcurrencyError{SlotType: slotType, IsTimeout: true}
				}
				return result.ReleaseFunc, nil
			}
			backoff = nextBackoff(backoff)
			timer.Reset(backoff)
		}
	}
}

// AcquireAccountSlotForWaitPlan is the admission boundary for scheduler queues.
// A free slot does not consume queue capacity. Every waiter must first own one
// of the persisted bounded queue entries; dependency failure cannot grant one.
func (h *ConcurrencyHelper) AcquireAccountSlotForWaitPlan(c *gin.Context, accountID int64, plan *service.AccountWaitPlan, isStream bool, streamStarted *bool) (func(), error) {
	if plan == nil {
		return nil, &ConcurrencyError{SlotType: "account"}
	}
	ctx, err := installConcurrencyExecutionContext(c)
	if err != nil {
		return nil, err
	}
	release, acquired, err := h.TryAcquireAccountSlot(ctx, accountID, plan.MaxConcurrency)
	if err != nil || acquired {
		return release, err
	}
	allowed, err := h.IncrementAccountWaitCount(ctx, accountID, plan.MaxWaiting)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, &WaitQueueFullError{SlotType: "account"}
	}
	defer h.DecrementAccountWaitCount(ctx, accountID)
	return h.waitForSlotWithPingTimeout(c, "account", accountID, plan.MaxConcurrency, plan.Timeout, isStream, streamStarted)
}

// A durable image worker owns admission until its stored result is complete.
// Its synchronous gateway call reuses the trusted worker lease, while the
// submitting HTTP request and independent workers remain separate executions.
func (h *ConcurrencyHelper) acquireWorkerUserSlotWithWait(c *gin.Context, userID int64, limit int) (func(), error) {
	if h == nil || h.concurrencyService == nil {
		return nil, fmt.Errorf("concurrency service is unavailable")
	}
	ctx, err := installConcurrencyExecutionContext(c)
	if err != nil {
		return nil, err
	}
	limit, err = h.concurrencyService.ResolveUserConcurrency(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	acquire := func(admissionCtx context.Context) (func(), bool, error) {
		if err := admissionCtx.Err(); err != nil {
			return nil, false, err
		}
		result, err := h.concurrencyService.AcquireWorkerUserSlot(ctx, userID, limit)
		if err != nil {
			return nil, false, err
		}
		if !result.Acquired {
			return nil, false, nil
		}
		if err := admissionCtx.Err(); err != nil {
			result.ReleaseFunc()
			return nil, false, err
		}
		c.Request = c.Request.WithContext(result.Context)
		return h.withAPIKeySlotFromGin(c, result.ReleaseFunc), true, nil
	}
	if release, acquired, err := acquire(ctx); err != nil || acquired {
		return release, err
	}
	queueLimit := service.CalculateMaxWait(limit) - limit
	if queueLimit < 1 {
		queueLimit = 1
	}
	allowed, err := h.IncrementWaitCount(ctx, userID, queueLimit)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, &WaitQueueFullError{SlotType: "user"}
	}
	defer h.DecrementWaitCount(ctx, userID)
	waitCtx, stopWait := context.WithTimeout(ctx, maxConcurrencyWait)
	defer stopWait()
	backoff := initialBackoff
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &ConcurrencyError{SlotType: "user", IsTimeout: true}
		case <-timer.C:
			release, acquired, err := acquire(waitCtx)
			if err != nil && waitCtx.Err() != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, &ConcurrencyError{SlotType: "user", IsTimeout: true}
			}
			if err != nil || acquired {
				return release, err
			}
			backoff = nextBackoff(backoff)
			timer.Reset(backoff)
		}
	}
}

// nextBackoff 计算下一次退避时间
// 性能优化：使用指数退避 + 随机抖动，避免惊群效应
// current: 当前退避时间
// 返回值：下一次退避时间（100ms ~ 2s 之间）
func nextBackoff(current time.Duration) time.Duration {
	// 指数退避：当前时间 * 1.5
	next := time.Duration(float64(current) * backoffMultiplier)
	if next > maxBackoff {
		next = maxBackoff
	}
	// 添加 ±20% 的随机抖动（jitter 范围 0.8 ~ 1.2）
	// 抖动可以分散多个请求的重试时间点，避免同时冲击 Redis
	jitter := 0.8 + rand.Float64()*0.4
	jittered := time.Duration(float64(next) * jitter)
	if jittered < initialBackoff {
		return initialBackoff
	}
	if jittered > maxBackoff {
		return maxBackoff
	}
	return jittered
}
