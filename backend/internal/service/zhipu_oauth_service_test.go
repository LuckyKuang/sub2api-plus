//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

// zhipuOAuthStubClient is a scripted ZCode platform. It records the sequencing
// the flow depends on (poll token reuse, bearer token exchange) and lets each
// test fail one stage at a time.
type zhipuOAuthStubClient struct {
	initFlow    *zcode.FlowInit
	initErr     error
	polls       []*zcode.FlowPoll
	pollErr     error
	exchange    *zcode.FlowReady
	exchangeErr error

	businessToken    string
	businessTokenErr error
	individual       *zcode.CodingPlanCredential
	individualErr    error
	team             *zcode.CodingPlanCredential
	teamErr          error

	pollCalls        int
	businessCalls    int
	individualCalls  int
	teamCalls        int
	lastPollToken    string
	lastFlowID       string
	lastTeamScope    [2]string
	lastExchangeArgs [3]string
}

func (s *zhipuOAuthStubClient) StartFlow(context.Context, string, string, string) (*zcode.FlowInit, error) {
	if s.initErr != nil {
		return nil, s.initErr
	}
	return s.initFlow, nil
}

func (s *zhipuOAuthStubClient) PollFlow(_ context.Context, _ string, flowID, pollToken, _ string) (*zcode.FlowPoll, error) {
	s.pollCalls++
	s.lastFlowID, s.lastPollToken = flowID, pollToken
	if s.pollErr != nil {
		return nil, s.pollErr
	}
	if len(s.polls) == 0 {
		return &zcode.FlowPoll{Status: zcode.FlowStatusPending}, nil
	}
	result := s.polls[0]
	if len(s.polls) > 1 {
		s.polls = s.polls[1:]
	}
	return result, nil
}

func (s *zhipuOAuthStubClient) ExchangeCode(_ context.Context, provider, code, redirectURI, state, _ string) (*zcode.FlowReady, error) {
	s.lastExchangeArgs = [3]string{provider, code, state}
	if s.exchangeErr != nil {
		return nil, s.exchangeErr
	}
	return s.exchange, nil
}

func (s *zhipuOAuthStubClient) ExchangeZaiBusinessToken(context.Context, string, string) (string, error) {
	s.businessCalls++
	if s.businessTokenErr != nil {
		return "", s.businessTokenErr
	}
	return s.businessToken, nil
}

func (s *zhipuOAuthStubClient) ResolveIndividualCodingPlanKey(context.Context, string, string, string) (*zcode.CodingPlanCredential, error) {
	s.individualCalls++
	if s.individualErr != nil {
		return nil, s.individualErr
	}
	return s.individual, nil
}

func (s *zhipuOAuthStubClient) ResolveTeamPlanKey(_ context.Context, _, _, organizationID, projectID, _ string) (*zcode.CodingPlanCredential, error) {
	s.teamCalls++
	s.lastTeamScope = [2]string{organizationID, projectID}
	if s.teamErr != nil {
		return nil, s.teamErr
	}
	return s.team, nil
}

func newZhipuOAuthTestService(t *testing.T, client *zhipuOAuthStubClient) *ZhipuOAuthService {
	t.Helper()
	svc := NewZhipuOAuthService(client, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func zhipuTestFlowInit() *zcode.FlowInit {
	return &zcode.FlowInit{
		FlowID:          "flow-1",
		AuthorizeURL:    "https://bigmodel.cn/login?appId=zcode&state=st-1",
		ExpiresAt:       time.Now().Add(10 * time.Minute),
		PollIntervalSec: 2,
	}
}

func zhipuTestReady(provider string) *zcode.FlowReady {
	return &zcode.FlowReady{
		ZCodeJWT: "zcode-jwt",
		User:     zcode.FlowUser{ID: "u-1", Name: "Ada", Email: "ada@example.com"},
		Provider: zcode.FlowProviderToken{AccessToken: provider + "-token", RefreshToken: "rt"},
	}
}

func TestZhipuOAuthStartLinkKeepsPollTokenServerSide(t *testing.T) {
	client := &zhipuOAuthStubClient{initFlow: zhipuTestFlowInit()}
	svc := newZhipuOAuthTestService(t, client)

	session, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderBigModel})
	require.NoError(t, err)
	require.Equal(t, zcode.ProviderBigModel, session.Provider)
	require.Equal(t, zhipuTestFlowInit().AuthorizeURL, session.AuthorizeURL)
	require.Equal(t, int64(2), session.IntervalSeconds)
	require.NotEmpty(t, session.SessionID)
	// The authorization URL, the poll credential and the flow id are all
	// server-held; only the opaque handle crosses to the panel.
	require.NotContains(t, session.SessionID, "flow-1")

	_, err = svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: "openai"})
	require.Error(t, err)
}

