package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/google/uuid"
)

const (
	offPeakOperationLease = time.Minute
	offPeakRequestLease   = time.Minute
	offPeakRequestRenew   = 15 * time.Second
)

func (m *ZhipuOffPeakTicketManager) WithPersistence(store ZhipuOffPeakStore, cfg config.ZhipuOffPeakConfig) error {
	d, err := newOffPeakDurable(store, cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.durable = d
	m.mu.Unlock()
	return nil
}

// A lost durable request lease cancels the physical send and body before the
// server-side expiry can allow another instance to settle its shared ticket.
func (t *ZhipuOffPeakTicket) bindRequest(request *http.Request) *http.Request {
	if t == nil || t.leaseContext == nil {
		return request
	}
	ctx, cancel := context.WithCancel(request.Context())
	context.AfterFunc(t.leaseContext, cancel)
	return request.WithContext(ctx)
}

func (m *ZhipuOffPeakTicketManager) persistence() *zhipuOffPeakDurable {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.durable
}

func offPeakStoreFailure() error {
	return infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_STORE_UNAVAILABLE", "off-peak durable admission is unavailable; retry later")
}

func (m *ZhipuOffPeakTicketManager) acquireDurable(parent context.Context, account *Account, auth zcode.OffPeakAuth, proxyURL string, d *zhipuOffPeakDurable) (*ZhipuOffPeakTicket, error) {
	ctx, cancel := context.WithTimeout(parent, m.acquireTimeout)
	defer cancel()
	ctx = WithAccountOutboundIdentity(ctx, account)
	identity, _ := outboundidentity.FromContext(ctx)
	generation := offPeakOwnerGeneration(account, auth, identity)
	takes := 0
	for {
		candidate := ZhipuOffPeakRecord{ID: uuid.NewString(), AccountID: account.ID, Generation: generation, TaskID: "offpeak-" + uuid.NewString(), KeyID: d.activeKeyID, RetainUntil: m.now().Add(d.retention)}
		snapshot := offPeakSnapshot{Account: minimalOffPeakAccount(account), Auth: auth, Identity: identity, ProxyURL: proxyURL}
		if usesZhipuProjectToken(account) {
			snapshot.Auth.PlanKey = ""
		}
		var err error
		candidate.Snapshot, err = d.encrypt(candidate, snapshot)
		if err != nil {
			return nil, offPeakStoreFailure()
		}
		current, err := d.store.Open(ctx, account, candidate)
		if err != nil {
			return nil, offPeakStoreFailure()
		}
		if current.State == "new" && current.LastError == "quota_exhausted" && current.NextAttempt.After(m.now()) {
			return nil, offPeakAcquireError(&zcode.OffPeakError{HTTPStatus: http.StatusTooManyRequests, Code: zcode.OffPeakCodeQuotaExhausted, NextTakeAt: current.NextAttempt})
		}
		for {
			if ctx.Err() != nil {
				return nil, offPeakQueueTimeout(current.Position, ctx.Err())
			}
			claimed, claimErr := d.store.Claim(ctx, current.ID, false, 0, offPeakOperationLease)
			if errors.Is(claimErr, ErrOffPeakLeaseBusy) {
				if claimed.ID == current.ID && claimed.State == "new" && claimed.LastError == "quota_exhausted" && claimed.NextAttempt.After(m.now()) {
					return nil, offPeakAcquireError(&zcode.OffPeakError{HTTPStatus: http.StatusTooManyRequests, Code: zcode.OffPeakCodeQuotaExhausted, NextTakeAt: claimed.NextAttempt})
				}
				if err = offPeakWait(ctx, m.stopCh, 100*time.Millisecond); err != nil {
					return nil, offPeakQueueTimeout(nil, err)
				}
				// Retry only this segment. An administrator or another instance
				// retiring it cannot authorize this old flow to create a new task.
				continue
			}
			if claimErr != nil {
				return nil, offPeakStoreFailure()
			}
			current = claimed
			if current.TicketID == "" {
				if current.State != "new" {
					m.preserveOffPeakControl(d, current)
					return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_TAKE_UNCERTAIN", "off-peak take result is unknown; automatic retake is suspended")
				}
				if takes >= 2 {
					m.preserveOffPeakControl(d, current)
					return nil, offPeakQueueTimeout(nil, context.DeadlineExceeded)
				}
				// This durable write precedes all Take traffic. A crash from this
				// point cannot silently create a second ticket after restart.
				if err = d.store.BeginTake(ctx, current); err != nil {
					return nil, offPeakStoreFailure()
				}
				takes++
				current.State = "uncertain"
				holder := offPeakSnapshotHolder(snapshot)
				var ticket *zcode.OffPeakTicket
				err = m.withProjectAuth(ctx, holder, func(a zcode.OffPeakAuth) error {
					var e error
					ticket, e = m.client.TakeTicket(ctx, current.TaskID, a, proxyURL)
					return e
				})
				if err != nil && (ticket == nil || ticket.TicketID == "") {
					var rejection *zcode.OffPeakError
					if errors.As(err, &rejection) && (rejection.Code == zcode.OffPeakCodeQuotaExhausted || rejection.Code == zcode.OffPeakCodeNoEligibility) {
						current.State = "new"
						if rejection.Code == zcode.OffPeakCodeQuotaExhausted {
							current.LastError = "quota_exhausted"
							current.NextAttempt = rejection.NextTakeAt
							if !current.NextAttempt.After(m.now()) {
								delay := rejection.RetryAfter
								if delay < offPeakPollFloor {
									delay = offPeakPollFloor
								}
								current.NextAttempt = m.now().Add(delay)
							}
						}
					}
					if saveErr := m.saveOffPeakControl(d, current); saveErr != nil {
						return nil, offPeakStoreFailure()
					}
					return nil, offPeakAcquireErrorSanitized(err)
				}
				if ticket == nil || ticket.TicketID == "" {
					m.preserveOffPeakControl(d, current)
					return nil, offPeakStoreFailure()
				}
				current.TicketID = ticket.TicketID
				validState := err == nil && validOffPeakState(ticket.State)
				if validState {
					current.State, current.Position = ticket.State, ticket.Position
				} else {
					// An unknown admission state cannot authorize a model send, but
					// its known remote ID must survive for original-owner Status and
					// cleanup. Keep the durable state within the uncertain contract.
					current.State = "uncertain"
					current.LastError = "take_state_invalid"
				}
				if err = m.saveOffPeakControl(d, current); err != nil {
					// Retain a late result only as retirement evidence. Do not hand
					// an uncommitted ticket to a model request.
					cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
					lateErr := d.store.RegisterLateTicket(cleanupCtx, current)
					done()
					if lateErr != nil {
						slog.Error("zhipu_offpeak.late_ticket_persistence_failed", "account_id", current.AccountID, "stage", "take", "reason", "store_unavailable")
					}
					return nil, offPeakStoreFailure()
				}
				if !validState {
					return nil, offPeakStoreFailure()
				}
				continue
			}
			// Known tickets always revalidate with Status, including the first
			// request after a process restart and ready/active reuse.
			restored, err := d.decrypt(current)
			if err != nil {
				current.LastError = "snapshot_unavailable"
				m.preserveOffPeakControl(d, current)
				return nil, offPeakStoreFailure()
			}
			holder := offPeakSnapshotHolder(restored)
			var statuses []zcode.OffPeakTicket
			var delay time.Duration
			err = m.withProjectAuth(outboundidentity.WithIdentity(ctx, restored.Identity), holder, func(a zcode.OffPeakAuth) error {
				var e error
				statuses, delay, e = m.client.TicketStatus(ctx, []string{current.TicketID}, a, restored.ProxyURL)
				return e
			})
			if err != nil {
				m.preserveOffPeakControl(d, current)
				return nil, offPeakAcquireErrorSanitized(err)
			}
			status, matched := matchingOffPeakStatus(statuses, current.TicketID)
			if !matched {
				m.preserveOffPeakControl(d, current)
				return nil, offPeakStoreFailure()
			}
			current.State, current.Position = status.State, status.Position
			switch current.State {
			case zcode.TicketReady, zcode.TicketActive:
				requestID := uuid.NewString()
				if err = d.store.Admit(ctx, current, account, requestID, offPeakRequestLease); err != nil {
					return nil, offPeakStoreFailure()
				}
				return m.durableRequestLease(parent, d, current, requestID), nil
			case zcode.TicketExpired, zcode.TicketNotFound, zcode.TicketSettled:
				current.Closed = true
				if err = m.saveOffPeakControl(d, current); err != nil {
					return nil, offPeakStoreFailure()
				}
				// A confirmed expired ticket starts a fresh task/queue position.
				// It still consumes a new rolling-window allowance upstream.
			default:
				if err = m.saveOffPeakControl(d, current); err != nil {
					return nil, offPeakStoreFailure()
				}
				if delay < offPeakPollFloor {
					delay = offPeakPollFloor
				}
				if delay > offPeakPollCeil {
					delay = offPeakPollCeil
				}
				if err = offPeakWait(ctx, m.stopCh, delay); err != nil {
					return nil, offPeakQueueTimeout(current.Position, err)
				}
				continue
			}
			break
		}
	}
}

func validOffPeakState(state string) bool {
	switch state {
	case zcode.TicketQueued, zcode.TicketReady, zcode.TicketActive, zcode.TicketExpired, zcode.TicketSettled, zcode.TicketNotFound:
		return true
	}
	return false
}

func matchingOffPeakStatus(statuses []zcode.OffPeakTicket, id string) (zcode.OffPeakTicket, bool) {
	var result zcode.OffPeakTicket
	count := 0
	for _, status := range statuses {
		if status.TicketID == id {
			count++
			result = status
		}
	}
	return result, count == 1 && validOffPeakState(result.State)
}

func offPeakSnapshotHolder(s offPeakSnapshot) *zhipuOffPeakHolder {
	return &zhipuOffPeakHolder{account: s.Account, auth: s.Auth, identity: s.Identity, proxyURL: s.ProxyURL}
}

func (m *ZhipuOffPeakTicketManager) saveOffPeakControl(d *zhipuOffPeakDurable, r ZhipuOffPeakRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.NextAttempt.IsZero() {
		r.NextAttempt = m.now()
	}
	return d.store.Save(ctx, r)
}

func (m *ZhipuOffPeakTicketManager) preserveOffPeakControl(d *zhipuOffPeakDurable, r ZhipuOffPeakRecord) {
	if err := m.saveOffPeakControl(d, r); err != nil {
		slog.Warn("zhipu_offpeak.control_commit_failed", "account_id", r.AccountID, "stage", "control", "reason", "store_unavailable")
	}
}

func offPeakAcquireErrorSanitized(err error) error {
	var rejection *zcode.OffPeakError
	if errors.As(err, &rejection) && (rejection.Code == zcode.OffPeakCodeNoEligibility || rejection.Code == zcode.OffPeakCodeQuotaExhausted || rejection.Code == zcode.OffPeakCodeQueued) {
		return offPeakAcquireError(rejection)
	}
	return infraerrors.New(http.StatusBadGateway, "ZHIPU_OFFPEAK_TICKET_FAILED", "off-peak control request failed; retry later")
}

func offPeakQueueTimeout(position *int, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return infraerrors.Newf(http.StatusTooManyRequests, "ZHIPU_OFFPEAK_QUEUE_TIMEOUT", "off-peak ticket is still queued at position %d; retry later", offPeakPosition(position))
}

func offPeakWait(ctx context.Context, stop <-chan struct{}, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stop:
		return context.Canceled
	case <-timer.C:
		return nil
	}
}

