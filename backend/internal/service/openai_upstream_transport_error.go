package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// openAITransportErrorTempUnschedDuration is how long an account is temporarily
// unscheduled after a durable transport failure (matches tokenRefreshTempUnschedDuration).
const openAITransportErrorTempUnschedDuration = 10 * time.Minute

// openAITransportFailoverBody is the OpenAI-format error body attached to the
// failover error for a transport-level failure. Kept identical to the legacy
// inline 502 body so the client-visible payload is unchanged if failover is
// ultimately exhausted.
var openAITransportFailoverBody = []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)

// openAITransportErrorClass describes how to react to a transport-level upstream
// failure — i.e. the HTTP round-trip never completed (proxy / DNS / TCP / TLS
// error, no HTTP status code received).
type openAITransportErrorClass struct {
	// Persistent marks failures where retrying the same endpoint is pointless:
	// expired or rejected proxy credentials, a dead proxy endpoint, or DNS/routing
	// failure. A proxy-backed OpenAI account is isolated through the shared proxy
	// circuit; only an account without that circuit is temporarily unscheduled.
	Persistent     bool
	Classification string
}

// persistentUpstreamTransportErrorMarkers are substrings (matched case-insensitively
// against the raw transport error) that indicate a durable proxy/network fault.
// Matched signals are intentionally specific failure *reasons*, not the operation
// (e.g. we match "connection refused", not "proxyconnect") so that a transient
// failure of the same operation (a proxy timeout) is NOT misclassified as durable.
var persistentUpstreamTransportErrorMarkers = []string{
	"authentication failed",         // SOCKS5 RFC1929 / proxy credentials rejected (expired account)
	"proxy authentication required", // HTTP proxy 407
	"connection refused",            // proxy/upstream endpoint down
	"no route to host",
	"network is unreachable",
	"no such host", // DNS resolution failure (bad/expired proxy hostname)
}

// classifyUpstreamTransportError decides whether a transport-level upstream error
// is durable (Persistent — evict the account + alert) or a transient blip
// (fail over to a healthy account but keep this one schedulable).
//
// Motivating incident: a SOCKS5 proxy whose subscription lapsed returned
// `username/password authentication failed`; the account was nonetheless
// rescheduled on every request, hard-failing users with 502s.
//
// Classification strategy (mirrors sanitizeStreamError in gateway_service.go):
//  1. Typed-error checks first (syscall constants, *net.DNSError) — portable and
//     unambiguous.
//  2. String-marker fallback for errors that have no typed form (e.g. the plain
//     string returned by golang.org/x/net/proxy for SOCKS5 credential rejection).
//     The network-layer string markers ("connection refused", "no route to host",
//     "network is unreachable", "no such host") are kept as a cross-platform safety
//     net even though the typed checks should cover them on modern Go+Linux.
func classifyOpenAITransportError(err error) openAITransportErrorClass {
	if err == nil {
		return openAITransportErrorClass{}
	}

	// — Typed checks (preferred) ——————————————————————————————————————————————
	if errors.Is(err, syscall.ECONNREFUSED) {
		return openAITransportErrorClass{Persistent: true, Classification: "connection_refused"}
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return openAITransportErrorClass{Persistent: true, Classification: "network_unreachable"}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return openAITransportErrorClass{Persistent: true, Classification: "dns_not_found"}
	}

	// — String-marker fallback ————————————————————————————————————————————————
	msg := strings.ToLower(err.Error())
	for _, marker := range persistentUpstreamTransportErrorMarkers {
		if strings.Contains(msg, marker) {
			classification := "network_unreachable"
			switch marker {
			case "authentication failed", "proxy authentication required":
				classification = "proxy_authentication_failed"
			case "connection refused":
				classification = "connection_refused"
			case "no such host":
				classification = "dns_not_found"
			}
			return openAITransportErrorClass{Persistent: true, Classification: classification}
		}
	}
	return openAITransportErrorClass{Classification: "transport_error"}
}

