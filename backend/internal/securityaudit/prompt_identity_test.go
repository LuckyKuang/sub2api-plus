package securityaudit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func promptIdentityTestContext(t *testing.T, version *atomic.Int64) context.Context {
	t.Helper()
	ctx := outboundidentity.WithResolver(context.Background(), func(_ context.Context, key string) outboundidentity.Identity {
		require.Equal(t, "openai:apikey", key)
		v := fmt.Sprintf("3.9.%d", version.Load())
		return outboundidentity.Identity{Preset: "grok", UserAgent: "xai-grok-workspace/" + v, Originator: "grok", Version: v,
			Headers: map[string]string{"x-grok-client-identifier": "grok", "x-grok-client-version": v}}
	})
	return outboundidentity.WithIdentity(ctx, outboundidentity.Identity{AccountID: 9, Preset: "claude", UserAgent: "claude-cli/9.9.9", Headers: map[string]string{"X-App": "cli"}})
}

func assertPromptSupplierHeaders(t *testing.T, h http.Header, version, token string) {
	t.Helper()
	require.Equal(t, "xai-grok-workspace/"+version, h.Get("User-Agent"))
	require.Equal(t, version, h.Get("x-grok-client-version"))
	require.Equal(t, "grok", h.Get("x-grok-client-identifier"))
	require.Equal(t, "Bearer "+token, h.Get("Authorization"))
	for _, name := range []string{"X-App", "Originator", "Version", "X-Stainless-Package-Version"} {
		require.Empty(t, h.Get(name), name)
	}
}

func TestPromptProbeAndScanSupplierIdentity(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	ctx := promptIdentityTestContext(t, &version)
	captured := make(chan http.Header, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		if r.URL.Path == "/v1/models" {
			version.Store(2)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()
	result := newProbeTestService().Probe(ctx, ProbeRequest{Endpoint: probeEndpoint(server.URL, "supplier-token")})
	require.True(t, result.OK, result.Message)
	assertPromptSupplierHeaders(t, <-captured, "3.9.1", "supplier-token")
	assertPromptSupplierHeaders(t, <-captured, "3.9.1", "supplier-token")
	_, err := NewOpenAICompatibleScanner().Scan(ctx, ActiveEndpoint{ID: "fresh", BaseURL: server.URL, Model: DefaultGuardModel, Token: "supplier-token", TimeoutMS: 1000}, "hello", AllScannerIDs)
	require.NoError(t, err)
	assertPromptSupplierHeaders(t, <-captured, "3.9.2", "supplier-token")
}

func TestPromptGuardIdentityAcrossChunksAndSupplierFailover(t *testing.T) {
	var version atomic.Int64
	version.Store(1)
	ctx := promptIdentityTestContext(t, &version)
	captured := make(chan http.Header, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		if r.Header.Get("Authorization") == "Bearer key-one" {
			version.Store(2)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()
	endpoints := []ActiveEndpoint{
		{ID: "one", BaseURL: server.URL, Model: DefaultGuardModel, Token: "key-one", TimeoutMS: 1000, InputLimit: 5, Enabled: true},
		{ID: "two", BaseURL: server.URL, Model: DefaultGuardModel, Token: "key-two", TimeoutMS: 1000, InputLimit: 5, Enabled: true},
	}
	evaluator := NewGuardEvaluator(NewOpenAICompatibleScanner(), nil, nil)
	decision, err := evaluator.Evaluate(ctx, guardConfig(endpoints...), PromptSnapshot{RequestID: "identity-test", ScanText: "helloworld", PromptLength: 10})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, decision.Kind)
	require.Equal(t, 2, decision.Result.ChunkTotal)
	for range 2 {
		assertPromptSupplierHeaders(t, <-captured, "3.9.1", "key-one")
		assertPromptSupplierHeaders(t, <-captured, "3.9.2", "key-two")
	}
	decision, err = evaluator.Evaluate(ctx, guardConfig(endpoints...), PromptSnapshot{ScanText: "hello", PromptLength: 5})
	require.NoError(t, err)
	require.Equal(t, DecisionAllow, decision.Kind)
	assertPromptSupplierHeaders(t, <-captured, "3.9.2", "key-one")
	assertPromptSupplierHeaders(t, <-captured, "3.9.2", "key-two")
}

func TestPromptScannerCompiledIdentity(t *testing.T) {
	captured := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Safety: Safe\nCategories: None"}}]}`))
	}))
	defer server.Close()
	ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity { return outboundidentity.Identity{} })
	_, err := NewOpenAICompatibleScanner().Scan(ctx, ActiveEndpoint{BaseURL: server.URL, Model: DefaultGuardModel, TimeoutMS: 1000}, "hello", AllScannerIDs)
	require.NoError(t, err)
	headers := <-captured
	require.Equal(t, service.DefaultOpenAICodexUserAgent, headers.Get("User-Agent"))
	require.Empty(t, headers.Get("Version"), "native OpenAI API-key omissions apply to audit suppliers too")
	require.Empty(t, headers.Get("Originator"))
}
