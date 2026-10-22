package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codexFingerprintIDsContextKey 是暂存在 gin context 的收敛 ID 集合键。
// 由 Forward（非透传）或 forwardOpenAIPassthrough（透传）解析后写入，请求
// 构造器读取用于出站头改写——请求体与出站头必须共享同一份 IDs，保证
// installation_id 一致。
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
	// thread_id 保留每个实际线程的独立映射与父子关系。
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

// codexFingerprintIDs holds only installation projection. Logical identifiers
// are resolved by the typed account mapper from their original protocol fields.
type codexFingerprintIDs struct {
	accountID      int64
	mode           codexFingerprintMode
	installationID string
}

func resolveCodexFingerprintIDsContext(ctx context.Context, account *Account, headers http.Header, mode codexFingerprintMode, tenant int64) (*codexFingerprintIDs, error) {
	if account == nil || mode == codexFingerprintOff {
		return nil, nil
	}
	installation := account.GetOpenAIDeviceID()
	if installation == "" {
		var err error
		installation, err = resolveOutboundMappedID(ctx, "codex-installation-v2", codexAccountIdentityNamespace(account), 4)
		if err != nil {
			return nil, err
		}
	}
	ids := &codexFingerprintIDs{accountID: account.ID, mode: mode, installationID: installation}
	// Session/thread/turn/window projection is owned by the typed account mapper.
	// Fingerprint settings no longer create a fresh logical turn on every send.
	return ids, nil
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
	var err error
	account, err = codexRequestIdentityOwner(ctx, account)
	if err != nil {
		return nil, err
	}
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
	mode := fingerprintAccount.GetCodexFingerprintMode()
	if policy == codexFingerprintPolicyLegacyCompact && mode != codexFingerprintOff {
		mode = codexFingerprintDevice
	}
	ids, err := resolveCodexFingerprintIDsContext(ctx, fingerprintAccount, clientHeaders, mode, getAPIKeyIDFromContext(c))
	if err != nil {
		return nil, err
	}
	storeCodexFingerprintIDs(c, ids)
	return ids, nil
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

// applyCodexFingerprintHeaders 按预计算的收敛 ID 改写出站 HTTP 头中的设备指纹。
// 在 buildUpstreamRequest 的白名单透传之后、applyOpenAIOutboundIdentity 之前调用。
func applyCodexFingerprintHeaders(h http.Header, ids *codexFingerprintIDs) {
	if h == nil || ids == nil {
		return
	}
	// Official Responses carries installation in body and canonical turn metadata,
	// never as a synthesized direct header.
	h.Del("x-codex-installation-id")
	rewriteCodexTurnMetadataFields(h, map[string]any{"installation_id": ids.installationID})
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
	rebuilt, err := marshalCodexTurnMetadata(metadata)
	if err != nil {
		return
	}
	h.Set("x-codex-turn-metadata", string(rebuilt))
}

// applyCodexFingerprintClientMetadata 按预计算的收敛 ID 改写请求体中的 client_metadata。
// 使用与头改写相同的 installation 快照。
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
	existing["x-codex-installation-id"] = ids.installationID
	rewriteClientMetadataEmbeddedTurnMetadata(existing, map[string]any{"installation_id": ids.installationID})
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
	if rebuilt, err := marshalCodexTurnMetadata(metadata); err == nil {
		clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}

func sanitizedCodexFingerprintExtraUpdates(updates map[string]any) map[string]any {
	return updates
}
