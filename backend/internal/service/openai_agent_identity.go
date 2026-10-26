package service

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	OpenAIAuthModeAgentIdentity          = "agentIdentity"
	agentIdentityAuthAPIBaseURL          = "https://auth.openai.com/api/accounts"
	agentIdentityTaskRegistrationTimeout = 30 * time.Second
)

var openAIAgentIdentityAuthAPIBaseURL = agentIdentityAuthAPIBaseURL

var agentIdentityTaskLocks sync.Map // map[int64]*sync.Mutex

type agentIdentityWSConnectionInvalidator interface {
	InvalidateAgentIdentityWSConnections(accountID int64)
}

type agentIdentityKey struct {
	runtimeID  string
	privateKey ed25519.PrivateKey
	taskID     string
}

type agentIdentityTaskRegistrationResponse struct {
	TaskID               string `json:"task_id"`
	TaskIDCamel          string `json:"taskId"`
	EncryptedTaskID      string `json:"encrypted_task_id"`
	EncryptedTaskIDCamel string `json:"encryptedTaskId"`
}

type agentIdentityTaskRecoveredError struct {
	ctx context.Context
}

func (e *agentIdentityTaskRecoveredError) Error() string {
	return "agent identity task recovered"
}

func (a *Account) IsOpenAIAgentIdentity() bool {
	if a == nil || !a.IsOpenAIOAuth() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(a.GetCredential(openAIAuthModeCredentialKey)), OpenAIAuthModeAgentIdentity)
}

func agentIdentityPrivateKey(account *Account) (ed25519.PrivateKey, error) {
	if account == nil {
		return nil, errors.New("agent identity account is nil")
	}
	raw := strings.TrimSpace(account.GetCredential("agent_private_key"))
	if raw == "" {
		return nil, errors.New("agent identity private key is missing")
	}
	der, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("agent identity private key is not valid base64")
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("agent identity private key is not valid PKCS#8")
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("agent identity private key is not Ed25519")
	}
	return privateKey, nil
}

// ValidateOpenAIAgentIdentityPrivateKey validates the stored PKCS#8 Ed25519
// form without returning or logging the key material.
func ValidateOpenAIAgentIdentityPrivateKey(encoded string) error {
	account := &Account{Credentials: map[string]any{"agent_private_key": encoded}}
	_, err := agentIdentityPrivateKey(account)
	return err
}

func agentIdentityKeyFromAccount(account *Account) (agentIdentityKey, error) {
	privateKey, err := agentIdentityPrivateKey(account)
	if err != nil {
		return agentIdentityKey{}, err
	}
	runtimeID := strings.TrimSpace(account.GetCredential("agent_runtime_id"))
	if runtimeID == "" {
		return agentIdentityKey{}, errors.New("agent identity runtime id is missing")
	}
	return agentIdentityKey{
		runtimeID:  runtimeID,
		privateKey: privateKey,
		taskID:     strings.TrimSpace(account.GetCredential("task_id")),
	}, nil
}

func buildAgentAssertion(key agentIdentityKey, now time.Time) (string, error) {
	if key.runtimeID == "" || key.taskID == "" {
		return "", errors.New("agent identity runtime or task id is missing")
	}
	timestamp := now.UTC().Format(time.RFC3339)
	payload := []byte(key.runtimeID + ":" + key.taskID + ":" + timestamp)
	signature, err := key.privateKey.Sign(nil, payload, crypto.Hash(0))
	if err != nil {
		return "", errors.New("failed to sign agent assertion")
	}
	envelope := map[string]string{
		"agent_runtime_id": key.runtimeID,
		"task_id":          key.taskID,
		"timestamp":        timestamp,
		"signature":        base64.StdEncoding.EncodeToString(signature),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", errors.New("failed to serialize agent assertion")
	}
	return "AgentAssertion " + base64.RawURLEncoding.EncodeToString(encoded), nil
}

