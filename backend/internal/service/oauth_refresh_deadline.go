package service

import (
	"context"
	"time"
)

// Cancellation delivery can lag behind a deadline when timers are contended.
// Credential persistence and retry attribution must honor the deadline itself.
func oauthRefreshContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
