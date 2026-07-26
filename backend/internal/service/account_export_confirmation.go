package service

import "context"

// AccountExportConfirmationLimiter protects the human confirmation step used
// before exporting account credentials. Implementations must fail closed when
// their backing store is unavailable.
type AccountExportConfirmationLimiter interface {
	Allowed(ctx context.Context, userID int64, clientIP string) (bool, error)
	RecordFailure(ctx context.Context, userID int64, clientIP string) error
	Reset(ctx context.Context, userID int64, clientIP string) error
}
