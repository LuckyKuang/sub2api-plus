package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error)
	GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	Update(ctx context.Context, sub *UserSubscription) error
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*UserSubscription, error)

	ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error)

	ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) error
	UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error

	ActivateWindows(ctx context.Context, id int64, start time.Time) error
	ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, newWindowStart time.Time) error
	ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	IncrementUsage(ctx context.Context, id int64, costUSD float64) error

	BatchUpdateExpiredStatus(ctx context.Context) (int64, error)
}

// FiveHourSubscriptionQuotaRepository is deliberately an optional extension
// of the long-lived subscription repository port. It keeps existing test and
// integration fakes compatible while allowing the concrete persistence layer
// to reset the rolling window independently from calendar-aligned windows.
type FiveHourSubscriptionQuotaRepository interface {
	ResetFiveHourUsage(ctx context.Context, id int64, newWindowStart time.Time) error
}

// AtomicSubscriptionUsageWindowsResetRepository is implemented by the primary
// persistence repository. It resets all selected windows in one UPDATE so an
// administrator never observes a partially reset quota after a process crash
// or a transient database failure between calendar and rolling-window writes.
// The legacy split interfaces remain as a compatibility fallback for external
// repository implementations and focused test doubles.
type AtomicSubscriptionUsageWindowsResetRepository interface {
	ResetUsageWindowsAtomically(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly, resetFiveHour bool, calendarWindowStart, fiveHourWindowStart time.Time) error
}
