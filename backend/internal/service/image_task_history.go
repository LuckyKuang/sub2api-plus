package service

import (
	"context"
	"strings"
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
	PromptPreview   string
	RequestedImages int
}

// ImageTaskHistoryFilter limits a task list to the authenticated API key.
type ImageTaskHistoryFilter struct {
	Status string
	Limit  int
	Offset int
}

// ImageTaskHistoryRepository persists compact async task state. Redis remains
// the short-lived execution store; this repository exists for the user list.
type ImageTaskHistoryRepository interface {
	Save(ctx context.Context, task *ImageTaskRecord) error
	List(ctx context.Context, owner ImageTaskOwner, filter ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error)
	Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTaskRecord, error)
	ListByUser(ctx context.Context, userID int64, filter ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error)
	GetByUser(ctx context.Context, userID int64, id string) (*ImageTaskRecord, error)
	DeleteFailed(ctx context.Context, owner ImageTaskOwner, id string) (bool, error)
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