func signAgentTaskRegistration(key agentIdentityKey, timestamp time.Time) (string, string, error) {
	if key.runtimeID == "" {
		return "", "", errors.New("agent identity runtime id is missing")
	}
	formatted := timestamp.UTC().Format(time.RFC3339)
	signature, err := key.privateKey.Sign(nil, []byte(key.runtimeID+":"+formatted), crypto.Hash(0))
	if err != nil {
		return "", "", errors.New("failed to sign agent task registration")
	}
	return formatted, base64.StdEncoding.EncodeToString(signature), nil
}

func decryptAgentTaskID(key agentIdentityKey, encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", errors.New("encrypted agent task id is not valid base64")
	}
	seed := key.privateKey.Seed()
	digest := sha512.Sum512(seed)
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	curvePublicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	if err != nil {
		return "", errors.New("failed to derive agent identity decryption key")
	}
	var curvePublic [32]byte
	copy(curvePublic[:], curvePublicBytes)
	plaintext, ok := box.OpenAnonymous(nil, ciphertext, &curvePublic, &curvePrivate)
	if !ok {
		return "", errors.New("failed to decrypt encrypted agent task id")
	}
	taskID := strings.TrimSpace(string(plaintext))
	if taskID == "" {
		return "", errors.New("decrypted agent task id is empty")
	}
	return taskID, nil
}

func registerAgentIdentityTaskWithIdentity(ctx context.Context, account *Account, identity openAIOutboundIdentity) (string, error) {
	account, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return "", err
	}
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return "", err
	}
	timestamp, signature, err := signAgentTaskRegistration(key, time.Now())
	if err != nil {
		return "", err
	}
	proxyURL, err := requestCredentialOwnerProxyURL(ctx, account)
	if err != nil {
		return "", err
	}
	// agent task 注册是官方 auth 面请求：默认头只有 originator / User-Agent /
	// residency，没有独立的 version 头；同时应用 Codex 自定义 CA。
	client, err := codexAuthPlaneHTTPClient(proxyURL, agentIdentityTaskRegistrationTimeout)
	if err != nil {
		return "", errors.New("invalid proxy configuration for agent task registration")
	}
	// 官方 RegisterTaskRequest { timestamp, signature }：用结构体固定字段顺序，
	// 不依赖 map 的字典序序列化。
	body, err := json.Marshal(agentIdentityTaskRegistrationRequest{
		Timestamp: timestamp,
		Signature: signature,
	})
	if err != nil {
		return "", errors.New("failed to serialize agent task registration")
	}
	url := strings.TrimRight(strings.TrimSpace(openAIAgentIdentityAuthAPIBaseURL), "/") + "/v1/agent/" + key.runtimeID + "/task/register"

	// 官方 login/auth/agent_identity.rs 最多重试
	// MAX_AGENT_IDENTITY_BOOTSTRAP_ATTEMPTS 次：瞬时 5xx/429/网络错误必须重试，
	// 而不是一次抖动就让整个 agent 会话起不来。
	var (
		lastErr error
		result  agentIdentityTaskRegistrationResponse
	)
	for attempt := 1; attempt <= openAIAgentIdentityTaskRegisterAttempts; attempt++ {
		result, lastErr = postAgentIdentityTaskRegistration(ctx, client, url, body, identity)
		if lastErr == nil {
			break
		}
		if !isRetryableAgentIdentityTaskRegistrationError(lastErr) || attempt == openAIAgentIdentityTaskRegisterAttempts {
			return "", lastErr
		}
		timer := time.NewTimer(agentIdentityTaskRegisterRetryBackoff * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	if taskID := strings.TrimSpace(result.TaskID); taskID != "" {
		return taskID, nil
	}
	if taskID := strings.TrimSpace(result.TaskIDCamel); taskID != "" {
		return taskID, nil
	}
	encrypted := strings.TrimSpace(result.EncryptedTaskID)
	if encrypted == "" {
		encrypted = strings.TrimSpace(result.EncryptedTaskIDCamel)
	}
	if encrypted == "" {
		return "", errors.New("agent task registration response omitted task id")
	}
	return decryptAgentTaskID(key, encrypted)
}

// agentIdentityTaskRegistrationRequest 固定官方 body 的字段顺序。
type agentIdentityTaskRegistrationRequest struct {
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
}

// openAIAgentIdentityTaskRegisterAttempts 对齐官方
// MAX_AGENT_IDENTITY_BOOTSTRAP_ATTEMPTS。
const openAIAgentIdentityTaskRegisterAttempts = 3

// agentIdentityTaskRegisterRetryBackoff 是官方 bootstrap 重试的起步退避。
const agentIdentityTaskRegisterRetryBackoff = 200 * time.Millisecond

// postAgentIdentityTaskRegistration 发起一次 task 注册请求并解出响应。
func postAgentIdentityTaskRegistration(ctx context.Context, client *http.Client, url string, body []byte, identity openAIOutboundIdentity) (agentIdentityTaskRegistrationResponse, error) {
	var empty agentIdentityTaskRegistrationResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return empty, errors.New("failed to build agent task registration request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", identity.Originator)
	req.Header.Set("User-Agent", identity.UserAgent)
	applyCodexResidencyHeader(ctx, req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return empty, fmt.Errorf("agent task registration request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return empty, &agentIdentityTaskRegistrationStatusError{statusCode: resp.StatusCode}
	}
	var result agentIdentityTaskRegistrationResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return empty, errors.New("agent task registration response is invalid")
	}
	return result, nil
}

// agentIdentityTaskRegistrationStatusError 保留状态码，供重试判定使用。
type agentIdentityTaskRegistrationStatusError struct {
	statusCode int
}

func (e *agentIdentityTaskRegistrationStatusError) Error() string {
	return fmt.Sprintf("agent task registration returned status %d", e.statusCode)
}

// isRetryableAgentIdentityTaskRegistrationError 只对瞬时故障重试：网络错误、
// 5xx 与 429。4xx 是确定性问题（签名、runtime、权限），重试无意义。
func isRetryableAgentIdentityTaskRegistrationError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var statusErr *agentIdentityTaskRegistrationStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode >= http.StatusInternalServerError ||
			statusErr.statusCode == http.StatusTooManyRequests ||
			statusErr.statusCode == http.StatusRequestTimeout
	}
	// 其余（构造/解码/业务）错误视为确定性问题。
	return false
}

