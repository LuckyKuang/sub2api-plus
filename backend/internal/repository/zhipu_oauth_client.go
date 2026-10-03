package repository

import (
	"context"
	"net/http"
	"time"

	sharedhttp "github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// ZhipuOAuthClient is the egress boundary for the ZCode platform account-link
// handshake and the GLM business credential derivation.
//
// The concrete client only knows how to build a proxy-aware HTTP client; every
// protocol decision lives in internal/pkg/zcode so the service layer stays
// transport-agnostic.
type ZhipuOAuthClient struct {
	bigModel string
	zai      string
	timeout  time.Duration
}

// NewZhipuOAuthServiceClient returns the handshake and credential-derivation
// port consumed by the service layer.
func NewZhipuOAuthServiceClient() service.ZhipuOAuthClient {
	return NewZhipuOAuthClient()
}

// NewZhipuOffPeakTicketClient returns the off-peak ticket port. It is a separate
// provider so the ticket manager and the account-link service never share
// transport state.
func NewZhipuOffPeakTicketClient() service.ZhipuOffPeakClient {
	return NewZhipuOAuthClient()
}

// NewZhipuOAuthClient builds the client from the environment-resolved origins.
func NewZhipuOAuthClient() *ZhipuOAuthClient {
	bigModelOrigin, zaiOrigin := zcode.ResolveBusinessOrigins()
	return &ZhipuOAuthClient{
		bigModel: bigModelOrigin,
		zai:      zaiOrigin,
		timeout:  zcode.CredentialRequestTimeout,
	}
}

// Origins reports the effective business origins for diagnostics.
func (c *ZhipuOAuthClient) Origins() (bigModel, zai string) {
	if c == nil {
		return zcode.DefaultBigModelBusinessOrigin, zcode.DefaultZaiBusinessOrigin
	}
	return c.bigModel, c.zai
}

// zhipuOAuthHTTPClient builds the proxy-aware client every handshake and
// business call uses. Redirects are not followed: a handshake call must observe
// the exact endpoint response instead of being bounced elsewhere.
func zhipuOAuthHTTPClient(proxyURL string) (*http.Client, error) {
	client, err := sharedhttp.GetClient(sharedhttp.Options{
		ProxyURL: proxyURL,
		Timeout:  zcode.CredentialRequestTimeout,
	})
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone, nil
}

// StartFlow opens a handshake flow and returns the authorization URL.
func (c *ZhipuOAuthClient) StartFlow(ctx context.Context, provider, pollToken, proxyURL string) (*zcode.FlowInit, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	handshake := zcode.NewHandshakeClient(client, zcode.ResolveHandshakeBaseURL())
	return handshake.Init(ctx, provider, pollToken)
}

// PollFlow reports the current flow state.
func (c *ZhipuOAuthClient) PollFlow(ctx context.Context, provider, flowID, pollToken, proxyURL string) (*zcode.FlowPoll, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	handshake := zcode.NewHandshakeClient(client, zcode.ResolveHandshakeBaseURL())
	return handshake.Poll(ctx, provider, flowID, pollToken)
}

// ExchangeCode redeems a pasted authorization code.
func (c *ZhipuOAuthClient) ExchangeCode(ctx context.Context, provider, code, redirectURI, state, proxyURL string) (*zcode.FlowReady, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	handshake := zcode.NewHandshakeClient(client, zcode.ResolveHandshakeBaseURL())
	return handshake.ExchangeCode(ctx, provider, code, redirectURI, state)
}

// ExchangeZaiBusinessToken converts a Z.ai OAuth token into a business token.
func (c *ZhipuOAuthClient) ExchangeZaiBusinessToken(ctx context.Context, oauthAccessToken, proxyURL string) (string, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return "", err
	}
	return zcode.NewCredentialClient(client, c.bigModel, c.zai).ExchangeZaiBusinessToken(ctx, oauthAccessToken)
}

// ResolveIndividualCodingPlanKey derives the personal coding-plan API key.
func (c *ZhipuOAuthClient) ResolveIndividualCodingPlanKey(ctx context.Context, provider, accessToken, proxyURL string) (*zcode.CodingPlanCredential, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return zcode.NewCredentialClient(client, c.bigModel, c.zai).ResolveIndividualCodingPlanKey(ctx, provider, accessToken)
}

// ResolveTeamPlanKey derives a team-plan project key.
func (c *ZhipuOAuthClient) ResolveTeamPlanKey(ctx context.Context, provider, accessToken, organizationID, projectID, proxyURL string) (*zcode.CodingPlanCredential, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return zcode.NewCredentialClient(client, c.bigModel, c.zai).ResolveTeamPlanKey(ctx, provider, accessToken, organizationID, projectID)
}

// Off-peak ticket protocol. The ticket client shares the same proxy-aware
// transport and origin guard as the handshake, because a ticket is issued by the
// same platform deployment.
func (c *ZhipuOAuthClient) offPeakClient(proxyURL string) (*zcode.OffPeakClient, error) {
	client, err := zhipuOAuthHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return zcode.NewOffPeakClient(client, zcode.ResolveOffPeakBaseURL()), nil
}

// Availability reads the off-peak take-number quota snapshot.
func (c *ZhipuOAuthClient) Availability(ctx context.Context, auth zcode.OffPeakAuth, proxyURL string) (*zcode.OffPeakAvailability, error) {
	client, err := c.offPeakClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return client.Availability(ctx, auth)
}

// TakeTicket requests an off-peak ticket for a task.
func (c *ZhipuOAuthClient) TakeTicket(ctx context.Context, taskID string, auth zcode.OffPeakAuth, proxyURL string) (*zcode.OffPeakTicket, error) {
	client, err := c.offPeakClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return client.TakeTicket(ctx, taskID, auth)
}

// TicketStatus reads the state of outstanding off-peak tickets.
func (c *ZhipuOAuthClient) TicketStatus(ctx context.Context, ticketIDs []string, auth zcode.OffPeakAuth, proxyURL string) ([]zcode.OffPeakTicket, time.Duration, error) {
	client, err := c.offPeakClient(proxyURL)
	if err != nil {
		return nil, 0, err
	}
	return client.TicketStatus(ctx, ticketIDs, auth)
}

// SettleTicket reports a finished off-peak ticket.
func (c *ZhipuOAuthClient) SettleTicket(ctx context.Context, ticketID string, auth zcode.OffPeakAuth, proxyURL string) error {
	client, err := c.offPeakClient(proxyURL)
	if err != nil {
		return err
	}
	return client.SettleTicket(ctx, ticketID, auth)
}