func (m *ZhipuOffPeakTicketManager) durableRequestLease(parent context.Context, d *zhipuOffPeakDurable, r ZhipuOffPeakRecord, requestID string) *ZhipuOffPeakTicket {
	leaseCtx, cancelLease := context.WithCancel(context.Background())
	stop := make(chan struct{})
	var once sync.Once
	var lost atomic.Bool
	ticket := &ZhipuOffPeakTicket{TicketID: r.TicketID, State: r.State, Position: r.Position, leaseContext: leaseCtx}
	ticket.release = func() {
		once.Do(func() {
			close(stop)
			cancelLease()
			if lost.Load() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := d.store.ReleaseRequest(ctx, r.ID, requestID); err != nil {
				slog.Warn("zhipu_offpeak.request_release_failed", "account_id", r.AccountID, "reason", "store_unavailable")
			}
		})
	}
	go func() {
		ticker := time.NewTicker(d.renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-parent.Done():
				cancelLease()
				return
			case <-m.stopCh:
				lost.Store(true)
				cancelLease()
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := d.store.RenewRequest(ctx, r.ID, requestID, offPeakRequestLease)
				cancel()
				if err != nil {
					lost.Store(true)
					cancelLease()
					slog.Warn("zhipu_offpeak.request_lease_lost", "account_id", r.AccountID, "reason", "renew_failed")
					return
				}
			}
		}
	}()
	return ticket
}

