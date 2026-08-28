//go:build integration

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const (
	asyncImageContainerTestBucket = "sub2api-async-integration"
	asyncImageContainerTestPNG    = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL4JwAAAABJRU5ErkJggg=="
)

// TestAsyncImageContainerWorkflowWithMinIO runs against isolated local
// PostgreSQL and MinIO containers. The application container applies the
// migrations before this test starts; this test then covers the real async HTTP
// flow, database-backed task list, S3 permission check, object upload and
// presigned result download without depending on an external image provider.
func TestAsyncImageContainerWorkflowWithMinIO(t *testing.T) {
	dsn := os.Getenv("SUB2API_ASYNC_TEST_DATABASE_URL")
	endpoint := os.Getenv("SUB2API_ASYNC_TEST_S3_ENDPOINT")
	if dsn == "" || endpoint == "" {
		t.Skip("set SUB2API_ASYNC_TEST_DATABASE_URL and SUB2API_ASYNC_TEST_S3_ENDPOINT to run the container workflow")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.PingContext(ctx))

	var historyTable sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, "SELECT to_regclass('public.async_image_tasks')").Scan(&historyTable))
	require.True(t, historyTable.Valid, "the application container must apply migration 187 before this workflow runs")

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("minio", "minio-test-secret", "")),
	)
	require.NoError(t, err)
	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	if _, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(asyncImageContainerTestBucket)}); err != nil {
		_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(asyncImageContainerTestBucket)})
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = s3Client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(asyncImageContainerTestBucket)})
	})

	storage, err := repository.NewS3ImageStorage(ctx, &config.ImageStorageConfig{
		Enabled: true, Endpoint: endpoint, Region: "us-east-1", Bucket: asyncImageContainerTestBucket,
		AccessKeyID: "minio", SecretAccessKey: "minio-test-secret", Prefix: "images/", ForcePathStyle: true,
		PresignExpiry: 1,
	})
	require.NoError(t, err)
	require.NoError(t, storage.Check(ctx), "the administrator connection check must validate bucket read/write/delete permissions")

	probes, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(asyncImageContainerTestBucket), Prefix: aws.String("images/.healthcheck/"),
	})
	require.NoError(t, err)
	require.Empty(t, probes.Contents, "connection health checks must remove their probe objects")

	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "images/", false, 0, nil), time.Hour, time.Minute)
	tasks.SetHistoryRepository(repository.NewImageTaskHistoryRepository(db))
	handler := &AsyncImageHandler{tasks: tasks}
	handler.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"b64_json": asyncImageContainerTestPNG}}})
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", handler.Submit)
	router.GET("/v1/images/tasks", handler.List)

	submitRequest := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"A bright integration test image"}`))
	submitRequest.Header.Set("Content-Type", "application/json")
	submitWriter := httptest.NewRecorder()
	router.ServeHTTP(submitWriter, submitRequest)
	require.Equal(t, http.StatusAccepted, submitWriter.Code, submitWriter.Body.String())

	var accepted struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(submitWriter.Body.Bytes(), &accepted))
	require.NotEmpty(t, accepted.TaskID)

	var completed service.ImageTask
	require.Eventually(t, func() bool {
		listRequest := httptest.NewRequest(http.MethodGet, "/v1/images/tasks?status=completed", nil)
		listWriter := httptest.NewRecorder()
		router.ServeHTTP(listWriter, listRequest)
		if listWriter.Code != http.StatusOK {
			return false
		}
		var response struct {
			Data []service.ImageTask `json:"data"`
		}
		if json.Unmarshal(listWriter.Body.Bytes(), &response) != nil || len(response.Data) != 1 || response.Data[0].TaskID != accepted.TaskID {
			return false
		}
		completed = response.Data[0]
		return completed.Status == service.ImageTaskStatusCompleted && completed.ImageURL != ""
	}, 15*time.Second, 100*time.Millisecond)

	imageResponse, err := http.Get(completed.ImageURL) //nolint:gosec // test reads the application-generated presigned URL.
	require.NoError(t, err)
	defer func() { _ = imageResponse.Body.Close() }()
	require.Equal(t, http.StatusOK, imageResponse.StatusCode)
	imageBytes, err := io.ReadAll(imageResponse.Body)
	require.NoError(t, err)
	require.NotEmpty(t, imageBytes)

	objects, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(asyncImageContainerTestBucket), Prefix: aws.String("images/" + accepted.TaskID),
	})
	require.NoError(t, err)
	require.Len(t, objects.Contents, 1, "the generated image must be offloaded to MinIO")
	t.Cleanup(func() {
		_, _ = s3Client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(asyncImageContainerTestBucket), Key: objects.Contents[0].Key,
		})
	})

	var storedStatus string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT status FROM async_image_tasks WHERE task_id = $1", accepted.TaskID).Scan(&storedStatus))
	require.Equal(t, service.ImageTaskStatusCompleted, storedStatus)
}
