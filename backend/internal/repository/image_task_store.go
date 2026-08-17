package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"

type imageTaskStore struct {
	rdb *redis.Client
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *imageTaskStore) DeleteIfStatus(ctx context.Context, id, status string) (bool, bool, error) {
	key := imageTaskKey(id)
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var deleted, exists bool
		err := s.rdb.Watch(ctx, func(tx *redis.Tx) error {
			data, err := tx.Get(ctx, key).Bytes()
			if err == redis.Nil {
				return nil
			}
			if err != nil {
				return err
			}
			exists = true
			var task struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(data, &task); err != nil {
				return err
			}
			if task.Status != status {
				return nil
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, key)
				return nil
			})
			if err == nil {
				deleted = true
			}
			return err
		}, key)
		if err == redis.TxFailedErr {
			continue
		}
		return deleted, exists, err
	}
	return false, false, redis.TxFailedErr
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

var _ service.ImageTaskStore = (*imageTaskStore)(nil)
