package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// openAICodexTurnStateHeader 是 Codex 的回合状态头。上游在响应头中铸造该
// 不透明 blob，客户端在同一回合的后续请求中原样回带（codex-rs 侧从
// /responses SSE、/responses/compact JSON 与 WS 握手三种响应中捕获，见
// codex-api/src/sse/responses.rs 与 endpoint/compact.rs）。
const openAICodexTurnStateHeader = "x-codex-turn-state"

// Provenance binds the exact opaque blob to its execution scope and real
// upstream principal. A relinked local row cannot authorize old state replay.
type openAICodexTurnStateOrigin struct {
	accountID int64
	owner     string
	expiresAt time.Time
}

// OpenAICodexTurnStateOriginStore is an optional shared provenance store.
// GatewayCache implementations that provide it make cross-owner echo guards
// effective across replicas; the process-local map remains the fallback.
type OpenAICodexTurnStateOriginStore interface {
	SetOpenAICodexTurnStateOrigin(ctx context.Context, seed string, owner string, ttl time.Duration) error
	GetOpenAICodexTurnStateOrigin(ctx context.Context, seed string) (string, error)
}

// Access-token refresh does not change the upstream principal. A local account
// row, however, is not principal evidence: administrators can relink that row.
// Unknown OAuth principals cannot authorize reuse of opaque upstream state.
func openAICodexStateOwner(ctx context.Context, account *Account) string {
	if account == nil {
		return ""
	}
	if requestCredentialOwnerBound(ctx, account) {
		owner, known := requestCredentialOwner(ctx, account)
		if !known || owner == nil {
			return ""
		}
		account = owner
	}
	principal := ""
	if account.IsOpenAIAgentIdentity() {
		key, err := agentIdentityKeyFromAccount(account)
		if err != nil {
			return ""
		}
		principal = fmt.Sprintf("agent-runtime:%s:public-key:%x", key.runtimeID, key.privateKey[32:])
	} else if account.Type == AccountTypeAPIKey {
		if key := strings.TrimSpace(account.GetCredential("api_key")); key != "" {
			principal = "api-key:" + key
		}
	} else if account.Type == AccountTypeSetupToken {
		if key := strings.TrimSpace(account.GetOpenAIAccessToken()); key != "" {
			principal = "setup-token:" + key
		}
	} else if accountID := strings.TrimSpace(account.GetChatGPTAccountID()); accountID != "" {
		principal = "chatgpt:" + accountID + ":user:" + strings.TrimSpace(account.GetCredential("chatgpt_user_id"))
	}
	if principal == "" {
		return ""
	}
	return fmt.Sprintf("owner-v2:%x", sha256.Sum256([]byte(principal)))
}

func openAICodexTurnStateRequestOwner(c *gin.Context, selected *Account) string {
	ctx := openAICodexTurnStateContext(c)
	// Credential evidence is keyed by the selected shadow, while its immutable
	// payload identifies the real parent. Check it before resolving the source.
	if requestCredentialOwnerBound(ctx, selected) {
		return openAICodexStateOwner(ctx, selected)
	}
	return openAICodexStateOwner(ctx, codexAccountIdentitySource(c, selected))
}

func openAICodexTurnStateProvenanceKey(c *gin.Context, blob string) string {
	seed := openAICodexTurnStateSeed(c)
	if seed == "" || strings.TrimSpace(blob) == "" {
		return ""
	}
	return fmt.Sprintf("blob-v2:%s:%x", seed, sha256.Sum256([]byte(blob)))
}

func openAICodexCredentialOwnerID(account *Account) int64 {
	if account == nil {
		return 0
	}
	if account.IsCredentialShadow() && account.ParentAccountID != nil && *account.ParentAccountID > 0 {
		return *account.ParentAccountID
	}
	return account.ID
}

func (s *OpenAIGatewayService) openAICodexTurnStateOriginStore() OpenAICodexTurnStateOriginStore {
	if s == nil || s.cache == nil {
		return nil
	}
	store, _ := s.cache.(OpenAICodexTurnStateOriginStore)
	return store
}

func openAICodexTurnStateContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

