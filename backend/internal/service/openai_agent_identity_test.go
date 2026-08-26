package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

func newTestAgentIdentityKey(t *testing.T) (agentIdentityKey, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return agentIdentityKey{
		runtimeID:  "runtime-test",
		privateKey: privateKey,
		taskID:     "task-test",
	}, base64.StdEncoding.EncodeToString(der)
}

func TestBuildAgentAssertionMatchesCodexEnvelopeAndSignature(t *testing.T) {
	key, _ := newTestAgentIdentityKey(t)
	now := time.Date(2026, 7, 14, 8, 9, 10, 0, time.FixedZone("UTC+8", 8*60*60))
	assertion, err := buildAgentAssertion(key, now)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(assertion, "AgentAssertion "))

	encoded := strings.TrimPrefix(assertion, "AgentAssertion ")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var envelope struct {
		AgentRuntimeID string `json:"agent_runtime_id"`
		TaskID         string `json:"task_id"`
		Timestamp      string `json:"timestamp"`
		Signature      string `json:"signature"`
	}
	require.NoError(t, json.Unmarshal(decoded, &envelope))
	require.Equal(t, "runtime-test", envelope.AgentRuntimeID)
	require.Equal(t, "task-test", envelope.TaskID)
	require.Equal(t, "2026-07-14T00:09:10Z", envelope.Timestamp)
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	require.NoError(t, err)
	publicKey, ok := key.privateKey.Public().(ed25519.PublicKey)
	require.True(t, ok)
	require.True(t, ed25519.Verify(publicKey, []byte("runtime-test:task-test:2026-07-14T00:09:10Z"), signature))
}

func TestDecryptAgentTaskIDSupportsCodexSealedBoxResponse(t *testing.T) {
	key, _ := newTestAgentIdentityKey(t)
	digest := sha512.Sum512(key.privateKey.Seed())
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	curvePublicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	require.NoError(t, err)
	var curvePublic [32]byte
	copy(curvePublic[:], curvePublicBytes)
	ciphertext, err := box.SealAnonymous(nil, []byte("task-sealed"), &curvePublic, rand.Reader)
	require.NoError(t, err)
	got, err := decryptAgentTaskID(key, base64.StdEncoding.EncodeToString(ciphertext))
	require.NoError(t, err)
	require.Equal(t, "task-sealed", got)
}

func TestRegisterAgentIdentityTaskAcceptsPlaintextAndEncryptedResponses(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/agent/runtime-test/task/register", r.URL.Path)
		var request map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.NotEmpty(t, request["timestamp"])
		require.NotEmpty(t, request["signature"])
		requestCount++
		if requestCount == 2 {
			digest := sha512.Sum512(key.privateKey.Seed())
			var curvePrivate [32]byte
			copy(curvePrivate[:], digest[:32])
			curvePrivate[0] &= 248
			curvePrivate[31] &= 127
			curvePrivate[31] |= 64
			curvePublicBytes, curveErr := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
			require.NoError(t, curveErr)
			var curvePublic [32]byte
			copy(curvePublic[:], curvePublicBytes)
			ciphertext, sealErr := box.SealAnonymous(nil, []byte("task-encrypted"), &curvePublic, rand.Reader)
			require.NoError(t, sealErr)
			_, _ = fmt.Fprintf(w, `{"encrypted_task_id":%q}`, base64.StdEncoding.EncodeToString(ciphertext))
			return
		}
		_, _ = w.Write([]byte(`{"task_id":"task-plain"}`))
	}))
	defer server.Close()
	oldBase := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })

	account := &Account{ID: 1, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":         OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":  key.runtimeID,
		"agent_private_key": privateKey,
	}}
	identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, nil)
	taskID, err := registerAgentIdentityTaskWithIdentity(context.Background(), account, identity)
	require.NoError(t, err)
	require.Equal(t, "task-plain", taskID)
	taskID, err = registerAgentIdentityTaskWithIdentity(context.Background(), account, identity)
	require.NoError(t, err)
	require.Equal(t, "task-encrypted", taskID)
}

