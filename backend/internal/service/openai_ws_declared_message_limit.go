package service

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Upstream advertises the largest single WebSocket message it accepts on the
// upgrade response. The declaration is optional and per connection: when it is
// absent the gateway keeps its own read/write limits and never derives a
// default from them (plus decision 4: no synthesized value).
const openAIWSMaxMessageBytesHeader = "x-codex-websocket-max-message-bytes"

const (
	// A declaration above this bound is treated as unparseable rather than as an
	// unbounded budget, so a hostile or broken upstream cannot disable enforcement.
	openAIWSDeclaredLimitMaxValue = 64 << 20
	openAIWSDeclaredLimitValueLen = 12
	openAIWSDeclaredLimitCacheTTL = 10 * time.Minute
	openAIWSDeclaredLimitEntries  = 4096
	// The reason string is shared with the close-driven path: an upstream 1009
	// close and a locally detected oversize payload both mean "use HTTP instead".
	openAIWSReasonRequestTooLarge = "message_too_big"
)

// parseOpenAIWSDeclaredMaxMessageBytes accepts only a positive decimal byte
// count. Anything else means "upstream did not declare a limit".
func parseOpenAIWSDeclaredMaxMessageBytes(headers http.Header) (int, bool) {
	if len(headers) == 0 {
		return 0, false
	}
	raw := strings.TrimSpace(openAIWSHeaderValue(headers, openAIWSMaxMessageBytesHeader))
	if raw == "" || len(raw) > openAIWSDeclaredLimitValueLen {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > openAIWSDeclaredLimitMaxValue {
		return 0, false
	}
	return limit, true
}

func openAIWSDeclaredMaxMessageBytesFromLease(lease *openAIWSConnLease) (int, bool) {
	if lease == nil {
		return 0, false
	}
	return parseOpenAIWSDeclaredMaxMessageBytes(lease.HandshakeHeaders())
}

// openAIWSHeaderValue reads a handshake header that may have been assembled with
// a non-canonical spelling: dialers clone maps they built themselves, so a plain
// Header.Get can miss a lower-case key.
func openAIWSHeaderValue(headers http.Header, name string) string {
	if value := strings.TrimSpace(headers.Get(name)); value != "" {
		return value
	}
	for key, values := range headers {
		if !strings.EqualFold(key, name) || len(values) == 0 {
			continue
		}
		return strings.TrimSpace(values[0])
	}
	return ""
}

type openAIWSDeclaredLimitEntry struct {
	limit     int
	expiresAt time.Time
}

// The declaration belongs to the upstream credential, not to one socket: a
// pooled connection may be reused after its handshake headers are gone from the
// request path, and a bridged turn must still see the limit learned earlier.
type openAIWSDeclaredLimitCache struct {
	mu      sync.Mutex
	entries map[string]openAIWSDeclaredLimitEntry
	now     func() time.Time
}

func newOpenAIWSDeclaredLimitCache() *openAIWSDeclaredLimitCache {
	return &openAIWSDeclaredLimitCache{
		entries: make(map[string]openAIWSDeclaredLimitEntry, 64),
		now:     time.Now,
	}
}

var openAIWSDeclaredLimits = newOpenAIWSDeclaredLimitCache()

func (c *openAIWSDeclaredLimitCache) remember(key string, limit int) {
	if c == nil || key == "" || limit <= 0 {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= openAIWSDeclaredLimitEntries {
		for candidate, entry := range c.entries {
			if !entry.expiresAt.After(now) {
				delete(c.entries, candidate)
			}
		}
		for candidate := range c.entries {
			if len(c.entries) < openAIWSDeclaredLimitEntries {
				break
			}
			delete(c.entries, candidate)
		}
	}
	c.entries[key] = openAIWSDeclaredLimitEntry{limit: limit, expiresAt: now.Add(openAIWSDeclaredLimitCacheTTL)}
}

func (c *openAIWSDeclaredLimitCache) lookup(key string) (int, bool) {
	if c == nil || key == "" {
		return 0, false
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return 0, false
	}
	if !entry.expiresAt.After(now) {
		delete(c.entries, key)
		return 0, false
	}
	return entry.limit, true
}

func (c *openAIWSDeclaredLimitCache) forget(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *openAIWSDeclaredLimitCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]openAIWSDeclaredLimitEntry, 64)
}

