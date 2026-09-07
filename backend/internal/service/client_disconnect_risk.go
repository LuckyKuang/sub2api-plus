package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type ClientDisconnectOutcome string

const (
	ClientDisconnectOutcomeCompleted    ClientDisconnectOutcome = "completed"
	ClientDisconnectOutcomeDisconnected ClientDisconnectOutcome = "client_disconnected"
	ClientDisconnectOutcomeNeutral      ClientDisconnectOutcome = "neutral"
)

type ClientDisconnectRiskBegin struct {
	UserID     int64
	Generation int64
	RequestID  string
	APIKeyID   int64
	Protocol   string
}

type ClientDisconnectRiskFinalize struct {
	UserID           int64
	Generation       int64
	Sequence         int64
	Outcome          ClientDisconnectOutcome
	Threshold        int
	Enforce          bool
	CompletionStatus string
	UsageSource      string
	UsageMissing     bool
}

type ClientDisconnectRiskResult struct {
	ConsecutiveCount int
	AutoBanned       bool
}

type ClientDisconnectRiskEvent struct {
	UserID           int64      `json:"user_id"`
	APIKeyID         *int64     `json:"api_key_id,omitempty"`
	RequestID        string     `json:"request_id"`
	Protocol         string     `json:"protocol"`
	Generation       int64      `json:"generation"`
	Sequence         int64      `json:"sequence"`
	Outcome          string     `json:"outcome"`
	CompletionStatus string     `json:"completion_status"`
	UsageSource      *string    `json:"usage_source,omitempty"`
	UsageMissing     bool       `json:"usage_missing"`
	ConsecutiveAfter *int       `json:"consecutive_after,omitempty"`
	Threshold        *int       `json:"threshold,omitempty"`
	Enforce          *bool      `json:"enforce,omitempty"`
	AutoBanned       bool       `json:"auto_banned"`
	AcceptedAt       time.Time  `json:"accepted_at"`
	FinalizedAt      *time.Time `json:"finalized_at,omitempty"`
}

type ClientDisconnectRiskEventFilter struct {
	UserID           int64
	APIKeyID         int64
	Outcome          string
	CompletionStatus string
	UsageMissing     *bool
	AutoBanned       *bool
	Page             int
	PageSize         int
}

type ClientDisconnectRiskRepository interface {
	Begin(ctx context.Context, input ClientDisconnectRiskBegin) (int64, error)
	Finalize(ctx context.Context, input ClientDisconnectRiskFinalize) (ClientDisconnectRiskResult, error)
	ClearUser(ctx context.Context, userID int64) error
	ListEvents(ctx context.Context, filter ClientDisconnectRiskEventFilter) ([]ClientDisconnectRiskEvent, int64, error)
}

type clientDisconnectRiskSettings struct {
	enabled    bool
	threshold  int
	generation int64
	expiresAt  time.Time
}

type ClientDisconnectRiskService struct {
	repo            ClientDisconnectRiskRepository
	settingService  *SettingService
	authInvalidator APIKeyAuthCacheInvalidator
	settings        atomic.Pointer[clientDisconnectRiskSettings]
	settingsRefresh sync.Mutex
}

func NewClientDisconnectRiskService(
	repo ClientDisconnectRiskRepository,
	settingService *SettingService,
	authInvalidator APIKeyAuthCacheInvalidator,
) *ClientDisconnectRiskService {
	service := &ClientDisconnectRiskService{
		repo:            repo,
		settingService:  settingService,
		authInvalidator: authInvalidator,
	}
	if settingService != nil {
		settingService.SubscribeClientDisconnectRiskRuntime(func() {
			service.settings.Store(nil)
		})
	}
	return service
}

func (s *ClientDisconnectRiskService) settingsFor(ctx context.Context) clientDisconnectRiskSettings {
	now := time.Now()
	if cached := s.settings.Load(); cached != nil && now.Before(cached.expiresAt) {
		return *cached
	}

	s.settingsRefresh.Lock()
	defer s.settingsRefresh.Unlock()
	if cached := s.settings.Load(); cached != nil && now.Before(cached.expiresAt) {
		return *cached
	}

	result := clientDisconnectRiskSettings{enabled: true, threshold: 10, generation: 1, expiresAt: now.Add(2 * time.Second)}
	if s.settingService != nil {
		settings, err := s.settingService.GetAllSettings(ctx)
		if err != nil {
			slog.Warn("client_disconnect_risk.settings_read_failed", "error", err)
			if cached := s.settings.Load(); cached != nil {
				return *cached
			}
		} else {
			result.enabled = settings.ClientDisconnectConsecutiveBanEnabled
			result.threshold = settings.ClientDisconnectConsecutiveBanThreshold
			result.generation = settings.ClientDisconnectConsecutiveBanGeneration
			if result.threshold < 1 || result.threshold > 1000 {
				result.threshold = 10
			}
			if result.generation < 1 {
				result.generation = 1
			}
		}
	}
	s.settings.Store(&result)
	return result
}

