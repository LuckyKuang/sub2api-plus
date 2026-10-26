package repository

import (
	"errors"
	"fmt"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// Lock and recheck the captured route as well as the account's binding. A plain
// MVCC EXISTS can still see the previous endpoint while a proxy editor holds a
// write lock. FOR SHARE waits for that editor, rechecks its committed tuple,
// and keeps it stable until the account/outbox statement commits. Proxy update
// workflows also acquire the proxy before invalidating dependent accounts.
func cnProxyOwnerPredicate(expected *service.Account, args []any) (string, []any, error) {
	if expected == nil || (expected.ProxyID == nil && expected.Proxy != nil) {
		return "", nil, errors.New("control proxy snapshot does not match its binding")
	}
	if _, err := expected.StrictProxyURL(); err != nil {
		return "", nil, err
	}
	if expected.ProxyID == nil {
		return "", args, nil
	}
	p := expected.Proxy
	first := len(args) + 1
	args = append(args, p.Protocol, p.Host, p.Port, p.Username, p.Password, p.Status, p.ExpiresAt)
	return fmt.Sprintf(`
		AND EXISTS (
			SELECT 1 FROM proxies AS p
			WHERE p.id = a.proxy_id AND p.deleted_at IS NULL
				AND p.protocol = $%d AND p.host = $%d AND p.port = $%d
				AND COALESCE(p.username, '') = $%d AND COALESCE(p.password, '') = $%d
				AND p.status = $%d AND p.expires_at IS NOT DISTINCT FROM $%d
			FOR SHARE
		)
	`, first, first+1, first+2, first+3, first+4, first+5, first+6), args, nil
}
