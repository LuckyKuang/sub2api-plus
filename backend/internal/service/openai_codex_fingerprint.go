package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codexFingerprintIDsContextKey 是暂存在 gin context 的收敛 ID 集合键。
// 由 Forward（非透传）或 forwardOpenAIPassthrough（透传）解析后写入，请求
// 构造器读取用于出站头改写——请求体与出站头必须共享同一份 IDs，保证
// turn_id 等随机字段一致。
const codexFingerprintIDsContextKey = "codex_fingerprint_ids"

// stageCodexFingerprintIDs 将本 attempt 解析出的收敛 ID 暂存到 gin context。
// 必须无条件覆写（含 nil）：failover 从收敛账号切到 off 账号时，上一账号的
// IDs 不得残留并被误应用到新账号的出站头（typed-nil 由应用侧 nil 守卫吸收）。
func stageCodexFingerprintIDs(c *gin.Context, ids *codexFingerprintIDs) {
	if c != nil {
		c.Set(codexFingerprintIDsContextKey, ids)
	}
}

func stagedCodexFingerprintIDs(c *gin.Context, account *Account) *codexFingerprintIDs {
	if c == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return nil
	}
	value, ok := c.Get(codexFingerprintIDsContextKey)
	if !ok {
		return nil
	}
	ids, ok := value.(*codexFingerprintIDs)
	if !ok || ids == nil || ids.accountID != account.ID {
		return nil
	}
	return ids
}

func (s *OpenAIGatewayService) applyStagedCodexFingerprintHeadersForAccount(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	headers http.Header,
) error {
	if account == nil || !account.UsesOpenAICodexProtocol() {
		return nil
	}
	fingerprintAccount, err := s.resolveCodexFingerprintAccount(ctx, account)
	if err != nil {
		return fmt.Errorf("resolve Codex fingerprint credential account: %w", err)
	}
	applyCodexFingerprintHeaders(headers, loadCodexFingerprintIDs(c, fingerprintAccount))
	return nil
}

func applyStagedCodexFingerprintClientMetadata(c *gin.Context, account *Account, reqBody map[string]any) bool { //nolint:unused // staged fingerprint helper kept for request-body path
	return applyCodexFingerprintClientMetadata(reqBody, stagedCodexFingerprintIDs(c, account))
}

// codexFingerprintMode 控制 OAuth 账号出站请求的设备指纹收敛强度。
// 多人共享同一 OAuth 账号时，每个用户的 Codex 客户端会携带各自不同的
// installation_id / session_id / thread_id，上游据此判定设备数和会话数。
// 收敛模式将这些标识改写为账号级恒定值，减少上游可见的设备/会话指纹。
type codexFingerprintMode string

const (
	// codexFingerprintOff 不做任何收敛，原样透传客户端标识。
	codexFingerprintOff codexFingerprintMode = "off"
	// codexFingerprintDevice 仅收敛 installation_id 为账号级恒定值。
	// 上游看到 1 台设备 + 多会话（每用户各自的 session）。
	codexFingerprintDevice codexFingerprintMode = "device"
	// codexFingerprintSession 收敛 installation_id + session_id，
	// thread_id 按客户端原始 session-id 确定性派生（每个真实 Codex 会话一个独立线程）。
	// 上游看到 1 台设备 + 1 会话 + N 线程，最接近正常用户 spawn 子代理的模式。
	codexFingerprintSession codexFingerprintMode = "session"
	// codexFingerprintFull 收敛所有标识：installation_id + session_id + thread_id。
	// 上游看到 1 台设备 + 1 会话 + 1 线程，最激进。
	codexFingerprintFull codexFingerprintMode = "full"
)

// CodexFingerprintModeExtraKey is the canonical persisted fingerprint mode.
const CodexFingerprintModeExtraKey = "codex_fingerprint_mode"

