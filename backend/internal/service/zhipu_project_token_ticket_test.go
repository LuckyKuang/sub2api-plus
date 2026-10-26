//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type projectTicketClient struct {
	ZhipuOffPeakClient
	take   func(zcode.OffPeakAuth) (*zcode.OffPeakTicket, error)
	status func(zcode.OffPeakAuth) ([]zcode.OffPeakTicket, time.Duration, error)
	settle func(zcode.OffPeakAuth) error
}

func (c projectTicketClient) TakeTicket(_ context.Context, _ string, auth zcode.OffPeakAuth, _ string) (*zcode.OffPeakTicket, error) {
	return c.take(auth)
}
func (c projectTicketClient) TicketStatus(_ context.Context, _ []string, auth zcode.OffPeakAuth, _ string) ([]zcode.OffPeakTicket, time.Duration, error) {
	return c.status(auth)
}
func (c projectTicketClient) SettleTicket(_ context.Context, _ string, auth zcode.OffPeakAuth, _ string) error {
	return c.settle(auth)
}

func newProjectTicketManager(t *testing.T, client projectTicketClient) (*ZhipuOffPeakTicketManager, *ZhipuProjectTokenManager) {
	t.Helper()
	var issued atomic.Int32
	issuer := &projectIssuerStub{issue: func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: fmt.Sprintf("%s-pat-%d", login, issued.Add(1)), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	tokens := NewZhipuProjectTokenManager(issuer, nil)
	manager := NewZhipuOffPeakTicketManager(client)
	manager.projectTokens = tokens
	t.Cleanup(manager.Stop)
	return manager, tokens
}

func TestProjectTokenTicketTakeRejectionRefreshesRejectedOwnerToken(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a := zhipuProjectOffPeakTestAccount()
			var used []string
			manager, _ := newProjectTicketManager(t, projectTicketClient{take: func(auth zcode.OffPeakAuth) (*zcode.OffPeakTicket, error) {
				used = append(used, auth.PlanKey)
				if len(used) == 1 {
					return nil, &zcode.OffPeakError{HTTPStatus: status}
				}
				return &zcode.OffPeakTicket{TicketID: "ready", State: zcode.TicketReady}, nil
			}})
			first, err := manager.TicketForRequest(context.Background(), a)
			if status == http.StatusUnauthorized {
				require.NoError(t, err)
				require.Equal(t, "ready", first.TicketID)
				first.Release()
				require.Len(t, used, 2, "one explicit 401 retries exactly once under the same owner")
				require.NotEqual(t, used[0], used[1])
				return
			}
			require.Error(t, err)
			require.Len(t, used, 1, "403,quota and server rejections are returned without immediate replay")
			ticket, err := manager.TicketForRequest(context.Background(), a)
			require.NoError(t, err)
			require.Equal(t, "ready", ticket.TicketID)
			ticket.Release()
			require.Len(t, used, 2)
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				require.NotEqual(t, used[0], used[1], "ticket authentication rejection must never reuse the explicitly rejected PAT")
			} else {
				require.Equal(t, used[0], used[1], "quota and temporary server errors do not reject a valid PAT")
			}
		})
	}
}

func TestProjectTokenTicketStatusRejectionRefreshesBeforeReuse(t *testing.T) {
	a := zhipuProjectOffPeakTestAccount()
	var rejected string
	statusCalls := 0
	manager, _ := newProjectTicketManager(t, projectTicketClient{
		take: func(zcode.OffPeakAuth) (*zcode.OffPeakTicket, error) {
			return &zcode.OffPeakTicket{TicketID: "retained", State: zcode.TicketReady}, nil
		},
		status: func(auth zcode.OffPeakAuth) ([]zcode.OffPeakTicket, time.Duration, error) {
			statusCalls++
			if statusCalls == 1 {
				rejected = auth.PlanKey
				return nil, 0, &zcode.OffPeakError{HTTPStatus: http.StatusUnauthorized}
			}
			require.NotEqual(t, rejected, auth.PlanKey, "ready-ticket revalidation must refresh a rejected PAT")
			return []zcode.OffPeakTicket{{TicketID: "retained", State: zcode.TicketReady}}, 0, nil
		},
	})
	lease, err := manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	lease.Release()
	ticket, err := manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, 2, statusCalls, "one status401 is retried once before reusing the ticket")
	require.Equal(t, "retained", ticket.TicketID)
	ticket.Release()
}

func TestProjectTokenTicketSettleRejectionRetainsOriginalOwner(t *testing.T) {
	a := zhipuProjectOffPeakTestAccount()
	var settleAuths []zcode.OffPeakAuth
	manager, _ := newProjectTicketManager(t, projectTicketClient{
		take: func(zcode.OffPeakAuth) (*zcode.OffPeakTicket, error) {
			return &zcode.OffPeakTicket{TicketID: "retained", State: zcode.TicketReady}, nil
		},
		settle: func(auth zcode.OffPeakAuth) error {
			settleAuths = append(settleAuths, auth)
			if len(settleAuths) == 1 {
				return &zcode.OffPeakError{HTTPStatus: http.StatusUnauthorized}
			}
			return nil
		},
	})
	lease, err := manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	lease.Release()
	oldJWT := a.GetCredential("zcode_jwt_token")
	a.Credentials["access_token"] = "replacement-login"
	a.Credentials["zcode_jwt_token"] = "replacement-jwt"
	manager.now = func() time.Time { return time.Now().Add(DefaultOffPeakSettleIdle + time.Minute) }
	manager.sweepOnce()
	manager.sweepOnce()
	require.Len(t, settleAuths, 2)
	require.NotEqual(t, settleAuths[0].PlanKey, settleAuths[1].PlanKey, "retry settlement must rotate the rejected PAT")
	require.Equal(t, oldJWT, settleAuths[1].JWT)
	require.Contains(t, settleAuths[1].PlanKey, "owner-login-pat-", "relink cannot transfer old-ticket settlement to the replacement login")
}