func TestZhipuOAuthPollLinkPendingThenReady(t *testing.T) {
	client := &zhipuOAuthStubClient{
		initFlow: zhipuTestFlowInit(),
		polls: []*zcode.FlowPoll{
			{Status: zcode.FlowStatusPending},
			{Status: zcode.FlowStatusReady, Ready: zhipuTestReady(zcode.ProviderBigModel)},
		},
	}
	svc := newZhipuOAuthTestService(t, client)
	session, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderBigModel})
	require.NoError(t, err)

	poll, err := svc.PollLink(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.True(t, poll.Pending)
	require.Nil(t, poll.Ready)

	poll, err = svc.PollLink(context.Background(), session.SessionID)
	require.NoError(t, err)
	require.False(t, poll.Pending)
	require.Equal(t, "bigmodel-token", poll.Ready.AccessToken)
	require.Equal(t, "zcode-jwt", poll.Ready.ZCodeJWT)
	require.Equal(t, "u-1", poll.Ready.User.ID)
	require.Equal(t, "flow-1", client.lastFlowID)
	require.Equal(t, 2, client.pollCalls)
}

func TestZhipuOAuthPollLinkFailedIsTerminal(t *testing.T) {
	client := &zhipuOAuthStubClient{
		initFlow: zhipuTestFlowInit(),
		polls:    []*zcode.FlowPoll{{Status: zcode.FlowStatusFailed}},
	}
	svc := newZhipuOAuthTestService(t, client)
	session, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderZai})
	require.NoError(t, err)
	_, err = svc.PollLink(context.Background(), session.SessionID)
	require.Error(t, err)
	// A failed flow is dropped so the operator restarts authorization instead of
	// polling a dead session.
	_, err = svc.PollLink(context.Background(), session.SessionID)
	require.Error(t, err)
}

func TestZhipuOAuthExchangeLinkAcceptsCallbackURL(t *testing.T) {
	client := &zhipuOAuthStubClient{
		initFlow: zhipuTestFlowInit(),
		exchange: zhipuTestReady(zcode.ProviderZai),
	}
	svc := newZhipuOAuthTestService(t, client)
	session, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderZai})
	require.NoError(t, err)

	poll, err := svc.ExchangeLink(context.Background(), session.SessionID,
		"https://zcode.z.ai/app/oauth/login?code=code-9&state=st-1", nil)
	require.NoError(t, err)
	require.False(t, poll.Pending)
	require.Equal(t, "zai-token", poll.Ready.AccessToken)
	require.Equal(t, [3]string{zcode.ProviderZai, "code-9", "st-1"}, client.lastExchangeArgs)

	// A bare code is accepted too, with the session state as the fallback.
	_, err = svc.ExchangeLink(context.Background(), session.SessionID, "bare-code", nil)
	require.NoError(t, err)
	require.Equal(t, "bare-code", client.lastExchangeArgs[1])
	require.Equal(t, "st-1", client.lastExchangeArgs[2])

	_, err = svc.ExchangeLink(context.Background(), session.SessionID, "   ", nil)
	require.Error(t, err)
	_, err = svc.ExchangeLink(context.Background(), "missing", "code", nil)
	require.Error(t, err)
}

func TestZhipuOAuthSessionIsSingleUse(t *testing.T) {
	client := &zhipuOAuthStubClient{initFlow: zhipuTestFlowInit()}
	svc := newZhipuOAuthTestService(t, client)
	session, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderBigModel})
	require.NoError(t, err)

	require.NoError(t, svc.ConsumeLinkSession(session.SessionID))
	require.Error(t, svc.ConsumeLinkSession(session.SessionID), "one authorization cannot mint two accounts")
}