func (m *ZhipuOffPeakTicketManager) sweepDurable(d *zhipuOffPeakDurable) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ids, err := d.store.Due(ctx, m.settleIdle, 50)
	cancel()
	if err != nil {
		slog.Warn("zhipu_offpeak.sweep_failed", "stage", "outbox", "reason", "store_unavailable")
		return
	}
	for _, id := range ids {
		select {
		case <-m.stopCh:
			return
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		r, err := d.store.Claim(ctx, id, true, m.settleIdle, offPeakOperationLease)
		if err != nil {
			cancel()
			continue
		}
		if !m.now().Before(r.RetainUntil) {
			r.Closed = true
			r.State = "abandoned"
			r.LastError = "retention_expired"
			err = d.store.Save(ctx, r)
			cancel()
			if err != nil {
				slog.Warn("zhipu_offpeak.retention_failed", "account_id", r.AccountID, "reason", "store_unavailable")
			}
			continue
		}
		snapshot, snapshotErr := d.decrypt(r)
		if snapshotErr != nil {
			err = snapshotErr
			r.LastError = "snapshot_unavailable"
		} else {
			holder := offPeakSnapshotHolder(snapshot)
			ctx = outboundidentity.WithIdentity(ctx, snapshot.Identity)
			var statuses []zcode.OffPeakTicket
			err = m.withProjectAuth(ctx, holder, func(a zcode.OffPeakAuth) error {
				var e error
				statuses, _, e = m.client.TicketStatus(ctx, []string{r.TicketID}, a, snapshot.ProxyURL)
				return e
			})
			if err == nil {
				status, matched := matchingOffPeakStatus(statuses, r.TicketID)
				if !matched {
					err = errors.New("off-peak status did not identify the outstanding ticket")
				} else if status.State != zcode.TicketSettled && status.State != zcode.TicketExpired && status.State != zcode.TicketNotFound {
					err = m.withProjectAuth(ctx, holder, func(a zcode.OffPeakAuth) error { return m.client.SettleTicket(ctx, r.TicketID, a, snapshot.ProxyURL) })
				}
			}
			if err != nil {
				r.LastError = "control_failed"
			}
		}
		cancel()
		if err == nil {
			r.State = zcode.TicketSettled
			r.Closed = true
			r.LastError = ""
		} else {
			r.SettlementPending = true
			shift := r.Attempts
			if shift > 8 {
				shift = 8
			}
			r.NextAttempt = m.now().Add(time.Duration(1<<shift) * time.Second)
		}
		if saveErr := m.saveOffPeakControl(d, r); saveErr != nil {
			slog.Warn("zhipu_offpeak.settlement_ack_failed", "account_id", r.AccountID, "stage", "outbox", "reason", "store_unavailable")
		}
	}
}
