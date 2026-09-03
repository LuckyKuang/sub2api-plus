//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
)

// TestClassifyUpstreamTransportError pins which transport-level upstream failures
// are "persistent" (retrying the same proxy/account is pointless — evict + alert)
// versus "transient" (a blip — fail over to a healthy account but do not evict).
//
// The motivating incident: a SOCKS5 proxy whose credentials expired returned
// `username/password authentication failed`, yet the account kept being scheduled.
func TestClassifyUpstreamTransportError(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		persistent     bool
		classification string
	}{
		// Durable — config/credential/routing problems. Retrying same proxy won't help.
		{"socks5 proxy credential rejected", errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 85.255.176.68:12324->chatgpt.com:443: username/password authentication failed`), true, "proxy_authentication_failed"},
		{"proxy connection refused", errors.New(`proxyconnect tcp: dial tcp 1.2.3.4:1080: connect: connection refused`), true, "connection_refused"},
		{"no route to host", errors.New(`dial tcp 1.2.3.4:443: connect: no route to host`), true, "network_unreachable"},
		{"dns resolution failure", errors.New(`dial tcp: lookup proxy.example.com: no such host`), true, "dns_not_found"},
		{"network unreachable", errors.New(`dial tcp 1.2.3.4:443: connect: network is unreachable`), true, "network_unreachable"},

		// Transient — a temporary blip. Fail over, but do NOT evict the account.
		{"client timeout", errors.New(`Post "https://chatgpt.com/...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`), false, "transport_error"},
		{"i/o timeout", errors.New(`dial tcp 1.2.3.4:443: i/o timeout`), false, "transport_error"},
		{"connection reset by peer", errors.New(`read tcp 10.0.0.1:5->2.2.2.2:443: read: connection reset by peer`), false, "transport_error"},
		{"unexpected eof", errors.New(`unexpected EOF`), false, "transport_error"},
		{"broken pipe", errors.New(`write tcp 10.0.0.1:5->2.2.2.2:443: write: broken pipe`), false, "transport_error"},

		{"nil error", nil, false, ""},

		// ── Typed-error cases ──────────────────────────────────────────────
		// ECONNREFUSED wrapped in the canonical net.OpError shape Go produces.
		{
			"ECONNREFUSED via net.OpError",
			&net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			},
			true,
			"connection_refused",
		},
		// Bare syscall error (errors.Is traverses the chain).
		{"ECONNREFUSED bare", syscall.ECONNREFUSED, true, "connection_refused"},
		{"EHOSTUNREACH bare", syscall.EHOSTUNREACH, true, "network_unreachable"},
		{"ENETUNREACH bare", syscall.ENETUNREACH, true, "network_unreachable"},

		// *net.DNSError with IsNotFound — permanent DNS lookup failure.
		{
			"DNS not found (IsNotFound=true)",
			&net.DNSError{Err: "no such host", Name: "proxy.example.com", IsNotFound: true},
			true,
			"dns_not_found",
		},
		// *net.DNSError with IsNotFound=false — transient DNS timeout (not persistent).
		{
			"DNS timeout (IsNotFound=false)",
			&net.DNSError{Err: "i/o timeout", Name: "proxy.example.com", IsTimeout: true},
			false,
			"transport_error",
		},

		// context.Canceled — client gone; NOT classified as persistent.
		{"context.Canceled", context.Canceled, false, "transport_error"},
		// context.DeadlineExceeded — slow upstream; NOT persistent.
		{"context.DeadlineExceeded", context.DeadlineExceeded, false, "transport_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOpenAITransportError(tc.err)
			if got.Persistent != tc.persistent {
				t.Fatalf("classifyOpenAITransportError(%q).Persistent = %v, want %v", errString(tc.err), got.Persistent, tc.persistent)
			}
			if got.Classification != tc.classification {
				t.Fatalf("classifyOpenAITransportError(%q).Classification = %q, want %q", errString(tc.err), got.Classification, tc.classification)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
