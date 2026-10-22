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
		for _, changed := range []string{"runtime", "key", "auth_mode", "proxy", "proxy_status", "fingerprint", "header"} {
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
					case "proxy_status":
						repo.account.Proxy = &Proxy{Status: "inactive"}
					case "fingerprint":
						repo.account.Credentials["user_agent"] = testOpenAIAccountUserAgent
					case "header":
						repo.account.Credentials["chatgpt_account_id"] = "organization-relinked"
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

func TestAgentIdentityTaskRecoveryBindsNewAttemptWithoutRewritingOld(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "shadow"}[shadow], func(t *testing.T) {
			_, key := newTestAgentIdentityKey(t)
			proxyID := int64(18)
			owner := &Account{ID: 456, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &proxyID,
				Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "proxy-original.invalid", Port: 8080, Username: "original", Password: "secret", Status: StatusActive},
				Credentials: map[string]any{
					"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": key,
					"task_id": "task-old", "user_agent": testOpenAIAccountUserAgent,
				}}
			selected := snapshotRequestCredentialAccount(owner)
			if shadow {
				selected = &Account{ID: 457, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID}
			}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			recordRequestCredentialOwner(ctx, owner, "")
			first := withRequestCredentialOwner(ctx, selected, "")
			current := snapshotRequestCredentialAccount(owner)
			current.Credentials["task_id"] = "task-new"
			repo := &agentIdentityCredentialsRepo{account: current}
			svc := &OpenAIGatewayService{accountRepo: repo}

			next, err := svc.recoverAgentIdentityTask(first, selected, "task-old")
			require.NoError(t, err)
			oldOwner, known := requestCredentialOwner(first, selected)
			require.True(t, known)
			newOwner, known := requestCredentialOwner(next, selected)
			require.True(t, known)
			require.Equal(t, "task-old", oldOwner.GetCredential("task_id"))
			require.Equal(t, "task-new", newOwner.GetCredential("task_id"))
			require.Equal(t, testOpenAIAccountUserAgent, newOwner.GetCredential("user_agent"))
			require.Equal(t, owner.ProxyID, newOwner.ProxyID)
			require.Equal(t, owner.Proxy, newOwner.Proxy)
			require.Equal(t, "runtime-original", newOwner.GetCredential("agent_runtime_id"))
			require.Equal(t, key, newOwner.GetCredential("agent_private_key"))
			require.Empty(t, repo.credentials, "adopting a same-signer task must not rewrite repository credentials")

			// Authentication and recursive acquisition can receive an older owner
			// pointer. The new attempt's authority must still supply its task.
			stale := snapshotRequestCredentialAccount(owner)
			oldHeaders, err := svc.buildOpenAIAuthenticationHeaders(first, selected, "")
			require.NoError(t, err)
			newHeaders, err := svc.buildOpenAIAuthenticationHeaders(next, stale, "")
			require.NoError(t, err)
			require.Equal(t, "task-old", decodeAgentAssertionTask(t, oldHeaders.Get("Authorization")))
			require.Equal(t, "task-new", decodeAgentAssertionTask(t, newHeaders.Get("Authorization")))
			_, kind, err := svc.GetAccessToken(next, stale)
			require.NoError(t, err)
			require.Equal(t, OpenAIAuthModeAgentIdentity, kind)
			reacquired, known := requestCredentialOwner(withRequestCredentialOwner(next, selected, ""), selected)
			require.True(t, known)
			require.Equal(t, "task-new", reacquired.GetCredential("task_id"), "recursive acquisition must not republish the rejected task")
		})
	}
}

func TestAgentIdentityTaskRecoveryRejectsRelinkedSigner(t *testing.T) {
	for _, changed := range []string{"runtime", "key", "type", "auth_mode", "proxy", "proxy_status", "fingerprint", "header"} {
		for _, source := range []string{"selected", "repository"} {
			t.Run(source+"/"+changed, func(t *testing.T) {
				_, key := newTestAgentIdentityKey(t)
				_, replacementKey := newTestAgentIdentityKey(t)
				account := &Account{ID: 458, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
					"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": key, "task_id": "task-old",
				}}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				recordRequestCredentialOwner(ctx, account, "")
				ctx = withRequestCredentialOwner(ctx, account, "")
				current := snapshotRequestCredentialAccount(account)
				current.Credentials["task_id"] = "task-new"
				relinked := current
				if source == "selected" {
					relinked = account
				}
				switch changed {
				case "runtime":
					relinked.Credentials["agent_runtime_id"] = "runtime-relinked"
				case "key":
					relinked.Credentials["agent_private_key"] = replacementKey
				case "type":
					relinked.Type = AccountTypeAPIKey
				case "auth_mode":
					relinked.Credentials["auth_mode"] = "oauth"
				case "proxy":
					proxyID := int64(19)
					relinked.ProxyID = &proxyID
				case "proxy_status":
					relinked.Proxy = &Proxy{Status: "inactive"}
				case "fingerprint":
					relinked.Credentials["user_agent"] = testOpenAIAccountUserAgent
				case "header":
					relinked.Credentials["chatgpt_account_id"] = "organization-relinked"
				}
				repo := &agentIdentityCredentialsRepo{account: current}
				svc := &OpenAIGatewayService{accountRepo: repo}
				_, err := svc.recoverAgentIdentityTask(ctx, account, "task-old")
				require.Error(t, err)
				oldOwner, known := requestCredentialOwner(ctx, account)
				require.True(t, known)
				require.Equal(t, "task-old", oldOwner.GetCredential("task_id"))
				require.Empty(t, repo.credentials, "a relink must not be overwritten by task recovery")
			})
		}
	}
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