func normalizeCodexFingerprintMode(raw any) (codexFingerprintMode, error) {
	if raw == nil {
		return codexFingerprintDevice, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", infraerrors.BadRequest(
			"CODEX_FINGERPRINT_MODE_INVALID",
			"codex_fingerprint_mode must be one of off, device, session, full",
		)
	}
	mode := codexFingerprintMode(strings.TrimSpace(value))
	if mode == "" {
		return codexFingerprintDevice, nil
	}
	switch mode {
	case codexFingerprintOff, codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull:
		return mode, nil
	default:
		return "", infraerrors.BadRequest(
			"CODEX_FINGERPRINT_MODE_INVALID",
			"codex_fingerprint_mode must be one of off, device, session, full",
		)
	}
}

// NormalizeCodexFingerprintMode persists an explicit canonical mode for every
// credential-owning OpenAI OAuth account. Shadows inherit their parent's mode.
func (a *Account) NormalizeCodexFingerprintMode() error {
	if a == nil {
		return nil
	}
	if !a.IsOpenAIOAuth() || a.IsCredentialShadow() {
		if a.Extra != nil {
			delete(a.Extra, CodexFingerprintModeExtraKey)
		}
		return nil
	}
	var raw any
	if a.Extra != nil {
		raw = a.Extra[CodexFingerprintModeExtraKey]
	}
	mode, err := normalizeCodexFingerprintMode(raw)
	if err != nil {
		return err
	}
	if a.Extra == nil {
		a.Extra = make(map[string]any, 1)
	}
	a.Extra[CodexFingerprintModeExtraKey] = string(mode)
	return nil
}

// withExistingCodexFingerprintModeIfOmitted carries the account's effective
// mode into a replacement extra map. It also canonicalizes malformed legacy
// values to the defensive device default when an update omits the field.
func withExistingCodexFingerprintModeIfOmitted(account *Account, extra map[string]any) map[string]any {
	if account == nil || !account.IsOpenAIOAuth() || account.IsCredentialShadow() {
		return extra
	}
	if _, provided := extra[CodexFingerprintModeExtraKey]; provided {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any, 1)
	}
	extra[CodexFingerprintModeExtraKey] = string(account.GetCodexFingerprintMode())
	return extra
}

func canonicalizeCodexFingerprintModeForOmittedExtraUpdate(account *Account) {
	if account == nil || !account.IsOpenAIOAuth() || account.IsCredentialShadow() {
		return
	}
	mode := account.GetCodexFingerprintMode()
	if account.Extra == nil {
		account.Extra = make(map[string]any, 1)
	}
	account.Extra[CodexFingerprintModeExtraKey] = string(mode)
}

func normalizeCodexFingerprintModeUpdateExtra(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	raw, provided := extra[CodexFingerprintModeExtraKey]
	if !provided {
		return nil
	}
	mode, err := normalizeCodexFingerprintMode(raw)
	if err != nil {
		return err
	}
	extra[CodexFingerprintModeExtraKey] = string(mode)
	return nil
}

// GetCodexFingerprintMode 从账号 extra JSON 读取指纹收敛模式。
//
// Device-only convergence is the defensive fallback for missing or malformed
// legacy data. Canonical persistence stores every mode explicitly. Accounts
// that do not own OpenAI OAuth credentials always remain off.
func (a *Account) GetCodexFingerprintMode() codexFingerprintMode {
	if a == nil || !a.IsOpenAIOAuth() || a.IsCredentialShadow() {
		return codexFingerprintOff
	}
	var raw any
	if a.Extra != nil {
		raw = a.Extra[CodexFingerprintModeExtraKey]
	}
	mode, err := normalizeCodexFingerprintMode(raw)
	if err != nil {
		return codexFingerprintDevice
	}
	return mode
}

// deriveStableUUIDv4 从种子确定性派生一个 UUIDv4 格式的字符串。
// 同一种子永远返回同一值。
func deriveStableUUIDv4(seed string) string {
	h := sha256.Sum256([]byte(seed))
	b := h[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]),
		binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]),
		binary.BigEndian.Uint16(b[8:10]),
		b[10:16])
}

