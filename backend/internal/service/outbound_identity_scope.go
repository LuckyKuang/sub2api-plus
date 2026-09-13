package service

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"
)

type outboundIdentityScopeKey struct{}

// A scope belongs to one accepted request or WS connection. The existing
// resolvers still select identities; the scope only retains their first result
// for each credential owner across retries and nested credential operations.
type outboundIdentityScope struct {
	codex   sync.Map
	presets sync.Map
}

type outboundIdentityOwner struct {
	id int64
	// Unsaved accounts have no stable ID. Distinguish them from each other and
	// from a pre-account OAuth exchange, whose owner is nil.
	unsaved *Account
}

func outboundIdentityOwnerKey(account *Account) outboundIdentityOwner {
	if account != nil && account.ID > 0 {
		return outboundIdentityOwner{id: account.ID}
	}
	return outboundIdentityOwner{unsaved: account}
}

func outboundIdentityScopeFromContext(ctx context.Context) *outboundIdentityScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(outboundIdentityScopeKey{}).(*outboundIdentityScope)
	return scope
}

// WithOutboundIdentityScope starts a forwarding scope after ingress audit.
// Retaining the scope on Gin also covers handler-level retries that re-enter a
// service with its original context. No account selection, configuration read
// or I/O occurs here.
func WithOutboundIdentityScope(ctx context.Context, c *gin.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil && c != nil {
		if stored, ok := c.Get("trusted_outbound_identity_scope"); ok {
			scope, _ = stored.(*outboundIdentityScope)
		}
	}
	if scope == nil {
		scope = &outboundIdentityScope{}
	}
	if c != nil {
		c.Set("trusted_outbound_identity_scope", scope)
	}
	if outboundIdentityScopeFromContext(ctx) == scope {
		return ctx
	}
	return context.WithValue(ctx, outboundIdentityScopeKey{}, scope)
}

// Detached model refreshes have their own deadline while retaining the
// identity that produced their headers, URL and cache key.
func carryOutboundIdentityScope(ctx, source context.Context) context.Context {
	if scope := outboundIdentityScopeFromContext(source); scope != nil {
		return context.WithValue(ctx, outboundIdentityScopeKey{}, scope)
	}
	return ctx
}
