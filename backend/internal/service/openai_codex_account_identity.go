package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexAccountIdentitySourceContextKey = "openai_codex_account_identity_source"

// prepareCodexAccountIdentitySource resolves credential shadows once per selected
// attempt. The handler reuses gin.Context across failover attempts, so every entry
// point overwrites the staged source before projecting outbound identity.
func (s *OpenAIGatewayService) prepareCodexAccountIdentitySource(ctx context.Context, c *gin.Context, account *Account) (*Account, error) {
	source := account
	if account != nil && account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
		source = resolved
	}
	if c != nil {
		c.Set(codexAccountIdentitySourceContextKey, source)
	}
	return source, nil
}

func codexAccountIdentitySource(c *gin.Context, fallback *Account) *Account {
	if c != nil && requestCredentialOwnerBound(openAICodexTurnStateContext(c), fallback) {
		if owner, known := requestCredentialOwner(openAICodexTurnStateContext(c), fallback); known {
			return owner
		}
		return fallback // the context-aware mapper rejects ambiguous evidence
	}
	if c != nil {
		if staged, ok := c.Get(codexAccountIdentitySourceContextKey); ok {
			if source, ok := staged.(*Account); ok && source != nil {
				return source
			}
		}
	}
	return fallback
}

// codexAccountIdentityNamespace returns a stable, credential-scoped namespace.
// Multiple local rows that use the same ChatGPT account intentionally share the
// same namespace. Setup tokens use an irreversible bearer fingerprint because
// they have no refresh lifecycle or imported account metadata. Plus does not
// persist a fingerprint seed; remaining OAuth rows fall back to the
// credential-owning account ID.
func codexAccountIdentityNamespace(account *Account) string {
	if account == nil {
		return ""
	}
	// Native API keys are explicit credential owners. Organization/project values
	// are not verified identities and bearer rotation must not create a device.
	if account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey {
		if ownerID := openAICodexCredentialOwnerID(account); ownerID > 0 {
			return fmt.Sprintf("apikey-account:%d", ownerID)
		}
		return ""
	}
	if !account.IsOpenAIOAuthLike() {
		return ""
	}
	if account.IsOpenAIAgentIdentity() {
		if key, err := agentIdentityKeyFromAccount(account); err == nil {
			digest := sha256.Sum256(key.privateKey[32:])
			return fmt.Sprintf("agent-runtime:%s:public-key:%x", key.runtimeID, digest[:])
		}
	}
	if upstreamAccountID := strings.TrimSpace(account.GetChatGPTAccountID()); upstreamAccountID != "" {
		if upstreamUserID := strings.TrimSpace(account.GetCredential("chatgpt_user_id")); upstreamUserID != "" {
			return "chatgpt:" + upstreamAccountID + ":user:" + upstreamUserID
		}
		return "chatgpt:" + upstreamAccountID
	}
	if account.Type == AccountTypeSetupToken {
		if token := strings.TrimSpace(account.GetOpenAIAccessToken()); token != "" {
			sum := sha256.Sum256([]byte("openai-setup-token:" + token))
			return fmt.Sprintf("setup-token:%x", sum[:16])
		}
	}
	if account.ID > 0 {
		return fmt.Sprintf("account:%d", account.ID)
	}
	return ""
}

// resolveCodexAccountIdentityValue preserves the timestamp of a valid v7
// identifier while namespacing its random bits. Legacy and synthetic inputs use
// the shared durable mapping; no request-local clock becomes a session ID.
func resolveCodexAccountIdentityValue(ctx context.Context, account *Account, apiKeyID int64, kind, raw string) (string, error) {
	owner, err := codexRequestIdentityOwner(ctx, account)
	if err != nil {
		return "", err
	}
	return resolveCodexAccountIdentityValueForOwner(ctx, owner, apiKeyID, kind, raw)
}

// A staged shadow parent and a selected row must both use the grant acquired
// for this attempt. An ambiguous acquisition cannot fall back to mutable data.
func codexRequestIdentityOwner(ctx context.Context, account *Account) (*Account, error) {
	if ctx != nil && account != nil {
		if evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence); ok &&
			(evidence.selected == outboundIdentityOwnerKey(account) || credentialOwnerKeyFromEvidence(evidence) == outboundIdentityOwnerKey(account)) {
			selected := evidence.selected.unsaved
			if selected == nil {
				selected = &Account{ID: evidence.selected.id}
			}
			owner, known := requestCredentialOwner(ctx, selected)
			if !known {
				return nil, fmt.Errorf("codex credential owner is unavailable")
			}
			return owner, nil
		}
	}
	return account, nil
}