type agentIdentityTaskOwnerGuard struct {
	validate func(*Account) error
	persist  func(context.Context, *Account, map[string]any) (bool, error)
}

func ensureAgentIdentityTaskForAccountWithIdentity(ctx context.Context, repo AccountRepository, wsInvalidator agentIdentityWSConnectionInvalidator, taskMu *sync.Mutex, account *Account, expectedTaskID string, identity openAIOutboundIdentity, ownerGuards ...agentIdentityTaskOwnerGuard) error {
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return nil
	}
	credAccount := account
	if account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, repo, account)
		if err != nil {
			return err
		}
		credAccount = resolved
	}
	if credAccount == nil || !credAccount.IsOpenAIAgentIdentity() {
		return errors.New("agent identity credentials are unavailable")
	}
	if requestCredentialOwnerBound(ctx, account) {
		owner, known := requestCredentialOwner(ctx, account)
		if !known || !sameAgentIdentitySigningOwner(owner, credAccount) {
			return errors.New("agent identity signing owner changed")
		}
	}
	observed := snapshotRequestCredentialAccount(credAccount)
	currentTaskID := strings.TrimSpace(credAccount.GetCredential("task_id"))
	if currentTaskID != "" && (expectedTaskID == "" || currentTaskID != expectedTaskID) {
		return nil
	}
	if taskMu == nil {
		return errors.New("agent identity task lock is unavailable")
	}
	sharedTaskMu := taskMu
	if credAccount.ID > 0 {
		candidate := &sync.Mutex{}
		actual, _ := agentIdentityTaskLocks.LoadOrStore(credAccount.ID, candidate)
		loadedTaskMu, ok := actual.(*sync.Mutex)
		if !ok {
			return errors.New("agent identity task lock has invalid type")
		}
		sharedTaskMu = loadedTaskMu
	}
	sharedTaskMu.Lock()
	defer sharedTaskMu.Unlock()
	validateOwner := func(candidate *Account) error {
		for _, guard := range ownerGuards {
			if guard.validate != nil {
				if err := guard.validate(candidate); err != nil {
					return err
				}
			}
		}
		return nil
	}
	guarded := len(ownerGuards) > 0
	if guarded && (len(ownerGuards) != 1 || ownerGuards[0].validate == nil || ownerGuards[0].persist == nil) {
		return errors.New("agent identity credential owner guard is invalid")
	}
	if guarded && (repo == nil || credAccount.ID <= 0) {
		return errors.New("agent identity credential owner cannot be verified")
	}
	// Re-read inside the shared lock. Different request paths often receive
	// independent repository snapshots; checking only the caller's snapshot
	// would allow sequential duplicate registrations after the first writer
	// has already persisted a new task.
	if repo != nil && credAccount.ID > 0 {
		refreshed, err := repo.GetByID(ctx, credAccount.ID)
		if err != nil {
			return fmt.Errorf("read agent identity task owner: %w", err)
		}
		if !sameAgentIdentityTaskGrant(observed, refreshed) {
			return errors.New("agent identity signing owner changed")
		}
		if err := validateOwner(refreshed); err != nil {
			return err
		}
		credAccount = snapshotRequestCredentialAccount(refreshed)
	}
	currentTaskID = strings.TrimSpace(credAccount.GetCredential("task_id"))
	if currentTaskID != "" && (expectedTaskID == "" || currentTaskID != expectedTaskID) {
		if !account.IsShadow() {
			account.Credentials = cloneRequestCredentialMap(observed.Credentials)
			account.Credentials["task_id"] = currentTaskID
		}
		return nil
	}
	if err := validateOwner(credAccount); err != nil {
		return err
	}
	conditional, conditionalOK := repo.(OAuthRefreshSuccessRepository)
	if !guarded && !conditionalOK {
		return errors.New("agent identity conditional task persistence is unavailable")
	}
	newTaskID, err := registerAgentIdentityTaskWithIdentity(ctx, credAccount, identity)
	if err != nil {
		return err
	}
	if guarded {
		// A relink while registration was in flight must not publish its task
		// into a replacement login. A same-owner concurrent task may be reused.
		refreshed, refreshErr := repo.GetByID(ctx, credAccount.ID)
		if refreshErr != nil || refreshed == nil {
			return errors.New("agent identity credential owner is unavailable")
		}
		if err := validateOwner(refreshed); err != nil {
			return err
		}
		if taskID := strings.TrimSpace(refreshed.GetCredential("task_id")); taskID != "" && taskID != currentTaskID {
			account.Credentials = shallowCopyMap(refreshed.Credentials)
			return nil
		}
	}
	credentials := make(map[string]any, len(credAccount.Credentials)+1)
	for key, value := range credAccount.Credentials {
		credentials[key] = value
	}
	credentials["task_id"] = newTaskID
	var applied bool
	if guarded {
		applied, err = ownerGuards[0].persist(ctx, credAccount, credentials)
	} else {
		applied, err = conditional.UpdateOAuthRefreshCredentialsIfUnchanged(ctx, credAccount, credentials)
	}
	if err != nil {
		return err
	}
	if !applied {
		if guarded {
			// Another replica may have won the task-only CAS. Reuse its task
			// only while the signing grant and the quota owner's full guard match.
			refreshed, refreshErr := repo.GetByID(ctx, credAccount.ID)
			if refreshErr != nil || refreshed == nil {
				return errors.New("agent identity credential owner is unavailable")
			}
			if !sameAgentIdentityTaskGrant(observed, refreshed) {
				return errors.New("agent identity signing owner changed")
			}
			if err := validateOwner(refreshed); err != nil {
				return err
			}
			if taskID := strings.TrimSpace(refreshed.GetCredential("task_id")); taskID != "" && taskID != currentTaskID {
				if !account.IsShadow() {
					account.Credentials = cloneRequestCredentialMap(observed.Credentials)
					account.Credentials["task_id"] = taskID
				}
				return nil
			}
		}
		return errors.New("agent identity task owner changed during registration")
	}
	if !account.IsShadow() {
		account.Credentials = cloneRequestCredentialMap(observed.Credentials)
		account.Credentials["task_id"] = newTaskID
	}
	if wsInvalidator != nil {
		wsInvalidator.InvalidateAgentIdentityWSConnections(credAccount.ID)
	}
	return nil
}

