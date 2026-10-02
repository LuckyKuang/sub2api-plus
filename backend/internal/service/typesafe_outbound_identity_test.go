//go:build unit || !integration

package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// TypeSafe 是登记过的一等 API-key 兼容平台：有明确的平台/类型默认映射和
// 预览／保存支持，复用既有可配置 preset，不发明 TypeSafe CLI 名称、版本或
// provider-specific identity header。
func TestTypeSafeOutboundIdentityRegistration(t *testing.T) {
	require.True(t, validOutboundAccountKey(PlatformTypeSafe, AccountTypeAPIKey))
	require.Equal(t, "codex", nativeOutboundPreset(PlatformTypeSafe))

	// TypeSafe accounts are API-key only; the existing native-family rule still
	// forbids an OAuth/setup-token account from selecting a foreign preset.
	require.Error(t, NormalizeAccountOutboundIdentity(PlatformTypeSafe, AccountTypeOAuth, map[string]any{
		outboundIdentityCredential: map[string]any{"preset": "claude"},
	}))

	identity := builtInOutboundIdentity("codex")
	require.NotEmpty(t, identity.UserAgent)
	require.NotContains(t, strings.ToLower(identity.UserAgent), "typesafe")
	for name, value := range identity.Headers {
		require.NotContains(t, strings.ToLower(name), "typesafe", "no invented TypeSafe header")
		require.NotContains(t, strings.ToLower(value), "typesafe", "no invented TypeSafe declaration")
	}
}

func TestTypeSafeOutboundIdentityPreviewAndSave(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Defaults["typesafe:apikey"] = "claude"
	svc, ctx := outboundIdentityTestSettings(t, config)

	account := &Account{ID: 77, Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Credentials: map[string]any{}}
	got, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.True(t, ok)
	require.Equal(t, "claude", got.Preset, "the configured type default governs the platform")

	preview, err := svc.PreviewOutboundIdentity(ctx, account, nil)
	require.NoError(t, err)
	require.Equal(t, got.UserAgent, preview.UserAgent, "preview uses the same candidate as forwarding")
	require.Equal(t, got.Headers, preview.Headers)

	// An explicit account candidate is normalized and saved exactly like any
	// other API-key compatible account.
	credentials := map[string]any{outboundIdentityCredential: map[string]any{"preset": " grok "}}
	require.NoError(t, NormalizeAccountOutboundIdentity(PlatformTypeSafe, AccountTypeAPIKey, credentials))
	stored, ok := credentials[outboundIdentityCredential].(OutboundIdentitySelection)
	require.True(t, ok)
	require.Equal(t, "grok", stored.Preset)

	// An invalid candidate falls through atomically to the type default.
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", UserAgent: "inbound/999.0.0"}
	fellThrough, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	require.Equal(t, "claude", fellThrough.Preset)
	require.NotContains(t, fellThrough.UserAgent, "inbound")
}

// ForwardSystemOne 必须经过既有的 owner preparation 边界：同一 owner 的重试沿用
// 已选快照，failover 换号解析新 owner，同一 operator 变更设置不改变在途请求。
func TestForwardSystemOneResolvesOwnerIdentitySnapshot(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.1"}
	settings, baseCtx := outboundIdentityTestSettings(t, config)

	var userAgents []string
	upstream := &systemOneHTTPUpstream{do: func(req *http.Request) (*http.Response, error) {
		userAgents = append(userAgents, req.Header.Get("User-Agent"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"answers":{},"usage":{"input_tokens":1}}`)),
		}, nil
	}}
	gateway := newSystemOneTestService(upstream)
	// The handler starts one forwarding scope after ingress audit.
	ctx := WithOutboundIdentityScope(baseCtx, nil)
	expectedFirst := settings.resolveDefaultOutboundIdentity(ctx, "claude").UserAgent

	first := &Account{ID: 501, Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "http://typesafe.test", "api_key": "ts-secret",
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "claude"},
	}}
	second := &Account{ID: 502, Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": "http://typesafe.test", "api_key": "ts-secret",
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"},
	}}

	_, err := gateway.ForwardSystemOne(ctx, newSystemOneTestContext(), first, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, expectedFirst, userAgents[0], "the native send must carry the owner snapshot")

	// Global settings change mid-request: same-owner sends keep their snapshot.
	updated := emptyOutboundIdentitySettings()
	updated.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.2"}
	require.NoError(t, settings.SetOutboundIdentitySettings(ctx, updated))
	_, err = gateway.ForwardSystemOne(ctx, newSystemOneTestContext(), first, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, expectedFirst, userAgents[1], "a same-owner retry retains its identity snapshot")

	// Failover selects another credential owner and resolves a new snapshot.
	_, err = gateway.ForwardSystemOne(ctx, newSystemOneTestContext(), second, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, builtInOutboundIdentity("grok").UserAgent, userAgents[2],
		"failover must resolve the new credential owner's identity")

	// A fresh operation sees the updated settings.
	fresh := WithOutboundIdentityScope(baseCtx, nil)
	_, err = gateway.ForwardSystemOne(fresh, newSystemOneTestContext(), first, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, settings.resolveDefaultOutboundIdentity(fresh, "claude").UserAgent, userAgents[3])
	require.NotEqual(t, expectedFirst, userAgents[3], "a new request observes updated settings")
}

// 结构约束：TypeSafe 的每个上游发送入口都必须先经过 prepareAccountOutboundRequest，
// 不能有裸 httpUpstream.Do 绕过最终身份准备边界。
func TestTypeSafeOutboundPathsPrepareIdentityBeforeSend(t *testing.T) {
	for _, file := range []string{"gateway_systemone.go", "account_test_service_typesafe.go"} {
		t.Run(file, func(t *testing.T) {
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, file, nil, 0)
			require.NoError(t, err)

			sends := 0
			ast.Inspect(parsed, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (selector.Sel.Name != "Do" && selector.Sel.Name != "DoWithTLS") {
					return true
				}
				owner, ok := selector.X.(*ast.SelectorExpr)
				if !ok || owner.Sel.Name != "httpUpstream" {
					return true
				}
				sends++
				require.NotEmpty(t, call.Args)
				prepared, ok := call.Args[0].(*ast.CallExpr)
				require.Truef(t, ok, "%s:%d must send a prepared request", file, fset.Position(call.Pos()).Line)
				ident, ok := prepared.Fun.(*ast.Ident)
				require.Truef(t, ok && ident.Name == "prepareAccountOutboundRequest",
					"%s:%d must prepare the account owner identity before the send", file, fset.Position(call.Pos()).Line)
				return true
			})
			require.Positive(t, sends, "expected at least one upstream send in %s", file)
		})
	}
}
