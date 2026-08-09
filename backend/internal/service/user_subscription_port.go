package service

import (
	"context"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error)
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

	// ActivateWindows 首次使用时激活用量窗口。日窗口按日历日对齐，锚点为当天 0 点
	// （dailyStart）；周/月窗口为期限对齐滚动窗口，锚点为激活时刻（periodicStart）。
	// 仅当三个窗口均未激活时生效。
	ActivateWindows(ctx context.Context, id int64, dailyStart, periodicStart time.Time) error
	// ResetUsageWindows 手动重置所选窗口的用量。日窗口锚点写入 dailyStart（当天 0 点，
	// 保持 0 点刷新节奏不漂移）；周/月窗口锚点写入 periodicStart（重置时刻）。
	ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error
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
	ResetUsageWindowsAtomically(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly, resetFiveHour bool, dailyStart, periodicStart time.Time) error
}
