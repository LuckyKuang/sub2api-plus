package zcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Off-peak ticket server. The ZCode idle plan is gated by a ticket issued by the
// platform: a model request is only admitted while the ticket is ready or active,
// and a settled ticket frees the take-number quota.
const (
	// DefaultOffPeakBaseURL is the ZCode platform API root that serves the
	// off-peak ticket routes.
	DefaultOffPeakBaseURL = "https://zcode.z.ai/api/v1"

	// EnvOffPeakBaseURL overrides DefaultOffPeakBaseURL.
	EnvOffPeakBaseURL = "ZHIPU_OFFPEAK_BASE_URL"

	offPeakPathPrefix = "/off-peak"

	// OffPeakRequestTimeout bounds one ticket call. The official client uses the
	// same 10s budget.
	OffPeakRequestTimeout = 10 * time.Second
)

// Ticket admission states reported by the platform.
const (
	TicketQueued   = "queued"
	TicketReady    = "ready"
	TicketActive   = "active"
	TicketExpired  = "expired"
	TicketSettled  = "settled"
	TicketNotFound = "not_found"
)

// Off-peak business codes the gateway must react to.
const (
	// OffPeakCodeNoEligibility reports that the account has no eligible coding
	// plan for the idle queue.
	OffPeakCodeNoEligibility = 3101
	// OffPeakCodeQuotaExhausted reports that the take-number quota is spent; the
	// response carries the next allowed take time.
	OffPeakCodeQuotaExhausted = 3103
	// OffPeakCodeTicketUnusable is returned by the model endpoint when the ticket
	// expired, was settled, or does not belong to the caller.
	OffPeakCodeTicketUnusable = 3102
	// OffPeakCodeLegacyTicketUnusable is the rolling-release equivalent of 3102.
	OffPeakCodeLegacyTicketUnusable = 3001
	// OffPeakCodeQueued is the model-endpoint queue valve; Retry-After applies.
	OffPeakCodeQueued = 3105
)

// Ticket returns whether the state permits a model request.
func (t *OffPeakTicket) Admitted() bool {
	if t == nil {
		return false
	}
	return t.State == TicketReady || t.State == TicketActive
}

// Terminal reports a state that cannot become admitted again.
func (t *OffPeakTicket) Terminal() bool {
	if t == nil {
		return true
	}
	switch t.State {
	case TicketSettled, TicketNotFound:
		return true
	default:
		return false
	}
}

// OffPeakTicket is one ticket snapshot.
type OffPeakTicket struct {
	TicketID string
	TaskID   string
	State    string
	// Position is the queue position while queued. It is nil once the platform
	// stops reporting a number.
	Position *int
	// NextPollAfter is the platform-requested poll delay, converted from the
	// wire's seconds.
	NextPollAfter time.Duration
	// ActiveDeadline bounds an active segment. The platform reports it but the
	// official client never consumes it; expiry surfaces as code 3102.
	ActiveDeadline time.Time
}

// OffPeakAvailability is the take-number quota snapshot.
type OffPeakAvailability struct {
	CanTakeNumber bool
	NextTakeAt    time.Time
}

// OffPeakError is a classified ticket-server rejection.
type OffPeakError struct {
	HTTPStatus int
	Code       int
	Message    string
	// RetryAfter is set for the queue valve (3105).
	RetryAfter time.Duration
	// NextTakeAt is set when the platform reports when the take quota resets.
	NextTakeAt time.Time
}

func (e *OffPeakError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "off-peak ticket request rejected"
	}
	if e.Code != 0 {
		return fmt.Sprintf("%s (code %d)", message, e.Code)
	}
	return message
}

// OffPeakClient speaks the ticket protocol.
type OffPeakClient struct {
	client  HTTPDoer
	baseURL string
	timeout time.Duration
}

// NewOffPeakClient builds a ticket client. An empty baseURL falls back to the
// published root.
func NewOffPeakClient(client HTTPDoer, baseURL string) *OffPeakClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &OffPeakClient{
		client:  client,
		baseURL: normalizeOrigin(baseURL, DefaultOffPeakBaseURL),
		timeout: OffPeakRequestTimeout,
	}
}

// ResolveOffPeakBaseURL reads the optional ticket-root override.
func ResolveOffPeakBaseURL() string {
	return normalizeOrigin(os.Getenv(EnvOffPeakBaseURL), DefaultOffPeakBaseURL)
}

