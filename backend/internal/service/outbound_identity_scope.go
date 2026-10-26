package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type outboundIdentityScopeKey struct{}

type outboundIdentitySettingsOwnerKey struct{}

type outboundIdentitySettingsSnapshot struct {
	defaults           sync.Map
	codexSettingsOnce  sync.Once
	codexSettings      codexOutboundSettingsSnapshot
	presetSettingsOnce sync.Once
	presetSettings     OutboundIdentitySettings
}

func withOutboundIdentitySettingsOwner(ctx context.Context, account *Account) context.Context {
	if account == nil {
		return ctx
	}
	return context.WithValue(ctx, outboundIdentitySettingsOwnerKey{}, credentialOwnerKey(account))
}

func requestIdentitySettingsSnapshot(ctx context.Context) *outboundIdentitySettingsSnapshot {
	scope := outboundIdentityScopeFromContext(ctx)
	if scope == nil {
		return nil
	}
	owner, _ := ctx.Value(outboundIdentitySettingsOwnerKey{}).(outboundIdentityOwner)
	stored, _ := scope.settings.LoadOrStore(owner, &outboundIdentitySettingsSnapshot{})
	snapshot, _ := stored.(*outboundIdentitySettingsSnapshot)
	return snapshot
}

type acquiredIdentityCacheKey struct {
	owner     outboundIdentityOwner
	candidate string
}

func requestCredentialIdentityCacheKey(ctx context.Context, account *Account, candidate string) any {
	key := outboundIdentityOwnerKey(account)
	if ctx != nil {
		if sent, ok := ctx.Value(requestCredentialTransportKey{}).(requestCredentialTransportEvidence); ok && sent.owner != nil && outboundIdentityOwnerKey(sent.owner) == key {
			return acquiredIdentityCacheKey{owner: key, candidate: candidate}
		}
		if evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence); ok &&
			(evidence.selected == key || credentialOwnerKeyFromEvidence(evidence) == key) {
			return acquiredIdentityCacheKey{owner: key, candidate: candidate}
		}
	}
	return key
}

// A scope belongs to one accepted request or WS connection. The existing
// resolvers still select identities; the scope only retains their first result
// for each credential owner across retries and nested credential operations.
type outboundIdentityScope struct {
	codex            sync.Map
	presets          sync.Map
	settings         sync.Map
	credentialOwners sync.Map
	grokRequests     sync.Map
	minimaxTurns     sync.Map
	deepseekTurns    sync.Map
	zcodeTurns       sync.Map
	zcodeQuery       string
	conversationOnce sync.Once
	conversation     string
}

type requestCredentialEvidenceKey struct {
	owner outboundIdentityOwner
	token [sha256.Size]byte
}

type requestCredentialOwnerContextKey struct{}

type requestCredentialOwnerEvidence struct {
	selected        outboundIdentityOwner
	credentialOwner outboundIdentityOwner
	owner           *Account
	transmitted     *atomic.Pointer[requestCredentialTransportEvidence]
}

type requestCredentialTransportEvidence struct {
	owner *Account
}

type requestCredentialTransportKey struct{}

type acquiredRequestCredentialEvidence struct {
	owner *Account
}

func snapshotRequestCredentialAccount(owner *Account) *Account {
	return snapshotOAuthRefreshAccount(owner)
}

func cloneRequestCredentialMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, item := range source {
		cloned[key] = cloneRequestCredentialValue(item)
	}
	return cloned
}

func cloneRequestCredentialValue(value any) any {
	switch source := value.(type) {
	case map[string]any:
		return cloneRequestCredentialMap(source)
	case []any:
		if source == nil {
			return ([]any)(nil)
		}
		cloned := make([]any, len(source))
		for index, item := range source {
			cloned[index] = cloneRequestCredentialValue(item)
		}
		return cloned
	case map[string]string:
		return maps.Clone(source)
	case []string:
		if source == nil {
			return ([]string)(nil)
		}
		cloned := make([]string, len(source))
		copy(cloned, source)
		return cloned
	case OutboundIdentitySelection:
		source.Headers = maps.Clone(source.Headers)
		return source
	case *OutboundIdentitySelection:
		if source == nil {
			return (*OutboundIdentitySelection)(nil)
		}
		cloned := *source
		cloned.Headers = maps.Clone(source.Headers)
		return &cloned
	default:
		return value
	}
}

func credentialOwnerKey(account *Account) outboundIdentityOwner {
	if account != nil && account.IsShadow() {
		return outboundIdentityOwner{id: *account.ParentAccountID}
	}
	return outboundIdentityOwnerKey(account)
}