// Task recovery may adopt another task for the same signing key. It must never
// combine a previously resolved identity/ID namespace with a relinked key or
// proxy. A task ID is deliberately absent from this principal comparison.
func sameAgentIdentitySigningOwner(left, right *Account) bool {
	return left != nil && right != nil && left.ID == right.ID && left.Platform == right.Platform &&
		left.Type == right.Type && left.IsOpenAIAgentIdentity() && right.IsOpenAIAgentIdentity() &&
		oauthRefreshProxyIDsEqual(left.ProxyID, right.ProxyID) && left.proxyURLOrEmpty() == right.proxyURLOrEmpty() &&
		openAICodexStateOwner(context.Background(), left) != "" &&
		openAICodexStateOwner(context.Background(), left) == openAICodexStateOwner(context.Background(), right)
}

func sameAgentIdentityTaskGrant(left, right *Account) bool {
	if !sameAgentIdentitySigningOwner(left, right) {
		return false
	}
	before, after := snapshotRequestCredentialAccount(left), snapshotRequestCredentialAccount(right)
	delete(before.Credentials, "task_id")
	delete(after.Credentials, "task_id")
	return sameRequestCredentialGrant(before, after)
}

func (s *OpenAIGatewayService) ensureAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) error {
	if s == nil {
		return errors.New("openai gateway service is nil")
	}
	return ensureAgentIdentityTaskForAccountWithIdentity(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, account, expectedTaskID, s.resolveOpenAIOutboundIdentity(ctx, account))
}

func isAgentIdentityTaskInvalidHTTPResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusUnauthorized {
		return false
	}
	lower := strings.ToLower(string(body))
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(lower)
	for _, marker := range []string{
		`"code":"invalid_task_id"`,
		`"code":"task_not_found"`,
		`"code":"task_expired"`,
		`"error":"invalid_task_id"`,
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	for _, marker := range []string{
		"invalid task_id",
		"invalid task id",
		"task_id is invalid",
		"task id is invalid",
		"task not found",
		"task expired",
		"unknown task_id",
		"unknown task id",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

type agentIdentityTaskRecoveryContextKey struct{}

func markAgentIdentityTaskRecoveryTried(ctx context.Context) context.Context {
	return context.WithValue(ctx, agentIdentityTaskRecoveryContextKey{}, true)
}

func agentIdentityTaskRecoveryWasTried(ctx context.Context) bool {
	tried, _ := ctx.Value(agentIdentityTaskRecoveryContextKey{}).(bool)
	return tried
}

func isAgentIdentityTaskInvalidWSDialError(err *openAIWSDialError) bool {
	return err != nil && isAgentIdentityTaskInvalidHTTPResponse(err.StatusCode, err.ResponseBody)
}

func (s *OpenAIGatewayService) buildOpenAIAuthenticationHeaders(ctx context.Context, account *Account, token string) (http.Header, error) {
	if account == nil {
		return nil, errors.New("account is nil")
	}
	credAccount := account
	if account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
		credAccount = resolved
	}
	if err := validateAgentIdentitySigningSnapshot(ctx, account, credAccount); err != nil {
		return nil, err
	}
	credAccount, err := requestCredentialAccount(ctx, credAccount)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	if credAccount != nil && credAccount.IsOpenAIAgentIdentity() {
		agentHeaders, err := buildAgentIdentityAuthenticationHeadersWithIdentity(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, credAccount, s.resolveOpenAIOutboundIdentity(ctx, credAccount))
		if err != nil {
			return nil, err
		}
		return agentHeaders, nil
	}
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func buildAgentIdentityAuthenticationHeadersWithIdentity(ctx context.Context, repo AccountRepository, wsInvalidator agentIdentityWSConnectionInvalidator, taskMu *sync.Mutex, account *Account, identity openAIOutboundIdentity) (http.Header, error) {
	if account == nil || !account.IsOpenAIAgentIdentity() {
		return nil, errors.New("agent identity account is required")
	}
	if err := ensureAgentIdentityTaskForAccountWithIdentity(ctx, repo, wsInvalidator, taskMu, account, "", identity); err != nil {
		return nil, err
	}
	key, err := agentIdentityKeyFromAccount(account)
	if err != nil {
		return nil, err
	}
	assertion, err := buildAgentAssertion(key, time.Now())
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", assertion)
	return headers, nil
}

func validateAgentIdentitySigningSnapshot(ctx context.Context, selected, signer *Account) error {
	if !requestCredentialOwnerBound(ctx, selected) {
		return nil
	}
	owner, known := requestCredentialOwner(ctx, selected)
	if signer.IsOpenAIAgentIdentity() || owner.IsOpenAIAgentIdentity() {
		if !known || !sameAgentIdentitySigningOwner(owner, signer) {
			return errors.New("agent identity signing owner changed")
		}
	}
	return nil
}

func (s *OpenAIGatewayService) refreshOpenAIAgentIdentityHeaders(ctx context.Context, account *Account, headers http.Header) (http.Header, error) {
	if account == nil {
		return cloneHeader(headers), nil
	}
	credAccount := account
	if account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return nil, err
		}
		credAccount = resolved
	}
	if err := validateAgentIdentitySigningSnapshot(ctx, account, credAccount); err != nil {
		return nil, err
	}
	credAccount, err := requestCredentialAccount(ctx, credAccount)
	if err != nil {
		return nil, err
	}
	if !credAccount.IsOpenAIAgentIdentity() {
		return cloneHeader(headers), nil
	}
	refreshed := cloneHeader(headers)
	if refreshed == nil {
		refreshed = make(http.Header)
	}
	// Resolve once so task registration and the retried upstream request use the
	// same identity snapshot even when an administrator updates settings while
	// recovery is in progress.
	identity := s.resolveOpenAIOutboundIdentity(ctx, credAccount)
	authHeaders, err := buildAgentIdentityAuthenticationHeadersWithIdentity(ctx, s.accountRepo, s, &s.agentIdentityTaskMu, credAccount, identity)
	if err != nil {
		return nil, err
	}
	refreshed.Set("Authorization", authHeaders.Get("Authorization"))
	// A task recovery must rebuild the complete Codex identity, not merely the
	// assertion. This prevents a prior mutable header set from leaving a stale
	// User-Agent, Originator, or Version on the retry.
	applyResolvedOpenAIOutboundIdentity(refreshed, identity, true)
	return refreshed, nil
}

