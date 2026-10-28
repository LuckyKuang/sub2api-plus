//go:build unit

package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func providerFilesMultipart(t *testing.T, fields map[string]string, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	for name, value := range fields {
		require.NoError(t, writer.WriteField(name, value))
	}
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func TestProviderFilesMultipartCleanupAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
	for _, tc := range []struct {
		name      string
		fields    map[string]string
		data      []byte
		wantError bool
	}{
		{"image and expiry", map[string]string{"expires_after[anchor]": "created_at", "expires_after[seconds]": "3600"}, png, false},
		{"no expiry means permanent", nil, png, false},
		{"partial expiry", map[string]string{"expires_after[seconds]": "3600"}, png, true},
		{"unknown field", map[string]string{"account_id": "other-account"}, png, true},
		{"fake image", nil, []byte("not an image"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := providerFilesMultipart(t, tc.fields, "test.png", tc.data)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/files", body)
			c.Request.Header.Set("Content-Type", contentType)
			file, cleanup, err := parseProviderFileUpload(c, service.PlatformDeepseek)
			if cleanup != nil {
				defer cleanup()
			}
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, len(png), file.Bytes)
			temporary, ok := file.Data.(*os.File)
			require.True(t, ok)
			name := temporary.Name()
			info, err := temporary.Stat()
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			cleanup()
			_, err = os.Stat(name)
			require.True(t, os.IsNotExist(err), "original bytes must be removed after request completion")
		})
	}
}

type providerFilesAuditRepository struct {
	service.ProviderFilesRepository
	touch func()
}

func (r *providerFilesAuditRepository) Get(context.Context, string) (*service.ProviderFile, error) {
	r.touch()
	return nil, service.ErrProviderFileNotFound
}
func (r *providerFilesAuditRepository) List(context.Context, service.ProviderFileScope, string, string, int) ([]*service.ProviderFile, error) {
	r.touch()
	return nil, nil
}

func TestProviderFilesAuditPrecedesACLRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, operation := range []string{"list", "metadata", "delete"} {
		t.Run(operation, func(t *testing.T) {
			method, path := http.MethodGet, "/v1/files"
			if operation != "list" {
				path += "/file-s2a-11111111-1111-4111-8111-111111111111"
			}
			if operation == "delete" {
				method = http.MethodDelete
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(method, path, nil)
			if operation != "list" {
				c.Params = gin.Params{{Key: "file_id", Value: "file-s2a-11111111-1111-4111-8111-111111111111"}}
			}
			groupID := int64(4)
			key := &service.APIKey{ID: 3, UserID: 2, GroupID: &groupID, Group: &service.Group{ID: 4, Platform: service.PlatformDeepseek}}
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2, Concurrency: 1})
			touched := 0
			repo := &providerFilesAuditRepository{touch: func() {
				touched++
				completed, exists := c.Get(securityAuditCompletedContextKey)
				require.True(t, exists)
				require.Equal(t, true, completed, "ACL reads must occur after the common audit boundary")
			}}
			h := &OpenAIGatewayHandler{concurrencyHelper: newGatewayAdmissionForTest().concurrencyHelper, providerFilesService: service.NewProviderFilesService(repo, nil), securityAuditCoordinator: securityaudit.NewCoordinator(nil, nil)}
			h.ProviderFiles(c)
			require.Equal(t, 1, touched)
		})
	}
}

func TestProviderFilesInferenceUnknownLocalAndUnsupportedBridges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(4)
	key := &service.APIKey{ID: 3, UserID: 2, GroupID: &groupID, Group: &service.Group{Platform: service.PlatformMiniMax}}
	h := &OpenAIGatewayHandler{providerFilesService: service.NewProviderFilesService(&providerFilesAuditRepository{touch: func() { t.Fatal("unsupported bridge must not read a mapping") }}, nil)}
	for _, protocol := range []string{service.APIProtocolAnthropic, service.APIProtocolResponses} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		_, err := h.resolveProviderFilesForInference(c, key, []byte(`{"messages":[{"content":[{"type":"video","source":{"type":"url","url":"mm_file://file-s2a-id"}}]}]}`), protocol)
		require.ErrorIs(t, err, service.ErrProviderFilesUnsupported)
	}
}

type providerFilesAuditAccountRepo struct {
	service.AccountRepository
	t     *testing.T
	c     *gin.Context
	reads int
}

func (r *providerFilesAuditAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	r.reads++
	value, exists := r.c.Get(securityAuditCompletedContextKey)
	require.True(r.t, exists)
	require.Equal(r.t, true, value)
	return nil, nil
}

func TestProviderFilesUploadAuditsBeforeSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	input, contentType := providerFilesMultipart(t, nil, "secret-filename.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/files", input)
	c.Request.Header.Set("Content-Type", contentType)
	groupID := int64(4)
	key := &service.APIKey{ID: 3, UserID: 2, GroupID: &groupID, Group: &service.Group{ID: 4, Platform: service.PlatformDeepseek}}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2, Concurrency: 1})
	repo := &providerFilesAuditAccountRepo{t: t, c: c}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cache := &domesticAuditConcurrencyCache{}
	h := &OpenAIGatewayHandler{providerFilesService: service.NewProviderFilesService(nil, gateway), securityAuditCoordinator: securityaudit.NewCoordinator(nil, nil), concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0)}
	h.ProviderFiles(c)
	require.Positive(t, repo.reads)
	require.Equal(t, 1, cache.userAcquisitions)
}

func TestProviderFilesCancelledUploadDoesNotRetainBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body, contentType := providerFilesMultipart(t, nil, "test.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/files", body).WithContext(ctx)
	c.Request.Header.Set("Content-Type", contentType)
	file, cleanup, err := parseProviderFileUpload(c, service.PlatformDeepseek)
	if cleanup != nil {
		cleanup()
	}
	require.Error(t, err)
	require.Nil(t, file)
}
