package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"
)

const openAIGroupQuotaFollowResetPollInterval = 5 * time.Second

type GroupQuotaFollowResetResult struct {
	EventID               int64
	GroupID               int64
	UserIDs               []int64
	AffectedSubscriptions int
	Skipped               bool
}

type OpenAIGroupQuotaFollowResetRepository interface {
	ObserveWeeklyReset(ctx context.Context, accountID int64, observation OpenAIWeeklyQuotaObservation) (int, error)
	ClaimWeeklyResetSources(ctx context.Context, limit int) ([]int64, error)
	ProcessNextPending(ctx context.Context) (*GroupQuotaFollowResetResult, error)
}

// OpenAIWeeklyQuotaObservation contains only raw, default weekly upstream data.
// A quota query can confirm earlier evidence; cached/display values cannot.
type OpenAIWeeklyQuotaObservation struct {
	ResetAt     time.Time
	ObservedAt  time.Time
	ReceivedAt  time.Time
	UsedPercent *float64
	QuotaQuery  bool
}

func validOpenAIWeeklyUsedPercent(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 100
}

// OpenAIGroupQuotaFollowResetService applies durable events and refreshes only
// eligible configured source accounts using the existing quota transport.
type OpenAIGroupQuotaFollowResetService struct {
	repo         OpenAIGroupQuotaFollowResetRepository
	billingCache *BillingCacheService
	quota        *OpenAIQuotaService
	ctx          context.Context
	cancel       context.CancelFunc
	wake         chan struct{}
	start        sync.Once
	stop         sync.Once
	wg           sync.WaitGroup
}

func NewOpenAIGroupQuotaFollowResetService(repo OpenAIGroupQuotaFollowResetRepository, billingCache *BillingCacheService) *OpenAIGroupQuotaFollowResetService {
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAIGroupQuotaFollowResetService{
		repo: repo, billingCache: billingCache, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
	}
}

func (s *OpenAIGroupQuotaFollowResetService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.start.Do(func() {
		setOpenAIGroupQuotaFollowResetObserver(s)
		s.wg.Add(1)
		go s.run()
		if s.quota != nil {
			s.wg.Add(1)
			go s.runSourceRefresh()
		}
		s.Notify()
	})
}

func (s *OpenAIGroupQuotaFollowResetService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() {
		clearOpenAIGroupQuotaFollowResetObserver(s)
		s.cancel()
		s.wg.Wait()
	})
}

func (s *OpenAIGroupQuotaFollowResetService) Notify() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *OpenAIGroupQuotaFollowResetService) Observe(ctx context.Context, accountID int64, observation OpenAIWeeklyQuotaObservation) {
	if s == nil || s.repo == nil || accountID <= 0 || observation.ResetAt.IsZero() || observation.ObservedAt.IsZero() {
		return
	}
	observation.ReceivedAt = time.Now().UTC()
	// The upstream observation remains valid if the downstream client has
	// disconnected. Persist it synchronously before billing, with a bounded wait.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	created, err := s.repo.ObserveWeeklyReset(ctx, accountID, observation)
	if err != nil {
		slog.Warn("openai_group_quota_follow_observe_failed", "account_id", accountID, "reset_at", observation.ResetAt.UTC(), "error", err)
		return
	}
	if created > 0 {
		s.Notify()
	}
}

func (s *OpenAIGroupQuotaFollowResetService) runSourceRefresh() {
	defer s.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		s.refreshSources()
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *OpenAIGroupQuotaFollowResetService) refreshSources() {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	ids, err := s.repo.ClaimWeeklyResetSources(ctx, 4)
	cancel()
	if err != nil {
		if s.ctx.Err() == nil {
			slog.Warn("openai_group_quota_follow_sources_failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if s.ctx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
		_, err := s.quota.queryUsage(ctx, id, false)
		cancel()
		if err != nil && s.ctx.Err() == nil {
			slog.Warn("openai_group_quota_follow_refresh_failed", "account_id", id, "error", err)
		}
	}
}

func (s *OpenAIGroupQuotaFollowResetService) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(openAIGroupQuotaFollowResetPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			s.processPending()
		case <-ticker.C:
			s.processPending()
		}
	}
}

func (s *OpenAIGroupQuotaFollowResetService) processPending() {
	for {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		result, err := s.repo.ProcessNextPending(ctx)
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("openai_group_quota_follow_process_failed", "error", err)
			}
			return
		}
		if result == nil {
			return
		}
		for _, userID := range result.UserIDs {
			if s.billingCache == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := s.billingCache.InvalidateSubscriptionAndNotify(ctx, userID, result.GroupID)
			cancel()
			if err != nil {
				slog.Warn("openai_group_quota_follow_cache_invalidate_failed", "event_id", result.EventID, "group_id", result.GroupID, "user_id", userID, "error", err)
			}
		}
		if result.Skipped {
			slog.Info("openai_group_quota_follow_skipped", "event_id", result.EventID, "group_id", result.GroupID)
			continue
		}
		slog.Info("openai_group_quota_follow_completed", "event_id", result.EventID, "group_id", result.GroupID, "affected_subscriptions", result.AffectedSubscriptions)
	}
}

var openAIGroupQuotaFollowResetObserver struct {
	sync.RWMutex
	service *OpenAIGroupQuotaFollowResetService
}

func setOpenAIGroupQuotaFollowResetObserver(service *OpenAIGroupQuotaFollowResetService) {
	openAIGroupQuotaFollowResetObserver.Lock()
	openAIGroupQuotaFollowResetObserver.service = service
	openAIGroupQuotaFollowResetObserver.Unlock()
}

func clearOpenAIGroupQuotaFollowResetObserver(service *OpenAIGroupQuotaFollowResetService) {
	openAIGroupQuotaFollowResetObserver.Lock()
	if openAIGroupQuotaFollowResetObserver.service == service {
		openAIGroupQuotaFollowResetObserver.service = nil
	}
	openAIGroupQuotaFollowResetObserver.Unlock()
}

func ObserveOpenAIWeeklyQuota(ctx context.Context, accountID int64, observation OpenAIWeeklyQuotaObservation) {
	openAIGroupQuotaFollowResetObserver.RLock()
	service := openAIGroupQuotaFollowResetObserver.service
	openAIGroupQuotaFollowResetObserver.RUnlock()
	if service != nil {
		service.Observe(ctx, accountID, observation)
	}
}

func observeOpenAIWeeklyUsageSnapshot(ctx context.Context, accountID int64, snapshot *OpenAICodexUsageSnapshot, quotaQuery bool) {
	resetAt, ok := snapshot.WeeklyResetAt()
	if !ok {
		return
	}
	var used *float64
	if snapshot.PrimaryWindowMinutes != nil && isOpenAIWeeklyQuotaWindowMinutes(int64(*snapshot.PrimaryWindowMinutes)) && snapshot.PrimaryResetAtUnix != nil && *snapshot.PrimaryResetAtUnix == resetAt.Unix() {
		used = snapshot.PrimaryUsedPercent
	} else {
		used = snapshot.SecondaryUsedPercent
	}
	if !validOpenAIWeeklyUsedPercent(used) {
		used = nil
	}
	ObserveOpenAIWeeklyQuota(ctx, accountID, OpenAIWeeklyQuotaObservation{
		ResetAt: resetAt, ObservedAt: codexSnapshotBaseTime(snapshot, time.Now()),
		UsedPercent: used, QuotaQuery: quotaQuery,
	})
}