// OffPeakAuth is the credential snapshot every ticket call carries. A ticket is
// issued to a specific plan, so the same snapshot must be used for take, status,
// settle and the model request that consumes the ticket.
type OffPeakAuth struct {
	JWT       string
	PlanKey   string
	TeamOrg   string
	TeamProj  string
	RequestID string
}

// Headers renders the ticket-call headers.
func (a OffPeakAuth) Headers() map[string]string {
	headers := map[string]string{
		"Authorization":         "Bearer " + strings.TrimSpace(a.JWT),
		"X-Coding-Plan-Api-Key": strings.TrimSpace(a.PlanKey),
		"Accept":                "application/json",
	}
	if org, project := strings.TrimSpace(a.TeamOrg), strings.TrimSpace(a.TeamProj); org != "" && project != "" {
		headers["bigmodel-organization"] = org
		headers["bigmodel-project"] = project
	}
	if requestID := strings.TrimSpace(a.RequestID); requestID != "" {
		headers["x-request-id"] = requestID
	}
	return headers
}

// Availability reads the take-number quota snapshot.
func (c *OffPeakClient) Availability(ctx context.Context, auth OffPeakAuth) (*OffPeakAvailability, error) {
	raw, err := c.do(ctx, http.MethodGet, offPeakPathPrefix+"/ticket/availability", auth, nil, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		CanTakeNumber *bool  `json:"can_take_number"`
		NextTakeAt    *int64 `json:"next_take_at"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode off-peak availability: %w", err)
	}
	if payload.CanTakeNumber == nil {
		return nil, fmt.Errorf("invalid off-peak availability response")
	}
	availability := &OffPeakAvailability{CanTakeNumber: *payload.CanTakeNumber}
	if payload.NextTakeAt != nil && *payload.NextTakeAt > 0 {
		availability.NextTakeAt = unixFromSecondsOrMillis(*payload.NextTakeAt)
	}
	// The contract requires next_take_at whenever the quota is unavailable, so a
	// bare false is a dirty response rather than a definitive "no".
	if !availability.CanTakeNumber && availability.NextTakeAt.IsZero() {
		return nil, fmt.Errorf("off-peak availability reported unavailable without a reset time")
	}
	return availability, nil
}

// TakeTicket requests a ticket for taskID. It never blocks: the platform answers
// with the current admission state.
func (c *OffPeakClient) TakeTicket(ctx context.Context, taskID string, auth OffPeakAuth) (*OffPeakTicket, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("off-peak task id is required")
	}
	body, err := json.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, offPeakPathPrefix+"/ticket", auth, body, nil)
	if err != nil {
		return nil, err
	}
	var payload offPeakTicketWire
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode off-peak ticket: %w", err)
	}
	ticket := payload.normalize()
	if ticket.TicketID == "" || ticket.State == "" {
		return nil, fmt.Errorf("invalid off-peak ticket response")
	}
	return ticket, nil
}

// TicketStatus reads the current state of the given tickets.
func (c *OffPeakClient) TicketStatus(ctx context.Context, ticketIDs []string, auth OffPeakAuth) ([]OffPeakTicket, time.Duration, error) {
	deduped := make([]string, 0, len(ticketIDs))
	seen := map[string]struct{}{}
	for _, id := range ticketIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		return nil, 0, fmt.Errorf("off-peak ticket ids are required")
	}
	body, err := json.Marshal(map[string][]string{"ticket_ids": deduped})
	if err != nil {
		return nil, 0, err
	}
	raw, err := c.do(ctx, http.MethodPost, offPeakPathPrefix+"/ticket/status", auth, body, nil)
	if err != nil {
		return nil, 0, err
	}
	var payload struct {
		NextPollAfter *float64            `json:"next_poll_after"`
		Tickets       []offPeakTicketWire `json:"tickets"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, 0, fmt.Errorf("decode off-peak ticket status: %w", err)
	}
	tickets := make([]OffPeakTicket, 0, len(payload.Tickets))
	for _, item := range payload.Tickets {
		tickets = append(tickets, *item.normalize())
	}
	return tickets, secondsToDuration(payload.NextPollAfter), nil
}

