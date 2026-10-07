package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

// Off-peak ticket acquisition budget. The platform's queue is unbounded — the
// official client polls until the operator cancels — so a synchronous gateway
// bounds the wait instead and reports a retryable failure when the budget
// expires.
const (
	// DefaultOffPeakAcquireTimeout is how long one request may wait for `ready`.
	DefaultOffPeakAcquireTimeout = 30 * time.Second
	// DefaultOffPeakSettleIdle is how long a ticket may sit unused before it is
	// settled. Settling frees the account's take-number quota.
	DefaultOffPeakSettleIdle = 60 * time.Second
	// Off-peak poll floor and ceiling, mirroring the official client's clamp.
	offPeakPollFloor = 2 * time.Second
	offPeakPollCeil  = 30 * time.Second
	// offPeakSweepInterval is the settle sweeper cadence.
	offPeakSweepInterval = 15 * time.Second
)

// ZhipuOffPeakClient is the egress port for the ticket protocol.
type ZhipuOffPeakClient interface {
	Availability(ctx context.Context, auth zcode.OffPeakAuth, proxyURL string) (*zcode.OffPeakAvailability, error)
	TakeTicket(ctx context.Context, taskID string, auth zcode.OffPeakAuth, proxyURL string) (*zcode.OffPeakTicket, error)
	TicketStatus(ctx context.Context, ticketIDs []string, auth zcode.OffPeakAuth, proxyURL string) ([]zcode.OffPeakTicket, time.Duration, error)
	SettleTicket(ctx context.Context, ticketID string, auth zcode.OffPeakAuth, proxyURL string) error
}

// ZhipuOffPeakTicket is the admission material one model request needs.
type ZhipuOffPeakTicket struct {
	TicketID string
	State    string
	// Position is the queue position when known, for diagnostics.
	Position *int
}

// zhipuOffPeakHolder owns the outstanding ticket of one account. A ticket is
// bound to a plan and is reused across requests until it expires or goes idle,
// which is the closest a stateless gateway can get to the official client's
// run-segment ticket.
type zhipuOffPeakHolder struct {
	gate     *semaphore.Weighted
	taskID   string
	ticketID string
	state    string
	position *int
	lastUsed time.Time
	auth     zcode.OffPeakAuth
	identity outboundidentity.Identity
	proxyURL string
}

// ZhipuOffPeakTicketManager acquires and retires off-peak tickets per account.
type ZhipuOffPeakTicketManager struct {
	client         ZhipuOffPeakClient
	acquireTimeout time.Duration
	settleIdle     time.Duration

	mu      sync.Mutex
	holders map[int64]*zhipuOffPeakHolder

	stopOnce sync.Once
	stopCh   chan struct{}

	// now is injectable for tests.
	now func() time.Time
}

// NewZhipuOffPeakTicketManager creates the manager and starts its settle sweeper.
func NewZhipuOffPeakTicketManager(client ZhipuOffPeakClient) *ZhipuOffPeakTicketManager {
	manager := &ZhipuOffPeakTicketManager{
		client:         client,
		acquireTimeout: DefaultOffPeakAcquireTimeout,
		settleIdle:     DefaultOffPeakSettleIdle,
		holders:        map[int64]*zhipuOffPeakHolder{},
		stopCh:         make(chan struct{}),
		now:            time.Now,
	}
	go manager.sweep()
	return manager
}

// WithAcquireTimeout overrides the per-request acquisition budget.
func (m *ZhipuOffPeakTicketManager) WithAcquireTimeout(timeout time.Duration) *ZhipuOffPeakTicketManager {
	if m != nil && timeout > 0 {
		m.acquireTimeout = timeout
	}
	return m
}

// WithSettleIdle overrides the idle window that retires a ticket.
func (m *ZhipuOffPeakTicketManager) WithSettleIdle(idle time.Duration) *ZhipuOffPeakTicketManager {
	if m != nil && idle > 0 {
		m.settleIdle = idle
	}
	return m
}

