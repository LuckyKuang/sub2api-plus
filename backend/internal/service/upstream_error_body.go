package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UpstreamErrorBodyReadTimeout bounds diagnostic reads after an unsuccessful
// response has arrived. Header timeouts no longer apply at this point, and the
// request context may intentionally be detached from the downstream client.
const UpstreamErrorBodyReadTimeout = 5 * time.Second

// readUpstreamErrorResponseBody bounds both bytes and time without leaving a
// background Read behind. Closing the actual network body interrupts its Read
// and frees the transport slot before a retry or failover starts. Successful
// inference streams are deliberately outside this diagnostic-only budget.
func readUpstreamErrorResponseBody(resp *http.Response, limit int64, timeout time.Duration) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = UpstreamErrorBodyReadTimeout
	}
	underlying := resp.Body
	closed := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		defer close(closed)
		_ = underlying.Close()
	})
	body, err := io.ReadAll(io.LimitReader(underlying, limit))
	if !timer.Stop() {
		// The timer callback owns Close. Wait for it to finish before returning
		// ownership to callers that also close the response or replace its body.
		<-closed
		return body, fmt.Errorf("read upstream error response body: %w", context.DeadlineExceeded)
	}
	return body, err
}