// SettleTicket reports a finished ticket. The platform is idempotent and treats
// unknown tickets as an acknowledgement, so a failed settle is recoverable.
func (c *OffPeakClient) SettleTicket(ctx context.Context, ticketID string, auth OffPeakAuth) error {
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return fmt.Errorf("off-peak ticket id is required")
	}
	_, err := c.do(ctx, http.MethodPost, offPeakPathPrefix+"/ticket/"+url.PathEscape(ticketID)+"/settle", auth, nil, nil)
	return err
}

// offPeakTicketWire is the wire shape of a ticket. The official client accepts
// both snake_case and the queued-position null form, and the platform has been
// observed to omit optional fields entirely.
type offPeakTicketWire struct {
	TicketID       string   `json:"ticket_id"`
	TaskID         string   `json:"task_id"`
	State          string   `json:"state"`
	Position       *int     `json:"position"`
	NextPollAfter  *float64 `json:"next_poll_after"`
	ActiveDeadline *int64   `json:"active_deadline"`
	ReadyDeadline  *int64   `json:"ready_deadline"`
}

func (w offPeakTicketWire) normalize() *OffPeakTicket {
	ticket := &OffPeakTicket{
		TicketID:      strings.TrimSpace(w.TicketID),
		TaskID:        strings.TrimSpace(w.TaskID),
		State:         strings.TrimSpace(w.State),
		NextPollAfter: secondsToDuration(w.NextPollAfter),
	}
	if w.Position != nil {
		position := *w.Position
		ticket.Position = &position
	}
	if deadline := w.ActiveDeadline; deadline != nil && *deadline > 0 {
		ticket.ActiveDeadline = unixFromSecondsOrMillis(*deadline)
	}
	return ticket
}

func secondsToDuration(seconds *float64) time.Duration {
	if seconds == nil || *seconds <= 0 {
		return 0
	}
	return time.Duration(*seconds * float64(time.Second))
}

// unixFromSecondsOrMillis accepts either unit. The platform reports
// `next_poll_after` in seconds but deadlines in milliseconds, and the wire has
// changed units before, so a magnitude test is safer than a fixed assumption.
func unixFromSecondsOrMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 1_000_000_000_000 {
		return time.Unix(value, 0)
	}
	return time.UnixMilli(value)
}

// do performs one ticket call, unwrapping the platform envelope and classifying
// business rejections.
func (c *OffPeakClient) do(ctx context.Context, method, path string, auth OffPeakAuth, body []byte, _ map[string]string) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = OffPeakRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	for name, value := range auth.Headers() {
		req.Header.Set(name, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyOffPeakFailure(resp, raw)
	}
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode off-peak envelope: %w", err)
	}
	// The platform unwraps only a zero code with a data payload; anything else is
	// a business rejection even on HTTP 200.
	if code := parseOffPeakCode(envelope.Code); code != 0 {
		return nil, &OffPeakError{HTTPStatus: resp.StatusCode, Code: code, Message: envelope.Msg}
	}
	if len(envelope.Data) == 0 {
		return json.RawMessage("{}"), nil
	}
	return envelope.Data, nil
}

// classifyOffPeakFailure turns a non-2xx response into a typed error, carrying
// the queue valve's Retry-After and the quota reset time.
func classifyOffPeakFailure(resp *http.Response, raw []byte) error {
	var payload struct {
		Code       json.RawMessage `json:"code"`
		Msg        string          `json:"msg"`
		Message    string          `json:"message"`
		NextTakeAt *int64          `json:"next_take_at"`
		Data       *struct {
			NextTakeAt *int64 `json:"next_take_at"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &payload)
	failure := &OffPeakError{
		HTTPStatus: resp.StatusCode,
		Code:       parseOffPeakCode(payload.Code),
		Message:    firstNonEmptyString(payload.Msg, payload.Message),
	}
	if payload.NextTakeAt != nil {
		failure.NextTakeAt = unixFromSecondsOrMillis(*payload.NextTakeAt)
	} else if payload.Data != nil && payload.Data.NextTakeAt != nil {
		failure.NextTakeAt = unixFromSecondsOrMillis(*payload.Data.NextTakeAt)
	}
	failure.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	return failure
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func parseOffPeakCode(raw json.RawMessage) int {
	trimmed := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if trimmed == "" || trimmed == "null" {
		return 0
	}
	code, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0
	}
	return code
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