func resolveCodexAccountIdentityValueForOwner(ctx context.Context, account *Account, apiKeyID int64, kind, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	namespace := codexAccountIdentityNamespace(account)
	if raw == "" || namespace == "" {
		return raw, nil
	}
	// A key replacement may represent another upstream principal. Device scope
	// remains pinned to the configured account, but execution IDs must not retain
	// the old grant's namespace merely because its local row was reused.
	if kind != "installation" && account.Type == AccountTypeAPIKey {
		if key := strings.TrimSpace(account.GetCredential("api_key")); key != "" {
			digest := sha256.Sum256([]byte(key))
			namespace += fmt.Sprintf(":grant:%x", digest[:])
		}
	}
	mode := account.GetCodexFingerprintMode()
	if kind == "installation" && mode != codexFingerprintOff {
		if pin := account.GetOpenAIDeviceID(); pin != "" {
			return pin, nil
		}
		return resolveOutboundMappedID(ctx, "codex-installation-v2", namespace, 4)
	}
	if mode == codexFingerprintSession || mode == codexFingerprintFull {
		if kind == "session" || mode == codexFingerprintFull && kind == "thread" {
			return resolveOutboundMappedID(ctx, "codex-converged-"+kind+"-v2", namespace, 7)
		}
	}
	if kind == "window" {
		if split := strings.LastIndexByte(raw, ':'); split > 0 {
			if _, err := strconv.ParseUint(raw[split+1:], 10, 64); err == nil {
				thread, err := resolveCodexAccountIdentityValueForOwner(ctx, account, apiKeyID, "thread", raw[:split])
				return thread + raw[split:], err
			}
		}
		// Legacy window carriers have no recoverable counter; preserve their opaque
		// shape rather than invent a compaction transition.
		return resolveOutboundMappedID(ctx, "codex-legacy-window-v2", fmt.Sprintf("%d:%s:%s", apiKeyID, namespace, raw), 7)
	}
	scope := fmt.Sprintf("tenant:%d:owner:%s:kind:%s:value:%s", apiKeyID, namespace, kind, raw)
	version := 7
	if kind == "installation" {
		version = 4
	}
	if id, err := uuid.Parse(raw); err == nil && id.Version() == 7 && version == 7 {
		digest := sha256.Sum256([]byte("codex-id-v2:" + scope))
		copy(id[6:], digest[:10])
		id[6] = id[6]&0x0f | 0x70
		id[8] = id[8]&0x3f | 0x80
		return id.String(), nil
	}
	return resolveOutboundMappedID(ctx, "codex-identity-v2", scope, version)
}

var codexAccountIdentityFields = []struct{ name, kind string }{
	{"installation_id", "installation"}, {"x-codex-installation-id", "installation"},
	{"session_id", "session"}, {"session-id", "session"},
	{"thread_id", "thread"}, {"thread-id", "thread"},
	{"parent_thread_id", "thread"}, {"x-codex-parent-thread-id", "thread"}, {"forked_from_thread_id", "thread"},
	{"turn_id", "turn"}, {"turn-id", "turn"}, {"parent_turn_id", "turn"}, {"root_turn_id", "turn"},
	{"window_id", "window"}, {"x-codex-window-id", "window"}, {"context_window_id", "context-window"},
	{"x-client-request-id", "thread"},
}

func applyCodexAccountIdentityFields(ctx context.Context, values map[string]any, account *Account, apiKeyID int64) (bool, error) {
	if values == nil || codexAccountIdentityNamespace(account) == "" {
		return false, nil
	}
	changed := false
	for _, field := range codexAccountIdentityFields {
		raw, ok := values[field.name].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		next, err := resolveCodexAccountIdentityValue(ctx, account, apiKeyID, field.kind, raw)
		if err != nil {
			return false, err
		}
		if next != raw {
			values[field.name] = next
			changed = true
		}
	}
	return changed, nil
}