func (s *ClientDisconnectRiskService) NewLifecycle(userID, apiKeyID int64, role, requestID, protocol string) *ClientDisconnectLifecycle {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil
	}
	return &ClientDisconnectLifecycle{
		service:   s,
		userID:    userID,
		apiKeyID:  apiKeyID,
		requestID: strings.TrimSpace(requestID),
		protocol:  strings.TrimSpace(protocol),
		exempt:    strings.EqualFold(strings.TrimSpace(role), RoleAdmin),
	}
}

func (s *ClientDisconnectRiskService) ClearUser(ctx context.Context, userID int64) {
	if s == nil || s.repo == nil || userID <= 0 {
		return
	}
	clearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.repo.ClearUser(clearCtx, userID); err != nil {
		slog.Warn("client_disconnect_risk.clear_failed", "user_id", userID, "error", err)
	}
}

func (s *ClientDisconnectRiskService) ListEvents(ctx context.Context, filter ClientDisconnectRiskEventFilter) ([]ClientDisconnectRiskEvent, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, nil
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	return s.repo.ListEvents(ctx, filter)
}

type ClientDisconnectLifecycle struct {
	service             *ClientDisconnectRiskService
	userID              int64
	apiKeyID            int64
	requestID           string
	protocol            string
	exempt              bool
	mu                  sync.Mutex
	beginTried          bool
	upstreamAccepted    bool
	accepted            bool
	finalizing          bool
	finalized           bool
	pendingFinalization *clientDisconnectPendingFinalization
	generation          int64
	sequence            int64
}

type clientDisconnectPendingFinalization struct {
	outcome          ClientDisconnectOutcome
	completionStatus string
	usageSource      string
	usageMissing     bool
}

func (l *ClientDisconnectLifecycle) Accepted(ctx context.Context) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.upstreamAccepted = true
	if l.beginTried || l.accepted || l.finalized {
		return
	}
	l.beginTried = true
	settings := l.service.settingsFor(ctx)
	requestID := l.requestID
	if requestID == "" {
		requestID = fmt.Sprintf("generated-%d-%d", l.userID, time.Now().UnixNano())
	}
	beginCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	sequence, err := l.service.repo.Begin(beginCtx, ClientDisconnectRiskBegin{
		UserID: l.userID, Generation: settings.generation, RequestID: requestID,
		APIKeyID: l.apiKeyID, Protocol: l.protocol,
	})
	if err != nil {
		l.beginTried = false
		slog.Warn("client_disconnect_risk.begin_failed", "user_id", l.userID, "request_id", requestID, "error", err)
		return
	}
	l.generation = settings.generation
	l.sequence = sequence
	l.accepted = sequence > 0
}

func (l *ClientDisconnectLifecycle) Completed(ctx context.Context) {
	l.finalize(ctx, ClientDisconnectOutcomeCompleted, "completed", "upstream_exact", false)
}

func (l *ClientDisconnectLifecycle) Disconnected(ctx context.Context) {
	l.finalize(ctx, ClientDisconnectOutcomeDisconnected, "client_disconnected", "", true)
}

func (l *ClientDisconnectLifecycle) DisconnectedWithUsage(ctx context.Context) {
	l.finalize(ctx, ClientDisconnectOutcomeDisconnected, "client_disconnected", "partial", false)
}

func (l *ClientDisconnectLifecycle) DisconnectedWithExactUsage(ctx context.Context) {
	l.finalize(ctx, ClientDisconnectOutcomeDisconnected, "client_disconnected", "upstream_exact", false)
}

func (l *ClientDisconnectLifecycle) Neutral(ctx context.Context) {
	l.finalize(ctx, ClientDisconnectOutcomeNeutral, "upstream_failed", "", true)
}

