//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type asyncCreateOutcomeUpstream struct {
	HTTPUpstream
	calls    int
	err      error
	response *http.Response
}

func (u *asyncCreateOutcomeUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	return u.response, u.err
}

func TestAsyncCreateNotSentEvidencePreservesOriginalErrorAndClassification(t *testing.T) {
	failure := &UpstreamFailoverError{
		Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeProvider,
		Reason: GrokCredentialReasonProviderConfig, NextAccountAction: NextAccountStop,
		ClientStatusCode: http.StatusServiceUnavailable, ClientMessage: "credential service unavailable",
	}
	original := fmt.Errorf("credential preparation: %w", failure)
	wrapped := withAsyncCreateNotSent(original)
	require.Equal(t, original.Error(), wrapped.Error())
	require.ErrorIs(t, wrapped, original)
	var classified *UpstreamFailoverError
	require.ErrorAs(t, wrapped, &classified)
	require.Same(t, failure, classified, "evidence cannot replace or reclassify the underlying failure")
	require.False(t, classified.AsyncCreateDefinitelyRejected, "a local unsent create is distinct from a provider rejection")
	require.False(t, classified.ShouldRetryNextAccount())
	require.False(t, classified.ShouldReportAccountScheduleFailure())
	require.Equal(t, http.StatusServiceUnavailable, classified.ClientStatusCode)
	require.Equal(t, "credential service unavailable", classified.ClientMessage)
	require.True(t, AsyncCreateWasNotSent(fmt.Errorf("outer layer: %w", wrapped)))
	require.Same(t, wrapped, withAsyncCreateNotSent(wrapped))
	require.False(t, AsyncCreateWasNotSent(original))
	require.False(t, AsyncCreateWasNotSent(context.DeadlineExceeded))
	require.False(t, AsyncCreateWasNotSent(nil))
}

func TestMediaCreateFailuresBeforeTransportHaveNotSentEvidence(t *testing.T) {
	for _, supplier := range []string{"grok", "seedance"} {
		scenarios := []string{"unsupported_account", "missing_credential", "invalid_base_url"}
		if supplier == "grok" {
			scenarios = append(scenarios, "owner_snapshot_failure", "credential_provider_failure")
		} else {
			scenarios = append(scenarios, "invalid_body", "request_construction_failure")
		}
		for _, scenario := range scenarios {
			t.Run(supplier+"/"+scenario, func(t *testing.T) {
				account := grokMediaContentTestAccount()
				body := []byte(`{"model":"grok-imagine-video","prompt":"waves"}`)
				if supplier == "seedance" {
					account = seedanceTestAccount()
					body = []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]}`)
				}
				var ctx context.Context = context.Background()
				switch scenario {
				case "unsupported_account":
					account.Type = AccountTypeOAuth
					if supplier == "grok" {
						account.Platform = PlatformOpenAI
					}
				case "missing_credential":
					delete(account.Credentials, "api_key")
				case "invalid_base_url":
					account.Credentials["base_url"] = "file:///invalid-provider-url"
				case "owner_snapshot_failure":
					account.Extra = map[string]any{"invalid_json_value": make(chan int)}
				case "credential_provider_failure":
					account.Type = AccountTypeOAuth
				case "invalid_body":
					body = []byte(`{"model":"video","content":[]}`)
				case "request_construction_failure":
					ctx = nil // net/http must reject a request without its context.
				}
				upstream := &asyncCreateOutcomeUpstream{}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				c, _ := grokMediaContentTestContext(http.MethodPost, "/v1/videos/generations", nil)
				var result *OpenAIForwardResult
				var err error
				if supplier == "seedance" {
					result, err = svc.ForwardSeedance(ctx, c, account, SeedanceEndpointCreate, "", body)
				} else {
					result, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Zero(t, upstream.calls, "local preparation errors must not attempt a provider create")
				require.True(t, AsyncCreateWasNotSent(err), "each pre-transport exit carries explicit no-create evidence: %v", err)
				if scenario == "credential_provider_failure" {
					var classified *UpstreamFailoverError
					require.ErrorAs(t, err, &classified)
					require.Equal(t, GatewayFailureStageAccountAuth, classified.Stage)
					require.Equal(t, GatewayFailureScopeProvider, classified.Scope)
					require.Equal(t, GrokCredentialReasonProviderConfig, classified.Reason)
					require.Equal(t, NextAccountStop, classified.NextAccountAction)
					require.Equal(t, http.StatusServiceUnavailable, classified.ClientStatusCode)
				}
			})
		}
	}
}

type asyncCreateResponseReadFailure struct{}

func (asyncCreateResponseReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("isolated response read failure")
}

func TestMediaCreateTransportAndResponseFailuresNeverClaimNotSent(t *testing.T) {
	for _, supplier := range []string{"grok", "seedance"} {
		for _, scenario := range []string{"transport_timeout", "response_read_failure"} {
			t.Run(supplier+"/"+scenario, func(t *testing.T) {
				upstream := &asyncCreateOutcomeUpstream{err: context.DeadlineExceeded}
				if scenario == "response_read_failure" {
					upstream.err = nil
					upstream.response = &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(asyncCreateResponseReadFailure{})}
				}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				c, _ := grokMediaContentTestContext(http.MethodPost, "/v1/videos/generations", nil)
				var result *OpenAIForwardResult
				var err error
				if supplier == "seedance" {
					result, err = svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointCreate, "", []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]}`))
				} else {
					result, err = svc.ForwardGrokMedia(context.Background(), c, grokMediaContentTestAccount(), GrokMediaEndpointVideosGenerations, "", []byte(`{"model":"grok-imagine-video","prompt":"waves"}`), "application/json")
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 1, upstream.calls)
				require.False(t, AsyncCreateWasNotSent(err), "entering Do makes creation ambiguous even when the response is lost")
				if supplier == "seedance" && scenario == "transport_timeout" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			})
		}
	}
}