func applyCodexAccountIdentityEmbeddedMetadata(ctx context.Context, values map[string]any, account *Account, apiKeyID int64) (bool, error) {
	raw, ok := values[openAIWSTurnMetadataHeader].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return false, nil
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil || metadata == nil {
		return false, nil
	}
	changed, err := applyCodexAccountIdentityFields(ctx, metadata, account, apiKeyID)
	if err != nil || !changed {
		return changed, err
	}
	rebuilt, err := marshalCodexTurnMetadata(metadata)
	if err != nil {
		return false, err
	}
	values[openAIWSTurnMetadataHeader] = string(rebuilt)
	return true, nil
}

func applyCodexAccountIdentityClientMetadataMap(ctx context.Context, requestBody map[string]any, account *Account, apiKeyID int64) (bool, error) {
	var err error
	account, err = codexRequestIdentityOwner(ctx, account)
	if err != nil {
		return false, err
	}
	if requestBody == nil || codexAccountIdentityNamespace(account) == "" {
		return false, nil
	}
	changed := false
	clientMetadata, _ := requestBody["client_metadata"].(map[string]any)
	if clientMetadata != nil {
		flatChanged, err := applyCodexAccountIdentityFields(ctx, clientMetadata, account, apiKeyID)
		if err != nil {
			return false, err
		}
		embeddedChanged, err := applyCodexAccountIdentityEmbeddedMetadata(ctx, clientMetadata, account, apiKeyID)
		if err != nil {
			return false, err
		}
		changed = flatChanged || embeddedChanged
	}
	// Cache identity has its own tenant-only resolver and is independent of IDs.
	return changed, nil
}

func applyCodexAccountIdentityClientMetadataRaw(body []byte, account *Account, apiKeyID int64, contexts ...context.Context) ([]byte, bool, error) {
	if len(body) == 0 || codexAccountIdentityNamespace(account) == "" || !gjson.ParseBytes(body).IsObject() {
		return body, false, nil
	}
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	cm := gjson.GetBytes(body, "client_metadata")
	if !cm.IsObject() {
		return body, false, nil
	}
	values := map[string]any{}
	if err := json.Unmarshal([]byte(cm.Raw), &values); err != nil {
		return body, false, fmt.Errorf("decode client metadata: %w", err)
	}
	wrapper := map[string]any{"client_metadata": values}
	changed, err := applyCodexAccountIdentityClientMetadataMap(ctx, wrapper, account, apiKeyID)
	if err != nil || !changed {
		return body, false, err
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return body, false, err
	}
	next, err := sjson.SetRawBytes(body, "client_metadata", raw)
	return next, err == nil, err
}

func applyCodexAccountIdentityHeaders(ctx context.Context, headers http.Header, account *Account, apiKeyID int64) error {
	if headers == nil {
		return nil
	}
	var err error
	account, err = codexRequestIdentityOwner(ctx, account)
	if err != nil {
		return err
	}
	stripCodexDirectInstallationHeader(headers, account)
	if codexAccountIdentityNamespace(account) == "" {
		return nil
	}
	for _, field := range codexAccountIdentityFields {
		if field.name == "session_id" {
			continue
		}
		raw := strings.TrimSpace(headers.Get(field.name))
		if raw == "" {
			continue
		}
		mapped, err := resolveCodexAccountIdentityValue(ctx, account, apiKeyID, field.kind, raw)
		if err != nil {
			return err
		}
		headers.Set(field.name, mapped)
	}
	if raw := strings.TrimSpace(headers.Get(openAIWSTurnMetadataHeader)); raw != "" {
		metadata := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &metadata); err == nil && metadata != nil {
			changed, err := applyCodexAccountIdentityFields(ctx, metadata, account, apiKeyID)
			if err != nil {
				return err
			}
			if changed {
				rebuilt, err := marshalCodexTurnMetadata(metadata)
				if err != nil {
					return err
				}
				headers.Set(openAIWSTurnMetadataHeader, string(rebuilt))
			}
		}
	}
	return nil
}

// Responses/WS carries installation in metadata, not this direct header.
// Remote-control endpoints have their own carriers and do not call this helper.
func stripCodexDirectInstallationHeader(headers http.Header, account *Account) {
	if account == nil || account.Platform != PlatformOpenAI {
		return
	}
	for key := range headers {
		if strings.EqualFold(key, "x-codex-installation-id") {
			delete(headers, key)
		}
	}
}
