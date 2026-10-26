//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountStrictProxyURLBindingContract(t *testing.T) {
	id := int64(71)
	for _, tc := range []struct {
		name, protocol, host, username, password, route string
	}{
		{"http", "http", "owner.example", "", "", "http://owner.example:8080"},
		{"https", "https", "owner.example", "", "", "https://owner.example:8080"},
		{"socks-remote-dns", "socks5", "owner.example", "", "", "socks5h://owner.example:8080"},
		{"explicit-remote-dns", "socks5h", "owner.example", "", "", "socks5h://owner.example:8080"},
		{"ipv6", "http", "::1", "", "", "http://[::1]:8080"},
		{"escaped-auth", "http", "owner.example", "owner@name", "private:password", "http://owner%40name:private%3Apassword@owner.example:8080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: tc.protocol, Host: tc.host, Port: 8080, Username: tc.username, Password: tc.password}}
			route, err := account.StrictProxyURL()
			require.NoError(t, err)
			require.Equal(t, tc.route, route)
		})
	}
	t.Run("saved-unbinding-authorizes-direct", func(t *testing.T) {
		account := &Account{Proxy: &Proxy{ID: id, Protocol: "file", Host: "stale.example"}}
		route, err := account.StrictProxyURL()
		require.NoError(t, err)
		require.Empty(t, route, "a residual association cannot undo an explicit saved unbinding")
	})
	t.Run("expired-disabled-snapshot-does-not-select-another-route", func(t *testing.T) {
		expired := time.Now().Add(-time.Hour)
		account := &Account{ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080, Status: "inactive", ExpiresAt: &expired, FallbackMode: FallbackModeDirect}}
		route, err := account.StrictProxyURL()
		require.NoError(t, err)
		require.Equal(t, "http://owner.example:8080", route, "expiry fallback must first commit a different binding in its owning workflow")
	})
}

func TestAccountStrictProxyURLRejectsInvalidBindingWithoutSecrets(t *testing.T) {
	for _, failure := range []string{"nil-account", "invalid-id", "missing-row", "different-row", "missing-host", "host-with-url", "host-with-path", "host-with-space", "host-with-port", "zero-port", "out-of-range-port", "unsupported-protocol", "username-only", "password-only"} {
		t.Run(failure, func(t *testing.T) {
			id := int64(71)
			account := &Account{ProxyID: &id, Proxy: &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080, Username: "private-owner", Password: "private-password"}}
			switch failure {
			case "nil-account":
				account = nil
			case "invalid-id":
				id = 0
			case "missing-row":
				account.Proxy = nil
			case "different-row":
				account.Proxy.ID++
			case "missing-host":
				account.Proxy.Host = ""
			case "host-with-url":
				account.Proxy.Host = "http://owner.example"
			case "host-with-path":
				account.Proxy.Host = "owner.example/path"
			case "host-with-space":
				account.Proxy.Host = " owner.example"
			case "host-with-port":
				account.Proxy.Host = "owner.example:8080"
			case "zero-port":
				account.Proxy.Port = 0
			case "out-of-range-port":
				account.Proxy.Port = 65536
			case "unsupported-protocol":
				account.Proxy.Protocol = "file"
			case "username-only":
				account.Proxy.Password = ""
			case "password-only":
				account.Proxy.Username = ""
			}
			route, err := account.StrictProxyURL()
			require.Error(t, err)
			require.Empty(t, route)
			require.NotContains(t, err.Error(), "private-owner")
			require.NotContains(t, err.Error(), "private-password")
		})
	}
}
