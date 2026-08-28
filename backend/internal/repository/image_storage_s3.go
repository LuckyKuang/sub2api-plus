package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/servertiming"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"go.uber.org/zap"
)

// S3ImageStorage 用 S3 兼容对象存储实现 service.ImageStorage。
type S3ImageStorage struct {
	client        *s3.Client
	bucket        string
	prefix        string
	publicBaseURL string
	presignExpiry time.Duration
}

var _ service.ImageStorage = (*S3ImageStorage)(nil)
var _ service.ImageStorageHealthChecker = (*S3ImageStorage)(nil)
var _ service.ImageStorageReader = (*S3ImageStorage)(nil)

// NewS3ImageStorage 依据配置构造 S3 图片存储（调用方应先确认 cfg.Active()）。
func NewS3ImageStorage(ctx context.Context, cfg *config.ImageStorageConfig) (*S3ImageStorage, error) {
	client, err := newS3Client(ctx, s3ClientParams{
		Endpoint:        cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		ForcePathStyle:  cfg.ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}

	expiry := time.Duration(cfg.PresignExpiry) * time.Hour
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}

	return &S3ImageStorage{
		client:        client,
		bucket:        cfg.Bucket,
		prefix:        strings.Trim(cfg.Prefix, "/"),
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
		presignExpiry: expiry,
	}, nil
}

// Save 上传图片字节，返回可访问 URL：配了 public_base_url 则返回公开直链，否则返回 presigned 临时链接。
func (s *S3ImageStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	finish := servertiming.ObserveDependency(ctx, "s3")
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	finish()
	if err != nil {
		return "", fmt.Errorf("S3 PutObject: %w", err)
	}

	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + strings.TrimLeft(key, "/"), nil
	}

	presignClient := s3.NewPresignClient(s.client)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(s.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return result.URL, nil
}

// Open reads a previously stored image object. The service layer constructs the
// key from a task ID and index; callers never use user-supplied object paths.
func (s *S3ImageStorage) Open(ctx context.Context, key string) (*service.ImageStorageObject, error) {
	if s == nil || s.client == nil || strings.TrimSpace(s.bucket) == "" {
		return nil, fmt.Errorf("S3 image storage is not configured")
	}
	finish := servertiming.ObserveDependency(ctx, "s3")
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	finish()
	if err != nil {
		return nil, fmt.Errorf("S3 GetObject: %w", err)
	}
	contentType := ""
	if result.ContentType != nil {
		contentType = *result.ContentType
	}
	size := int64(-1)
	if result.ContentLength != nil {
		size = *result.ContentLength
	}
	return &service.ImageStorageObject{Body: result.Body, ContentType: contentType, Size: size}, nil
}

// Check verifies the bucket and the exact object permissions async image tasks
// need. Constructing an AWS SDK client alone is lazy and does not validate the
// endpoint, credentials, bucket, or write policy.
func (s *S3ImageStorage) Check(ctx context.Context) error {
	if s == nil || s.client == nil || strings.TrimSpace(s.bucket) == "" {
		return fmt.Errorf("S3 image storage is not configured")
	}

	finish := servertiming.ObserveDependency(ctx, "s3")
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	finish()
	if err != nil {
		return fmt.Errorf("S3 HeadBucket failed: %w", err)
	}

	key := ".healthcheck/" + uuid.NewString()
	if s.prefix != "" {
		key = s.prefix + "/" + key
	}
	contentType := "application/octet-stream"
	finish = servertiming.ObserveDependency(ctx, "s3")
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(nil),
		ContentType: aws.String(contentType),
	})
	finish()
	if err != nil {
		return fmt.Errorf("S3 PutObject health check failed: %w", err)
	}

	cleanupNeeded := true
	// Delete the probe when a subsequent validation step fails so an unsuccessful
	// check never leaves an accumulating object under the user-facing prefix.
	defer func() {
		if !cleanupNeeded {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, cleanupErr := s.client.DeleteObject(cleanupCtx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if cleanupErr != nil {
			logger.L().Warn("image_storage.healthcheck_cleanup_failed", zap.Error(cleanupErr))
		}
	}()

	finish = servertiming.ObserveDependency(ctx, "s3")
	_, err = s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	finish()
	if err != nil {
		return fmt.Errorf("S3 HeadObject health check failed: %w", err)
	}

	// A public base URL can make browser previews work even when the configured
	// credentials cannot read objects. ZIP downloads use GetObject server-side,
	// so validate that permission explicitly rather than reporting a false
	// positive from write-only checks.
	finish = servertiming.ObserveDependency(ctx, "s3")
	getResult, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	finish()
	if err != nil {
		return fmt.Errorf("S3 GetObject health check failed: %w", err)
	}
	if getResult == nil || getResult.Body == nil {
		return fmt.Errorf("S3 GetObject health check failed: empty response body")
	}
	_, readErr := io.Copy(io.Discard, getResult.Body)
	closeErr := getResult.Body.Close()
	if readErr != nil {
		return fmt.Errorf("S3 GetObject health check read failed: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("S3 GetObject health check close failed: %w", closeErr)
	}

	// Execute deletion synchronously. S3-compatible stores generally return a
	// successful status even when the object does not exist, so reaching this
	// point validates the credentials' delete permission as well.
	finish = servertiming.ObserveDependency(ctx, "s3")
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	finish()
	if err != nil {
		return fmt.Errorf("S3 DeleteObject health check failed: %w", err)
	}
	cleanupNeeded = false
	return nil
}