// handleOpenAIUpstreamTransportError handles a transport-level upstream failure
// (Do/DoWithTLS returned a non-HTTP error: proxy/DNS/TCP/TLS). It:
//  1. records the failure in Ops error logs (status 0, kind=request_error);
//  2. for durable faults (expired/rejected proxy creds, dead proxy, DNS/routing)
//     temporarily unschedules the account (DB + in-memory) and logs a stable
//     warn event that alert rules can key on;
//  3. returns an error that is *UpstreamFailoverError (so the handler fails over
//     to a healthy account) for all non-canceled errors, or a plain error for
//     context.Canceled (client gone — no failover, no eviction).
//
// It deliberately does NOT write to the response: the handler owns the response
// (failover, or a protocol-correct error once failover is exhausted).
//
// passthrough tags the Ops error event for the OpenAI passthrough forward path.
func (s *OpenAIGatewayService) handleOpenAIUpstreamTransportError(ctx context.Context, c *gin.Context, account *Account, err error, passthrough bool) error {
	if err == nil {
		return nil
	}
	safeErr := sanitizeUpstreamErrorMessage(err.Error())
	classification := classifyOpenAITransportError(err)
	SetOpsRoutingDiagnostics(c, &OpsRoutingDiagnostics{TransportFailure: classification.Classification})
	setOpsUpstreamError(c, 0, safeErr, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: 0,
		Passthrough:        passthrough,
		Kind:               "transport_error",
		Scope:              string(GatewayFailureScopeProvider),
		Reason:             "openai_transport_" + classification.Classification,
		Message:            safeErr,
	})

	// Client disconnected: do NOT fail over to another account and do NOT evict
	// this one — the upstream never had a chance to exhibit a fault.
	if errors.Is(err, context.Canceled) || (errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return err
	}

	// Transport attempt reached the network path; count as Ollama Cloud activity.
	if s != nil {
		scheduleOllamaCloudUsageActivity(s.deferredService, account)
	}

	if account != nil && classification.Classification != "" {
		s.recordOpenAIProxyTransportFailure(account, classification.Classification)
	}
	// 插件已把请求交给上游时，自动切换账号可能造成重复扣费或重复执行。
	var pluginErr *PluginTransportError
	if errors.As(err, &pluginErr) && pluginErr.RequestSent {
		return err
	}

	if classification.Persistent && s.shouldTempUnscheduleOpenAITransportAccount(account) {
		s.tempUnscheduleOpenAITransportError(ctx, account, safeErr)
	}

	return &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		ResponseBody:           openAITransportFailoverBody,
		RequestScopedTransient: true,
		Scope:                  GatewayFailureScopeProvider,
		Reason:                 GatewayFailureReason("openai_transport_" + classification.Classification),
		NextAccountAction:      NextAccountRetry,
	}
}

// shouldTempUnscheduleOpenAITransportAccount keeps a shared-proxy outage at
// proxy scope. A configured OpenAI proxy is represented by the bounded circuit
// and must not also leave an account-specific ten-minute block behind: doing so
// would make recovery of the proxy circuit ineffective for accounts that share
// the same endpoint. If the circuit is explicitly disabled for incident
// diagnosis, retain the legacy account-level protection instead.
func (s *OpenAIGatewayService) shouldTempUnscheduleOpenAITransportAccount(account *Account) bool {
	if _, usesProxyCircuit := openAIProxyStreamCircuitProxyID(account); !usesProxyCircuit {
		return true
	}
	circuit := s.getOpenAIProxyStreamCircuit()
	return circuit == nil || circuit.settings.disabled
}

// tempUnscheduleOpenAITransportError marks an account temporarily unschedulable
// after a durable transport failure, both persistently (DB, survives restart)
// and in-memory (immediate scheduler effect before the DB/account cache propagates).
//
// Log semantics:
//   - "openai.account_temp_unscheduled_transport" — emitted ONLY after a
//     successful DB write (both in-memory + persisted).
//   - "openai.account_temp_unscheduled_transport_memory_only" — emitted when
//     accountRepo is nil (in-memory only; no persistence).
//   - "openai.account_temp_unscheduled_transport_failed" — DB write attempted
//     but returned an error.
func (s *OpenAIGatewayService) tempUnscheduleOpenAITransportError(ctx context.Context, account *Account, safeErr string) {
	if s == nil || account == nil {
		return
	}
	until := time.Now().Add(openAITransportErrorTempUnschedDuration)
	reason := "upstream transport error (proxy/network): " + safeErr

	// Immediate in-memory block (honoured by the scheduler at selection time),
	// effective even if the DB write below fails or the account cache lags.
	s.BlockAccountScheduling(account, until, "transport_error")

	if s.accountRepo == nil {
		// No DB configured — block is in-memory only; emit a distinct event so
		// operators are not misled into thinking the block survived a restart.
		logger.L().With(zap.String("component", "service.openai_gateway")).Warn(
			"openai.account_temp_unscheduled_transport_memory_only",
			zap.Int64("account_id", account.ID),
			zap.String("account_name", account.Name),
			zap.String("platform", account.Platform),
			zap.Time("until", until),
			zap.String("reason", reason),
		)
		return
	}

	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIAccountStateUpdateTimeout)
	defer cancel()
	if err := s.accountRepo.SetTempUnschedulable(bgCtx, account.ID, until, reason); err != nil {
		logger.L().With(zap.String("component", "service.openai_gateway")).Warn(
			"openai.account_temp_unscheduled_transport_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
		return
	}

	// DB write succeeded: both in-memory and persisted.
	logger.L().With(zap.String("component", "service.openai_gateway")).Warn(
		"openai.account_temp_unscheduled_transport",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("platform", account.Platform),
		zap.Time("until", until),
		zap.String("reason", reason),
	)
}

// classifyUpstreamTransportError is the shared Anthropic/Bedrock alias for the
// Plus OpenAI transport classifier so durable proxy/network faults keep the
// same eviction semantics across gateways.
func classifyUpstreamTransportError(err error) openAITransportErrorClass {
	return classifyOpenAITransportError(err)
}
