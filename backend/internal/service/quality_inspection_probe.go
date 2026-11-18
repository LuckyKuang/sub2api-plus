package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// QualityProbeOutcome 一次账号探测的结构化结果。
type QualityProbeOutcome struct {
	Text         string
	FirstTokenMs *int
	DurationMs   *int
	ErrorCode    string
	FailureClass string
	Status       string
}

var qualityHTTPStatusPattern = regexp.MustCompile(`\b(\d{3})\b`)

// QualityProbe 复用账号连通性测试的既有平台适配，把 SSE 结果转成结构化输出。
//
// 为什么复用而不是重写协议层：出站身份、代理、账号映射与平台分流都集中在
// AccountTestService；另写一套会立即产生分叉，违反 docs/OUTBOUND_IDENTITY.md
// 的单一来源约束。这里通过内存 ResponseWriter 捕获事件流，不经过真实网络连接。
type QualityProbe struct {
	tester *AccountTestService
}

// NewQualityProbe 构造探测适配器。
func NewQualityProbe(tester *AccountTestService) *QualityProbe {
	return &QualityProbe{tester: tester}
}

// Run 对指定账号发起一次文本探测。
func (p *QualityProbe) Run(ctx context.Context, accountID int64, model, prompt string) (QualityProbeOutcome, error) {
	if p == nil || p.tester == nil {
		return QualityProbeOutcome{}, errors.New("account test service is unavailable")
	}
	recorder := newProbeRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = (&http.Request{}).WithContext(ctx)
	ginCtx.Set(accountTestBackgroundKey, true)

	started := time.Now()
	testErr := p.tester.TestAccountConnection(ginCtx, accountID, model, prompt, "")
	elapsed := int(time.Since(started).Milliseconds())

	body := recorder.Body()
	text, errMsg := parseTestSSEOutput(body)
	outcome := QualityProbeOutcome{Text: text, DurationMs: &elapsed}
	if first := recorder.FirstEventOffset(); first >= 0 {
		if at, ok := recorder.OffsetTime(first); ok {
			firstMs := int(at.Sub(started).Milliseconds())
			if firstMs >= 0 {
				outcome.FirstTokenMs = &firstMs
			}
		}
	}

	if testErr != nil && errMsg == "" {
		errMsg = testErr.Error()
	}
	if errMsg == "" && strings.TrimSpace(text) == "" {
		errMsg = "模型返回空回答"
	}
	if errMsg == "" {
		outcome.Status = QualityStatusPass
		return outcome, nil
	}

	outcome.ErrorCode, outcome.FailureClass, outcome.Status = classifyQualityProbeError(errMsg)
	return outcome, nil
}

// classifyQualityProbeError 把上游错误映射为稳定错误码与失败分类。
// quota 单列，不当作答错；本地配置错误不计入上游故障。
func classifyQualityProbeError(message string) (string, string, string) {
	lower := strings.ToLower(message)
	code := "REQUEST_FAILED"
	if match := qualityHTTPStatusPattern.FindStringSubmatch(message); len(match) == 2 {
		code = "HTTP_" + match[1]
	}
	switch {
	case strings.Contains(message, "429") || strings.Contains(lower, "rate limit"):
		return "HTTP_429", QualityFailureUpstream, QualityStatusQuota
	case strings.Contains(lower, "insufficient") || strings.Contains(lower, "quota") ||
		strings.Contains(lower, "额度") || strings.Contains(lower, "余额"):
		return "QUOTA_EXHAUSTED", QualityFailureUpstream, QualityStatusQuota
	case strings.Contains(lower, "401") || strings.Contains(lower, "403") ||
		strings.Contains(lower, "authentication") || strings.Contains(lower, "unauthorized"):
		return code, QualityFailureLocal, QualityStatusError
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") ||
		strings.Contains(lower, "eof") || strings.Contains(lower, "connection") ||
		strings.Contains(lower, "empty") || strings.Contains(lower, "空回答"):
		return code, QualityFailureUpstream, QualityStatusError
	default:
		if strings.HasPrefix(code, "HTTP_5") {
			return code, QualityFailureUpstream, QualityStatusError
		}
		return code, QualityFailureLocal, QualityStatusError
	}
}

// probeRecorder 记录每次写入的字节偏移与时间，用于估算首正文延迟。
type probeRecorder struct {
	header   http.Header
	buf      bytes.Buffer
	status   int
	written  bool
	size     int
	offsets  []probeCheckpoint
	firstAt  time.Time
	hasFirst bool
	body     []byte
}

type probeCheckpoint struct {
	offset int
	at     time.Time
}

func newProbeRecorder() *probeRecorder {
	return &probeRecorder{header: make(http.Header), status: http.StatusOK}
}

func (r *probeRecorder) Header() http.Header { return r.header }

func (r *probeRecorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.status = status
	r.written = true
}

func (r *probeRecorder) WriteHeaderNow() { r.written = true }

func (r *probeRecorder) Write(data []byte) (int, error) {
	now := time.Now()
	if !r.hasFirst {
		r.firstAt, r.hasFirst = now, true
	}
	r.offsets = append(r.offsets, probeCheckpoint{offset: r.size, at: now})
	r.written = true
	n, err := r.buf.Write(data)
	r.size += n
	return n, err
}

func (r *probeRecorder) WriteString(value string) (int, error) { return r.Write([]byte(value)) }

func (r *probeRecorder) Flush() {}

func (r *probeRecorder) Size() int { return r.size }

func (r *probeRecorder) Status() int { return r.status }

func (r *probeRecorder) Written() bool { return r.written }

func (r *probeRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack is not supported")
}

func (r *probeRecorder) CloseNotify() <-chan bool {
	ch := make(chan bool, 1)
	return ch
}

func (r *probeRecorder) Pusher() http.Pusher { return nil }

// Body 返回完整的 SSE 事件流文本。
func (r *probeRecorder) Body() string { return r.buf.String() }

// FirstEventOffset 返回首个 content 事件的字节偏移；没有内容事件时返回 -1。
func (r *probeRecorder) FirstEventOffset() int {
	body := r.buf.String()
	index := strings.Index(body, `"type":"content"`)
	if index < 0 {
		return -1
	}
	// 回退到该事件所在行的起点，偏移只用于时间估算。
	if start := strings.LastIndex(body[:index], "\n"); start >= 0 {
		return start + 1
	}
	return 0
}

// OffsetTime 找到覆盖该偏移的写入时间。
func (r *probeRecorder) OffsetTime(offset int) (time.Time, bool) {
	if len(r.offsets) == 0 {
		return time.Time{}, false
	}
	var found time.Time
	for _, checkpoint := range r.offsets {
		if checkpoint.offset > offset {
			break
		}
		found = checkpoint.at
	}
	if found.IsZero() {
		return r.firstAt, r.hasFirst
	}
	return found, true
}

// 断言 gin.ResponseWriter 契约，编译期即可发现接口缺失。
var _ gin.ResponseWriter = (*probeRecorder)(nil)
