//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIMissingRefreshQuarantineRepo struct{ *rateLimitAccountRepoStub }

func (r *openAIMissingRefreshQuarantineRepo) SetCredentialErrorIfUnchanged(ctx context.Context, expectedOwner *Account, reason string) (bool, error) {
	id := expectedOwner.ID
	return true, r.SetError(ctx, id, reason)
}

type credentialFenceRepo struct {
	AccountRepository
	observed *Account
	applied  bool
	err      error
	calls    int
}

func (r *credentialFenceRepo) SetCredentialErrorIfUnchanged(_ context.Context, expectedOwner *Account, _ string) (bool, error) {
	r.calls++
	r.observed = snapshotRequestCredentialAccount(expectedOwner)
	return r.applied, r.err
}

// Account-auth failures must recover within the original request. A provider
// dependency outage is a stop, and an exact failed credential snapshot fences
// quarantine against a newer grant. No raw provider error is client-visible.
func TestCredentialFailureScopeAndSnapshotRegression(t *testing.T) {
	for _, scenario := range []string{"revoked", "concurrent-rotation", "write-failure", "shared-provider", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &credentialFenceRepo{applied: scenario == "revoked"}
			if scenario == "write-failure" {
				repo.err = errors.New("database unavailable secret-detail")
			}
			svc := &OpenAIGatewayService{accountRepo: repo}
			selected := &Account{ID: 1, Platform: PlatformKimi, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old"}}
			failed := snapshotOAuthRefreshAccount(selected)
			failed.Credentials["access_token"] = "actually-rejected"
			cause := errors.New("invalid_grant secret-detail")
			err := WithCredentialFailureAccount(NewAccountCredentialFailure("grant_revoked", cause), failed)
			if scenario == "shared-provider" {
				err = NewProviderCredentialFailure("provider_unavailable", cause)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			got := svc.handleOpenAIUpstreamTransportError(ctx, nil, selected, err, false)
			if scenario == "cancelled" {
				require.ErrorIs(t, got, context.Canceled)
				require.Zero(t, repo.calls)
				return
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, got, &failure)
			require.True(t, failure.IsCredentialFailure())
			require.Equal(t, CredentialUnavailableClientMessage, failure.ClientMessage)
			require.NotContains(t, failure.Error(), "secret-detail")
			if scenario == "shared-provider" || scenario == "write-failure" {
				require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
				require.False(t, failure.ShouldRetryNextAccount())
				require.False(t, failure.ShouldReportAccountScheduleFailure())
			} else {
				require.Equal(t, GatewayFailureScopeAccount, failure.Scope)
				require.True(t, failure.AllowsOpenAIAccountSwitch())
			}
			if scenario == "shared-provider" {
				require.Zero(t, repo.calls)
			} else {
				require.Equal(t, "actually-rejected", repo.observed.GetCredential("access_token"))
				require.Equal(t, "old", selected.GetCredential("access_token"))
			}
		})
	}
}

func TestExplicitTransportRetryDoesNotRequireCredentialHealthPenalty(t *testing.T) {
	svc := &OpenAIGatewayService{}
	err := svc.handleOpenAIUpstreamTransportError(context.Background(), nil, &Account{ID: 1, Platform: PlatformOpenAI}, errors.New("connection reset before response headers"), false)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.AllowsOpenAIAccountSwitch())
	require.False(t, failure.ShouldReportAccountScheduleFailure())

	for _, failure := range []*UpstreamFailoverError{
		newOpenAIUpstreamFailoverError(http.StatusServiceUnavailable, nil, []byte(`{"error":{"code":"server_is_overloaded","message":"Selected model is at capacity"}}`), "Selected model is at capacity", false),
		{Scope: GatewayFailureScopeProvider},
		{Scope: GatewayFailureScopeRequest},
		{NextAccountAction: NextAccountStop, Scope: GatewayFailureScopeAccount},
	} {
		require.False(t, failure.AllowsOpenAIAccountSwitch(), "shared/request failures must not gain owner migration merely by skipping health penalties")
	}
}

func TestRawChatFallbackMissingCredentialsAllowAccountRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformMiniMax, PlatformDeepseek, PlatformStepFun} {
		for _, protocol := range []string{"responses", "messages"} {
			t.Run(platform+"/"+protocol, func(t *testing.T) {
				repo := &credentialFenceRepo{applied: true}
				svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}}
				account := &Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_protocol": APIProtocolChatCompletions}}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				body := `{"model":"fixture-model","input":"hello"}`
				if protocol == "messages" {
					body = `{"model":"fixture-model","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, strings.NewReader(body))
				var err error
				if protocol == "responses" {
					_, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, []byte(body), time.Now())
				} else {
					_, err = svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, []byte(body), "")
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.True(t, failure.AllowsOpenAIAccountSwitch(), "an unconfigured native API key must recover through account selection")
				require.True(t, failure.IsCredentialFailure())
				require.Equal(t, 1, repo.calls)
				require.False(t, c.Writer.Written(), "credential acquisition must leave the downstream response replayable")
			})
		}
	}
}