// resolveConvergedInstallationID 返回账号级恒定的 installation_id。
// 优先使用管理员配置的真实 device_id，无则从 accountID 确定性派生。
func resolveConvergedInstallationID(account *Account) string {
	if account == nil {
		return ""
	}
	if deviceID := account.GetOpenAIDeviceID(); deviceID != "" {
		return deviceID
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-install-id:v1:%d", account.ID))
}

// resolveConvergedSessionID 返回账号级恒定的 session_id。
func resolveConvergedSessionID(account *Account) string {
	if account == nil {
		return ""
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-session-id:v1:%d", account.ID))
}

// resolveConvergedThreadID 按客户端原始 session-id 确定性派生 thread_id。
// 每个真实 Codex 会话（不同客户端启动实例）获得一个独立线程，
// 模拟正常用户 spawn 子代理或开多窗口的模式。
func resolveConvergedThreadID(account *Account, clientSessionID string) string {
	if account == nil || clientSessionID == "" {
		return ""
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-thread-id:v1:%d:%s", account.ID, clientSessionID))
}

// codexFingerprintIDs 收敛后的完整 ID 集合。
// 由 resolveCodexFingerprintIDs 一次性生成，同一个实例在头改写和体改写之间共享，
// 确保所有载体中的 turn_id、turn_started_at_unix_ms 等请求级字段一致。
type codexFingerprintIDs struct {
	accountID           int64
	mode                codexFingerprintMode
	installationID      string
	sessionID           string
	threadID            string
	turnID              string
	windowID            string
	turnStartedAtUnixMS int64
}

// resolveCodexFingerprintIDs 按收敛模式计算出站 ID 集合。
// clientSessionID 是客户端原始的 session-id 头值（连字符形式），用于 session 模式下
// 的 thread_id 派生——每个真实 Codex 会话得到一个独立线程。
// 返回 nil 表示 off 模式，不需要改写。
// 注意：包含随机生成的 turn_id，调用方必须只调用一次并共享结果给头改写和体改写。
func resolveCodexFingerprintIDs(account *Account, clientSessionID string, mode codexFingerprintMode) *codexFingerprintIDs {
	if account == nil || mode == codexFingerprintOff {
		return nil
	}

	ids := &codexFingerprintIDs{accountID: account.ID, mode: mode}

	ids.installationID = resolveConvergedInstallationID(account)
	if ids.installationID == "" {
		return nil
	}

	switch mode {
	case codexFingerprintDevice:
		return ids

	case codexFingerprintSession:
		ids.sessionID = resolveConvergedSessionID(account)
		ids.threadID = resolveConvergedThreadID(account, clientSessionID)
		if ids.threadID == "" {
			ids.threadID = ids.sessionID
		}
		ids.turnID = uuid.Must(uuid.NewV7()).String()
		ids.windowID = ids.threadID + ":0"
		ids.turnStartedAtUnixMS = time.Now().UnixMilli()
		return ids

	case codexFingerprintFull:
		ids.sessionID = resolveConvergedSessionID(account)
		ids.threadID = ids.sessionID
		ids.turnID = uuid.Must(uuid.NewV7()).String()
		ids.windowID = ids.threadID + ":0"
		ids.turnStartedAtUnixMS = time.Now().UnixMilli()
		return ids
	}

	return nil
}

// extractClientSessionID 从请求头中提取客户端原始的会话标识。
// 优先取 session-id（连字符形式，Codex CLI 标准），回退到 session_id（下划线形式）。
// 返回的值尚未被 isolateOpenAISessionID 改写，是客户端的真实标识。
func extractClientSessionID(h http.Header) string {
	if v := strings.TrimSpace(h.Get("session-id")); v != "" {
		return v
	}
	return strings.TrimSpace(h.Get("session_id"))
}

const ginCodexFingerprintIDsKey = "codex_fingerprint_ids"

type codexFingerprintRequestPolicy uint8

const (
	codexFingerprintPolicyNonSession codexFingerprintRequestPolicy = iota
	codexFingerprintPolicyOrdinary
	codexFingerprintPolicyNativeCompact
	codexFingerprintPolicyLegacyCompact
)

func resolveCodexFingerprintRequestPolicy(c *gin.Context, body []byte) codexFingerprintRequestPolicy {
	if isOpenAIResponsesCompactPath(c) {
		return codexFingerprintPolicyLegacyCompact
	}
	if !isCodexFingerprintSessionPath(c) {
		return codexFingerprintPolicyNonSession
	}
	if isOpenAINativeCompactionV2(c) || HasCompactionTriggerInInput(body) {
		return codexFingerprintPolicyNativeCompact
	}
	return codexFingerprintPolicyOrdinary
}

func isCodexFingerprintSessionPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := strings.TrimSuffix(strings.TrimSpace(c.Request.URL.Path), "/")
	for _, prefix := range [...]string{
		"/backend-api/codex/responses",
		"/openai/v1/responses",
		"/v1/responses",
		"/responses",
	} {
		if path == prefix {
			return true
		}
	}
	for _, endpoint := range [...]string{
		"/openai/v1/chat/completions",
		"/v1/chat/completions",
		"/chat/completions",
		"/openai/v1/messages",
		"/v1/messages",
		"/messages",
	} {
		if path == endpoint {
			return true
		}
	}
	return false
}

// resolveCodexFingerprintAccount returns the account that owns the OAuth
// credential. Spark shadows are schedulable routing entries, but they do not
// own an upstream Codex device or session identity.
func (s *OpenAIGatewayService) resolveCodexFingerprintAccount(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || !account.IsCredentialShadow() {
		return account, nil
	}
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account repository is unavailable")
	}
	return resolveCredentialAccount(ctx, s.accountRepo, account)
}

func storeCodexFingerprintIDs(c *gin.Context, ids *codexFingerprintIDs) {
	if c == nil {
		return
	}
	if ids == nil {
		c.Set(ginCodexFingerprintIDsKey, nil)
		return
	}
	c.Set(ginCodexFingerprintIDsKey, ids)
}

func loadCodexFingerprintIDs(c *gin.Context, account *Account) *codexFingerprintIDs {
	if c == nil || account == nil {
		return nil
	}
	ids := stagedCodexFingerprintIDs(c, account)
	if ids == nil || ids.accountID != account.ID {
		return nil
	}
	return ids
}

func (s *OpenAIGatewayService) prepareCodexFingerprintIDs(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	clientHeaders http.Header,
	policy codexFingerprintRequestPolicy,
) (*codexFingerprintIDs, error) {
	storeCodexFingerprintIDs(c, nil)
	if account == nil || !account.IsOpenAIOAuth() || policy == codexFingerprintPolicyNonSession {
		return nil, nil
	}

	fingerprintAccount, err := s.resolveCodexFingerprintAccount(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("resolve Codex fingerprint credential account: %w", err)
	}
	ids := resolveCodexFingerprintIDsForPolicy(fingerprintAccount, clientHeaders, policy)
	storeCodexFingerprintIDs(c, ids)
	return ids, nil
}

func resolveCodexFingerprintIDsForPolicy(
	account *Account,
	clientHeaders http.Header,
	policy codexFingerprintRequestPolicy,
) *codexFingerprintIDs {
	if account == nil || !account.IsOpenAIOAuth() || policy == codexFingerprintPolicyNonSession {
		return nil
	}
	mode := account.GetCodexFingerprintMode()
	if policy == codexFingerprintPolicyLegacyCompact && mode != codexFingerprintOff {
		mode = codexFingerprintDevice
	}
	if mode == codexFingerprintOff {
		return nil
	}
	clientSessionID := ""
	if clientHeaders != nil {
		clientSessionID = extractClientSessionID(clientHeaders)
	}
	return resolveCodexFingerprintIDs(account, clientSessionID, mode)
}

func (s *OpenAIGatewayService) prepareCodexFingerprintMap(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	reqBody map[string]any,
) (bool, error) {
	var clientHeaders http.Header
	if c != nil && c.Request != nil {
		clientHeaders = c.Request.Header
	}
	ids, err := s.prepareCodexFingerprintIDs(
		ctx,
		c,
		account,
		clientHeaders,
		resolveCodexFingerprintRequestPolicy(c, nil),
	)
	if err != nil {
		return false, err
	}
	return applyCodexFingerprintClientMetadata(reqBody, ids), nil
}

func (s *OpenAIGatewayService) prepareCodexFingerprintRaw(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) ([]byte, bool, error) {
	var clientHeaders http.Header
	if c != nil && c.Request != nil {
		clientHeaders = c.Request.Header
	}
	ids, err := s.prepareCodexFingerprintIDs(
		ctx,
		c,
		account,
		clientHeaders,
		resolveCodexFingerprintRequestPolicy(c, body),
	)
	if err != nil {
		return body, false, err
	}
	return applyCodexFingerprintClientMetadataRaw(body, ids)
}

// resolveCodexFingerprintIDsFromRequest 从客户端原始请求头中提取 session-id，
// 结合账号配置一次性解析收敛 ID 集合。调用方应将返回的 ids 同时传给
// applyCodexFingerprintHeaders 和 applyCodexFingerprintClientMetadata。
func resolveCodexFingerprintIDsFromRequest(account *Account, clientHeaders http.Header) *codexFingerprintIDs {
	return resolveCodexFingerprintIDsForPolicy(account, clientHeaders, codexFingerprintPolicyOrdinary)
}

// applyCodexFingerprintHeaders 按预计算的收敛 ID 改写出站 HTTP 头中的设备指纹。
// 在 buildUpstreamRequest 的白名单透传之后、applyOpenAIOutboundIdentity 之前调用。
func applyCodexFingerprintHeaders(h http.Header, ids *codexFingerprintIDs) {
	if h == nil || ids == nil {
		return
	}

	// 所有非 off 模式都收敛 installation_id
	h.Set("x-codex-installation-id", ids.installationID)

	if ids.mode == codexFingerprintDevice {
		rewriteCodexTurnMetadataFields(h, map[string]any{
			"installation_id": ids.installationID,
		})
		return
	}

	// session / full 模式：改写所有相关头
	h.Set("x-codex-window-id", ids.windowID)
	h.Set("x-client-request-id", ids.threadID)
	// 连字符形式和下划线形式都改写，保证一致
	h.Set("session-id", ids.sessionID)
	h.Set("session_id", ids.sessionID)
	h.Set("thread-id", ids.threadID)

	rewriteCodexTurnMetadataFields(h, map[string]any{
		"installation_id":         ids.installationID,
		"session_id":              ids.sessionID,
		"thread_id":               ids.threadID,
		"turn_id":                 ids.turnID,
		"window_id":               ids.windowID,
		"turn_started_at_unix_ms": ids.turnStartedAtUnixMS,
	})
}

// rewriteCodexTurnMetadataFields 解析 x-codex-turn-metadata 头中的 JSON，
// 替换指定字段后回写。保留未指定字段原样（如 sandbox、thread_source 等）。
func rewriteCodexTurnMetadataFields(h http.Header, fields map[string]any) {
	values := h.Values("x-codex-turn-metadata")
	if len(values) == 0 {
		return
	}
	raw := strings.TrimSpace(values[0])
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	for k, v := range fields {
		metadata[k] = v
	}
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	h.Set("x-codex-turn-metadata", string(rebuilt))
}

// applyCodexFingerprintClientMetadata 按预计算的收敛 ID 改写请求体中的 client_metadata。
// 使用与头改写相同的 ids 实例，确保 turn_id 等随机字段一致。
func applyCodexFingerprintClientMetadata(reqBody map[string]any, ids *codexFingerprintIDs) bool {
	if reqBody == nil || ids == nil {
		return false
	}

	existing, _ := reqBody["client_metadata"].(map[string]any)
	if existing == nil {
		existing = make(map[string]any)
	}

	if !applyCodexFingerprintToClientMetadataMap(existing, ids) {
		return false
	}
	reqBody["client_metadata"] = existing
	return true
}

// applyCodexFingerprintToClientMetadataMap 是 client_metadata 改写的共享核心，
// map 版（非透传，body 已解码）与 raw 字节版（透传热路径）都经由它，保证两条
// 路径的收敛语义永不漂移。
func applyCodexFingerprintToClientMetadataMap(existing map[string]any, ids *codexFingerprintIDs) bool {
	if existing == nil || ids == nil {
		return false
	}

	modified := false

	if ids.installationID != "" {
		existing["x-codex-installation-id"] = ids.installationID
		modified = true
	}

	if ids.mode == codexFingerprintDevice {
		rewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{
			"installation_id": ids.installationID,
		})
		return modified
	}

	// session / full 模式
	existing["session_id"] = ids.sessionID
	existing["thread_id"] = ids.threadID
	existing["turn_id"] = ids.turnID
	existing["x-codex-window-id"] = ids.windowID

	rewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{
		"installation_id":         ids.installationID,
		"session_id":              ids.sessionID,
		"thread_id":               ids.threadID,
		"turn_id":                 ids.turnID,
		"window_id":               ids.windowID,
		"turn_started_at_unix_ms": ids.turnStartedAtUnixMS,
	})
	return true
}