func credentialOwnerHasToken(owner *Account, token string) bool {
	if owner == nil {
		return false
	}
	// AgentIdentity authenticates with a signed assertion, never an access
	// token. Freeze the validated signing owner before registration/signing;
	// an empty bearer credential must still be rejected for every other type.
	if owner.IsOpenAIAgentIdentity() {
		_, err := agentIdentityKeyFromAccount(owner)
		return strings.TrimSpace(token) == "" && err == nil
	}
	if strings.TrimSpace(token) == "" {
		return false
	}
	var expected string
	switch {
	case isZhipuStartPlan(owner):
		expected = owner.GetCredential("zcode_jwt_token")
	case owner.Platform == PlatformZhipu:
		expected = owner.GetOpenAIProtocolAPIKey()
	case owner.Type == AccountTypeAPIKey:
		if owner.Platform == PlatformGrok {
			expected = owner.GetCredential("api_key")
		} else {
			expected = owner.GetOpenAIProtocolAPIKey()
		}
	case owner.Platform == PlatformGrok:
		expected = owner.GetGrokAccessToken()
	default:
		expected = owner.GetCredential("access_token")
	}
	return strings.TrimSpace(expected) == strings.TrimSpace(token)
}

// Acquisition records the exact successful grant, including credentials
// refreshed after selection. The token digest never leaves this request scope.
// The same token can accompany different grants. Conflicting acquisition
// snapshots become ambiguous, so a later attempt cannot borrow newer evidence.
func recordRequestCredentialOwner(ctx context.Context, owner *Account, token string) {
	if scope := outboundIdentityScopeFromContext(ctx); scope != nil && credentialOwnerHasToken(owner, token) {
		key := requestCredentialEvidenceKey{owner: outboundIdentityOwnerKey(owner), token: sha256.Sum256([]byte(strings.TrimSpace(token)))}
		evidence := &acquiredRequestCredentialEvidence{owner: snapshotRequestCredentialAccount(owner)}
		stored, loaded := scope.credentialOwners.LoadOrStore(key, evidence)
		for loaded {
			previous, ok := stored.(*acquiredRequestCredentialEvidence)
			if !ok || previous.owner == nil || sameRequestCredentialGrant(previous.owner, evidence.owner) {
				break
			}
			replacement := &acquiredRequestCredentialEvidence{}
			if sameAgentIdentityTaskGrant(previous.owner, evidence.owner) {
				// Task bootstrap changes the assertion's task, not its signer.
				// Existing attempts retain their own frozen evidence; the next
				// acquisition must carry the task that will actually be sent.
				replacement = evidence
			}
			if scope.credentialOwners.CompareAndSwap(key, previous, replacement) {
				break
			}
			stored, loaded = scope.credentialOwners.Load(key)
		}
	}
}

func sameRequestCredentialGrant(left, right *Account) bool {
	return left != nil && right != nil && left.ID == right.ID && left.Platform == right.Platform && left.Type == right.Type &&
		reflect.DeepEqual(left.Credentials, right.Credentials) && reflect.DeepEqual(left.ProxyID, right.ProxyID) && reflect.DeepEqual(left.Proxy, right.Proxy)
}

// Each transport attempt gets its own immutable evidence. Reusing an identity
// scope across WS turns or HTTP refresh retries cannot replace older evidence.
func withRequestCredentialOwner(ctx context.Context, selected *Account, token string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	evidence := requestCredentialOwnerEvidence{selected: outboundIdentityOwnerKey(selected), credentialOwner: credentialOwnerKey(selected), transmitted: &atomic.Pointer[requestCredentialTransportEvidence]{}}
	acquired := false
	if scope := outboundIdentityScopeFromContext(ctx); scope != nil {
		key := requestCredentialEvidenceKey{owner: credentialOwnerKey(selected), token: sha256.Sum256([]byte(strings.TrimSpace(token)))}
		if stored, ok := scope.credentialOwners.Load(key); ok {
			if acquiredEvidence, valid := stored.(*acquiredRequestCredentialEvidence); valid {
				acquired = true
				evidence.owner = snapshotRequestCredentialAccount(acquiredEvidence.owner)
			}
		}
	}
	if !acquired && selected != nil && !selected.IsShadow() && credentialOwnerHasToken(selected, token) {
		evidence.owner = snapshotRequestCredentialAccount(selected)
	}
	// A newly acquired attempt cannot inherit a native sender's evidence from
	// an earlier retry or failover. The sender publishes its own snapshot later.
	ctx = context.WithValue(ctx, requestCredentialTransportKey{}, false)
	return context.WithValue(ctx, requestCredentialOwnerContextKey{}, evidence)
}

func requestCredentialOwnerBound(ctx context.Context, selected *Account) bool {
	if ctx == nil || selected == nil {
		return false
	}
	evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence)
	return ok && evidence.selected == outboundIdentityOwnerKey(selected)
}

func requestCredentialOwner(ctx context.Context, selected *Account) (*Account, bool) {
	if ctx == nil || selected == nil {
		return nil, false
	}
	evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence)
	if !ok || evidence.selected != outboundIdentityOwnerKey(selected) {
		return nil, false
	}
	if evidence.transmitted != nil {
		if actual := evidence.transmitted.Load(); actual != nil {
			return snapshotRequestCredentialAccount(actual.owner), actual.owner != nil
		}
	}
	return snapshotRequestCredentialAccount(evidence.owner), evidence.owner != nil
}

