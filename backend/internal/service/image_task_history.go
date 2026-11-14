package service

import (
	"context"
	"strings"
	"time"
)

const (
	imageTaskDefaultListLimit = 20
	imageTaskMaxListLimit     = 100
	imageTaskPromptPreviewMax = 500
)

// ImageTaskMetadata contains the non-sensitive request fields needed by the
// user-facing task list. The original request payload and API key never enter
// the task history.
type ImageTaskMetadata struct {
	RequestType     string
	Model           string
	Platform        string
	PromptPreview   string
	RequestedImages int
}

// ImageTaskHistoryFilter limits a task list. Owner scoping is applied by the
// caller; an owner without an API key ID lists every key of that user.
type ImageTaskHistoryFilter struct {
	Status   string
	Platform string
	Model    string
	Since    int64
	Until    int64
	Limit    int
	Offset   int
}

// ImageTaskHistoryRepository persists compact async task state. Redis remains
// the short-lived execution store; this repository serves the user list and
// history reads after the execution record expires.
type ImageTaskHistoryRepository interface {
	Save(ctx context.Context, task *ImageTaskRecord) error
	List(ctx context.Context, owner ImageTaskOwner, filter ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error)
	Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTaskRecord, error)
	DeleteFailed(ctx context.Context, owner ImageTaskOwner, id string) (bool, error)
	// HasAnyTaskForUser reports whether the user owns any ordinary image task.
	HasAnyTaskForUser(ctx context.Context, userID int64) (bool, error)
}

// ImageTaskHistoryRetryRepository persists records whose history write failed.
// The payload is the compact task record: results and object keys, never image
// bytes, so a restart can finish the write without keeping the cache record.
// ImageTaskHistoryRetry is one pending compensation attempt.
type ImageTaskHistoryRetry struct {
	Record   *ImageTaskRecord
	Attempts int
}

type ImageTaskHistoryRetryRepository interface {
	Enqueue(ctx context.Context, record *ImageTaskRecord) error
	Due(ctx context.Context, limit int) ([]ImageTaskHistoryRetry, error)
	Delete(ctx context.Context, taskID string) error
	// Reschedule pushes the row out and increments its attempt counter.
	Reschedule(ctx context.Context, taskID string, next time.Time) error
}

// ImageTaskHistoryRetryQueue is the narrow compensation hook the task service
// uses so a failed history write is not silently lost with the Redis TTL.
type ImageTaskHistoryRetryQueue interface {
	Enqueue(ctx context.Context, record *ImageTaskRecord) error
}

// ImageTaskListResponse is the OpenAI-compatible task collection returned by
// GET /images/tasks. Each task remains scoped to the API key that created it.
type ImageTaskListResponse struct {
	Object  string       `json:"object"`
	Data    []*ImageTask `json:"data"`
	HasMore bool         `json:"has_more"`
}

func normalizeImageTaskMetadata(in ImageTaskMetadata) ImageTaskMetadata {
	in.RequestType = strings.TrimSpace(in.RequestType)
	if in.RequestType != "edit" {
		in.RequestType = "generation"
	}
	in.Model = truncateImageTaskText(strings.TrimSpace(in.Model), 128)
	in.Platform = strings.ToLower(truncateImageTaskText(strings.TrimSpace(in.Platform), 32))
	in.PromptPreview = truncateImageTaskText(strings.TrimSpace(in.PromptPreview), imageTaskPromptPreviewMax)
	if in.RequestedImages <= 0 {
		in.RequestedImages = 1
	}
	return in
}

func truncateImageTaskText(value string, max int) string {
	if max <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func normalizeImageTaskHistoryFilter(filter ImageTaskHistoryFilter) ImageTaskHistoryFilter {
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Platform = strings.ToLower(strings.TrimSpace(filter.Platform))
	filter.Model = truncateImageTaskText(strings.TrimSpace(filter.Model), 128)
	if filter.Since < 0 {
		filter.Since = 0
	}
	if filter.Until < 0 {
		filter.Until = 0
	}
	if filter.Limit <= 0 {
		filter.Limit = imageTaskDefaultListLimit
	}
	if filter.Limit > imageTaskMaxListLimit {
		filter.Limit = imageTaskMaxListLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}