// openAICodexTurnStateSeed 返回溯源表键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话标识时返回空串；无法证实归属的 opaque state 不回带。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	scope, _ := c.Get("openai_codex_turn_state_scope")
	if value, ok := scope.(string); ok && value != "" {
		return "execution-v1:" + value
	}
	if scope := openAICodexTurnStateExecutionScope(c, nil); scope != "" {
		return "execution-v1:" + scope
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

func stageOpenAICodexTurnStateScope(c *gin.Context, body []byte) {
	if c == nil {
		return
	}
	scope := openAICodexTurnStateExecutionScope(c, body)
	c.Set("openai_codex_turn_state_scope", scope)
}

func openAICodexTurnStateExecutionScope(c *gin.Context, body []byte) string {
	// Legacy clients expose only a session. Keep their provenance key while
	// distinguishing modern sibling threads and independent execution lanes.
	if resolveOpenAIWSClientThreadID(c, body) == "" && resolveOpenAIWSExecutionLane(c, body) == "" {
		return ""
	}
	scope, _ := resolveOpenAIWSExecutionScope(c, body, getAPIKeyIDFromContext(c))
	return scope
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头。
// Provenance is recorded separately after the writer commits. 上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
}

// stageOpenAICodexTurnState 将上游 turn-state 暂存到延迟提交的响应头集合
// （首输出守卫路径先缓存头、见到首个输出事件才提交）。此处**不**记录铸造
// 账号：该 attempt 仍可能在首输出超时后 failover，暂存头会被整体丢弃，
// 客户端从未收到该 blob。溯源必须在真正提交时记录，见
// noteStagedOpenAICodexTurnStateCommitted。
func stageOpenAICodexTurnState(dst *http.Header, upstream http.Header) {
	if dst == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		if *dst != nil {
			dst.Del(canonical)
		}
		return
	}
	if *dst == nil {
		*dst = http.Header{}
	}
	dst.Set(canonical, state)
}

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account, extractOpenAICodexTurnState(staged))
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// noteOpenAICodexTurnStateProvenance records scope + exact blob -> sent owner.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account, blob string) {
	ownerID := openAICodexCredentialOwnerID(account)
	if s == nil || ownerID <= 0 {
		return
	}
	seed := openAICodexTurnStateProvenanceKey(c, blob)
	owner := openAICodexTurnStateRequestOwner(c, account)
	if seed == "" || owner == "" {
		return
	}
	ttl := s.openAIWSSessionStickyTTL()
	s.openaiCodexTurnStateOrigins.Store(seed, openAICodexTurnStateOrigin{
		accountID: ownerID,
		owner:     owner,
		expiresAt: time.Now().Add(ttl),
	})
	if store := s.openAICodexTurnStateOriginStore(); store != nil {
		_ = store.SetOpenAICodexTurnStateOrigin(openAICodexTurnStateContext(c), seed, owner, ttl)
	}
	s.sweepOpenAICodexTurnStateOrigins()
}

// guardOpenAICodexTurnStateEcho retains only blobs whose provenance proves
// the same upstream principal. Missing, expired and legacy records cannot
// authorize opaque state replay. The guard strips but never injects state.
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header) {
	if s == nil || h == nil || account == nil {
		return
	}
	if strings.TrimSpace(h.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	owner := openAICodexTurnStateRequestOwner(c, account)
	// A known migration is stronger evidence than an absent/expired provenance
	// record. Never let loss of metadata authorize the old owner's opaque state.
	if c != nil && c.Request != nil {
		if openAISessionTakeoverPending(c.Request.Context()) {
			h.Del(openAICodexTurnStateHeader)
			return
		}
	}
	seed := openAICodexTurnStateProvenanceKey(c, h.Get(openAICodexTurnStateHeader))
	if seed == "" || owner == "" {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	if store := s.openAICodexTurnStateOriginStore(); store != nil {
		originOwner, err := store.GetOpenAICodexTurnStateOrigin(openAICodexTurnStateContext(c), seed)
		if err == nil && originOwner != "" {
			if originOwner != owner {
				h.Del(openAICodexTurnStateHeader)
			}
			return
		}
	}
	raw, ok := s.openaiCodexTurnStateOrigins.Load(seed)
	if !ok {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	origin, ok := raw.(openAICodexTurnStateOrigin)
	if !ok {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		h.Del(openAICodexTurnStateHeader)
		return
	}
	if !origin.expiresAt.IsZero() && time.Now().After(origin.expiresAt) {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		h.Del(openAICodexTurnStateHeader)
		return
	}
	if origin.owner == "" || origin.owner != owner {
		h.Del(openAICodexTurnStateHeader)
	}
}

// sweepOpenAICodexTurnStateOrigins 机会式清扫过期溯源记录：每 256 次写入
// 全量遍历一轮，防止仅靠读侧惰性删除导致的慢泄漏（会话键无上界）。
func (s *OpenAIGatewayService) sweepOpenAICodexTurnStateOrigins() {
	if s.openaiCodexTurnStateWrites.Add(1)%256 != 0 {
		return
	}
	now := time.Now()
	s.openaiCodexTurnStateOrigins.Range(func(key, value any) bool {
		origin, ok := value.(openAICodexTurnStateOrigin)
		if !ok || (!origin.expiresAt.IsZero() && now.After(origin.expiresAt)) {
			s.openaiCodexTurnStateOrigins.Delete(key)
		}
		return true
	})
}
