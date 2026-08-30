package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskDownloadStorage struct {
	objects  map[string][]byte
	savedURL string
}

func (s *imageTaskDownloadStorage) Save(_ context.Context, key, _ string, data []byte) (string, error) {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), data...)
	if s.savedURL != "" {
		return s.savedURL, nil
	}
	return "https://cdn.example.test/" + key, nil
}

func (s *imageTaskDownloadStorage) Open(_ context.Context, key string) (*ImageStorageObject, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, ErrImageTaskNotFound
	}
	return &ImageStorageObject{Body: io.NopCloser(bytes.NewReader(data)), ContentType: "image/png", Size: int64(len(data))}, nil
}

func TestImageTaskServiceStreamDownloadZipContainsAllTaskImages(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &imageTaskDownloadStorage{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", false, 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 11, APIKeyID: 22}
	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)

	imageOne := append([]byte(nil), pngBytes...)
	imageTwo := append(append([]byte(nil), pngBytes...), []byte("two")...)
	result := []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(imageOne) + `"},{"b64_json":"` + base64.StdEncoding.EncodeToString(imageTwo) + `"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	var archive bytes.Buffer
	count, err := svc.StreamDownloadZip(context.Background(), owner, created.ID, &archive)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, 2)
	require.Equal(t, "image-1.png", reader.File[0].Name)
	require.Equal(t, "image-2.png", reader.File[1].Name)
	expected := map[string][]byte{"image-1.png": imageOne, "image-2.png": imageTwo}
	for _, file := range reader.File {
		content, openErr := file.Open()
		require.NoError(t, openErr)
		data, readErr := io.ReadAll(content)
		require.NoError(t, readErr)
		require.NoError(t, content.Close())
		require.Equal(t, expected[file.Name], data)
	}

	var forbidden bytes.Buffer
	_, err = svc.StreamDownloadZip(context.Background(), ImageTaskOwner{UserID: owner.UserID, APIKeyID: owner.APIKeyID + 1}, created.ID, &forbidden)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

func TestImageTaskServiceStreamDownloadZipUsesStoredKeyAfterConfigChange(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &imageTaskDownloadStorage{savedURL: "https://cdn.example.test/download?id=opaque"}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images-old/", true, 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 11, APIKeyID: 22}
	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)

	result := []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))
	require.Len(t, store.task.StorageKeys, 1)
	require.True(t, strings.HasPrefix(store.task.StorageKeys[0], "images-old/"))

	svc.uploader = NewImageResultUploader(storage, "images-new/", false, 0, nil)
	var archive bytes.Buffer
	count, err := svc.StreamDownloadZip(context.Background(), owner, created.ID, &archive)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, 1)
	require.Equal(t, "image-1.png", reader.File[0].Name)
}

func TestImageTaskServiceStreamDownloadZipFallsBackToDurableHistory(t *testing.T) {
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	storage := &imageTaskDownloadStorage{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", false, 0, nil), time.Hour, time.Minute)
	svc.SetHistoryRepository(history)
	owner := ImageTaskOwner{UserID: 11, APIKeyID: 22}
	created, err := svc.CreateWithMetadata(context.Background(), owner, ImageTaskMetadata{RequestedImages: 2})
	require.NoError(t, err)

	imageOne := append([]byte(nil), pngBytes...)
	imageTwo := append(append([]byte(nil), pngBytes...), []byte("two")...)
	result := []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(imageOne) + `"},{"b64_json":"` + base64.StdEncoding.EncodeToString(imageTwo) + `"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))
	store.task = nil

	var archive bytes.Buffer
	count, err := svc.StreamDownloadZip(context.Background(), owner, created.ID, &archive)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NotEmpty(t, archive.Bytes())

	_, err = svc.StreamDownloadZip(context.Background(), ImageTaskOwner{UserID: owner.UserID, APIKeyID: owner.APIKeyID + 1}, created.ID, io.Discard)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

func TestImageTaskServiceCompletesAndDownloadsAfterSubmissionsAreDisabled(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &imageTaskDownloadStorage{}
	uploader := NewImageResultUploader(storage, "images/", false, 0, nil)
	enabled := true
	svc := NewImageTaskServiceWithResolver(store, func() (*ImageResultUploader, bool) {
		return uploader, enabled
	}, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 11, APIKeyID: 22}

	require.True(t, svc.Enabled())
	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)

	// The task was accepted while submissions were enabled. Disabling them must
	// not prevent the in-flight result from being offloaded or downloaded later.
	enabled = false
	require.False(t, svc.Enabled())
	result := []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))
	require.Len(t, store.task.StorageKeys, 1)
	require.NotContains(t, string(store.task.Result), "b64_json")

	var archive bytes.Buffer
	count, err := svc.StreamDownloadZip(context.Background(), owner, created.ID, &archive)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, 1)
	require.Equal(t, "image-1.png", reader.File[0].Name)
}

func TestImageTaskServiceStreamDownloadZipRejectsTasksWithoutImages(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(&imageTaskDownloadStorage{}, "images/", false, 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, []byte(`{"data":[]}`)))

	_, err = svc.StreamDownloadZip(context.Background(), owner, created.ID, io.Discard)
	require.ErrorIs(t, err, ErrImageTaskNoImages)
}
