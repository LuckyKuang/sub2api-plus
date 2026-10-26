package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

type imageResultDownloadScopeKey struct{}

// Image results can outlive the gateway call. This memory-only scope captures
// the actual sender, including a refresh or handler-level account failover,
// without persisting credentials or resolving a newer account/proxy row.
type imageResultDownloadScope struct {
	mu              sync.Mutex
	owner           *Account
	identityContext context.Context
	codexHeaders    http.Header
	client          *http.Client
}

// WithImageTaskResultDownloadScope requires provider URL downloads during task
// completion to use the credential owner captured by the actual gateway send.
func WithImageTaskResultDownloadScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, imageResultDownloadScopeKey{}, &imageResultDownloadScope{})
}

func captureImageResultDownloadOwner(request *http.Request, account *Account) {
	ctx := request.Context()
	scope, _ := ctx.Value(imageResultDownloadScopeKey{}).(*imageResultDownloadScope)
	if scope == nil {
		return
	}
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		owner = nil
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.client != nil {
		scope.client.CloseIdleConnections()
		scope.client = nil
	}
	scope.owner = snapshotRequestCredentialAccount(owner)
	scope.identityContext = context.WithoutCancel(ctx)
	scope.codexHeaders = make(http.Header)
	if owner != nil && owner.Platform == PlatformOpenAI {
		for _, name := range []string{"User-Agent", "Originator", "Version"} {
			if value := request.Header.Get(name); value != "" {
				scope.codexHeaders.Set(name, value)
			}
		}
	}
}

func imageResultDownloadClient(ctx context.Context) (*http.Client, context.Context, *Account, error) {
	scope, _ := ctx.Value(imageResultDownloadScopeKey{}).(*imageResultDownloadScope)
	if scope == nil {
		return nil, ctx, nil, nil
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.owner == nil {
		return nil, ctx, nil, errors.New("image result credential owner is unavailable")
	}
	proxyURL, err := requestCredentialOwnerProxyURL(context.Background(), scope.owner)
	if err != nil {
		return nil, ctx, nil, err
	}
	if scope.client == nil {
		var proxy func(*http.Request) (*url.URL, error)
		if proxyURL != "" {
			parsed, err := url.Parse(proxyURL)
			if err != nil {
				return nil, ctx, nil, errors.New("image result proxy configuration is invalid")
			}
			proxy = http.ProxyURL(parsed)
		}
		// An explicit unbound account is direct. Environment proxies cannot
		// replace either that choice or a configured account proxy.
		scope.client = newImageDownloadHTTPClient(proxy)
	}
	identityContext := scope.identityContext
	if identityContext == nil {
		identityContext = ctx
	}
	// Keep completion's cancellation/deadline while resolving identity from
	// the already captured provider operation, never mutable global settings.
	identityContext = carryOutboundIdentityScope(ctx, identityContext)
	if identity, ok := outboundidentity.FromContext(scope.identityContext); ok {
		identityContext = outboundidentity.WithIdentity(identityContext, identity)
	}
	return scope.client, identityContext, snapshotRequestCredentialAccount(scope.owner), nil
}

func applyImageResultDownloadIdentity(ctx context.Context, req *http.Request, owner *Account) {
	ApplyAccountOutboundIdentity(req.Context(), owner, req)
	if scope, _ := ctx.Value(imageResultDownloadScopeKey{}).(*imageResultDownloadScope); scope != nil {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		for name, values := range scope.codexHeaders {
			req.Header[name] = append([]string(nil), values...)
		}
	}
}

func closeImageResultDownloadClient(ctx context.Context) {
	if scope, _ := ctx.Value(imageResultDownloadScopeKey{}).(*imageResultDownloadScope); scope != nil {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		if scope.client != nil {
			scope.client.CloseIdleConnections()
			scope.client = nil
		}
	}
}
