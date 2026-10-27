package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	GatewayTaskKindBatchImage        = "batch_image"
	GatewayTaskKindAsyncImage        = "async_image"
	GatewayTaskKindGrokVideo         = "grok_video"
	GatewayTaskKindSeedance          = "seedance"
	DefaultMaxUnfinishedTasksPerUser = 100
)

var (
	ErrGatewayUnfinishedTaskLimit       = infraerrors.New(http.StatusTooManyRequests, "UNFINISHED_TASK_LIMIT", "User unfinished task limit reached; wait for existing tasks to finish or cancel them")
	ErrGatewayUnfinishedTaskUnavailable = infraerrors.New(http.StatusServiceUnavailable, "UNFINISHED_TASK_UNAVAILABLE", "Unable to confirm unfinished task capacity; please retry later")
	ErrGatewayTaskReservationConflict   = infraerrors.New(http.StatusConflict, "TASK_RESERVATION_CONFLICT", "Task reservation is missing or has a different owner or binding")
)

type GatewayTaskOwner struct{ UserID, APIKeyID int64 }

type GatewayTaskReservation struct {
	ID    string
	Owner GatewayTaskOwner
	Kind  string
}

// Reserve must serialize by user and persist the capacity check plus insert in
// one transaction. No process-local counter or TTL may free an upstream task.
type GatewayUnfinishedTaskRepository interface {
	Reserve(context.Context, *GatewayTaskReservation, int) error
	Bind(context.Context, *GatewayTaskReservation, string) error
	MarkUncertain(context.Context, *GatewayTaskReservation, string) error
	Release(context.Context, *GatewayTaskReservation, string) error
	ReleaseTask(context.Context, GatewayTaskOwner, string, string, string) error
}

type GatewayUnfinishedTaskService struct {
	repo  GatewayUnfinishedTaskRepository
	limit int
}

func NewGatewayUnfinishedTaskService(repo GatewayUnfinishedTaskRepository, cfg *config.Config) *GatewayUnfinishedTaskService {
	limit := DefaultMaxUnfinishedTasksPerUser
	if cfg != nil && cfg.Gateway.MaxUnfinishedTasksPerUser > 0 {
		limit = cfg.Gateway.MaxUnfinishedTasksPerUser
	}
	return &GatewayUnfinishedTaskService{repo: repo, limit: limit}
}

func validGatewayTaskKind(kind string) bool {
	switch kind {
	case GatewayTaskKindBatchImage, GatewayTaskKindAsyncImage, GatewayTaskKindGrokVideo, GatewayTaskKindSeedance:
		return true
	default:
		return false
	}
}

func validGatewayTaskReservation(r *GatewayTaskReservation) bool {
	return r != nil && r.ID != "" && r.Owner.UserID > 0 && r.Owner.APIKeyID > 0 && validGatewayTaskKind(r.Kind)
}

func (s *GatewayUnfinishedTaskService) Reserve(ctx context.Context, owner GatewayTaskOwner, kind string) (*GatewayTaskReservation, error) {
	if s == nil || s.repo == nil || owner.UserID <= 0 || owner.APIKeyID <= 0 || !validGatewayTaskKind(kind) {
		return nil, ErrGatewayUnfinishedTaskUnavailable
	}
	r := &GatewayTaskReservation{ID: uuid.NewString(), Owner: owner, Kind: kind}
	if err := s.repo.Reserve(ctx, r, s.limit); err != nil {
		return nil, gatewayUnfinishedTaskError(err)
	}
	return r, nil
}

func (s *GatewayUnfinishedTaskService) Bind(ctx context.Context, r *GatewayTaskReservation, taskID string) error {
	if s == nil || s.repo == nil || !validGatewayTaskReservation(r) || strings.TrimSpace(taskID) == "" {
		return ErrGatewayUnfinishedTaskUnavailable
	}
	return gatewayUnfinishedTaskError(s.repo.Bind(ctx, r, strings.TrimSpace(taskID)))
}

func (s *GatewayUnfinishedTaskService) MarkUncertain(ctx context.Context, r *GatewayTaskReservation, reason string) error {
	if s == nil || s.repo == nil || !validGatewayTaskReservation(r) {
		return ErrGatewayUnfinishedTaskUnavailable
	}
	return gatewayUnfinishedTaskError(s.repo.MarkUncertain(ctx, r, gatewayTaskReason(reason)))
}

func (s *GatewayUnfinishedTaskService) Release(ctx context.Context, r *GatewayTaskReservation, reason string) error {
	if s == nil || s.repo == nil || !validGatewayTaskReservation(r) {
		return ErrGatewayUnfinishedTaskUnavailable
	}
	return gatewayUnfinishedTaskError(s.repo.Release(ctx, r, gatewayTaskReason(reason)))
}

func (s *GatewayUnfinishedTaskService) ReleaseTask(ctx context.Context, owner GatewayTaskOwner, kind, taskID, reason string) error {
	if s == nil || s.repo == nil || owner.UserID <= 0 || owner.APIKeyID <= 0 || !validGatewayTaskKind(kind) || strings.TrimSpace(taskID) == "" {
		return ErrGatewayUnfinishedTaskUnavailable
	}
	return gatewayUnfinishedTaskError(s.repo.ReleaseTask(ctx, owner, kind, strings.TrimSpace(taskID), gatewayTaskReason(reason)))
}

func gatewayTaskReason(reason string) string {
	// Reasons are stable internal codes, never upstream bodies or user text.
	if len(reason) > 80 || reason == "" {
		return "unspecified"
	}
	for _, c := range reason {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return "unspecified"
	}
	return reason
}

func gatewayUnfinishedTaskError(err error) error {
	if err == nil || errors.Is(err, ErrGatewayUnfinishedTaskLimit) || errors.Is(err, ErrGatewayTaskReservationConflict) || errors.Is(err, ErrGatewayUnfinishedTaskUnavailable) {
		return err
	}
	return ErrGatewayUnfinishedTaskUnavailable.WithCause(err)
}