func TestZhipuOAuthBuildAccountMaterialForEachPlan(t *testing.T) {
	client := &zhipuOAuthStubClient{
		businessToken: "zai-business",
		individual:    &zcode.CodingPlanCredential{APIKey: "ak.sk", OrganizationID: "org-1", ProjectID: "proj-1"},
		team:          &zcode.CodingPlanCredential{APIKey: "tak.tsk", OrganizationID: "org-team", ProjectID: "proj-team"},
	}
	svc := newZhipuOAuthTestService(t, client)

	t.Run("individual coding plan uses the derived key", func(t *testing.T) {
		material, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider:    zcode.ProviderBigModel,
			PlanKind:    ZhipuPlanIndividualCodingPlan,
			AccessToken: "bm-token",
			User:        ZhipuLinkUser{ID: "u-1"},
		})
		require.NoError(t, err)
		require.Equal(t, "ak.sk", material.Credentials["api_key"])
		require.Equal(t, "bm-token", material.Credentials["access_token"])
		require.Equal(t, AccountModeCoding, material.Credentials["account_mode"])
		require.Equal(t, APIProtocolAdaptive, material.Credentials["api_protocol"])
		require.Equal(t, DefaultZhipuCodingBaseURL, material.BaseURL)
		require.Equal(t, map[string]any{
			APIProtocolChatCompletions: DefaultZhipuCodingBaseURL,
			APIProtocolAnthropic:       DefaultZhipuAnthropicBaseURL,
		}, material.Credentials[credentialAPIBaseURLs])
		// BigModel already returns a business token, so no exchange happens.
		require.Zero(t, client.businessCalls)
	})

	t.Run("zai exchanges the oauth token for a business token", func(t *testing.T) {
		client.businessCalls = 0
		material, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider:    zcode.ProviderZai,
			PlanKind:    ZhipuPlanIndividualCodingPlan,
			AccessToken: "zai-oauth",
			User:        ZhipuLinkUser{ID: "u-1"},
		})
		require.NoError(t, err)
		require.Equal(t, 1, client.businessCalls)
		require.Equal(t, "zai-business", material.Credentials["access_token"])
		require.Equal(t, DefaultZaiCodingBaseURL, material.BaseURL)
	})

	t.Run("team coding plan carries the scope headers", func(t *testing.T) {
		material, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider:    zcode.ProviderBigModel,
			PlanKind:    ZhipuPlanTeamCodingPlan,
			TeamOrg:     "org-team",
			TeamProject: "proj-team",
			AccessToken: "bm-token",
		})
		require.NoError(t, err)
		require.Equal(t, "tak.tsk", material.Credentials["api_key"])
		require.Equal(t, [2]string{"org-team", "proj-team"}, client.lastTeamScope)
		require.Equal(t, true, material.Credentials[credKeyHeaderOverrideEnabled])
		overrides, ok := material.Credentials[credKeyHeaderOverrides].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "org-team", overrides["bigmodel-organization"])
		require.Equal(t, "proj-team", overrides["bigmodel-project"])

		// A team plan without its scope is rejected before any key is derived.
		_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanTeamCodingPlan, AccessToken: "bm-token",
		})
		require.Error(t, err)
	})

	t.Run("start plan authenticates with the zcode token", func(t *testing.T) {
		material, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel,
			PlanKind: ZhipuPlanStartPlan,
			ZCodeJWT: "zcode-jwt",
		})
		require.NoError(t, err)
		require.Equal(t, "zcode-jwt", material.Credentials["api_key"])
		require.Equal(t, AnthropicAPIKeyAuthSchemeAuthorizationBearer, material.Credentials[anthropicAPIKeyAuthSchemeExtraKey])
		require.Equal(t, map[string]any{APIProtocolAnthropic: zcodePlanAnthropicBaseURL}, material.Credentials[credentialAPIBaseURLs])
		require.Equal(t, zcodePlanAnthropicBaseURL, material.BaseURL)

		_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanStartPlan,
		})
		require.Error(t, err, "a start plan without its zcode token cannot be linked")
	})

	t.Run("off-peak owns the token, the plan key and the ticket declarations", func(t *testing.T) {
		material, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider:    zcode.ProviderBigModel,
			PlanKind:    ZhipuPlanOffPeak,
			AccessToken: "bm-token",
			ZCodeJWT:    "zcode-jwt",
		})
		require.NoError(t, err)
		require.Equal(t, "zcode-jwt", material.Credentials["api_key"])
		require.Equal(t, "zcode-jwt", material.Credentials["zcode_jwt_token"])
		require.Equal(t, "ak.sk", material.Credentials["off_peak_plan_key"])
		require.Equal(t, AnthropicAPIKeyAuthSchemeAuthorizationBearer, material.Credentials[anthropicAPIKeyAuthSchemeExtraKey])
		require.Equal(t, map[string]any{APIProtocolAnthropic: zcodeOffPeakAnthropicBaseURL}, material.Credentials[credentialAPIBaseURLs])
		require.Equal(t, zcodeOffPeakAnthropicBaseURL, material.BaseURL)
		overrides, ok := material.Credentials[credKeyHeaderOverrides].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "ak.sk", overrides["x-coding-plan-api-key"])
		require.NotContains(t, overrides, "bigmodel-organization", "a personal off-peak plan carries no team scope")

		// The team variant adds the scope headers.
		material, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanOffPeak,
			AccessToken: "bm-token", ZCodeJWT: "zcode-jwt",
			TeamOrg: "org-team", TeamProject: "proj-team",
		})
		require.NoError(t, err)
		overrides, ok = material.Credentials[credKeyHeaderOverrides].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "org-team", overrides["bigmodel-organization"])
		require.Equal(t, "proj-team", overrides["bigmodel-project"])
	})

	// Start-plan and off-peak carry no estate-specific credential of their own,
	// so both estates must produce the same plan gateway mapping.
	t.Run("start plan and off-peak are estate independent", func(t *testing.T) {
		for _, provider := range []string{zcode.ProviderBigModel, zcode.ProviderZai} {
			start, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
				Provider: provider, PlanKind: ZhipuPlanStartPlan, ZCodeJWT: "zcode-jwt",
			})
			require.NoError(t, err, provider)
			require.Equal(t, map[string]any{APIProtocolAnthropic: zcodePlanAnthropicBaseURL}, start.Credentials[credentialAPIBaseURLs])

			idle, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
				Provider: provider, PlanKind: ZhipuPlanOffPeak,
				AccessToken: provider + "-token", ZCodeJWT: "zcode-jwt",
			})
			require.NoError(t, err, provider)
			require.Equal(t, map[string]any{APIProtocolAnthropic: zcodeOffPeakAnthropicBaseURL}, idle.Credentials[credentialAPIBaseURLs])
			require.Equal(t, "ak.sk", idle.Credentials["off_peak_plan_key"])
			require.Equal(t, AnthropicAPIKeyAuthSchemeAuthorizationBearer, idle.Credentials[anthropicAPIKeyAuthSchemeExtraKey])
		}
	})

	t.Run("invalid inputs are rejected", func(t *testing.T) {
		_, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: "openai", PlanKind: ZhipuPlanIndividualCodingPlan, AccessToken: "t",
		})
		require.Error(t, err)
		_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel, PlanKind: "unknown-plan", AccessToken: "t",
		})
		require.Error(t, err)
		_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
			Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanIndividualCodingPlan,
		})
		require.Error(t, err, "the derived key needs the linked access token")
	})
}