func (l *ClientDisconnectLifecycle) finalize(ctx context.Context, outcome ClientDisconnectOutcome, completionStatus, usageSource string, usageMissing bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	retryBegin := l.upstreamAccepted && !l.accepted && !l.beginTried && !l.finalized
	l.mu.Unlock()
	if retryBegin {
		l.Accepted(ctx)
	}
	l.mu.Lock()
	if !l.accepted || l.finalized || l.sequence <= 0 {
		l.mu.Unlock()
		return
	}
	if l.pendingFinalization == nil {
		l.pendingFinalization = &clientDisconnectPendingFinalization{
			outcome: outcome, completionStatus: completionStatus,
			usageSource: usageSource, usageMissing: usageMissing,
		}
	}
	if l.finalizing {
		l.mu.Unlock()
		return
	}
	l.finalizing = true
	pending := *l.pendingFinalization
	generation := l.generation
	sequence := l.sequence
	l.mu.Unlock()
	settings := l.service.settingsFor(ctx)
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result, err := l.service.repo.Finalize(finalizeCtx, ClientDisconnectRiskFinalize{
		UserID: l.userID, Generation: generation, Sequence: sequence,
		Outcome: pending.outcome, Threshold: settings.threshold,
		Enforce:          settings.enabled && settings.generation == generation && !l.exempt,
		CompletionStatus: pending.completionStatus,
		UsageSource:      pending.usageSource,
		UsageMissing:     pending.usageMissing,
	})
	l.mu.Lock()
	l.finalizing = false
	if err == nil {
		l.finalized = true
	}
	l.mu.Unlock()
	if err != nil {
		slog.Warn("client_disconnect_risk.finalize_failed", "user_id", l.userID, "sequence", sequence, "outcome", pending.outcome, "error", err)
		return
	}
	if result.AutoBanned {
		if l.service.authInvalidator != nil {
			invalidateCtx, invalidateCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			l.service.authInvalidator.InvalidateAuthCacheByUserID(invalidateCtx, l.userID)
			invalidateCancel()
		}
		slog.Warn("client_disconnect_risk.user_auto_banned",
			"user_id", l.userID,
			"api_key_id", l.apiKeyID,
			"request_id", l.requestID,
			"protocol", l.protocol,
			"consecutive_count", result.ConsecutiveCount,
			"threshold", settings.threshold,
		)
	}
}

type clientDisconnectLifecycleContextKey struct{}

func WithClientDisconnectLifecycle(ctx context.Context, lifecycle *ClientDisconnectLifecycle) context.Context {
	if lifecycle == nil {
		return ctx
	}
	return context.WithValue(ctx, clientDisconnectLifecycleContextKey{}, lifecycle)
}

func clientDisconnectLifecycleFromContext(ctx context.Context) *ClientDisconnectLifecycle {
	if ctx == nil {
		return nil
	}
	lifecycle, _ := ctx.Value(clientDisconnectLifecycleContextKey{}).(*ClientDisconnectLifecycle)
	return lifecycle
}

func MarkClientDisconnectUpstreamAccepted(ctx context.Context) {
	clientDisconnectLifecycleFromContext(ctx).Accepted(ctx)
}

func MarkClientDisconnectRequestCompleted(ctx context.Context) {
	clientDisconnectLifecycleFromContext(ctx).Completed(ctx)
}

func MarkClientDisconnectRequestDisconnected(ctx context.Context) {
	clientDisconnectLifecycleFromContext(ctx).Disconnected(ctx)
}

func MarkClientDisconnectRequestDisconnectedWithUsage(ctx context.Context) {
	clientDisconnectLifecycleFromContext(ctx).DisconnectedWithUsage(ctx)
}

// finalizeUnbilledClientDisconnectRequest settles an accepted metadata-only
// upstream request. A client cancellation wins over a successful upstream
// response, while upstream/protocol failures remain neutral via the handler's
// deferred fallback.
func finalizeUnbilledClientDisconnectRequest(ctx context.Context, completed bool) {
	if ctx == nil {
		return
	}
	if ctx.Err() != nil {
		MarkClientDisconnectRequestDisconnected(ctx)
		return
	}
	if completed {
		MarkClientDisconnectRequestCompleted(ctx)
	}
}

// finalizeClientDisconnectState classifies an accepted request at the
// forwarding boundary. It returns true when a successful non-stream result
// must also be exposed to billing as client-disconnected.
func finalizeClientDisconnectState(
	ctx context.Context,
	stream bool,
	clientDisconnect bool,
	usageComplete bool,
	downstreamCanceled bool,
	err error,
) bool {
	lifecycle := clientDisconnectLifecycleFromContext(ctx)
	if clientDisconnect {
		lifecycle.DisconnectedWithUsage(ctx)
		return false
	}
	if !stream && downstreamCanceled {
		if err == nil && usageComplete {
			lifecycle.DisconnectedWithExactUsage(ctx)
		} else {
			lifecycle.Disconnected(ctx)
		}
		return true
	}
	if err == nil && usageComplete {
		lifecycle.Completed(ctx)
		return false
	}
	if downstreamCanceled {
		lifecycle.Disconnected(ctx)
	}
	return false
}

func clientRequestCanceled(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func finalizeClientDisconnectForwardResult(ctx context.Context, c *gin.Context, result *OpenAIForwardResult, err error) {
	if result == nil {
		finalizeClientDisconnectState(ctx, false, false, false, clientRequestCanceled(c), err)
		return
	}
	usageComplete := result.UsageComplete()
	if finalizeClientDisconnectState(ctx, result.Stream, result.ClientDisconnect, usageComplete, clientRequestCanceled(c), err) {
		result.ClientDisconnect = true
		result.ClientDisconnectUsageSource = UsageSourceUpstreamExact
	}
}