func (s *OpenAIGatewayService) recoverAgentIdentityTask(ctx context.Context, account *Account, expectedTaskID string) (context.Context, error) {
	if s == nil {
		return ctx, errors.New("openai gateway service is nil")
	}
	ctx = WithOutboundIdentityScope(ctx, nil)
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return ctx, err
	}
	if owner != nil && owner.IsShadow() {
		owner, err = resolveCredentialAccount(ctx, s.accountRepo, owner)
		if err != nil {
			return ctx, err
		}
	}
	if owner == nil || !owner.IsOpenAIAgentIdentity() {
		return ctx, errors.New("agent identity credentials are unavailable")
	}
	// The rejected task belongs to this attempt, even if the selected row has
	// since changed. Only an unbound caller may supply its own rejected task.
	if owner != account || strings.TrimSpace(expectedTaskID) == "" {
		expectedTaskID = strings.TrimSpace(owner.GetCredential("task_id"))
	}
	before := snapshotRequestCredentialAccount(owner)
	if !account.IsShadow() && !sameAgentIdentityTaskGrant(before, account) {
		return ctx, errors.New("agent identity signing owner changed")
	}
	recovered := snapshotRequestCredentialAccount(owner)
	if err := s.ensureAgentIdentityTask(ctx, recovered, expectedTaskID); err != nil {
		return ctx, err
	}
	if !sameAgentIdentityTaskGrant(before, recovered) {
		return ctx, errors.New("agent identity task recovery changed credential owner")
	}
	// Explicitly acquire the task-only update for the next physical attempt.
	// The context of the rejected attempt must retain its original task.
	recordRequestCredentialOwner(ctx, recovered, "")
	next := withRequestCredentialOwner(ctx, account, "")
	acquired, err := requestCredentialAccount(next, account)
	if err != nil || !sameRequestCredentialGrant(acquired, recovered) {
		return ctx, errors.New("agent identity recovered credential owner is unavailable")
	}
	if !account.IsShadow() && sameAgentIdentityTaskGrant(before, account) {
		account.Credentials = cloneRequestCredentialMap(account.Credentials)
		account.Credentials["task_id"] = recovered.GetCredential("task_id")
	}
	return next, nil
}

