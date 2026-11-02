package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	OutboundNative         = "native"
	OutboundCodexCLIBridge = "codex_cli_bridge"
	OutboundCodexCLITask   = "codex_cli_task"
)

// OutboundMetadata is an allowlisted observation at the gateway dispatch
// boundary. It proves dispatch, not provider receipt or execution of a tier.
// cli_process proves only a verified exec process started; no wire identity or
// tier is claimed for that transport. Historical/unobserved rows remain nil.
type OutboundMetadata struct {
	Transport     string `json:"transport"`
	Evidence      string `json:"evidence"`
	CLIVersion    string `json:"cli_version,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
	Originator    string `json:"originator,omitempty"`
	ClientVersion string `json:"client_version,omitempty"`
	TierKnown     bool   `json:"tier_known"`
	ServiceTier   string `json:"service_tier,omitempty"`
}

func (m *OutboundMetadata) Clone() *OutboundMetadata {
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

func (m *OutboundMetadata) TransportName() string {
	if m == nil {
		return "unknown"
	}
	switch m.Transport {
	case OutboundNative, OutboundCodexCLIBridge, OutboundCodexCLITask:
		return m.Transport
	}
	return "unknown"
}

type outboundObservation struct {
	mu    sync.Mutex
	value *OutboundMetadata
}
type outboundObservationKey struct{}
type outboundCLIExecutionKey struct{}

func beginOutboundObservation(ctx context.Context) (context.Context, *outboundObservation) {
	observation := &outboundObservation{}
	return context.WithValue(ctx, outboundObservationKey{}, observation), observation
}
func (o *outboundObservation) snapshot() *OutboundMetadata {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.value.Clone()
}
func observeOutbound(ctx context.Context, metadata *OutboundMetadata) {
	if o, ok := ctx.Value(outboundObservationKey{}).(*outboundObservation); ok {
		o.mu.Lock()
		o.value = metadata.Clone()
		o.mu.Unlock()
	}
}
func boundedOutboundDeclaration(value string, limit int) string {
	if len(value) > limit || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}
func outboundMetadataForPayload(headers http.Header, payload []byte) *OutboundMetadata {
	m := &OutboundMetadata{Transport: OutboundNative, Evidence: "gateway_send",
		UserAgent:     boundedOutboundDeclaration(headers.Get("User-Agent"), 512),
		Originator:    boundedOutboundDeclaration(headers.Get("Originator"), 128),
		ClientVersion: boundedOutboundDeclaration(headers.Get("Version"), 64)}
	if json.Valid(payload) {
		m.TierKnown = true
		m.ServiceTier = optionalStringValue(extractOpenAIServiceTierFromBody(payload))
	}
	return m
}

// GetBody reads a separate cursor; diagnostics must never consume the send body
// or retain prompts. Requests without a reusable JSON body keep tier unknown.
func observeOutboundHTTP(request *http.Request, owner *Account) {
	if _, ok := request.Context().Value(outboundObservationKey{}).(*outboundObservation); !ok {
		return
	}
	var payload []byte
	if request.GetBody != nil {
		if body, err := request.GetBody(); err == nil {
			payload, _ = io.ReadAll(io.LimitReader(body, 16<<20))
			_ = body.Close()
		}
	}
	metadata := outboundMetadataForPayload(request.Header, payload)
	// This dispatcher also serves protocol adapters. Absence of service_tier
	// in an Anthropic/media/control request cannot clear that protocol's tier.
	path := strings.TrimSuffix(request.URL.Path, "/")
	openAIProtocol := strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/responses/compact") || strings.HasSuffix(path, "/chat/completions")
	if owner == nil || !owner.IsOpenAI() || !openAIProtocol {
		metadata.TierKnown, metadata.ServiceTier = false, ""
	}
	if version, ok := request.Context().Value(outboundCLIExecutionKey{}).(string); ok {
		metadata.Transport, metadata.CLIVersion = OutboundCodexCLIBridge, version
	}
	observeOutbound(request.Context(), metadata)
}