// Stop ends the settle sweeper. It is idempotent.
func (m *ZhipuOffPeakTicketManager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// Acquire returns an admitted ticket for the account, waiting up to the
// configured budget for the platform to promote it.
func (m *ZhipuOffPeakTicketManager) Acquire(ctx context.Context, account *Account, auth zcode.OffPeakAuth, proxyURL string) (*ZhipuOffPeakTicket, error) {
	if m == nil || m.client == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket client is not configured")
	}
	if account == nil || account.ID <= 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", "off-peak ticket acquisition requires a persisted account")
	}
	if strings.TrimSpace(auth.JWT) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_CREDENTIAL_MISSING", "off-peak ticket acquisition requires the zcode token")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, m.acquireTimeout)
	defer cancel()
	ctx = WithAccountOutboundIdentity(ctx, account)
	holder := m.holderFor(account.ID)
	// One acquisition per account at a time: the platform issues a ticket to a
	// plan, so concurrent takes would waste take-number quota and race the quota
	// window rather than increase throughput.
	if err := holder.gate.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer holder.gate.Release(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A ticket belongs to the credentials that acquired it. Retire it with that
	// snapshot before accepting a changed plan, grant or proxy.
	previous, next := holder.auth, auth
	previous.RequestID, next.RequestID = "", ""
	if holder.ticketID != "" && (previous != next || holder.proxyURL != proxyURL) {
		settleCtx := outboundidentity.WithIdentity(ctx, holder.identity)
		if err := m.client.SettleTicket(settleCtx, holder.ticketID, holder.auth, holder.proxyURL); err != nil {
			return nil, offPeakAcquireError(err)
		}
		holder.ticketID, holder.taskID = "", ""
	}
	if holder.ticketID == "" {
		holder.auth = auth
		holder.identity, _ = outboundidentity.FromContext(ctx)
		holder.proxyURL = proxyURL
	}
	// Admission can expire while requests keep the local idle clock alive.
	// Recheck a reused ready/active ticket before handing it to another request.
	if holder.ticketID != "" && (holder.state == zcode.TicketReady || holder.state == zcode.TicketActive) {
		statusCtx := outboundidentity.WithIdentity(ctx, holder.identity)
		tickets, _, err := m.client.TicketStatus(statusCtx, []string{holder.ticketID}, holder.auth, holder.proxyURL)
		if err != nil {
			return nil, offPeakAcquireError(err)
		}
		holder.state = zcode.TicketNotFound
		for _, ticket := range tickets {
			if ticket.TicketID == holder.ticketID {
				holder.state, holder.position = ticket.State, ticket.Position
				break
			}
		}
	}
	holder.lastUsed = m.now()
	if holder.taskID == "" {
		// Official offPeakTaskService uses offpeak-${randomUUID()}.
		holder.taskID = "offpeak-" + uuid.NewString()
	}
	return m.acquireLocked(outboundidentity.WithIdentity(ctx, holder.identity), holder)
}

func (m *ZhipuOffPeakTicketManager) acquireLocked(ctx context.Context, holder *zhipuOffPeakHolder) (*ZhipuOffPeakTicket, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline, _ := ctx.Deadline()
	if existing := m.admittedLocked(holder); existing != nil {
		return existing, nil
	}
	// One initial take plus one re-take after an expiry while queued, mirroring
	// the official sync loop; more than that means the queue is not advancing.
	const maxTakes = 2
	takes := 0
	for {
		if holder.ticketID == "" {
			if takes >= maxTakes {
				return nil, infraerrors.New(http.StatusTooManyRequests, "ZHIPU_OFFPEAK_QUEUE_TIMEOUT", "off-peak tickets keep expiring before admission; retry later")
			}
			takes++
			ticket, err := m.client.TakeTicket(ctx, holder.taskID, holder.auth, holder.proxyURL)
			if err != nil {
				return nil, offPeakAcquireError(err)
			}
			holder.lastUsed = m.now()
			holder.ticketID = ticket.TicketID
			holder.state = ticket.State
			holder.position = ticket.Position
		}
		if admitted := m.admittedLocked(holder); admitted != nil {
			return admitted, nil
		}
		if holder.state == zcode.TicketSettled || holder.state == zcode.TicketNotFound || holder.state == zcode.TicketExpired {
			// A terminal or expired ticket cannot be promoted; drop it and take a
			// fresh one within the remaining budget.
			holder.ticketID = ""
			continue
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, infraerrors.Newf(http.StatusTooManyRequests, "ZHIPU_OFFPEAK_QUEUE_TIMEOUT", "off-peak ticket is still queued at position %d; retry later", offPeakPosition(holder.position))
		}
		delay := m.nextPollDelay(ctx, holder, deadline)
		if admitted := m.admittedLocked(holder); admitted != nil {
			return admitted, nil
		}
		if holder.state != zcode.TicketQueued {
			continue
		}
		if delay <= 0 {
			delay = offPeakPollFloor
		}
		if delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, infraerrors.Newf(http.StatusTooManyRequests, "ZHIPU_OFFPEAK_QUEUE_TIMEOUT", "off-peak ticket is still queued at position %d; retry later", offPeakPosition(holder.position))
			}
			return nil, ctx.Err()
		case <-m.stopCh:
			timer.Stop()
			return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket manager is stopping")
		case <-timer.C:
		}
	}
}

