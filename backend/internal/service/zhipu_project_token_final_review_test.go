//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type projectTokenReviewWire func(*http.Request) (*http.Response, error)

func (f projectTokenReviewWire) Do(req *http.Request) (*http.Response, error) { return f(req) }

type projectTokenReviewIssuer struct {
	ZhipuOAuthClient
	client *zcode.CredentialClient
}

func (c projectTokenReviewIssuer) IssueProjectAccessToken(ctx context.Context, provider, login, organization, project, keyID, _ string) (*zcode.ProjectAccessToken, error) {
	return c.client.IssueProjectAccessToken(ctx, provider, login, organization, project, keyID)
}

func TestProjectTokenRelinkSettlementKeepsOriginalIdentityAtIssuance(t *testing.T) {
	account := zhipuProjectOffPeakTestAccount()
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "3.14.3"}
	now := time.Now()
	type physicalIssuance struct{ authorization, userAgent, version, bodyVersion string }
	var issuances []physicalIssuance
	issuer := projectTokenReviewIssuer{client: zcode.NewCredentialClient(projectTokenReviewWire(func(req *http.Request) (*http.Response, error) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		issuances = append(issuances, physicalIssuance{req.Header.Get("Authorization"), req.UserAgent(), req.Header.Get("X-ZCode-App-Version"), body["clientVersion"]})
		lifetime := 4 * time.Minute
		if len(issuances) > 1 {
			lifetime = time.Hour
		}
		payload := fmt.Sprintf(`{"code":0,"data":{"accessToken":"physical-pat-%d","tokenType":"Bearer","expiresIn":%d,"expiresAt":%d}}`, len(issuances), int(lifetime/time.Second), now.Add(lifetime).Unix())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, nil
	}), "https://bigmodel.cn", "https://api.z.ai")}
	tokens := NewZhipuProjectTokenManager(issuer, nil)
	tokens.now = func() time.Time { return now }
	tokens.jitter = func() time.Duration { return 0 }
	tickets := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "original-ticket", State: zcode.TicketReady}, {TicketID: "replacement-ticket", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, tickets)
	manager.projectTokens = tokens
	manager.now = func() time.Time { return now }
	manager.WithSettleIdle(time.Hour)
	ticket, err := manager.TicketForRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "original-ticket", ticket.TicketID)
	ticket.Release()

	replacement, err := snapshotCNControlAccount(account)
	require.NoError(t, err)
	replacement.Credentials["access_token"] = "replacement-login"
	replacement.Credentials["zcode_jwt_token"] = "replacement-jwt"
	replacement.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "0.16.9"}
	now = now.Add(5 * time.Minute)
	replacementCtx := withRequestCredentialOwner(context.Background(), replacement, replacement.GetOpenAIProtocolAPIKey())
	ticket, err = manager.TicketForRequest(replacementCtx, replacement)
	require.NoError(t, err)
	require.Equal(t, "replacement-ticket", ticket.TicketID)
	ticket.Release()
	require.Equal(t, []string{"original-ticket"}, tickets.settled)
	require.Equal(t, account.GetCredential("zcode_jwt_token"), tickets.settleAuths[0].JWT)
	oldOwnerIssuances := 0
	newOwnerIssuances := 0
	for _, issuance := range issuances {
		switch issuance.authorization {
		case "Bearer owner-login":
			oldOwnerIssuances++
			require.Equal(t, "ZCode/3.14.3", issuance.userAgent, "replacement request scope cannot overwrite the old ticket's issuer identity")
			require.Equal(t, "3.14.3", issuance.version)
			require.Equal(t, "3.14.3", issuance.bodyVersion)
		case "Bearer replacement-login":
			newOwnerIssuances++
			require.Equal(t, "ZCode/0.16.9", issuance.userAgent)
			require.Equal(t, "0.16.9", issuance.version)
			require.Equal(t, "0.16.9", issuance.bodyVersion)
		default:
			t.Fatalf("unexpected credential owner on issuance")
		}
	}
	require.Equal(t, 2, oldOwnerIssuances, "model and ticket share one PAT cache; expired old-ticket settlement renews only the original owner")
	require.Equal(t, 1, newOwnerIssuances, "replacement model and ticket share one PAT issued under the new owner")
}

func TestProjectTokenHardRefreshFailureCannotRevivePreviousCredential(t *testing.T) {
	for _, failure := range []struct {
		name          string
		token         *zcode.ProjectAccessToken
		err           error
		cancelRefresh bool
	}{
		{name: "unauthorized", err: &zcode.ProjectTokenHTTPError{Status: http.StatusUnauthorized}},
		{name: "forbidden", err: &zcode.ProjectTokenHTTPError{Status: http.StatusForbidden}},
		{name: "disabled", err: errors.New("project token disabled")},
		{name: "invalid issuance", err: errors.New("invalid project token response")},
		{name: "nil response"},
		{name: "empty token", token: &zcode.ProjectAccessToken{ExpiresAt: time.Now().Add(time.Hour)}},
		{name: "expired token", token: &zcode.ProjectAccessToken{Token: "expired-pat", ExpiresAt: time.Now().Add(-time.Hour)}},
		// ZCode packages/shared/src/project-access-token.ts resolve().catch
		// deletes the cache after an in-flight issuance is cancelled. This does
		// not concern a request cancelled before it starts a refresh.
		{name: "cancelled issuance", cancelRefresh: true},
	} {
		t.Run(failure.name, func(t *testing.T) {
			now := time.Now()
			calls := 0
			refreshCtx, cancelRefresh := context.WithCancel(context.Background())
			defer cancelRefresh()
			issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
				calls++
				switch calls {
				case 1:
					return &zcode.ProjectAccessToken{Token: "previous-pat", ExpiresAt: now.Add(4 * time.Minute)}, nil
				case 2:
					if failure.cancelRefresh {
						cancelRefresh()
						return nil, context.Canceled
					}
					return failure.token, failure.err
				default:
					return nil, &zcode.ProjectTokenHTTPError{Status: http.StatusServiceUnavailable}
				}
			}}
			tokens := NewZhipuProjectTokenManager(issuer, nil)
			tokens.now = func() time.Time { return now }
			tokens.jitter = func() time.Duration { return 0 }
			account := projectTokenAccount()
			token, err := tokens.Token(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, "previous-pat", token)
			now = now.Add(121 * time.Second)
			token, err = tokens.Token(refreshCtx, account)
			require.Error(t, err)
			require.Empty(t, token)
			token, err = tokens.Token(context.Background(), account)
			require.Error(t, err, "a later transient failure cannot resurrect a token invalidated by the previous hard failure")
			require.Empty(t, token)
			require.Equal(t, 3, calls)
		})
	}
}
