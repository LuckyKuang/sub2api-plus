//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type openAITransientRecoveryRepo struct {
	AccountRepository
	current        *Account
	readFailure    bool
	resetAfterRead bool
	reads          int
}

func (r *openAITransientRecoveryRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	if r.readFailure {
		return nil, errors.New("injected transient observation read failure")
	}
	owner, err := CaptureOAuthCredentialOwner(r.current)
	if r.resetAfterRead {
		r.current.Extra[RuntimeRecoveryGenerationExtraKey] = "after-reset"
	}
	return owner, err
}

func TestOpenAIAPIKeyModelFailureKeepsRecoveryObservationBoundary(t *testing.T) {
	for _, order := range []string{"unchanged", "reset-before-read", "reset-after-read", "relink-before-read", "proxy-edit-before-read", "read-failure"} {
		t.Run(order, func(t *testing.T) {
			account := grokStrictProxyAccount(AccountTypeAPIKey, "valid-bound")
			account.ID, account.Platform = time.Now().UnixNano(), PlatformOpenAI
			delete(account.Credentials, outboundIdentityCredential)
			account.Credentials["user_agent"] = DefaultOpenAICodexUserAgent
			account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "before-reset"}
			observed, err := CaptureOAuthCredentialOwner(account)
			require.NoError(t, err)
			repo := &openAITransientRecoveryRepo{current: account, readFailure: order == "read-failure", resetAfterRead: order == "reset-after-read"}
			switch order {
			case "reset-before-read":
				account.Extra[RuntimeRecoveryGenerationExtraKey] = "after-reset"
			case "relink-before-read":
				account.Credentials["api_key"] = "replacement-provider-key"
			case "proxy-edit-before-read":
				account.Proxy.Password = "replacement-route-password"
			}
			cfg := &config.Config{}
			gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil)}
			gateway.handleOpenAIAccountUpstreamError(context.Background(), observed, http.StatusServiceUnavailable, nil, []byte(`{"error":{"message":"isolated transient failure"}}`), "gpt-5.5")
			if order == "reset-after-read" {
				repo.resetAfterRead = false
			}
			gateway.handleOpenAIAccountUpstreamError(context.Background(), observed, http.StatusServiceUnavailable, nil, []byte(`{"error":{"message":"isolated transient failure"}}`), "gpt-5.5")
			wantBlocked := order == "unchanged" || order == "reset-before-read"
			require.Equal(t, wantBlocked, gateway.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"), "a new failure binds the recovery generation observed with the same credential and route; uncertain or replaced owners cannot publish a model streak")
			require.GreaterOrEqual(t, repo.reads, 2, "each upstream response owns its own state observation boundary")
		})
	}
}

func TestOpenAIAPIKeyModelSuccessClearsOnlyItsRecoveryGeneration(t *testing.T) {
	for _, which := range []string{"current-success", "historical-success"} {
		t.Run(which, func(t *testing.T) {
			account := &Account{ID: time.Now().UnixNano(), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{RuntimeRecoveryGenerationExtraKey: "old-generation"}}
			old, err := CaptureOAuthCredentialOwner(account)
			require.NoError(t, err)
			gateway := &OpenAIGatewayService{}
			for range 2 {
				gateway.recordOpenAIAccountModelTransientFailure(old, "gpt-5.5", time.Now())
			}
			account.Extra[RuntimeRecoveryGenerationExtraKey] = "new-generation"
			for range 2 {
				gateway.recordOpenAIAccountModelTransientFailure(account, "gpt-5.5", time.Now())
			}
			success := account
			if which == "historical-success" {
				success = old
			}
			gateway.ReportOpenAIAccountScheduleResult(success, "gpt-5.5", true, nil)
			require.Equal(t, which == "historical-success", gateway.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"), "a delayed historical success cannot erase a recovered generation's new failures")
			if which == "historical-success" {
				require.False(t, gateway.isOpenAIAccountModelRuntimeBlocked(old, "gpt-5.5"))
			}
		})
	}
}