func TestProjectTokenCapturesProxyObjectBeforeIssuance(t *testing.T) {
	a := projectTokenAccount()
	proxyID := int64(8)
	a.ProxyID = &proxyID
	a.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "old.proxy.test", Port: 8080}
	repo := &refreshAPIAccountRepo{account: a}
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		a.Proxy.Host = "replacement.proxy.test"
		return &zcode.ProjectAccessToken{Token: "late-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	_, err := NewZhipuProjectTokenManager(issuer, repo).Token(context.Background(), a)
	require.EqualValues(t, 1, issuer.calls.Load(), "the fixture must reach issuance before the in-place proxy mutation")
	require.Error(t, err, "in-place proxy changes cannot mutate the captured credential-owner snapshot")
}

func TestProjectTokenLateTicketRejectionKeepsRotatedCredential(t *testing.T) {
	a := zhipuProjectOffPeakTestAccount()
	now := time.Now()
	var issued atomic.Int32
	issuer := &projectIssuerStub{issue: func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: fmt.Sprintf("%s-pat-%d", login, issued.Add(1)), ExpiresAt: now.Add(time.Hour)}, nil
	}}
	tokens := NewZhipuProjectTokenManager(issuer, nil)
	tokens.now = func() time.Time { return now }
	tokens.jitter = func() time.Duration { return 0 }
	var manager *ZhipuOffPeakTicketManager
	var rotated string
	client := projectTicketClient{
		take: func(zcode.OffPeakAuth) (*zcode.OffPeakTicket, error) {
			return &zcode.OffPeakTicket{TicketID: "retained", State: zcode.TicketReady}, nil
		},
		status: func(used zcode.OffPeakAuth) ([]zcode.OffPeakTicket, time.Duration, error) {
			now = now.Add(59 * time.Minute)
			var err error
			rotated, err = tokens.tokenForTicket(context.Background(), manager.holderFor(a.ID).account)
			require.NoError(t, err)
			require.NotEqual(t, used.PlanKey, rotated)
			return nil, 0, &zcode.OffPeakError{HTTPStatus: http.StatusUnauthorized}
		},
	}
	manager = NewZhipuOffPeakTicketManager(client)
	manager.projectTokens = tokens
	t.Cleanup(manager.Stop)
	lease, err := manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	lease.Release()
	_, err = manager.TicketForRequest(context.Background(), a)
	require.Error(t, err)
	count := issuer.calls.Load()
	current, err := tokens.tokenForTicket(context.Background(), manager.holderFor(a.ID).account)
	require.NoError(t, err)
	require.Equal(t, rotated, current, "a late rejection applies only to the PAT sent on that request")
	require.Equal(t, count, issuer.calls.Load(), "a late rejection cannot evict the newer valid credential")
}

func TestProjectTokenResponseRejectsCapturedRequestOwner(t *testing.T) {
	a := projectTokenAccount()
	previous := projectTokenAccount()
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: "sent-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	manager := NewZhipuProjectTokenManager(issuer, nil)
	req, err := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", nil)
	require.NoError(t, err)
	require.NoError(t, manager.Prepare(req, a))
	a.Credentials["access_token"] = "replacement-login"
	manager.Observe(req, a, &http.Response{StatusCode: http.StatusUnauthorized})
	issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return nil, errors.New("refresh unavailable")
	}
	_, err = manager.Token(context.Background(), previous)
	require.Error(t, err, "rejection must target the credential owner captured when the request was authenticated")
}

func TestProjectTokenTicketScopeHeadersRequireExplicitTeamPlan(t *testing.T) {
	for _, kind := range []string{"personal", "team"} {
		t.Run(kind, func(t *testing.T) {
			a := zhipuProjectOffPeakTestAccount()
			a.Credentials["zhipu_project_kind"] = kind
			var sent zcode.OffPeakAuth
			manager, _ := newProjectTicketManager(t, projectTicketClient{
				take: func(auth zcode.OffPeakAuth) (*zcode.OffPeakTicket, error) {
					sent = auth
					return &zcode.OffPeakTicket{TicketID: "ready", State: zcode.TicketReady}, nil
				},
			})
			_, err := manager.TicketForRequest(context.Background(), a)
			require.NoError(t, err)
			if kind == "team" {
				require.Equal(t, "org", sent.TeamOrg)
				require.Equal(t, "project", sent.TeamProj)
			} else {
				require.Empty(t, sent.TeamOrg, "personal project metadata is not a team declaration")
				require.Empty(t, sent.TeamProj)
			}
		})
	}
}
