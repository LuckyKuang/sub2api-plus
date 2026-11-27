//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestQualityCompositeResolver_ExplicitAnyRoute(t *testing.T) {
	repo := &fakeCompositeRepo{routes: []CompositeModelRoute{{
		ID: 1, PublicModel: "gpt-route", MatchType: CompositeRouteMatchExact,
		TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-upstream",
		Endpoint: CompositeRouteEndpointAny, Enabled: true,
	}}}
	resolver := NewQualityCompositeResolver(NewCompositeRouteResolver(repo))
	decision, err := resolver.Resolve(context.Background(), 1, "gpt-route", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if decision.TargetPlatform != PlatformOpenAI || decision.UpstreamModel != "gpt-upstream" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestQualityCompositeResolver_EndpointOrder(t *testing.T) {
	// 只声明 messages 端点的路由：请求 any 时必须通过端点顺序探测命中。
	repo := &fakeCompositeRepo{routes: []CompositeModelRoute{{
		ID: 2, PublicModel: "claude-route", MatchType: CompositeRouteMatchPrefix,
		TargetPlatform: PlatformAnthropic, UpstreamModel: "claude-upstream",
		Endpoint: CompositeRouteEndpointMessages, Enabled: true,
	}}}
	resolver := NewQualityCompositeResolver(NewCompositeRouteResolver(repo))
	decision, err := resolver.Resolve(context.Background(), 1, "claude-route-latest", CompositeRouteEndpointAny)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if decision.UpstreamModel != "claude-upstream" {
		t.Fatalf("unexpected upstream model: %+v", decision)
	}
}

func TestQualityCompositeResolver_Unresolved(t *testing.T) {
	resolver := NewQualityCompositeResolver(NewCompositeRouteResolver(&fakeCompositeRepo{}))
	_, err := resolver.Resolve(context.Background(), 1, "totally-unknown-model", CompositeRouteEndpointAny)
	if !errors.Is(err, ErrQualityCompositeUnresolved) {
		t.Fatalf("expected unresolved, got %v", err)
	}
}

func TestQualityCompositeResolver_Ambiguous(t *testing.T) {
	base := NewCompositeRouteResolver(&fakeCompositeRepo{})
	base.SetModelOwnershipResolver(func(context.Context, int64, string) (CompositeModelOwnership, error) {
		return CompositeModelOwnership{Ambiguous: true}, nil
	})
	resolver := NewQualityCompositeResolver(base)
	_, err := resolver.Resolve(context.Background(), 1, "shared-model", CompositeRouteEndpointAny)
	if !errors.Is(err, ErrQualityCompositeUnresolved) {
		t.Fatalf("ambiguous model must be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "multiple provider platforms") {
		t.Fatalf("error should carry the ambiguity reason, got %v", err)
	}
}

func TestQualityCompositeResolver_RejectsNonConcretePlatform(t *testing.T) {
	repo := &fakeCompositeRepo{routes: []CompositeModelRoute{{
		ID: 3, PublicModel: "weird", MatchType: CompositeRouteMatchExact,
		TargetPlatform: PlatformComposite, UpstreamModel: "weird-up", Endpoint: CompositeRouteEndpointAny, Enabled: true,
	}}}
	resolver := NewQualityCompositeResolver(NewCompositeRouteResolver(repo))
	_, err := resolver.Resolve(context.Background(), 1, "weird", CompositeRouteEndpointAny)
	if !errors.Is(err, ErrQualityCompositeUnresolved) {
		t.Fatalf("non-concrete platform must be rejected, got %v", err)
	}
}

func TestQualityCompositeResolver_DetectorFallbackWithoutRepo(t *testing.T) {
	resolver := NewQualityCompositeResolver(nil)
	decision, err := resolver.Resolve(context.Background(), 1, "gpt-5.1-codex", CompositeRouteEndpointAny)
	if err != nil {
		t.Fatalf("detector fallback: %v", err)
	}
	if decision.TargetPlatform == "" || decision.Source != CompositeRouteSourceDetector {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}
