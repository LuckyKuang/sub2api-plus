//go:build unit || !integration

package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Expectations below come from the pinned Files contracts, not from the
// existing inference adapters. A local reference is an authorization boundary.
func TestProviderFilesContractValidation(t *testing.T) {
	t.Run("DeepSeek actual media bytes and expiry", func(t *testing.T) {
		_, err := ValidateProviderFileUpload(PlatformDeepseek, "image.png", "image/png", "", 0, bytes.NewReader([]byte("not a PNG")))
		require.Error(t, err)
		png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
		for _, seconds := range []int64{1, 3599, 2592001} {
			_, err := ValidateProviderFileUpload(PlatformDeepseek, "image.png", "image/png", "", seconds, bytes.NewReader(png))
			require.Error(t, err)
		}
		file, err := ValidateProviderFileUpload(PlatformDeepseek, "image.png", "image/png", "", 3600, bytes.NewReader(png))
		require.NoError(t, err)
		require.EqualValues(t, len(png), file.Bytes)
		require.Equal(t, "image/png", file.MIMEType)
		require.Len(t, file.ContentDigest, 64)
	})
	t.Run("MiniMax unsupported purpose never falls through", func(t *testing.T) {
		_, err := ValidateProviderFileUpload(PlatformMiniMax, "image.png", "image/png", "image_understanding", 0, strings.NewReader("image"))
		require.ErrorIs(t, err, ErrProviderFilesUnsupported)
	})
	t.Run("filename length is bytes", func(t *testing.T) {
		_, err := ValidateProviderFileUpload(PlatformDeepseek, strings.Repeat("图", 171), "image/png", "", 0, strings.NewReader("image"))
		require.Error(t, err)
	})
}

type providerFilesSizedMedia struct{ size, offset int64 }

func (r *providerFilesSizedMedia) Read(buffer []byte) (int, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	n := len(buffer)
	if int64(n) > r.size-r.offset {
		n = int(r.size - r.offset)
	}
	for i := 0; i < n; i++ {
		buffer[i] = 0
		position := r.offset + int64(i)
		if position < 8 {
			buffer[i] = []byte("\x89PNG\r\n\x1a\n")[position]
		}
	}
	r.offset += int64(n)
	return n, nil
}
func (r *providerFilesSizedMedia) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		r.offset = offset
	case io.SeekCurrent:
		r.offset += offset
	case io.SeekEnd:
		r.offset = r.size + offset
	}
	return r.offset, nil
}

func TestProviderFiles64MiBContentBoundary(t *testing.T) {
	file, err := ValidateProviderFileUpload(PlatformDeepseek, "max.png", "image/png", "", 0, &providerFilesSizedMedia{size: 64 << 20})
	require.NoError(t, err)
	require.EqualValues(t, 64<<20, file.Bytes)
	_, err = ValidateProviderFileUpload(PlatformDeepseek, "oversize.png", "image/png", "", 0, &providerFilesSizedMedia{size: (64 << 20) + 1})
	require.Error(t, err)
}

func TestProviderFilesPrincipalDoesNotUseProxyOrDisplayIdentity(t *testing.T) {
	account := &Account{ID: 10, CreatedAt: time.Unix(100, 0), Platform: PlatformDeepseek, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "old", "oauth_login_id": "authorization-a", "oauth_region": "cn"}}
	before, err := providerFilePrincipal(account)
	require.NoError(t, err)
	account.Name = "renamed"
	proxyID := int64(9)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: 9, Host: "proxy.test", Port: 8080, Protocol: "http", Status: StatusActive}
	after, err := providerFilePrincipal(account)
	require.NoError(t, err)
	require.Equal(t, before, after, "same authorization survives proxy and display edits")
	account.Credentials["access_token"] = "manually-replaced"
	replaced, err := providerFilePrincipal(account)
	require.NoError(t, err)
	require.NotEqual(t, before, replaced, "DeepSeek has no official refresh; an editable login ID cannot prove a replacement token's principal")
	account.Credentials["access_token"] = "old"
	account.Credentials["oauth_login_id"] = "authorization-b"
	changed, err := providerFilePrincipal(account)
	require.NoError(t, err)
	require.NotEqual(t, before, changed, "a different login must not inherit file access")
	account.Credentials["oauth_login_id"] = "authorization-a"
	account.CreatedAt = account.CreatedAt.Add(time.Second)
	recreated, err := providerFilePrincipal(account)
	require.NoError(t, err)
	require.NotEqual(t, before, recreated, "a recreated row cannot inherit an old row's files")
}

