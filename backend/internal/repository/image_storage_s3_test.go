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
