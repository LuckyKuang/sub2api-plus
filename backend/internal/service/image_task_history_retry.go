package service

import (
	"context"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	defaultImageTaskHistoryRetryInterval = 30 * time.Second
	imageTaskHistoryRetryBatchLimit      = 20
	imageTaskHistoryRetryMaxBackoff      = 5 * time.Minute
)

// ImageTaskHistoryRetryService re-applies terminal task records whose PostgreSQL
// write failed. The payload lives in the database, so a process restart does not
// lose the result index; only the compact record is stored.
type ImageTaskHistoryRetryService struct {
	history  ImageTaskHistoryRepository
	retry    ImageTaskHistoryRetryRepository
	interval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewImageTaskHistoryRetryService(history ImageTaskHistoryRepository, retry ImageTaskHistoryRetryRepository) *ImageTaskHistoryRetryService {
	return &ImageTaskHistoryRetryService{history: history, retry: retry, interval: defaultImageTaskHistoryRetryInterval}
}

// Enqueue records one failed history write for later compensation.
func (s *ImageTaskHistoryRetryService) Enqueue(ctx context.Context, record *ImageTaskRecord) error {
	if s == nil || s.retry == nil {
		return ErrImageTaskUnavailable
	}
	return s.retry.Enqueue(ctx, record)
}

// RunOnce retries every due record. Success removes the row; failure reschedules
// it with exponential backoff capped at five minutes.
func (s *ImageTaskHistoryRetryService) RunOnce(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.retry == nil || s.history == nil {
		return 0, nil
	}
	pending, err := s.retry.Due(ctx, imageTaskHistoryRetryBatchLimit)
	if err != nil {
		return 0, err
	}
	saved := 0
	for _, item := range pending {
		record := item.Record
		if record == nil || record.ID == "" {
			continue
		}
		if saveErr := s.history.Save(ctx, record); saveErr != nil {
			_ = s.retry.Reschedule(ctx, record.ID, now.Add(imageTaskHistoryRetryBackoff(item.Attempts)))
			logger.L().Warn("image_task.history_retry_failed", zap.String("task_id", record.ID), zap.Int("attempts", item.Attempts+1), zap.Error(saveErr))
			continue
		}
		if deleteErr := s.retry.Delete(ctx, record.ID); deleteErr != nil {
			logger.L().Warn("image_task.history_retry_delete_failed", zap.String("task_id", record.ID), zap.Error(deleteErr))
			continue
		}
		saved++
	}
	return saved, nil
}

func imageTaskHistoryRetryBackoff(attempts int) time.Duration {
	delay := time.Duration(5) * time.Second
	for i := 0; i < attempts && delay < imageTaskHistoryRetryMaxBackoff; i++ {
		delay *= 2
	}
	if delay > imageTaskHistoryRetryMaxBackoff {
		delay = imageTaskHistoryRetryMaxBackoff
	}
	return delay
}

// Start launches the retry loop. It is safe to call once per process.
func (s *ImageTaskHistoryRetryService) Start() {
	if s == nil || s.retry == nil || s.history == nil || s.interval <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			_, _ = s.RunOnce(ctx, time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Stop ends the loop; tests use it to avoid leaking goroutines.
func (s *ImageTaskHistoryRetryService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
