//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentityTaskCannotCrossConcurrentRelink(t *testing.T) {
	// OUTBOUND_IDENTITY requires the assertion, client triple and execution IDs
	// to belong to one snapshot, even when task bootstrap re-reads the row.
	for _, phase := range []string{"before_task_read", "during_registration"} {
		for _, changed := range []string{"runtime", "key", "auth_mode", "proxy"} {
			t.Run(phase+"/"+changed, func(t *testing.T) {
				_, originalKey := newTestAgentIdentityKey(t)
				_, replacementKey := newTestAgentIdentityKey(t)
				account := &Account{ID: 451, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
					"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": originalKey,
				}}
				repo := &agentIdentityCredentialsRepo{account: cloneAgentIdentityTestAccount(account)}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				recordRequestCredentialOwner(ctx, account, "")
				ctx = withRequestCredentialOwner(ctx, account, "")
				relink := func() {
					repo.mu.Lock()
					defer repo.mu.Unlock()
					switch changed {
					case "runtime":
						repo.account.Credentials["agent_runtime_id"] = "runtime-relinked"
					case "key":
						repo.account.Credentials["agent_private_key"] = replacementKey
					case "auth_mode":
						repo.account.Credentials["auth_mode"] = "oauth"
					case "proxy":
						proxyID := int64(19)
						repo.account.ProxyID = &proxyID
					}
				}
				registrations := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					registrations++
					require.Equal(t, "/v1/agent/runtime-original/task/register", r.URL.Path)
					if phase == "during_registration" {
						relink()
					}
					_, _ = w.Write([]byte(`{"task_id":"obsolete-task"}`))
				}))
				defer server.Close()
				originalURL := openAIAgentIdentityAuthAPIBaseURL
				openAIAgentIdentityAuthAPIBaseURL = server.URL
				t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = originalURL })
				if phase == "before_task_read" {
					relink()
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("session-id", "signing-session")
				svc := &OpenAIGatewayService{accountRepo: repo}
				req, err := svc.buildUpstreamRequestOpenAIPassthrough(ctx, c, account, []byte(`{"model":"gpt-5.6","input":"hello"}`), "")
				require.Error(t, err)
				require.Nil(t, req, "no inference request may combine old IDs with a new signer")
				require.Empty(t, repo.account.GetCredential("task_id"), "late bootstrap must not overwrite the relinked row")
				require.Empty(t, repo.credentials)
				if phase == "before_task_read" {
					require.Zero(t, registrations)
				} else {
					require.Equal(t, 1, registrations)
				}
			})
		}
	}
}

func TestAgentIdentityTaskReadAdoptsOnlySameOwnerTask(t *testing.T) {
	_, key := newTestAgentIdentityKey(t)
	account := &Account{ID: 452, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": key,
		"user_agent": testOpenAIAccountUserAgent,
	}}
	current := cloneAgentIdentityTestAccount(account)
	current.Credentials["task_id"] = "task-from-other-worker"
	current.Credentials["user_agent"] = DefaultOpenAICodexUserAgent
	repo := &agentIdentityCredentialsRepo{account: current}
	identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, nil)
	headers, err := buildAgentIdentityAuthenticationHeadersWithIdentity(context.Background(), repo, nil, &sync.Mutex{}, account, identity)
	require.NoError(t, err)
	require.NotEmpty(t, headers.Get("Authorization"))
	require.Equal(t, "task-from-other-worker", account.GetCredential("task_id"))
	require.Equal(t, testOpenAIAccountUserAgent, account.GetCredential("user_agent"), "task read must preserve the captured client fingerprint")
	require.Empty(t, repo.credentials, "a same-owner task already exists")
}

func TestAgentIdentityTaskRecoveryAdvancesOnlyNextAcquisition(t *testing.T) {
	_, key := newTestAgentIdentityKey(t)
	account := &Account{ID: 453, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": key, "task_id": "task-old",
	}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, account, "")
	first := withRequestCredentialOwner(ctx, account, "")
	account.Credentials["task_id"] = "task-recovered"
	recordRequestCredentialOwner(ctx, account, "")
	second := withRequestCredentialOwner(ctx, account, "")
	firstOwner, known := requestCredentialOwner(first, account)
	require.True(t, known)
	secondOwner, known := requestCredentialOwner(second, account)
	require.True(t, known)
	require.Equal(t, "task-old", firstOwner.GetCredential("task_id"))
	require.Equal(t, "task-recovered", secondOwner.GetCredential("task_id"))
	require.Equal(t, openAICodexStateOwner(first, account), openAICodexStateOwner(second, account), "task recovery does not create a new signing principal")
	account.Credentials["agent_runtime_id"] = "runtime-relinked"
	recordRequestCredentialOwner(ctx, account, "")
	_, known = requestCredentialOwner(withRequestCredentialOwner(ctx, account, ""), account)
	require.False(t, known, "a different signer under the same empty bearer remains ambiguous")
}

func TestAgentIdentityShadowCannotSignWithRelinkedParent(t *testing.T) {
	for _, changed := range []string{"runtime", "key", "auth_mode", "proxy", "same_owner_task"} {
		t.Run(changed, func(t *testing.T) {
			_, key := newTestAgentIdentityKey(t)
			_, replacementKey := newTestAgentIdentityKey(t)
			parent := &Account{ID: 454, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": key,
				"task_id": "task-original", "chatgpt_account_id": "organization-original",
			}}
			shadow := &Account{ID: 455, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			recordRequestCredentialOwner(ctx, parent, "")
			ctx = withRequestCredentialOwner(ctx, shadow, "")
			current := snapshotRequestCredentialAccount(parent)
			switch changed {
			case "runtime":
				current.Credentials["agent_runtime_id"] = "runtime-relinked"
			case "key":
				current.Credentials["agent_private_key"] = replacementKey
			case "auth_mode":
				current.Credentials["auth_mode"] = "oauth"
			case "proxy":
				proxyID := int64(19)
				current.ProxyID = &proxyID
			case "same_owner_task":
				current.Credentials["task_id"] = "task-recovered"
			}
			current.Credentials["chatgpt_account_id"] = "organization-later"
			svc := &OpenAIGatewayService{accountRepo: &agentIdentityCredentialsRepo{account: current}}
			for _, builder := range []string{"initial", "retry"} {
				var headers http.Header
				var err error
				if builder == "initial" {
					headers, err = svc.buildOpenAIAuthenticationHeaders(ctx, shadow, "")
				} else {
					headers, err = svc.refreshOpenAIAgentIdentityHeaders(ctx, shadow, http.Header{"Authorization": {"stale"}})
				}
				if changed == "same_owner_task" {
					require.NoError(t, err, builder)
					require.Contains(t, headers.Get("Authorization"), "AgentAssertion ", builder)
				} else {
					require.Error(t, err, builder)
					require.Nil(t, headers, builder)
				}
			}
			headers := make(http.Header)
			require.NoError(t, resolveAndSetOpenAIChatGPTAccountHeaders(ctx, svc.accountRepo, headers, shadow))
			require.Equal(t, "organization-original", headers.Get("chatgpt-account-id"), "organization declarations use the acquired owner")
		})
	}
}