func TestEnsureAgentIdentityTaskPersistsAndRedactsCredentials(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"task_id":"task-persisted"}`))
	}))
	defer server.Close()
	oldBase := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })

	repo := &agentIdentityCredentialsRepo{}
	account := &Account{ID: 7, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":          OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":   key.runtimeID,
		"agent_private_key":  privateKey,
		"chatgpt_account_id": "account-test",
	}}
	service := &OpenAIGatewayService{accountRepo: repo}
	require.NoError(t, service.ensureAgentIdentityTask(context.Background(), account, ""))
	require.Equal(t, "task-persisted", account.GetCredential("task_id"))
	require.Equal(t, "task-persisted", repo.credentials["task_id"])
	require.True(t, IsSensitiveCredentialKey("agent_private_key"))
	redacted := make(map[string]any)
	for key, value := range account.Credentials {
		if !IsSensitiveCredentialKey(key) {
			redacted[key] = value
		}
	}
	require.NotContains(t, string(mustAgentIdentityJSON(t, redacted)), privateKey)
}

func TestEnsureAgentIdentityTaskSharesLockAcrossServicesForSameAccount(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	account := &Account{ID: 9001, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":         OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":  key.runtimeID,
		"agent_private_key": privateKey,
	}}
	repo := &agentIdentityCredentialsRepo{account: account}
	registerCalls := 0
	var registerMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registerMu.Lock()
		registerCalls++
		registerMu.Unlock()
		_, _ = w.Write([]byte(`{"task_id":"task-shared"}`))
	}))
	defer server.Close()
	oldBase := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })

	start := make(chan struct{})
	errors := make(chan error, 2)
	requests := []*Account{cloneAgentIdentityTestAccount(account), cloneAgentIdentityTestAccount(account)}
	for _, request := range requests {
		go func() {
			<-start
			identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), request, nil)
			errors <- ensureAgentIdentityTaskForAccountWithIdentity(context.Background(), repo, nil, &sync.Mutex{}, request, "", identity)
		}()
	}
	close(start)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	registerMu.Lock()
	defer registerMu.Unlock()
	require.Equal(t, 1, registerCalls)
	require.Equal(t, "task-shared", repo.account.GetCredential("task_id"))
}

func TestRefreshOpenAIAgentIdentityHeadersUsesOneIdentitySnapshot(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	settingsRepo := &agentIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent:     "codex-tui/9.9.9 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 9.9.9)",
		SettingKeyOpenAICodexClientVersion: "0.200.1",
	}}
	settings := &SettingService{settingRepo: settingsRepo}
	account := &Account{ID: 9002, Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{
		"auth_mode":         OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":  key.runtimeID,
		"agent_private_key": privateKey,
	}}
	repo := &agentIdentityCredentialsRepo{account: account}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "codex-tui/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.200.1)", r.Header.Get("User-Agent"))
		require.Equal(t, "codex-tui", r.Header.Get("Originator"))
		require.Equal(t, "0.200.1", r.Header.Get("Version"))
		settingsRepo.setValue(SettingKeyOpenAICodexClientVersion, "0.201.0")
		settings.InvalidateOpenAICodexClientVersionCache()
		_, _ = w.Write([]byte(`{"task_id":"task-refreshed"}`))
	}))
	defer server.Close()
	oldBase := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })

	service := &OpenAIGatewayService{accountRepo: repo, settingService: settings}
	refreshed, err := service.refreshOpenAIAgentIdentityHeaders(context.Background(), account, http.Header{
		"User-Agent": {"stale-client/1.0"},
		"Originator": {"stale-client"},
		"Version":    {"1.0"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, refreshed.Get("Authorization"))
	require.Equal(t, "codex-tui/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.200.1)", refreshed.Get("User-Agent"))
	require.Equal(t, "codex-tui", refreshed.Get("Originator"))
	require.Equal(t, "0.200.1", refreshed.Get("Version"))

	nextIdentity := service.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, "0.201.0", nextIdentity.Version)
}

func cloneAgentIdentityTestAccount(account *Account) *Account {
	copy := *account
	copy.Credentials = shallowCopyMap(account.Credentials)
	return &copy
}

type agentIdentityCredentialsRepo struct {
	AccountRepository
	credentials map[string]any
	account     *Account
	mu          sync.Mutex
}

type agentIdentitySettingRepoStub struct {
	mu     sync.RWMutex
	values map[string]string
}

func (s *agentIdentitySettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (s *agentIdentitySettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *agentIdentitySettingRepoStub) Set(context.Context, string, string) error { return nil }

func (s *agentIdentitySettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (s *agentIdentitySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *agentIdentitySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *agentIdentitySettingRepoStub) Delete(context.Context, string) error { return nil }

func (s *agentIdentitySettingRepoStub) setValue(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (r *agentIdentityCredentialsRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

func (r *agentIdentityCredentialsRepo) UpdateCredentials(_ context.Context, _ int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials = credentials
	return nil
}

func mustAgentIdentityJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}
