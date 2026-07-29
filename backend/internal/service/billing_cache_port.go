package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status              string
	ExpiresAt           time.Time
	DailyUsage          float64
	WeeklyUsage         float64
	MonthlyUsage        float64
	FiveHourUsage       float64
	DailyWindowStart    *time.Time
	WeeklyWindowStart   *time.Time
	MonthlyWindowStart  *time.Time
	FiveHourWindowStart *time.Time
	// FiveHourStatePresent distinguishes a new cache entry with an inactive
	// window from an entry written before five-hour quota support existed.
	FiveHourStatePresent bool
	Version              int64
}
