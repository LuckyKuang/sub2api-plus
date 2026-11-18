package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type stubImageStorage struct {
	lastKey  string
	lastType string
	lastData []byte
	saveErr  error
	openData []byte
}

func (s *stubImageStorage) Save(_ context.Context, key, contentType string, data []byte) (string, error) {
	if s.saveErr != nil {
		return "", s.saveErr
	}
	s.lastKey = key
	s.lastType = contentType
	s.lastData = append([]byte{}, data...)
	return "https://cdn.example/" + key, nil
}

func (s *stubImageStorage) Open(_ context.Context, _ string) (*ImageStorageObject, error) {
	return &ImageStorageObject{Body: io.NopCloser(bytes.NewReader(s.openData)), Size: int64(len(s.openData))}, nil
}

type deletingImageStorage struct {
	stubImageStorage
	deleted []string
}

func (s *deletingImageStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func newArtifactStore(storage ImageStorage) *QualityArtifactStore {
	return NewQualityArtifactStore(func() (*ImageResultUploader, bool) {
		return NewImageResultUploader(storage, "images", true, 0, nil), true
	})
}

func TestQualityArtifactStore_SaveKeyAndContentType(t *testing.T) {
	storage := &stubImageStorage{}
	store := newArtifactStore(storage)
	key, size, truncated, err := store.Save(context.Background(), 3, 9, 42, "<svg></svg>")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasPrefix(key, "images/quality-inspection/") {
		t.Fatalf("key = %q, want prefix images/quality-inspection/", key)
	}
	if !strings.HasSuffix(key, "/3/9-42.html") {
		t.Fatalf("key = %q, want .../3/9-42.html", key)
	}
	if storage.lastType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", storage.lastType)
	}
	if size != int64(len("<svg></svg>")) || truncated {
		t.Fatalf("size = %d truncated = %v", size, truncated)
	}
}

func TestQualityArtifactStore_TruncatesOversizeArtifact(t *testing.T) {
	storage := &stubImageStorage{}
	store := newArtifactStore(storage)
	payload := strings.Repeat("a", qualityArtifactMaxBytes+32)
	_, size, truncated, err := store.Save(context.Background(), 1, 2, 3, payload)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !truncated {
		t.Fatal("oversize artifact must be marked truncated")
	}
	if size != int64(qualityArtifactMaxBytes) || len(storage.lastData) != qualityArtifactMaxBytes {
		t.Fatalf("size = %d stored = %d", size, len(storage.lastData))
	}
}

func TestQualityArtifactStore_Open(t *testing.T) {
	storage := &stubImageStorage{openData: []byte("<html></html>")}
	store := newArtifactStore(storage)
	data, err := store.Open(context.Background(), "images/quality-inspection/x.html")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(data) != "<html></html>" {
		t.Fatalf("data = %q", data)
	}
	if _, err := store.Open(context.Background(), "  "); err == nil {
		t.Fatal("empty key must be rejected")
	}
}

func TestQualityArtifactStore_DeleteUnsupported(t *testing.T) {
	store := newArtifactStore(&stubImageStorage{})
	if err := store.Delete(context.Background(), "images/quality-inspection/x.html"); !errors.Is(err, errQualityDeleteUnsupported) {
		t.Fatalf("expected unsupported deletion, got %v", err)
	}
}

func TestQualityArtifactStore_DeleteSupported(t *testing.T) {
	storage := &deletingImageStorage{}
	store := newArtifactStore(storage)
	if err := store.Delete(context.Background(), "images/quality-inspection/x.html"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(storage.deleted) != 1 {
		t.Fatalf("deleted = %v", storage.deleted)
	}
}

func TestQualityArtifactStore_UnavailableWithoutResolver(t *testing.T) {
	store := NewQualityArtifactStore(nil)
	if store.Available() {
		t.Fatal("nil resolver must be unavailable")
	}
	if _, _, _, err := store.Save(context.Background(), 1, 1, 1, "x"); !errors.Is(err, ErrQualityStorageUnavailable) {
		t.Fatalf("expected storage unavailable, got %v", err)
	}
	empty := NewQualityArtifactStore(func() (*ImageResultUploader, bool) { return nil, false })
	if empty.Available() {
		t.Fatal("nil uploader must be unavailable")
	}
}