func openAIWSDeclaredLimitAccountKey(account *Account) string {
	if account == nil || account.ID <= 0 {
		return ""
	}
	return strconv.FormatInt(account.ID, 10)
}

// rememberOpenAIWSDeclaredMaxMessageBytes is called at dial time, where only the
// account ID is available. Every transport path (ingress, v2, passthrough) then
// sees the same declaration for that upstream account.
func rememberOpenAIWSDeclaredMaxMessageBytes(accountID int64, headers http.Header) {
	if accountID <= 0 {
		return
	}
	limit, ok := parseOpenAIWSDeclaredMaxMessageBytes(headers)
	if !ok {
		return
	}
	openAIWSDeclaredLimits.remember(strconv.FormatInt(accountID, 10), limit)
}

// noteOpenAIWSDeclaredMessageLimit records what this connection's handshake
// declared. A connection without the header clears nothing: the cached value may
// still describe the same credential after a header-less reconnect.
func (s *OpenAIGatewayService) noteOpenAIWSDeclaredMessageLimit(account *Account, lease *openAIWSConnLease) (int, bool) {
	limit, ok := openAIWSDeclaredMaxMessageBytesFromLease(lease)
	if !ok {
		return 0, false
	}
	openAIWSDeclaredLimits.remember(openAIWSDeclaredLimitAccountKey(account), limit)
	return limit, true
}

func (s *OpenAIGatewayService) openAIWSDeclaredMessageLimitForAccount(account *Account) (int, bool) {
	return openAIWSDeclaredLimits.lookup(openAIWSDeclaredLimitAccountKey(account))
}

// openAIWSDeclaredLimitBlocksPayload reports whether a known declaration already
// rules out the WebSocket transport for this payload. It is the pre-dial form of
// the write-time check, so a repeated oversize turn never dials a socket it must
// abandon.
func (s *OpenAIGatewayService) openAIWSDeclaredLimitBlocksPayload(account *Account, payloadBytes int) bool {
	limit, ok := s.openAIWSDeclaredMessageLimitForAccount(account)
	if !ok || limit <= 0 {
		return false
	}
	return payloadBytes > limit
}

type openAIWSDeclaredLimitExceededError struct {
	limit  int
	length int
}

func (e *openAIWSDeclaredLimitExceededError) Error() string {
	if e == nil {
		return "upstream websocket message limit exceeded"
	}
	return fmt.Sprintf("request is %d bytes but upstream accepts at most %d bytes per websocket message", e.length, e.limit)
}

func isOpenAIWSDeclaredLimitExceeded(err error) bool {
	var target *openAIWSDeclaredLimitExceededError
	return errors.As(err, &target)
}

// openAIWSDeclaredLimitFromError exposes the declared limit for logging without
// re-reading the lease, which may already be released at that point.
func openAIWSDeclaredLimitFromError(err error) int {
	var target *openAIWSDeclaredLimitExceededError
	if errors.As(err, &target) && target != nil {
		return target.limit
	}
	return 0
}

// checkOpenAIWSDeclaredMessageLimit returns a fallback-classified error so the
// caller abandons the WebSocket transport for this payload. Retrying over
// WebSocket cannot succeed: the payload, not the connection, is too large.
func (s *OpenAIGatewayService) checkOpenAIWSDeclaredMessageLimit(account *Account, lease *openAIWSConnLease, payloadBytes int) error {
	limit, ok := s.noteOpenAIWSDeclaredMessageLimit(account, lease)
	if !ok || limit <= 0 || payloadBytes <= limit {
		return nil
	}
	return wrapOpenAIWSFallback(openAIWSReasonRequestTooLarge, &openAIWSDeclaredLimitExceededError{
		limit:  limit,
		length: payloadBytes,
	})
}

// isOpenAIWSDeclaredLimitFallbackReason matches both a locally detected oversize
// payload and an upstream 1009 close. Both resolve by replaying over HTTP.
func isOpenAIWSDeclaredLimitFallbackReason(reason string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(reason), "prewarm_")
	return base == openAIWSReasonRequestTooLarge
}