// A failed key derivation must not leave a half-linked account behind, and the
// platform rejection must surface as a bad-gateway rather than a silent default.
func TestZhipuOAuthBuildAccountMaterialFailsClosed(t *testing.T) {
	client := &zhipuOAuthStubClient{individualErr: errors.New("business endpoint returned status 500")}
	svc := newZhipuOAuthTestService(t, client)
	_, err := svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
		Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanIndividualCodingPlan, AccessToken: "t",
	})
	require.Error(t, err)

	client = &zhipuOAuthStubClient{businessTokenErr: errors.New("zai business login rejected the oauth token")}
	svc = newZhipuOAuthTestService(t, client)
	_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
		Provider: zcode.ProviderZai, PlanKind: ZhipuPlanIndividualCodingPlan, AccessToken: "t",
	})
	require.Error(t, err)
	require.Zero(t, client.individualCalls, "the key must not be derived from an unexchanged token")
}

func TestZhipuOAuthCapabilitiesReportEveryImplementedPlan(t *testing.T) {
	svc := newZhipuOAuthTestService(t, &zhipuOAuthStubClient{})
	capabilities := svc.Capabilities()
	require.True(t, capabilities.Enabled)
	require.Equal(t, zcode.Providers, capabilities.Providers)
	require.Equal(t, capabilities.PlanKinds, capabilities.SupportedPlanKinds,
		"every declared plan kind is implemented")
	require.Contains(t, capabilities.SupportedPlanKinds, ZhipuPlanOffPeak)
}

// The handshake is a pre-account authorization call, so it must advertise the
// native Zhipu preset rather than an account snapshot or a caller header.
func TestZhipuOAuthStartLinkUsesNativeOutboundIdentity(t *testing.T) {
	t.Setenv(zcode.VersionEnv, "")
	ctx := context.Background()
	resolved := withNativeOAuthOutboundIdentity(ctx, PlatformZhipu)
	identity, ok := outboundidentity.FromContext(resolved)
	require.True(t, ok)
	require.Equal(t, "zcode", identity.Preset)
	require.Equal(t, "ZCode/"+zcode.DefaultVersion, identity.UserAgent)
	require.Equal(t, zcode.DefaultVersion, identity.Version)
}

func TestZhipuOAuthStartLinkPropagatesPlatformFailure(t *testing.T) {
	client := &zhipuOAuthStubClient{initErr: errors.New("oauth endpoint returned status 502")}
	svc := newZhipuOAuthTestService(t, client)
	_, err := svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderBigModel})
	require.Error(t, err)

	// An unconfigured deployment fails closed instead of attempting a call.
	svc = newZhipuOAuthTestService(t, &zhipuOAuthStubClient{})
	svc.client = nil
	_, err = svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderBigModel})
	require.Error(t, err)
	_, err = svc.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
		Provider: zcode.ProviderBigModel, PlanKind: ZhipuPlanIndividualCodingPlan, AccessToken: "t",
	})
	require.Error(t, err)
}
