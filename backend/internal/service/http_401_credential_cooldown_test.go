//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type http401CredentialCooldownRepo struct {
	http401CredentialFenceRepo
	cooldownCalls, unconditionalCooldowns int
}

type http401GrokCooldownRepo struct {
	grokOutcomeOwnerRepo
	attempts int
	expected *Account
}

func (r *http401GrokCooldownRepo) ApplyGrokRequestOutcomeIfUnchanged(ctx context.Context, expected *Account, outcome GrokRequestOutcome) (GrokRequestOutcomeResult, error) {
	r.attempts++
	r.expected = snapshotRequestCredentialAccount(expected)
	return r.grokOutcomeOwnerRepo.ApplyGrokRequestOutcomeIfUnchanged(ctx, expected, outcome)
}

func (r *http401CredentialCooldownRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.unconditionalCooldowns++
	return errors.New("unconditional credential cooldown is forbidden")
}

func (r *http401CredentialCooldownRepo) SetCredentialTempUnschedulableIfUnchanged(_ context.Context, expectedOwner *Account, until time.Time, message string) (bool, error) {
	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	credentials := expectedOwner.Credentials
	proxyID := expectedOwner.ProxyID
	r.cooldownCalls++
	r.attempted = snapshotRequestCredentialAccount(expectedOwner)
	if r.writeErr != nil {
		return false, r.writeErr
	}
	current := r.current
	if current == nil || current.Status != StatusActive || current.ID != id || current.Platform != platform || current.Type != kind || !reflect.DeepEqual(current.Credentials, credentials) || (!reflect.DeepEqual(current.ProxyID, proxyID) || !reflect.DeepEqual(current.Proxy, expectedOwner.Proxy)) {
		return false, nil
	}
	current.TempUnschedulableUntil, current.TempUnschedulableReason = &until, message
	return true, nil
}

