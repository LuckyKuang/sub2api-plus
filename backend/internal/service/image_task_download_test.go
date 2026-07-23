package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskDownloadStorage struct {
	objects map[string][]byte
}

func (s *imageTaskDownloadStorage) Save(_ context.Context, key, _ string, data []byte) (string, error) {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), data...)
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
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
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

func TestImageTaskServiceStreamDownloadZipRejectsTasksWithoutImages(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(&imageTaskDownloadStorage{}, "images/", 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, []byte(`{"data":[]}`)))

	_, err = svc.StreamDownloadZip(context.Background(), owner, created.ID, io.Discard)
	require.ErrorIs(t, err, ErrImageTaskNoImages)
}