func TestProviderFilesRewriteAndACL(t *testing.T) {
	key := &APIKey{ID: 3, UserID: 2, GroupID: int64Ptr(4), Group: &Group{Platform: PlatformDeepseek}}
	account := &Account{ID: 11, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "owner-key", "api_protocol": APIProtocolAnthropic}}
	principal, err := providerFilePrincipal(account)
	require.NoError(t, err)
	record := &ProviderFile{PublicID: "file-s2a-11111111-1111-4111-8111-111111111111", UserID: 2, APIKeyID: 3, GroupID: 4, Platform: PlatformDeepseek, AccountID: 11, Principal: principal, RemoteID: "file-api-upstream", State: ProviderFileReady}
	store := &providerFilesTestStore{records: map[string]*ProviderFile{record.PublicID: record}}
	svc := NewProviderFilesService(store, nil)
	body := []byte(`{"model":"deepseek-flash","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"` + record.PublicID + `"}},{"type":"text","text":"inspect"}]}],"unknown":{"keep":1}}`)
	ctx, rewritten, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, body)
	require.NoError(t, err)
	require.Contains(t, string(rewritten), `"file_id":"file-api-upstream"`)
	require.Contains(t, string(rewritten), `"unknown":{"keep":1}`)
	require.True(t, providerFilesAccountAllowed(ctx, account))
	other := *account
	other.ID = 12
	require.False(t, providerFilesAccountAllowed(ctx, &other), "file owner is a hard constraint, not a sticky preference")
	for _, tc := range []struct {
		name   string
		change func()
	}{
		{"other key", func() { key.ID++ }},
		{"other user", func() { key.UserID++ }},
		{"other group", func() { *key.GroupID++ }},
		{"revoked", func() { record.State = ProviderFileRevoked }},
		{"expired", func() { record.ExpiresAt = time.Now().Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key.ID, key.UserID, *key.GroupID, record.State, record.ExpiresAt = 3, 2, 4, ProviderFileReady, time.Time{}
			tc.change()
			_, _, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, body)
			require.ErrorIs(t, err, ErrProviderFileNotFound)
		})
	}
}

func TestProviderFilesUnknownLocalIDNeverBecomesNative(t *testing.T) {
	key := &APIKey{ID: 3, UserID: 2, GroupID: int64Ptr(4)}
	svc := NewProviderFilesService(&providerFilesTestStore{records: map[string]*ProviderFile{}}, nil)
	for _, body := range []string{
		`{"messages":[{"content":[{"type":"file","file_id":"file-s2a-missing"}]}]}`,
		`{"messages":[{"content":[{"type":"video","source":{"type":"url","url":"mm_file://file-s2a-missing"}}]}]}`,
	} {
		_, _, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, []byte(body))
		require.ErrorIs(t, err, ErrProviderFileNotFound)
	}
	native := []byte(`{"messages":[{"content":[{"type":"file","file_id":"file-api-external"},{"type":"image","source":{"type":"base64","data":"AA=="}}]}],"unknown":{"file_id":"file-s2a-text"}}`)
	_, result, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, native)
	require.NoError(t, err)
	require.Equal(t, native, result, "external native references and unknown fields retain their existing semantics")
}

type providerFilesTestStore struct {
	records    map[string]*ProviderFile
	publishErr error
}

func (s *providerFilesTestStore) Reserve(_ context.Context, file *ProviderFile, _ *Account) (*ProviderFile, bool, error) {
	for _, old := range s.records {
		if old.IdempotencyDigest == file.IdempotencyDigest {
			return old, false, nil
		}
	}
	s.records[file.PublicID] = file
	return file, true, nil
}
func (s *providerFilesTestStore) Get(_ context.Context, id string) (*ProviderFile, error) {
	if f := s.records[id]; f != nil {
		return f, nil
	}
	return nil, ErrProviderFileNotFound
}
func (s *providerFilesTestStore) GetByIdempotency(_ context.Context, digest string) (*ProviderFile, error) {
	for _, file := range s.records {
		if file.IdempotencyDigest == digest {
			return file, nil
		}
	}
	return nil, ErrProviderFileNotFound
}
func (s *providerFilesTestStore) List(_ context.Context, _ ProviderFileScope, _ string, _ string, _ int) ([]*ProviderFile, error) {
	return nil, nil
}
func (s *providerFilesTestStore) Publish(_ context.Context, f *ProviderFile, _ *Account) error {
	if s.publishErr != nil {
		return s.publishErr
	}
	s.records[f.PublicID] = f
	return nil
}
func (s *providerFilesTestStore) SetState(_ context.Context, id, state, remote string) error {
	f := s.records[id]
	f.State = state
	if remote != "" {
		f.RemoteID = remote
	}
	return nil
}
func (s *providerFilesTestStore) BeginDelete(_ context.Context, id, token, digest string) (*ProviderFile, bool, error) {
	f := s.records[id]
	if f == nil {
		return nil, false, ErrProviderFileNotFound
	}
	f.State = ProviderFileDeletePending
	f.MutationToken = token
	if digest != "" {
		f.RequestSnapshotDigest = digest
	}
	return f, true, nil
}
func (s *providerFilesTestStore) ListCleanup(_ context.Context, _ int) ([]*ProviderFile, error) {
	var result []*ProviderFile
	for _, f := range s.records {
		if f.RemoteID != "" && (f.State == ProviderFileUncertain || f.State == ProviderFileDeletePending) {
			copy := *f
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (s *providerFilesTestStore) FinishDelete(_ context.Context, id, token string, success bool) error {
	f := s.records[id]
	if f.MutationToken != token {
		return ErrProviderFileConflict
	}
	if success {
		f.State = ProviderFileDeleted
	}
	f.MutationToken = ""
	return nil
}