func TestHTTP401OAuthCooldownUsesExactSentGrant(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		shadows := []bool{false}
		if platform == PlatformOpenAI {
			shadows = append(shadows, true)
		}
		for _, shadow := range shadows {
			for _, scenario := range []string{"unchanged", "reauthorized", "write-failure", "ambiguous", "unknown-bearer"} {
				name := platform + map[bool]string{false: "/owner/", true: "/shadow/"}[shadow] + scenario
				t.Run(name, func(t *testing.T) {
					owner := &Account{ID: 41, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "sent-T1", "api_key": "sent-T1", "refresh_token": "sent-S1"}}
					if platform == PlatformZhipu {
						// Linked start-plan grants send the ZCode JWT as Bearer
						// against their explicitly declared Anthropic endpoint.
						owner.Credentials["oauth_provider"] = "bigmodel"
						owner.Credentials["plan_kind"] = ZhipuPlanStartPlan
						owner.Credentials["account_mode"] = AccountModeCoding
						owner.Credentials["api_protocol"] = APIProtocolAdaptive
						owner.Credentials["zcode_jwt_token"] = "sent-T1"
						owner.Credentials[credentialAPIBaseURLs] = map[string]any{APIProtocolAnthropic: "https://zcode.z.ai/api/v1/zcode-plan/anthropic"}
						owner.Credentials[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
					}
					selected := owner
					if shadow {
						parentID := owner.ID
						selected = &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, ParentAccountID: &parentID}
					}
					ctx := WithOutboundIdentityScope(context.Background(), nil)
					recordRequestCredentialOwner(ctx, owner, "sent-T1")
					repo := &http401CredentialCooldownRepo{http401CredentialFenceRepo: http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(owner)}}
					if scenario == "reauthorized" || scenario == "ambiguous" {
						repo.current.Credentials["refresh_token"] = "new-login-S2"
					}
					if scenario == "ambiguous" {
						recordRequestCredentialOwner(ctx, repo.current, "sent-T1")
					}
					if scenario == "write-failure" {
						repo.writeErr = errors.New("persistence unavailable")
					}
					bearer := "sent-T1"
					if scenario == "unknown-bearer" {
						bearer = "unknown-T9"
					}
					ctx = withRequestCredentialOwner(ctx, selected, bearer)
					blocker := &runtimeBlockRecorder{}
					invalidator := &tokenCacheInvalidatorRecorder{}
					cfg := &config.Config{RateLimit: config.RateLimitConfig{OAuth401CooldownMinutes: 7}}
					rl := NewRateLimitService(repo, nil, cfg, nil, nil)
					rl.SetAccountRuntimeBlocker(blocker)
					rl.SetTokenCacheInvalidator(invalidator)
					gateway := &OpenAIGatewayService{accountRepo: repo, rateLimitService: rl}
					start := time.Now()
					if platform == PlatformGrok {
						// Exercise the production response handler used by Grok
						// senders. Grok owns its 10-minute outcome transaction and
						// runtime publication; it does not use the shared OAuth cache.
						grokRepo := &http401GrokCooldownRepo{grokOutcomeOwnerRepo: grokOutcomeOwnerRepo{current: repo.current, failStorage: scenario == "write-failure"}}
						gateway.accountRepo = grokRepo
						gateway.handleGrokAccountUpstreamError(ctx, selected, http.StatusUnauthorized, nil, []byte(`{"error":{"message":"invalid token"}}`))
						attempts := grokRepo.attempts
						require.False(t, gateway.handleOpenAIAccountUpstreamError(ctx, selected, http.StatusUnauthorized, nil, []byte(`{"error":{"message":"invalid token"}}`)))
						require.Equal(t, attempts, grokRepo.attempts, "generic formatting cannot apply a second Grok response policy")
						if scenario == "ambiguous" || scenario == "unknown-bearer" {
							require.Zero(t, attempts)
						} else {
							require.Equal(t, 1, attempts)
							require.Equal(t, owner.ID, grokRepo.expected.ID)
							require.Equal(t, owner.Credentials, grokRepo.expected.Credentials)
						}
						require.Zero(t, grokRepo.legacyWrites+repo.cooldownCalls+repo.unconditionalCooldowns+repo.casCalls+repo.unconditionalCalls)
						require.Empty(t, invalidator.accounts, "Grok's response policy does not invalidate the shared OAuth token cache")
						require.Empty(t, blocker.accounts, "Grok publishes its own owner-scoped runtime block after the outcome CAS")
						require.Equal(t, StatusActive, grokRepo.current.Status)
						if scenario == "unchanged" {
							require.Equal(t, 1, grokRepo.atomicWrites)
							require.NotNil(t, grokRepo.current.TempUnschedulableUntil)
							require.WithinDuration(t, start.Add(10*time.Minute), *grokRepo.current.TempUnschedulableUntil, time.Second)
							require.True(t, gateway.isOpenAIAccountRuntimeBlocked(grokRepo.current))
						} else {
							require.Zero(t, grokRepo.atomicWrites)
							require.Nil(t, grokRepo.current.TempUnschedulableUntil)
							require.False(t, gateway.isOpenAIAccountRuntimeBlocked(grokRepo.current), "a stale or uncommitted response cannot publish runtime quarantine")
						}
						return
					}
					require.True(t, gateway.handleOpenAIAccountUpstreamError(ctx, selected, http.StatusUnauthorized, nil, []byte(`{"error":{"message":"invalid token"}}`)))
					require.Zero(t, repo.unconditionalCooldowns)
					require.Zero(t, repo.casCalls, "recoverable OAuth grants retain active status")
					require.Zero(t, repo.unconditionalCalls)
					require.Equal(t, StatusActive, repo.current.Status)
					if scenario == "ambiguous" || scenario == "unknown-bearer" {
						require.Zero(t, repo.cooldownCalls)
					} else {
						require.Equal(t, 1, repo.cooldownCalls)
						require.Equal(t, owner.ID, repo.attempted.ID)
						require.Equal(t, owner.Credentials, repo.attempted.Credentials)
					}
					if scenario == "unchanged" {
						require.NotNil(t, repo.current.TempUnschedulableUntil)
						require.WithinDuration(t, start.Add(7*time.Minute), *repo.current.TempUnschedulableUntil, time.Second)
						require.Len(t, blocker.accounts, 1)
						require.Equal(t, owner.ID, blocker.accounts[0].ID)
						require.Len(t, invalidator.accounts, 1)
					} else {
						require.Nil(t, repo.current.TempUnschedulableUntil)
						require.Empty(t, blocker.accounts)
						require.Empty(t, invalidator.accounts, "a stale 401 cannot delete a concurrent login's cached token")
					}
				})
			}
		}
	}
}
