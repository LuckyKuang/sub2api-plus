package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// qualityArtifactMaxBytes 单个作品对象上限，与探测输出上限保持同一量级。
const qualityArtifactMaxBytes = 8 << 20

// QualityArtifactStore 作品对象存储：与异步生图共用同一存储桶与凭证。
type QualityArtifactStore struct {
	resolve ImageStorageResolver
}

// NewQualityArtifactStore 注入对象存储解析器（来自 ImageStorageSettingService.Resolver）。
func NewQualityArtifactStore(resolve ImageStorageResolver) *QualityArtifactStore {
	return &QualityArtifactStore{resolve: resolve}
}

func (s *QualityArtifactStore) uploader() (*ImageResultUploader, error) {
	if s == nil || s.resolve == nil {
		return nil, ErrQualityStorageUnavailable
	}
	uploader, _ := s.resolve()
	if uploader == nil || uploader.storage == nil {
		return nil, ErrQualityStorageUnavailable
	}
	return uploader, nil
}

// Available 报告对象存储是否已配置且可用（被关闭时仍返回 true，以允许历史读取）。
func (s *QualityArtifactStore) Available() bool {
	_, err := s.uploader()
	return err == nil
}

// Save 写入作品 HTML，返回对象键与字节数。
func (s *QualityArtifactStore) Save(ctx context.Context, planID, runID, resultID int64, html string) (string, int64, bool, error) {
	uploader, err := s.uploader()
	if err != nil {
		return "", 0, false, err
	}
	truncated := false
	if len(html) > qualityArtifactMaxBytes {
		html = html[:qualityArtifactMaxBytes]
		truncated = true
	}
	name := fmt.Sprintf("%d/%d-%d.html", planID, runID, resultID)
	// 与生图共享桶但不共享前缀：<prefix>/quality-inspection/<yyyy/mm/dd>/...
	prefix := strings.TrimRight(uploader.prefix, "/")
	if prefix == "" {
		prefix = "quality-inspection"
	} else {
		prefix += "/quality-inspection"
	}
	key := buildObjectStorageKey(prefix, "quality-inspection", uploader.appendDatePath, time.Now(), name)
	if _, err := uploader.storage.Save(ctx, key, "text/html; charset=utf-8", []byte(html)); err != nil {
		return "", 0, truncated, err
	}
	return key, int64(len(html)), truncated, nil
}

// Open 读取作品字节。作品始终经本服务路由转出，不把预签名 URL 交给浏览器。
func (s *QualityArtifactStore) Open(ctx context.Context, key string) ([]byte, error) {
	uploader, err := s.uploader()
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("artifact key is empty")
	}
	reader, ok := uploader.storage.(ImageStorageReader)
	if !ok {
		return nil, errors.New("object storage does not support reading stored objects")
	}
	object, err := reader.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = object.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(object.Body, qualityArtifactMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > qualityArtifactMaxBytes {
		return nil, errors.New("artifact exceeds size limit")
	}
	return data, nil
}

// Delete 删除作品对象。适配器未实现删除接口时返回 errQualityDeleteUnsupported。
var errQualityDeleteUnsupported = errors.New("object storage does not support deletion")

func (s *QualityArtifactStore) Delete(ctx context.Context, key string) error {
	uploader, err := s.uploader()
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	deleter, ok := uploader.storage.(ImageStorageDeleter)
	if !ok {
		return errQualityDeleteUnsupported
	}
	return deleter.Delete(ctx, key)
}