func (s *OpenAIGatewayService) isAgentIdentityAccount(ctx context.Context, account *Account) bool {
	if account == nil {
		return false
	}
	credAccount := account
	if account.IsShadow() {
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			return false
		}
		credAccount = resolved
	}
	return credAccount != nil && credAccount.IsOpenAIAgentIdentity()
}

// redactAgentIdentitySensitiveBody removes credential values before an
// upstream error can reach logs, ops events, or returned error text. Agent
// Identity responses should not echo these values, but keeping this boundary
// defensive prevents accidental disclosure if an upstream error does.
func redactAgentIdentitySensitiveBodyForAccount(ctx context.Context, repo AccountRepository, account *Account, body []byte) []byte {
	if account == nil || len(body) == 0 {
		return body
	}
	credAccount := account
	if account != nil && account.IsShadow() {
		if resolved, err := resolveCredentialAccount(ctx, repo, account); err == nil && resolved != nil {
			credAccount = resolved
		}
	}
	if credAccount == nil || !credAccount.IsOpenAIAgentIdentity() {
		return body
	}
	redacted := string(body)
	for _, key := range []string{
		"agent_private_key",
		"agent_runtime_id",
		"task_id",
		"access_token",
		"refresh_token",
		"id_token",
		"api_key",
		"session_key",
		"cookie",
	} {
		if value := strings.TrimSpace(credAccount.GetCredential(key)); value != "" {
			redacted = strings.ReplaceAll(redacted, value, "[redacted]")
		}
	}
	const assertionPrefix = "AgentAssertion "
	for offset := 0; offset < len(redacted); {
		relativeStart := strings.Index(redacted[offset:], assertionPrefix)
		if relativeStart < 0 {
			break
		}
		start := offset + relativeStart
		valueStart := start + len(assertionPrefix)
		end := valueStart
		for end < len(redacted) && !strings.ContainsRune(" \t\r\n\"',}", rune(redacted[end])) {
			end++
		}
		redacted = redacted[:valueStart] + "[redacted]" + redacted[end:]
		offset = valueStart + len("[redacted]")
	}
	return []byte(redacted)
}

func (s *OpenAIGatewayService) redactAgentIdentitySensitiveBody(ctx context.Context, account *Account, body []byte) []byte {
	if !s.isAgentIdentityAccount(ctx, account) {
		return body
	}
	return redactAgentIdentitySensitiveBodyForAccount(ctx, s.accountRepo, account, body)
}