// applyCodexFingerprintClientMetadataRaw 在原始 JSON 字节上改写 client_metadata，
// 供透传路径使用——透传是热路径，禁止对可能高达数十 MB 的 body 做全量
// Unmarshal（见 forwardOpenAIPassthrough 的轻量提取注释）。实现为：gjson 提取
// client_metadata 小对象单独解码，经共享核心改写后 sjson 一次性拼回，body
// 其余字节原样保留。语义与 applyCodexFingerprintClientMetadata 逐点一致
// （含"非对象值整体替换为收敛集合"的行为）。
func applyCodexFingerprintClientMetadataRaw(body []byte, ids *codexFingerprintIDs) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil {
		return body, false, nil
	}
	// 非 JSON 对象的 body（数组/标量/畸形）没有 client_metadata 语义，
	// sjson 在这类根上写字段会改写整体结构，直接放行保持原样。
	if !gjson.ParseBytes(body).IsObject() {
		return body, false, nil
	}

	existing := map[string]any{}
	if cm := gjson.GetBytes(body, "client_metadata"); cm.IsObject() {
		if err := json.Unmarshal([]byte(cm.Raw), &existing); err != nil {
			return body, false, fmt.Errorf("decode client_metadata for fingerprint: %w", err)
		}
	}

	if !applyCodexFingerprintToClientMetadataMap(existing, ids) {
		return body, false, nil
	}

	raw, err := json.Marshal(existing)
	if err != nil {
		return body, false, fmt.Errorf("encode converged client_metadata: %w", err)
	}
	next, err := sjson.SetRawBytes(body, "client_metadata", raw)
	if err != nil {
		return body, false, fmt.Errorf("splice converged client_metadata: %w", err)
	}
	return next, true, nil
}

// rewriteClientMetadataEmbeddedTurnMetadata 改写 client_metadata 中内嵌的
// x-codex-turn-metadata。规范 JSON 字符串和对象保留无关字段；已存在的
// null、数组、标量或畸形字符串重建为规范 JSON 字符串。
func rewriteClientMetadataEmbeddedTurnMetadata(clientMetadata map[string]any, fields map[string]any) {
	value, exists := clientMetadata["x-codex-turn-metadata"]
	if !exists {
		return
	}
	var metadata map[string]any
	switch typed := value.(type) {
	case string:
		_ = json.Unmarshal([]byte(typed), &metadata)
	case map[string]any:
		metadata = typed
	case map[string]string:
		metadata = make(map[string]any, len(typed))
		for key, item := range typed {
			metadata[key] = item
		}
	}
	if metadata == nil {
		metadata = make(map[string]any, len(fields))
	}
	for k, v := range fields {
		metadata[k] = v
	}
	if rebuilt, err := json.Marshal(metadata); err == nil {
		clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}

func sanitizedCodexFingerprintExtraUpdates(updates map[string]any) map[string]any {
	return updates
}