// Once credentials have been acquired, both the selected route and its parent
// must resolve the same immutable owner. Pre-acquisition helpers retain their
// explicit account; ambiguous acquired evidence cannot fall back to that row.
func requestCredentialAccount(ctx context.Context, account *Account) (*Account, error) {
	if ctx == nil || account == nil {
		return account, nil
	}
	evidence, bound := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence)
	key := outboundIdentityOwnerKey(account)
	applies := bound && (evidence.selected == key || credentialOwnerKeyFromEvidence(evidence) == key)
	if sent, ok := ctx.Value(requestCredentialTransportKey{}).(requestCredentialTransportEvidence); ok {
		expected := credentialOwnerKey(account)
		if applies {
			expected = credentialOwnerKeyFromEvidence(evidence)
		}
		if sent.owner == nil || outboundIdentityOwnerKey(sent.owner) != expected {
			return nil, errors.New("request transmitted credential owner is unavailable")
		}
		return snapshotRequestCredentialAccount(sent.owner), nil
	}
	if !applies {
		return account, nil
	}
	selected := evidence.selected.unsaved
	if selected == nil {
		selected = &Account{ID: evidence.selected.id}
	}
	owner, known := requestCredentialOwner(ctx, selected)
	if !known {
		return nil, errors.New("request credential owner is unavailable")
	}
	return owner, nil
}

func requestCredentialOwnerProxyURL(ctx context.Context, account *Account) (string, error) {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil || owner == nil {
		return "", err
	}
	return owner.StrictProxyURL()
}

// A native transport can refresh after its outer protocol handler acquired a
// token. It records the owner that actually supplied its final request; it does
// not select another account or look up a mutable latest-owner scope entry.
func recordTransmittedRequestCredentialOwner(req *http.Request, owner *Account, token string) {
	recordRequestCredentialOwner(req.Context(), owner, token)
	bound := withRequestCredentialOwner(req.Context(), owner, token)
	snapshot, _ := requestCredentialOwner(bound, owner)
	actual := requestCredentialTransportEvidence{owner: snapshot}
	*req = *req.WithContext(context.WithValue(req.Context(), requestCredentialTransportKey{}, actual))
}

// The wrapper publishes only the final response's immutable evidence once.
// Each outer attempt owns its own slot, so a later retry cannot rewrite an
// earlier response, and Gin stream readers see the same transmitted snapshot.
func finalizeTransmittedRequestCredentialOwner(ctx context.Context, sent *http.Request) {
	if ctx == nil || sent == nil {
		return
	}
	evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence)
	if !ok || evidence.transmitted == nil {
		return
	}
	actual, ok := sent.Context().Value(requestCredentialTransportKey{}).(requestCredentialTransportEvidence)
	if !ok {
		return
	}
	if actual.owner != nil && outboundIdentityOwnerKey(actual.owner) != credentialOwnerKeyFromEvidence(evidence) {
		actual.owner = nil
	}
	evidence.transmitted.CompareAndSwap(nil, &requestCredentialTransportEvidence{owner: snapshotRequestCredentialAccount(actual.owner)})
}

func credentialOwnerKeyFromEvidence(evidence requestCredentialOwnerEvidence) outboundIdentityOwner {
	if evidence.credentialOwner.id != 0 || evidence.credentialOwner.unsaved != nil {
		return evidence.credentialOwner
	}
	if evidence.owner != nil {
		return outboundIdentityOwnerKey(evidence.owner)
	}
	return evidence.selected
}

// Stream readers use Gin's request context after the forwarding method has
// acquired its credential. Bind only this attempt's evidence and restore it on
// return, including nested forwarding and handler-level account retries.
func bindRequestCredentialOwnerContext(ctx context.Context, c *gin.Context) func() {
	if ctx == nil || c == nil || c.Request == nil {
		return func() {}
	}
	evidence, ok := ctx.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence)
	if !ok {
		return func() {}
	}
	original := c.Request.Context()
	c.Request = c.Request.WithContext(context.WithValue(original, requestCredentialOwnerContextKey{}, evidence))
	return func() { c.Request = c.Request.WithContext(original) }
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
	if account != nil && account.unsavedCredentialOwner != nil {
		return outboundIdentityOwner{unsaved: account.unsavedCredentialOwner}
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
		ctx = context.WithValue(ctx, outboundIdentityScopeKey{}, scope)
	}
	if source != nil {
		if evidence, ok := source.Value(requestCredentialOwnerContextKey{}).(requestCredentialOwnerEvidence); ok {
			ctx = context.WithValue(ctx, requestCredentialOwnerContextKey{}, evidence)
		}
	}
	return ctx
}
