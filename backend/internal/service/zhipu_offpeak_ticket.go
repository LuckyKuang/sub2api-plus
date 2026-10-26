package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
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
	// settled. Settlement does not restore the rolling take-number allowance.
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
	Position     *int
	release      func()
	leaseContext context.Context
}

// Release completes this model attempt. Copies of a ticket share the same
// idempotent release, so EOF, cancellation and Close cannot release it twice.
func (t *ZhipuOffPeakTicket) Release() {
	if t != nil && t.release != nil {
		t.release()
	}
}

// zhipuOffPeakHolder owns the outstanding ticket of one account. A ticket is
// bound to a plan and is reused across requests until it expires or goes idle,
// which is the closest a stateless gateway can get to the official client's
// run-segment ticket.
type zhipuOffPeakHolder struct {
	gate            *semaphore.Weighted
	taskID          string
	ticketID        string
	state           string
	position        *int
	leaseMu         sync.Mutex
	active          int
	lastUsed        time.Time
	retired         bool
	auth            zcode.OffPeakAuth
	identity        outboundidentity.Identity
	proxyURL        string
	tokenGeneration string
	account         *Account
}

// ZhipuOffPeakTicketManager acquires and retires off-peak tickets per account.
type ZhipuOffPeakTicketManager struct {
	projectTokens  *ZhipuProjectTokenManager
	client         ZhipuOffPeakClient
	acquireTimeout time.Duration
	settleIdle     time.Duration

	mu      sync.Mutex
	holders map[int64]*zhipuOffPeakHolder
	retired map[*zhipuOffPeakHolder]struct{}
	durable *zhipuOffPeakDurable

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
		retired:        map[*zhipuOffPeakHolder]struct{}{},
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
// configured budget for the platform to promote it. The caller must release
// the returned lease after the physical model attempt and response body finish.
func (m *ZhipuOffPeakTicketManager) Acquire(ctx context.Context, account *Account, auth zcode.OffPeakAuth, proxyURL string) (*ZhipuOffPeakTicket, error) {
	if m == nil || m.client == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket client is not configured")
	}
	if account == nil || account.ID <= 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", "off-peak ticket acquisition requires a persisted account")
	}
	snapshot, snapshotErr := snapshotCNControlAccount(account)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	account = snapshot
	ownerProxy, routeErr := account.StrictProxyURL()
	providedProxy, _, parseErr := proxyurl.Parse(proxyURL)
	if routeErr != nil || parseErr != nil || providedProxy != ownerProxy {
		return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OFFPEAK_PROXY_INVALID", "off-peak credential proxy is unavailable")
	}
	proxyURL = ownerProxy
	if strings.TrimSpace(auth.JWT) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OFFPEAK_CREDENTIAL_MISSING", "off-peak ticket acquisition requires the zcode token")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if durable := m.persistence(); durable != nil {
		return m.acquireDurable(ctx, account, auth, proxyURL, durable)
	}
	ctx, cancel := context.WithTimeout(ctx, m.acquireTimeout)
	defer cancel()
	ctx = WithAccountOutboundIdentity(ctx, account)
	generation := ""
	if usesZhipuProjectToken(account) {
		_, generation = projectTokenIdentityScope(ctx, account)
	}
	holder, err := m.lockHolder(ctx, account, auth, proxyURL, generation)
	if err != nil {
		return nil, err
	}
	// One acquisition per credential generation at a time: concurrent takes for
	// the same plan would waste quota rather than increase throughput.
	defer holder.gate.Release(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A ticket belongs to its acquiring snapshot. lockHolder already isolated
	// live old generations; an unused old ticket can be settled immediately.
	if holder.ticketID != "" && m.holderCredentialChanged(holder, account, auth, proxyURL, generation) {
		settleCtx := outboundidentity.WithIdentity(ctx, holder.identity)
		if err := m.withProjectAuth(settleCtx, holder, func(auth zcode.OffPeakAuth) error {
			return m.client.SettleTicket(settleCtx, holder.ticketID, auth, holder.proxyURL)
		}); err != nil {
			return nil, offPeakAcquireError(err)
		}
		m.projectTokens.forgetTicket(outboundidentity.WithIdentity(context.Background(), holder.identity), holder.account)
		holder.ticketID, holder.taskID = "", ""
	}
	if holder.ticketID == "" {
		credentials, err := cloneAccountJSONMap(account.Credentials)
		if err != nil {
			return nil, errors.New("off_peak_credential_snapshot_invalid")
		}
		copy := *account
		copy.Credentials = credentials
		if account.Proxy != nil {
			proxy := *account.Proxy
			copy.Proxy = &proxy
		}
		holder.account = &copy
		holder.auth = auth
		holder.identity, _ = outboundidentity.FromContext(ctx)
		holder.proxyURL = proxyURL
		holder.tokenGeneration = generation
	}
	holder.auth = auth
	// Admission can expire while requests keep the local idle clock alive.
	// Recheck a reused ready/active ticket before handing it to another request.
	if holder.ticketID != "" && (holder.state == zcode.TicketReady || holder.state == zcode.TicketActive) {
		statusCtx := outboundidentity.WithIdentity(ctx, holder.identity)
		var tickets []zcode.OffPeakTicket
		err := m.withProjectAuth(statusCtx, holder, func(auth zcode.OffPeakAuth) error {
			var err error
			tickets, _, err = m.client.TicketStatus(statusCtx, []string{holder.ticketID}, auth, holder.proxyURL)
			return err
		})
		if err != nil {
			return nil, offPeakAcquireError(err)
		}
		ticket, matched := matchingOffPeakStatus(tickets, holder.ticketID)
		if !matched {
			return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OFFPEAK_STATUS_INVALID", "off-peak status did not identify the outstanding ticket")
		}
		holder.state, holder.position = ticket.State, ticket.Position
	}
	m.touchHolder(holder)
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
			var ticket *zcode.OffPeakTicket
			err := m.withProjectAuth(ctx, holder, func(auth zcode.OffPeakAuth) error {
				var err error
				ticket, err = m.client.TakeTicket(ctx, holder.taskID, auth, holder.proxyURL)
				return err
			})
			if ticket != nil && ticket.TicketID != "" {
				// Preserve a known remote ticket even when the response is partial
				// or its state cannot authorize this acquisition. A later request
				// revalidates it through Status instead of spending another Take.
				m.touchHolder(holder)
				holder.ticketID = ticket.TicketID
				holder.state, holder.position = "uncertain", nil
			}
			if err != nil {
				return nil, offPeakAcquireError(err)
			}
			if ticket == nil || ticket.TicketID == "" || !validOffPeakState(ticket.State) {
				return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OFFPEAK_TICKET_FAILED", "off-peak take did not return a valid admission state")
			}
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
		if holder.state != zcode.TicketQueued && holder.state != "uncertain" {
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
	holder.leaseMu.Lock()
	holder.lastUsed = m.now()
	holder.active++
	holder.leaseMu.Unlock()
	position := holder.position
	holder.position = nil
	var once sync.Once
	return &ZhipuOffPeakTicket{TicketID: holder.ticketID, State: holder.state, Position: position, release: func() {
		once.Do(func() {
			holder.leaseMu.Lock()
			defer holder.leaseMu.Unlock()
			holder.active--
			if holder.active == 0 {
				holder.lastUsed = m.now()
			}
		})
	}}
}

func (m *ZhipuOffPeakTicketManager) touchHolder(holder *zhipuOffPeakHolder) {
	holder.leaseMu.Lock()
	holder.lastUsed = m.now()
	holder.leaseMu.Unlock()
}

func (m *ZhipuOffPeakTicketManager) holderCredentialChanged(holder *zhipuOffPeakHolder, account *Account, auth zcode.OffPeakAuth, proxyURL, generation string) bool {
	previous, next := holder.auth, auth
	if m.projectTokens != nil && usesZhipuProjectToken(account) {
		previous.PlanKey, next.PlanKey = "", ""
	}
	return previous != next || holder.proxyURL != proxyURL || holder.tokenGeneration != generation
}

// A new credential generation may start while the old generation streams.
// Keep its old ticket, owner, proxy and lease counts together until it drains;
// neither a new admission nor a credential rotation may settle that live ticket.
func (m *ZhipuOffPeakTicketManager) lockHolder(ctx context.Context, account *Account, auth zcode.OffPeakAuth, proxyURL, generation string) (*zhipuOffPeakHolder, error) {
	for {
		holder := m.holderFor(account.ID)
		if err := holder.gate.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.holders[account.ID] != holder {
			m.mu.Unlock()
			holder.gate.Release(1)
			continue
		}
		holder.leaseMu.Lock()
		active := holder.active
		holder.leaseMu.Unlock()
		if holder.ticketID != "" && active > 0 && m.holderCredentialChanged(holder, account, auth, proxyURL, generation) {
			holder.retired = true
			m.retired[holder] = struct{}{}
			m.holders[account.ID] = &zhipuOffPeakHolder{gate: semaphore.NewWeighted(1)}
			m.mu.Unlock()
			holder.gate.Release(1)
			continue
		}
		m.mu.Unlock()
		return holder, nil
	}
}

// Resolve each admission/status/settlement attempt from the original owner,
// including detached idle cleanup. Never keep a PAT as a static ticket header.
func (m *ZhipuOffPeakTicketManager) withProjectAuth(ctx context.Context, holder *zhipuOffPeakHolder, send func(zcode.OffPeakAuth) error) error {
	if !usesZhipuProjectToken(holder.account) {
		return send(holder.auth)
	}
	if m.projectTokens == nil {
		return errors.New("project_token_service_unavailable")
	}
	ctx = outboundidentity.WithIdentity(context.WithValue(ctx, outboundIdentityScopeKey{}, &outboundIdentityScope{}), holder.identity)
	ctx = context.WithValue(ctx, requestCredentialOwnerContextKey{}, requestCredentialOwnerEvidence{})
	ctx = context.WithValue(ctx, requestCredentialTransportKey{}, false)
	for attempt := 0; attempt < 2; attempt++ {
		token, err := m.projectTokens.tokenForTicket(ctx, holder.account)
		if err != nil {
			return err
		}
		holder.auth.PlanKey = token
		err = send(holder.auth)
		m.projectTokens.observeTicketRejection(ctx, holder.account, token, err)
		var rejection *zcode.OffPeakError
		if err == nil || !errors.As(err, &rejection) || rejection.HTTPStatus != http.StatusUnauthorized || attempt == 1 {
			return err
		}
	}
	return nil
}

// nextPollDelay polls the platform once and returns the next delay.
func (m *ZhipuOffPeakTicketManager) nextPollDelay(ctx context.Context, holder *zhipuOffPeakHolder, deadline time.Time) time.Duration {
	var tickets []zcode.OffPeakTicket
	var platformDelay time.Duration
	err := m.withProjectAuth(ctx, holder, func(auth zcode.OffPeakAuth) error {
		var err error
		tickets, platformDelay, err = m.client.TicketStatus(ctx, []string{holder.ticketID}, auth, holder.proxyURL)
		return err
	})
	if err != nil {
		// A failed status poll is transient: keep the ticket and retry at the
		// floor rather than discarding admission state.
		return offPeakPollFloor
	}
	ticket, matched := matchingOffPeakStatus(tickets, holder.ticketID)
	if !matched {
		// Missing, duplicate or unknown states cannot grant admission or turn
		// an outstanding ticket into a terminal ticket eligible for re-take.
		return offPeakPollFloor
	}
	holder.state, holder.position = ticket.State, ticket.Position
	if ticket.Admitted() {
		return 0
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
	if durable := m.persistence(); durable != nil {
		m.sweepDurable(durable)
		return
	}
	now := m.now()
	m.mu.Lock()
	holders := make([]*zhipuOffPeakHolder, 0, len(m.holders))
	for _, holder := range m.holders {
		holders = append(holders, holder)
	}
	for holder := range m.retired {
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
		holder.leaseMu.Lock()
		busy := holder.active > 0 || (!holder.retired && now.Sub(holder.lastUsed) < m.settleIdle)
		holder.leaseMu.Unlock()
		if busy {
			holder.gate.Release(1)
			continue
		}
		ticketID, proxyURL := holder.ticketID, holder.proxyURL
		// The detached settlement owns the same credentials and identity snapshot
		// as the acquisition; background settings must not select a new fingerprint.
		settleCtx, cancel := context.WithTimeout(outboundidentity.WithIdentity(context.Background(), holder.identity), 30*time.Second)
		// Keep the holder locked until settlement finishes: a concurrent request
		// must not be handed the ticket while the platform is retiring it.
		err := m.withProjectAuth(settleCtx, holder, func(auth zcode.OffPeakAuth) error { return m.client.SettleTicket(settleCtx, ticketID, auth, proxyURL) })
		cancel()
		if err == nil {
			m.projectTokens.forgetTicket(outboundidentity.WithIdentity(context.Background(), holder.identity), holder.account)
			holder.ticketID = ""
			holder.state = zcode.TicketSettled
			holder.taskID = ""
			m.mu.Lock()
			delete(m.retired, holder)
			m.mu.Unlock()
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
	// TicketForRequest returns the lease that admits this physical request.
	TicketForRequest(ctx context.Context, account *Account) (*ZhipuOffPeakTicket, error)
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
// The plan JWT and scope come from the credential owner; the short-lived
// coding-plan token is resolved again by the final sending wrapper.
func applyZhipuOffPeakTicketHeader(ctx context.Context, account *Account, headers http.Header) (*ZhipuOffPeakTicket, error) {
	if account == nil || !account.IsZhipu() || strings.TrimSpace(account.GetCredential("plan_kind")) != ZhipuPlanOffPeak {
		return nil, nil
	}
	// An inbound/overridden ticket can never substitute for admission failure.
	deleteHeaderAllForms(headers, "X-Off-Peak-Ticket-ID")
	raw := zhipuOffPeakProvider.Load()
	provider, ok := raw.(zhipuOffPeakTicketProvider)
	if !ok {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket manager is unavailable")
	}
	ticket, err := provider.TicketForRequest(ctx, account)
	if err != nil {
		return nil, err
	}
	if ticket == nil || strings.TrimSpace(ticket.TicketID) == "" {
		ticket.Release()
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak admission returned no ticket")
	}
	headers.Set("X-Off-Peak-Ticket-ID", ticket.TicketID)
	return ticket, nil
}

// TicketForRequest resolves an admitted ticket for an off-peak account. It is
// the implementation the gateway registers through
// SetZhipuOffPeakTicketProvider.
func (m *ZhipuOffPeakTicketManager) TicketForRequest(ctx context.Context, account *Account) (*ZhipuOffPeakTicket, error) {
	if m == nil || account == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_UNAVAILABLE", "off-peak ticket manager is unavailable")
	}
	snapshot, snapshotErr := snapshotCNControlAccount(account)
	if snapshotErr != nil {
		return nil, snapshotErr
	}
	account = snapshot
	ctx = WithAccountOutboundIdentity(ctx, account)
	proxyURL, routeErr := account.StrictProxyURL()
	if routeErr != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OFFPEAK_PROXY_INVALID", "off-peak credential proxy is unavailable")
	}
	jwt := strings.TrimSpace(account.GetCredential("zcode_jwt_token"))
	if jwt == "" {
		jwt = strings.TrimSpace(account.GetCredential("api_key"))
	}
	planKey := strings.TrimSpace(account.GetCredential("off_peak_plan_key"))
	if planKey == "" {
		planKey = strings.TrimSpace(account.GetCredential("api_key"))
	}
	if usesZhipuProjectToken(account) {
		var err error
		planKey, err = m.projectTokens.Token(ctx, account)
		if err != nil {
			return nil, err
		}
	}
	auth := zcode.OffPeakAuth{JWT: jwt, PlanKey: planKey}
	if account.GetCredential("zhipu_project_kind") == "team" {
		auth.TeamOrg = strings.TrimSpace(account.GetCredential("zhipu_organization"))
		auth.TeamProj = strings.TrimSpace(account.GetCredential("zhipu_project"))
	}
	return m.Acquire(ctx, account, auth, proxyURL)
}

// holdZhipuOffPeakResponse transfers the admission lease from the synchronous
// send to the response body, including streams that outlive the idle window.
func holdZhipuOffPeakResponse(ctx context.Context, ticket *ZhipuOffPeakTicket, response *http.Response, err error) {
	if ticket == nil {
		return
	}
	if err != nil || response == nil || response.Body == nil {
		ticket.Release()
		return
	}
	body := &zhipuOffPeakResponseBody{ReadCloser: response.Body, release: ticket.Release}
	body.mu.Lock()
	body.stopCancel = context.AfterFunc(ctx, func() { _ = body.Close() })
	body.mu.Unlock()
	response.Body = body
}

type zhipuOffPeakResponseBody struct {
	io.ReadCloser
	once       sync.Once
	closeOnce  sync.Once
	closeErr   error
	mu         sync.Mutex
	stopCancel func() bool
	release    func()
}

func (b *zhipuOffPeakResponseBody) finish() {
	b.once.Do(func() {
		b.mu.Lock()
		stop := b.stopCancel
		b.stopCancel = nil
		b.mu.Unlock()
		if stop != nil {
			stop()
		}
		b.release()
	})
}

func (b *zhipuOffPeakResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *zhipuOffPeakResponseBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.ReadCloser.Close() })
	b.finish()
	return b.closeErr
}

// proxyURLOrEmpty reports the account's proxy URL when one is configured.
func (a *Account) proxyURLOrEmpty() string {
	if a == nil || a.Proxy == nil {
		return ""
	}
	return a.Proxy.URL()
}
