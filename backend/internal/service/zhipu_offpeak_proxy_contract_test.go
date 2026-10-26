//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

// A stored binding owns every ticket and PAT operation. Missing hydration,
// another record, or malformed configuration cannot authorize a direct send.
func TestZhipuOffPeakRejectsBrokenBoundProxyBeforeCredentialOrTicketSend(t *testing.T) {
	for _, operation := range []string{"ticket_for_request", "acquire"} {
		for _, route := range []string{"missing", "mismatched", "malformed", "empty_host", "invalid_port"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				account := zhipuProjectOffPeakTestAccount()
				proxyID := int64(314)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "bound.invalid", Port: 8080, Username: "private-proxy-user", Password: "private-proxy-password"}
				switch route {
				case "missing":
					account.Proxy = nil
				case "mismatched":
					account.Proxy.ID++
				case "malformed":
					account.Proxy.Protocol = "file"
				case "empty_host":
					account.Proxy.Host = ""
				case "invalid_port":
					account.Proxy.Port = 0
				}
				tickets := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "must-not-be-issued", State: zcode.TicketReady}}}
				issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
					return &zcode.ProjectAccessToken{Token: "must-not-be-issued-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
				}}
				manager := NewZhipuOffPeakTicketManager(tickets)
				manager.Stop()
				manager.projectTokens = NewZhipuProjectTokenManager(issuer, nil)
				var err error
				if operation == "ticket_for_request" {
					_, err = manager.TicketForRequest(context.Background(), account)
				} else {
					_, err = manager.Acquire(context.Background(), account, zhipuOffPeakTestAuth(), "")
				}
				require.Error(t, err)
				require.NotContains(t, err.Error(), "private-proxy-user")
				require.NotContains(t, err.Error(), "private-proxy-password")
				require.Zero(t, issuer.calls.Load(), "an invalid bound route must fail before PAT issuance")
				require.Zero(t, tickets.takeCalls, "an invalid bound route cannot reach ticket take")
				require.Zero(t, tickets.statusCalls)
				require.Empty(t, tickets.settleAttempts)
			})
		}
	}
}

func TestZhipuOffPeakAcquireRejectsReplacementProxyArgument(t *testing.T) {
	account := zhipuOffPeakTestAccount()
	proxyID := int64(315)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "owner.invalid", Port: 8080}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "wrong-route", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)
	manager.Stop()
	_, err := manager.Acquire(context.Background(), account, zhipuOffPeakTestAuth(), "http://replacement.invalid:8080")
	require.Error(t, err, "a separate caller argument cannot replace the captured credential owner's bound route")
	require.Zero(t, client.takeCalls)
}