// admittedTickLocked returns the ticket when the current state admits a model
// request, refreshing the idle clock.
func (m *ZhipuOffPeakTicketManager) admittedLocked(holder *zhipuOffPeakHolder) *ZhipuOffPeakTicket {
	if holder.ticketID == "" {
		return nil
	}
	if holder.state != zcode.TicketReady && holder.state != zcode.TicketActive {
		return nil
	}
	holder.lastUsed = m.now()
	position := holder.position
	holder.position = nil
	return &ZhipuOffPeakTicket{TicketID: holder.ticketID, State: holder.state, Position: position}
}

// nextPollDelay polls the platform once and returns the next delay.
func (m *ZhipuOffPeakTicketManager) nextPollDelay(ctx context.Context, holder *zhipuOffPeakHolder, deadline time.Time) time.Duration {
	tickets, platformDelay, err := m.client.TicketStatus(ctx, []string{holder.ticketID}, holder.auth, holder.proxyURL)
	if err != nil {
		// A failed status poll is transient: keep the ticket and retry at the
		// floor rather than discarding admission state.
		return offPeakPollFloor
	}
	for _, ticket := range tickets {
		if ticket.TicketID != holder.ticketID {
			continue
		}
		holder.state = ticket.State
		holder.position = ticket.Position
		if ticket.Admitted() {
			return 0
		}
	}
	delay := platformDelay
	if delay <= 0 {
		delay = offPeakPollFloor
	}
	if delay < offPeakPollFloor {
		delay = offPeakPollFloor
	}
	if delay > offPeakPollCeil {
		delay = offPeakPollCeil
	}
	if remaining := time.Until(deadline); remaining < delay {
		return remaining
	}
	return delay
}

// sweep retires tickets that have gone idle. Settling is best-effort: the
// platform is idempotent and reclaims un-settled tickets by timeout, so a failed
// settle is retried on the next cycle instead of failing a request.
func (m *ZhipuOffPeakTicketManager) sweep() {
	ticker := time.NewTicker(offPeakSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.sweepOnce()
		}
	}
}

func (m *ZhipuOffPeakTicketManager) sweepOnce() {
	now := m.now()
	m.mu.Lock()
	holders := make([]*zhipuOffPeakHolder, 0, len(m.holders))
	for _, holder := range m.holders {
		holders = append(holders, holder)
	}
	m.mu.Unlock()
	for _, holder := range holders {
		if !holder.gate.TryAcquire(1) {
			continue
		}
		if holder.ticketID == "" {
			holder.gate.Release(1)
			continue
		}
		if now.Sub(holder.lastUsed) < m.settleIdle {
			holder.gate.Release(1)
			continue
		}
		ticketID, auth, proxyURL := holder.ticketID, holder.auth, holder.proxyURL
		// The detached settlement owns the same credentials and identity snapshot
		// as the acquisition; background settings must not select a new fingerprint.
		settleCtx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), holder.identity), 30*time.Second)
		// Keep the holder locked until settlement finishes: a concurrent request
		// must not be handed the ticket while the platform is retiring it.
		err := m.client.SettleTicket(settleCtx, ticketID, auth, proxyURL)
		cancel()
		if err == nil {
			holder.ticketID = ""
			holder.state = zcode.TicketSettled
			holder.taskID = ""
		}
		holder.gate.Release(1)
	}
}

func (m *ZhipuOffPeakTicketManager) holderFor(accountID int64) *zhipuOffPeakHolder {
	m.mu.Lock()
	defer m.mu.Unlock()
	if holder, ok := m.holders[accountID]; ok {
		return holder
	}
	holder := &zhipuOffPeakHolder{gate: semaphore.NewWeighted(1)}
	m.holders[accountID] = holder
	return holder
}

func offPeakPosition(position *int) int {
	if position == nil {
		return 0
	}
	return *position
}

