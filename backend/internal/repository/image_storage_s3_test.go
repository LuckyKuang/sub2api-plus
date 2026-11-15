//go:build unit || !integration

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestS3ImageStorageCheckRequiresGetObjectPermission(t *testing.T) {
	var getCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCalls.Add(1)
			http.Error(w, "read access denied", http.StatusForbidden)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint:        server.URL,
		Region:          "us-east-1",
		Bucket:          "images",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "async-images",
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	err = storage.Check(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "S3 GetObject health check failed")
	require.Greater(t, getCalls.Load(), int32(0))
}

func TestS3ImageUploadPreservesSignedCredentialsWithoutProjectHeaders(t *testing.T) {
	for _, accessID := range []string{"storage-admin", "SuB2ApI-minio"} {
		t.Run(accessID, func(t *testing.T) {
			var calls atomic.Int32
			observed := make(chan http.Header, 1)
			received := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				observed <- r.Header.Clone()
				body, _ := io.ReadAll(r.Body)
				received <- string(body)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			storage, err := NewS3ImageStorage(ctx, &config.ImageStorageConfig{Endpoint: server.URL, Region: "us-east-1", Bucket: "existing-images", AccessKeyID: accessID, SecretAccessKey: "test-secret", ForcePathStyle: true, PublicBaseURL: server.URL})
			require.NoError(t, err)
			_, err = storage.Save(ctx, "image.png", "image/png", []byte("image-bytes"))
			if accessID != "storage-admin" {
				require.Error(t, err)
				require.Zero(t, calls.Load(), "a branded S3 credential must never leave the process")
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, "image-bytes", <-received)
			sent := <-observed
			require.Contains(t, sent.Get("Authorization"), "Credential=storage-admin/")
			require.Contains(t, sent.Get("Authorization"), "Signature=")
			for name, values := range sent {
				require.NotContains(t, strings.ToLower(name), "sub2api")
				for _, value := range values {
					require.NotContains(t, strings.ToLower(value), "sub2api")
				}
			}
		})
	}
}

// R6: object existence and storage authorization are separate read outcomes.
// Checks must use a signed HEAD of the exact historical key, never a URL fetch.
func TestS3ImageStorageChecksExactArchivedObject(t *testing.T) {
	for _, status := range []int{200, 404, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodHead, r.Method)
				require.Equal(t, "/images/old-prefix/task-0.png", r.URL.Path)
				require.Contains(t, r.Header.Get("Authorization"), "Signature=")
				w.WriteHeader(status)
			}))
			defer server.Close()
			storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{Endpoint: server.URL, Region: "us-east-1", Bucket: "images", AccessKeyID: "storage-reader", SecretAccessKey: "secret", ForcePathStyle: true})
			require.NoError(t, err)
			err = storage.CheckStoredImage(context.Background(), "old-prefix/task-0.png")
			switch status {
			case 200:
				require.NoError(t, err)
			case 404:
				require.ErrorIs(t, err, service.ErrImageTaskObjectMissing)
			case 403:
				require.Error(t, err)
				require.NotErrorIs(t, err, service.ErrImageTaskObjectMissing)
			}
		})
	}
}