// offPeakAcquireError maps a ticket-server rejection onto a gateway error,
// preserving the retry hint so the caller can pace itself.
func offPeakAcquireError(err error) error {
	var offPeakErr *zcode.OffPeakError
	if !errors.As(err, &offPeakErr) {
		return infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OFFPEAK_TICKET_FAILED", "off-peak ticket request failed: %v", err)
	}
	switch offPeakErr.Code {
	case zcode.OffPeakCodeNoEligibility:
		return infraerrors.New(http.StatusForbidden, "ZHIPU_OFFPEAK_NOT_ELIGIBLE", "this account has no eligible coding plan for the idle queue")
	case zcode.OffPeakCodeQuotaExhausted, zcode.OffPeakCodeQueued:
		return infraerrors.Newf(http.StatusTooManyRequests, "ZHIPU_OFFPEAK_QUOTA_EXHAUSTED", "off-peak take-number quota exhausted: %s", offPeakRetryHint(offPeakErr))
	default:
		return infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OFFPEAK_TICKET_FAILED", "off-peak ticket request failed: %v", offPeakErr)
	}
}

func offPeakRetryHint(err *zcode.OffPeakError) string {
	if err == nil {
		return "retry later"
	}
	switch {
	case !err.NextTakeAt.IsZero():
		return "retry after " + err.NextTakeAt.UTC().Format(time.RFC3339)
	case err.RetryAfter > 0:
		return "retry after " + err.RetryAfter.String()
	default:
		return "retry later"
	}
}

// zhipuOffPeakTicketProvider is the request-path hook that supplies the dynamic
// ticket header. It is a package-level provider for the same reason the Codex
// canonical User-Agent resolver is: the protocol builder must not acquire a
// dependency on the settings or account services.
type zhipuOffPeakTicketProvider interface {
	// TicketForRequest returns the ticket id that admits this request.
	TicketForRequest(ctx context.Context, account *Account) (string, error)
}

var zhipuOffPeakProvider atomic.Value

// SetZhipuOffPeakTicketProvider wires the ticket provider at startup.
func SetZhipuOffPeakTicketProvider(provider zhipuOffPeakTicketProvider) {
	if provider == nil {
		return
	}
	zhipuOffPeakProvider.Store(provider)
}

// applyZhipuOffPeakTicketHeader injects the per-request off-peak declarations.
// The static declarations (bearer plan token, coding-plan key, team scope) stay
// owned by the account credential and header-override layers; only the ticket id
// is request-scoped.
func applyZhipuOffPeakTicketHeader(ctx context.Context, account *Account, headers http.Header) error {
	if account == nil || !account.IsZhipu() || strings.TrimSpace(account.GetCredential("plan_kind")) != ZhipuPlanOffPeak {
		return nil
	}
	// An inbound/overridden ticket can never substitute for admission failure.
	deleteHeaderAllForms(headers, "X-Off-Peak-Ticket-ID")
	raw := zhipuOffPeakProvider.Load()
	provider, ok := raw.(zhipuOffPeakTicketProvider)
	if !ok {
		return infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket manager is unavailable")
	}
	ticket, err := provider.TicketForRequest(ctx, account)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ticket) == "" {
		return infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak admission returned no ticket")
	}
	headers.Set("X-Off-Peak-Ticket-ID", ticket)
	return nil
}

// TicketForRequest resolves an admitted ticket for an off-peak account. It is
// the implementation the gateway registers through
// SetZhipuOffPeakTicketProvider.
func (m *ZhipuOffPeakTicketManager) TicketForRequest(ctx context.Context, account *Account) (string, error) {
	if m == nil || account == nil {
		return "", infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket manager is unavailable")
	}
	jwt := strings.TrimSpace(account.GetCredential("zcode_jwt_token"))
	if jwt == "" {
		jwt = strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	}
	planKey := strings.TrimSpace(account.GetCredential("off_peak_plan_key"))
	if planKey == "" {
		planKey = strings.TrimSpace(account.GetCredential("api_key"))
	}
	auth := zcode.OffPeakAuth{
		JWT:      jwt,
		PlanKey:  planKey,
		TeamOrg:  strings.TrimSpace(account.GetCredential("zhipu_organization")),
		TeamProj: strings.TrimSpace(account.GetCredential("zhipu_project")),
	}
	ticket, err := m.Acquire(ctx, account, auth, account.proxyURLOrEmpty())
	if err != nil {
		return "", err
	}
	return ticket.TicketID, nil
}

// proxyURLOrEmpty reports the account's proxy URL when one is configured.
func (a *Account) proxyURLOrEmpty() string {
	if a == nil || a.Proxy == nil {
		return ""
	}
	return a.Proxy.URL()
}
