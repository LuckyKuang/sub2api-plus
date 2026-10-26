package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ProviderFileIDPrefix            = "file-s2a-"
	ProviderFileMaxBytes      int64 = 64 << 20
	ProviderFileReady               = "ready"
	ProviderFileUploading           = "uploading"
	ProviderFileUncertain           = "uncertain"
	ProviderFileRevoked             = "revoked"
	ProviderFileDeletePending       = "delete_pending"
	ProviderFileDeleted             = "deleted"
)

var (
	ErrProviderFilesUnsupported = errors.New("file operation is not supported for this provider profile or purpose")
	ErrProviderFileNotFound     = errors.New("file not found or unavailable; upload the original bytes again")
	ErrProviderFileConflict     = errors.New("file operation is incomplete, uncertain, or conflicts with the original upload; no upload was replayed")
	ErrProviderFileOwnerChanged = errors.New("file owner changed; upload the original bytes again")
	ErrProviderFileStore        = errors.New("file persistence is unavailable")
	ErrProviderFileQuota        = errors.New("file storage limit exceeded")
	ErrProviderFileUpstream     = errors.New("file provider operation failed")
)

type ProviderFileScope struct {
	UserID, APIKeyID, GroupID int64
	Platform                  string
}

type ProviderFile struct {
	PublicID                                          string
	UserID, APIKeyID, GroupID                         int64
	Platform                                          string
	AccountID                                         int64
	Principal, RemoteID, State                        string
	Filename, MIMEType, Purpose                       string
	Bytes                                             int64
	ContentDigest, IdempotencyDigest, ParameterDigest string
	CreatedAt, ExpiresAt                              time.Time
	MutationToken                                     string
	RequestSnapshotDigest                             string
}

// The operational table contains metadata and digests only. Reserve and Publish
// must check the complete current account/proxy snapshot atomically with writes.
type ProviderFilesRepository interface {
	Reserve(context.Context, *ProviderFile, *Account) (*ProviderFile, bool, error)
	Get(context.Context, string) (*ProviderFile, error)
	GetByIdempotency(context.Context, string) (*ProviderFile, error)
	List(context.Context, ProviderFileScope, string, string, int) ([]*ProviderFile, error)
	Publish(context.Context, *ProviderFile, *Account) error
	SetState(context.Context, string, string, string) error
	BeginDelete(context.Context, string, string, string) (*ProviderFile, bool, error)
	FinishDelete(context.Context, string, string, bool) error
	ListCleanup(context.Context, int) ([]*ProviderFile, error)
}

type ProviderFileUpload struct {
	Filename, MIMEType, Purpose, ContentDigest string
	Bytes, ExpirySeconds                       int64
	Data                                       io.ReadSeeker
}

type ProviderFilesService struct {
	repo                ProviderFilesRepository
	gateway             *OpenAIGatewayService
	recoveryCtx         context.Context
	stop                context.CancelFunc
	startOnce, stopOnce sync.Once
}

func NewProviderFilesService(repo ProviderFilesRepository, gateway *OpenAIGatewayService) *ProviderFilesService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProviderFilesService{repo: repo, gateway: gateway, recoveryCtx: ctx, stop: cancel}
}

func ProviderFileAccessScope(key *APIKey, platform string) (ProviderFileScope, error) {
	if key == nil || key.ID <= 0 || key.UserID <= 0 || key.GroupID == nil || *key.GroupID <= 0 {
		return ProviderFileScope{}, ErrProviderFileNotFound
	}
	return ProviderFileScope{key.UserID, key.ID, *key.GroupID, platform}, nil
}

func providerFileScopeAllows(scope ProviderFileScope, f *ProviderFile) bool {
	return f != nil && scope.UserID == f.UserID && scope.APIKeyID == f.APIKeyID && scope.GroupID == f.GroupID && scope.Platform == f.Platform
}

func providerFilesIdempotencyDigest(scope ProviderFileScope, key string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%s:anthropic-files:%s", scope.UserID, scope.APIKeyID, scope.GroupID, scope.Platform, key)))
	return hex.EncodeToString(digest[:])
}

// ResolveUploadRetry runs after audit and received-byte validation. It pins a
// repeated explicit key to its original owner before ordinary scheduling can
// select a different account. Unknown keys retain normal pool selection.
func (s *ProviderFilesService) ResolveUploadRetry(ctx context.Context, key *APIKey, platform, idempotency string) (context.Context, error) {
	if idempotency == "" {
		return ctx, nil
	}
	scope, err := ProviderFileAccessScope(key, platform)
	if err != nil {
		return ctx, err
	}
	file, err := s.repo.GetByIdempotency(ctx, providerFilesIdempotencyDigest(scope, idempotency))
	if errors.Is(err, ErrProviderFileNotFound) {
		return ctx, nil
	}
	if err != nil {
		return ctx, ErrProviderFileStore
	}
	if !providerFileScopeAllows(scope, file) {
		return ctx, ErrProviderFileNotFound
	}
	if file.State != ProviderFileReady || !file.ExpiresAt.IsZero() && !file.ExpiresAt.After(time.Now()) {
		return ctx, ErrProviderFileConflict
	}
	return context.WithValue(ctx, providerFilesOwnerContextKey{}, providerFilesOwnerConstraint{file.AccountID, file.Principal}), nil
}

// Input is already spooled into a bounded private temporary file. The digest is
// always computed from received bytes; a client digest cannot authorize reuse.
func ValidateProviderFileUpload(platform, name, declaredMIME, purpose string, expiry int64, input io.ReadSeeker) (*ProviderFileUpload, error) {
	if input == nil || name == "" || len(name) > 512 || filepath.Base(name) != name || strings.ContainsAny(name, "\x00\r\n/\\") {
		return nil, errors.New("invalid file name")
	}
	if platform != PlatformDeepseek && platform != PlatformMiniMax {
		return nil, ErrProviderFilesUnsupported
	}
	if platform == PlatformDeepseek && (purpose != "" && purpose != "user_data" || expiry != 0 && (expiry < 3600 || expiry > 2592000)) {
		return nil, errors.New("invalid file purpose or expiry")
	}
	if platform == PlatformMiniMax && (purpose != "video_understanding" || expiry != 0) {
		return nil, ErrProviderFilesUnsupported
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(input, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	head = head[:n]
	mimeType := ""
	if platform == PlatformDeepseek {
		switch {
		case len(head) >= 8 && bytes.Equal(head[:8], []byte("\x89PNG\r\n\x1a\n")):
			mimeType = "image/png"
		case len(head) >= 3 && bytes.Equal(head[:3], []byte{255, 216, 255}):
			mimeType = "image/jpeg"
		case len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a"):
			mimeType = "image/gif"
		case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
			mimeType = "image/webp"
		}
	} else {
		mimeType = providerFileMiniMaxVideoMIME(head)
	}
	if mimeType == "" || declaredMIME != "" && declaredMIME != "application/octet-stream" && declaredMIME != mimeType {
		return nil, errors.New("file content does not match a supported media type")
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(input, ProviderFileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if size <= 0 || size > ProviderFileMaxBytes {
		return nil, errors.New("file exceeds the 64 MiB gateway limit")
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return &ProviderFileUpload{Filename: name, MIMEType: mimeType, Purpose: purpose, ContentDigest: hex.EncodeToString(digest.Sum(nil)), Bytes: size, ExpirySeconds: expiry, Data: input}, nil
}

// This checks a bounded container signature and declared brands, not tracks or
// media decodability. Image formats also use ftyp and cannot imply video support.
func providerFileMiniMaxVideoMIME(head []byte) string {
	if len(head) < 16 || string(head[4:8]) != "ftyp" {
		return ""
	}
	size := int(binary.BigEndian.Uint32(head[:4]))
	if size < 16 || size > len(head) || (size-16)%4 != 0 {
		return ""
	}
	mimeType := ""
	switch string(head[8:12]) {
	case "qt  ":
		mimeType = "video/quicktime"
	case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "iso7", "iso8", "iso9", "isoa", "isob", "isoc", "isod", "mp41", "mp42", "avc1", "av01", "M4V ":
		mimeType = "video/mp4"
	default:
		return ""
	}
	// Generic ISO major brands do not override an image compatibility brand.
	for offset := 16; offset < size; offset += 4 {
		switch string(head[offset : offset+4]) {
		case "avif", "avis", "avio", "avci", "avcs", "heic", "heix", "heim", "heis", "hevc", "hevx", "hevs", "mif1", "mif2", "msf1", "miaf", "MA1A", "MA1B":
			return ""
		}
	}
	return mimeType
}

// There is no authoritative provider principal for imported API keys. A key
// digest or server-issued authorization epoch is deliberately more conservative
// than inferring identity from an email, display name or account row ID.
func providerFilePrincipal(a *Account) (string, error) {
	if a == nil || a.ID <= 0 || a.IsShadow() {
		return "", ErrProviderFileOwnerChanged
	}
	grant := a.GetCredential("oauth_login_id")
	if a.Type == AccountTypeAPIKey {
		grant = "key:" + a.GetCredential("api_key")
	} else if a.Platform == PlatformDeepseek && grant != "" {
		// DeepSeek publishes no account refresh flow. A manually replaced
		// token retaining an editable login ID is not proof of one principal.
		if strings.TrimSpace(a.GetCredential("access_token")) == "" {
			return "", ErrProviderFileOwnerChanged
		}
		grant += "\x00token:" + a.GetCredential("access_token")
	} else if grant == "" {
		grant = "legacy:" + a.GetCredential("access_token")
	}
	if grant == "key:" || grant == "legacy:" {
		return "", ErrProviderFileOwnerChanged
	}
	namespace := a.GetAnthropicProtocolBaseURL()
	if a.Platform == PlatformMiniMax {
		namespace = "https://api.minimax.io"
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("files:v1\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s", a.ID, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.Platform, a.Type, namespace, a.GetCredential("oauth_region")+"\x00"+grant)))
	return hex.EncodeToString(digest[:]), nil
}

func providerFileProfileSupported(a *Account) bool {
	if a == nil || a.IsShadow() {
		return false
	}
	if a.Platform == PlatformDeepseek {
		return a.IsDomesticOAuth() || a.Type == AccountTypeAPIKey && a.IsAnthropicProtocol()
	}
	if a.Platform == PlatformMiniMax && a.Type == AccountTypeAPIKey {
		base := strings.TrimRight(a.GetOpenAIFormatBaseURL(), "/")
		return (base == "https://api.minimax.io" || base == "https://api.minimax.io/v1") && a.GetCredential("oauth_region") != "cn" && a.GetAPIProtocol() == APIProtocolChatCompletions
	}
	return false
}

// Upload candidates still use the gateway scheduler's authorization, health,
// proxy, concurrency and runtime gates; this filter only narrows capabilities.
func (s *ProviderFilesService) SelectUploadAccount(ctx context.Context, key *APIKey, platform string) (*AccountSelectionResult, error) {
	if s == nil || s.gateway == nil || (platform != PlatformDeepseek && platform != PlatformMiniMax) {
		return nil, ErrProviderFilesUnsupported
	}
	ctx = WithOpenAIProfitControlSuppressed(ctx)
	accounts, err := s.gateway.listSchedulableAccounts(ctx, key.GroupID, platform)
	if err != nil {
		return nil, err
	}
	excluded := map[int64]struct{}{}
	for i := range accounts {
		if !providerFileProfileSupported(&accounts[i]) {
			excluded[accounts[i].ID] = struct{}{}
		}
	}
	selection, _, err := s.gateway.SelectAccountWithSchedulerForCapability(ctx, key.GroupID, "", "", "", excluded, OpenAIUpstreamTransportHTTPSSE, "", false, false, false, platform)
	if err != nil {
		return nil, err
	}
	if selection == nil || !providerFileProfileSupported(selection.Account) {
		if selection != nil && selection.Acquired && selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return nil, ErrProviderFilesUnsupported
	}
	return selection, nil
}

func (s *ProviderFilesService) Upload(ctx context.Context, key *APIKey, a *Account, input *ProviderFileUpload, idempotency string) (*ProviderFile, error) {
	if s == nil || s.repo == nil || s.gateway == nil || s.gateway.httpUpstream == nil {
		return nil, ErrProviderFileStore
	}
	if !providerFileProfileSupported(a) {
		return nil, ErrProviderFilesUnsupported
	}
	if len(idempotency) > 128 {
		return nil, errors.New("invalid Idempotency-Key")
	}
	for _, ch := range idempotency {
		if ch < 33 || ch > 126 {
			return nil, errors.New("invalid Idempotency-Key")
		}
	}
	owner, err := snapshotCNControlAccount(a)
	if err != nil {
		return nil, err
	}
	scope, err := ProviderFileAccessScope(key, owner.Platform)
	if err != nil {
		return nil, err
	}
	principal, err := providerFilePrincipal(owner)
	if err != nil {
		return nil, err
	}
	if input == nil {
		return nil, errors.New("file is required")
	}
	if idempotency == "" {
		idempotency = uuid.NewString()
	}
	keyDigest := providerFilesIdempotencyDigest(scope, idempotency)
	params := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s", input.Filename, input.MIMEType, input.Purpose, input.ExpirySeconds, input.Bytes, input.ContentDigest)))
	file := &ProviderFile{PublicID: ProviderFileIDPrefix + uuid.NewString(), UserID: scope.UserID, APIKeyID: scope.APIKeyID, GroupID: scope.GroupID, Platform: scope.Platform, AccountID: owner.ID, Principal: principal, Filename: input.Filename, MIMEType: input.MIMEType, Purpose: input.Purpose, Bytes: input.Bytes, ContentDigest: input.ContentDigest, IdempotencyDigest: keyDigest, ParameterDigest: hex.EncodeToString(params[:]), State: ProviderFileUploading, CreatedAt: time.Now().UTC()}
	file.RequestSnapshotDigest, err = providerFileRequestSnapshotDigest(owner)
	if err != nil {
		return nil, err
	}
	if input.ExpirySeconds > 0 {
		file.ExpiresAt = file.CreatedAt.Add(time.Duration(input.ExpirySeconds) * time.Second)
	}
	if file.Platform == PlatformMiniMax {
		file.ExpiresAt = file.CreatedAt.Add(6 * 24 * time.Hour)
	}
	reserved, created, err := s.repo.Reserve(ctx, file, owner)
	if err != nil {
		if errors.Is(err, ErrProviderFileOwnerChanged) || errors.Is(err, ErrProviderFileQuota) {
			return nil, err
		}
		return nil, ErrProviderFileStore
	}
	if !created {
		if !providerFileScopeAllows(scope, reserved) || reserved.ParameterDigest != file.ParameterDigest || reserved.Principal != principal || reserved.AccountID != owner.ID {
			return nil, ErrProviderFileConflict
		}
		if reserved.State != ProviderFileReady || !reserved.ExpiresAt.IsZero() && !reserved.ExpiresAt.After(time.Now()) {
			return nil, ErrProviderFileConflict
		}
		return reserved, nil
	}
	remote, err := s.uploadRemote(ctx, owner, input)
	if err != nil {
		updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.repo.SetState(updateCtx, file.PublicID, ProviderFileUncertain, "")
		return nil, ErrProviderFileUpstream
	}
	file.RemoteID = remote
	file.State = ProviderFileReady
	if err = s.repo.Publish(ctx, file, owner); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_ = s.repo.SetState(cleanupCtx, file.PublicID, ProviderFileUncertain, remote)
		if owner.Platform == PlatformDeepseek {
			if s.deleteRemote(cleanupCtx, owner, remote) == nil {
				_ = s.repo.SetState(cleanupCtx, file.PublicID, ProviderFileDeleted, remote)
			}
		}
		return nil, ErrProviderFileStore
	}
	return file, nil
}

func (s *ProviderFilesService) remoteURL(a *Account, remoteID string) (string, error) {
	if !providerFileProfileSupported(a) {
		return "", ErrProviderFilesUnsupported
	}
	base := a.GetAnthropicProtocolBaseURL()
	if a.Platform == PlatformMiniMax {
		base = "https://api.minimax.io"
	}
	base, err := s.gateway.validateUpstreamBaseURL(base)
	if err != nil {
		return "", err
	}
	base = strings.TrimRight(base, "/")
	if a.Platform == PlatformMiniMax {
		return base + "/v1/files/upload", nil
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	base += "/files"
	if remoteID != "" {
		base += "/" + url.PathEscape(remoteID)
	}
	return base, nil
}

func (s *ProviderFilesService) remoteRequest(ctx context.Context, a *Account, method, remoteID, contentType string, body io.Reader) (*http.Response, error) {
	target, err := s.remoteURL(a, remoteID)
	if err != nil {
		return nil, err
	}
	proxy, err := a.StrictProxyURL()
	if err != nil {
		return nil, err
	}
	ctx = WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(ctx), a)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if a.Platform == PlatformMiniMax {
		req.Header.Set("Authorization", "Bearer "+a.GetCredential("api_key"))
	} else {
		req.Header.Set("Anthropic-Beta", "files-api-2025-04-14")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		if a.IsDomesticOAuth() {
			req.Header.Set("X-Dsh-Auth-Token", a.GetCredential("access_token"))
		} else {
			req.Header.Set("X-Api-Key", a.GetCredential("api_key"))
		}
	}
	response, err := s.gateway.httpUpstream.Do(prepareAccountOutboundRequest(req, a), proxy, a.ID, maxInt(a.Concurrency, 1))
	if err != nil {
		return nil, errors.New("file provider request failed")
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("file provider response unavailable")
	}
	return response, nil
}

func (s *ProviderFilesService) uploadRemote(ctx context.Context, owner *Account, input *ProviderFileUpload) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		var err error
		if owner.Platform == PlatformMiniMax {
			err = form.WriteField("purpose", input.Purpose)
		}
		if err == nil && input.ExpirySeconds > 0 {
			err = form.WriteField("expires_after[anchor]", "created_at")
			if err == nil {
				err = form.WriteField("expires_after[seconds]", strconv.FormatInt(input.ExpirySeconds, 10))
			}
		}
		if err == nil {
			header := make(map[string][]string)
			header["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.ReplaceAll(strings.ReplaceAll(input.Filename, `\`, `\\`), `"`, `\"`))}
			header["Content-Type"] = []string{input.MIMEType}
			part, partErr := form.CreatePart(header)
			err = partErr
			if err == nil {
				_, err = io.Copy(part, input.Data)
			}
		}
		if err == nil {
			err = form.Close()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	response, err := s.remoteRequest(ctx, owner, http.MethodPost, "", form.FormDataContentType(), reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-done
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	_ = reader.Close()
	writeErr := <-done
	if writeErr != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		return "", ErrProviderFileUpstream
	}
	if err != nil || len(data) > 1<<20 || response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("file provider rejected upload or returned an incomplete response")
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(data, &wire) != nil {
		return "", errors.New("invalid file provider response")
	}
	remote := ""
	if owner.Platform == PlatformMiniMax {
		var base struct {
			StatusCode *int64 `json:"status_code"`
		}
		var meta struct {
			ID json.RawMessage `json:"file_id"`
		}
		if json.Unmarshal(wire["base_resp"], &base) != nil || base.StatusCode == nil || *base.StatusCode != 0 || json.Unmarshal(wire["file"], &meta) != nil {
			return "", errors.New("file provider business operation failed")
		}
		if json.Unmarshal(meta.ID, &remote) != nil {
			var number json.Number
			if json.Unmarshal(meta.ID, &number) != nil {
				return "", errors.New("invalid file provider ID")
			}
			value, err := strconv.ParseInt(number.String(), 10, 64)
			if err != nil || value < 0 || value > 9007199254740991 {
				return "", errors.New("invalid file provider ID")
			}
			remote = number.String()
		}
	} else {
		var kind string
		_ = json.Unmarshal(wire["id"], &remote)
		_ = json.Unmarshal(wire["type"], &kind)
		if kind != "file" {
			return "", errors.New("unexpected Files API family")
		}
	}
	if remote == "" || len(remote) > 256 || strings.ContainsAny(remote, "/\\\x00\r\n") || strings.HasPrefix(remote, ProviderFileIDPrefix) {
		return "", errors.New("invalid file provider ID")
	}
	return remote, nil
}

func (s *ProviderFilesService) deleteRemote(ctx context.Context, owner *Account, remote string) error {
	response, err := s.remoteRequest(ctx, owner, http.MethodDelete, remote, "", nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 || response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("file provider deletion failed")
	}
	var result struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &result) != nil || result.ID != remote || result.Type != "file_deleted" {
		return errors.New("file provider did not acknowledge deletion")
	}
	return nil
}

func (s *ProviderFilesService) Get(ctx context.Context, key *APIKey, platform, id string) (*ProviderFile, error) {
	if s == nil || s.repo == nil {
		return nil, ErrProviderFileStore
	}
	scope, err := ProviderFileAccessScope(key, platform)
	if err != nil {
		return nil, err
	}
	file, err := s.repo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrProviderFileNotFound) {
			return nil, err
		}
		return nil, ErrProviderFileStore
	}
	if !providerFileScopeAllows(scope, file) || file.State != ProviderFileReady || file.RemoteID == "" || !file.ExpiresAt.IsZero() && !file.ExpiresAt.After(time.Now()) {
		return nil, ErrProviderFileNotFound
	}
	return file, nil
}

func (s *ProviderFilesService) List(ctx context.Context, key *APIKey, platform, after, before string, limit int) ([]*ProviderFile, error) {
	if s == nil || s.repo == nil {
		return nil, ErrProviderFileStore
	}
	if platform != PlatformDeepseek {
		return nil, ErrProviderFilesUnsupported
	}
	scope, err := ProviderFileAccessScope(key, platform)
	if err != nil {
		return nil, err
	}
	for _, cursor := range []string{after, before} {
		if cursor != "" {
			if _, err = s.Get(ctx, key, platform, cursor); err != nil {
				return nil, err
			}
		}
	}
	return s.repo.List(ctx, scope, after, before, limit)
}

func (s *ProviderFilesService) Delete(ctx context.Context, key *APIKey, platform, id string) error {
	if s == nil || s.repo == nil {
		return ErrProviderFileStore
	}
	scope, err := ProviderFileAccessScope(key, platform)
	if err != nil {
		return err
	}
	file, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !providerFileScopeAllows(scope, file) {
		return ErrProviderFileNotFound
	}
	if file.State == ProviderFileDeleted {
		return nil
	}
	var owner *Account
	var ownerErr error
	deleteSnapshot := ""
	if platform != PlatformMiniMax {
		if s.gateway == nil || s.gateway.accountRepo == nil {
			ownerErr = ErrProviderFileStore
		} else {
			owner, ownerErr = s.gateway.accountRepo.GetByID(ctx, file.AccountID)
			if ownerErr == nil {
				principal, err := providerFilePrincipal(owner)
				if err != nil || principal != file.Principal {
					ownerErr = ErrProviderFileOwnerChanged
				} else {
					owner, ownerErr = snapshotCNControlAccount(owner)
					if ownerErr == nil {
						deleteSnapshot, ownerErr = providerFileRequestSnapshotDigest(owner)
					}
				}
			}
		}
	}
	token := uuid.NewString()
	file, acquired, err := s.repo.BeginDelete(ctx, id, token, deleteSnapshot)
	if err != nil {
		return err
	}
	if !acquired {
		return ErrProviderFileConflict
	}
	finished := false
	defer func() {
		if !finished {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.repo.FinishDelete(releaseCtx, id, token, false)
		}
	}()
	if platform == PlatformMiniMax {
		finished = s.repo.FinishDelete(ctx, id, token, false) == nil
		return ErrProviderFilesUnsupported
	}
	if ownerErr != nil || owner == nil {
		return ErrProviderFileOwnerChanged
	}
	slot, err := s.gateway.tryAcquireAccountSlot(ctx, owner.ID, maxInt(owner.Concurrency, 1))
	if err != nil {
		return ErrProviderFileStore
	}
	if slot == nil || !slot.Acquired {
		return ErrProviderFileConflict
	}
	if slot.ReleaseFunc != nil {
		defer slot.ReleaseFunc()
	}
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = s.deleteRemote(deleteCtx, owner, file.RemoteID); err != nil {
		return ErrProviderFileUpstream
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	err = s.repo.FinishDelete(finishCtx, id, token, true)
	finished = err == nil
	return err
}

type providerFilesOwnerConstraint struct {
	AccountID int64
	Principal string
}
type providerFilesOwnerContextKey struct{}

func providerFilesAccountAllowed(ctx context.Context, account *Account) bool {
	constraint, ok := ctx.Value(providerFilesOwnerContextKey{}).(providerFilesOwnerConstraint)
	if !ok {
		return true
	}
	if account == nil || account.ID != constraint.AccountID {
		return false
	}
	principal, err := providerFilePrincipal(account)
	return err == nil && principal == constraint.Principal
}

// Scheduler projections intentionally omit credentials and row creation. They
// may narrow by account ID only; the hydrated winner must pass the full scope
// check before returning to the forwarding handler.
func providerFilesAccountCandidateAllowed(ctx context.Context, account *Account) bool {
	constraint, ok := ctx.Value(providerFilesOwnerContextKey{}).(providerFilesOwnerConstraint)
	if !ok {
		return true
	}
	if account == nil || account.ID != constraint.AccountID {
		return false
	}
	if account.CredentialGeneration != "" || account.CNQuotaGeneration != "" {
		return true
	}
	return providerFilesAccountAllowed(ctx, account)
}

// ProviderFileReferencesPresent inspects decoded protocol fields, so equivalent
// JSON escapes have identical ownership semantics. Text and unknown fields do
// not acquire Files semantics merely by mentioning its namespace.
func ProviderFileReferencesPresent(body []byte) bool {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return false
	}
	found := false
	_ = visitProviderFileReferenceFields(root, func(_ string, target map[string]any, field string) error {
		raw, _ := target[field].(string)
		if strings.HasPrefix(strings.TrimPrefix(raw, "mm_file://"), ProviderFileIDPrefix) {
			found = true
		}
		return nil
	})
	return found
}

func visitProviderFileReferenceFields(root map[string]any, visit func(string, map[string]any, string) error) error {
	var content func(any) error
	content = func(value any) error {
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		for _, item := range items {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := block["type"].(string)
			var target map[string]any
			field := "file_id"
			switch kind {
			case "file", "input_file":
				target = block
				if nested, ok := block["file"].(map[string]any); ok {
					target = nested
				}
			case "image", "video", "input_image", "image_url", "video_url":
				if source, ok := block["source"].(map[string]any); ok {
					sourceType, _ := source["type"].(string)
					switch sourceType {
					case "file":
						target = source
					case "url":
						target = source
						field = "url"
					}
				}
				if target == nil {
					if nested, ok := block[kind].(map[string]any); ok {
						target = nested
						field = "url"
					} else if _, ok := block["image_url"].(string); ok {
						target = block
						field = "image_url"
					}
				}
			case "tool_result":
				if err := content(block["content"]); err != nil {
					return err
				}
			}
			if target != nil {
				if err := visit(kind, target, field); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := content(root["input"]); err != nil {
		return err
	}
	for _, field := range []string{"messages", "input"} {
		if messages, ok := root[field].([]any); ok {
			for _, message := range messages {
				if object, ok := message.(map[string]any); ok {
					if err := content(object["content"]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// ResolveReferences visits protocol content only. Tool arguments, unknown
// sibling objects and native references never become mapping lookup fallbacks.
func (s *ProviderFilesService) ResolveReferences(ctx context.Context, key *APIKey, platform string, body []byte) (context.Context, []byte, error) {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return ctx, nil, err
	}
	changed := false
	var constraint providerFilesOwnerConstraint
	err := visitProviderFileReferenceFields(root, func(kind string, target map[string]any, field string) error {
		raw, ok := target[field].(string)
		if !ok {
			return nil
		}
		prefix := ""
		id := raw
		if strings.HasPrefix(id, "mm_file://") {
			prefix = "mm_file://"
			id = strings.TrimPrefix(id, prefix)
		}
		if !strings.HasPrefix(id, ProviderFileIDPrefix) {
			return nil
		}
		if platform == PlatformMiniMax && kind != "video_url" {
			return ErrProviderFilesUnsupported
		}
		file, err := s.Get(ctx, key, platform, id)
		if err != nil {
			return err
		}
		if constraint.AccountID != 0 && (constraint.AccountID != file.AccountID || constraint.Principal != file.Principal) {
			return ErrProviderFileOwnerChanged
		}
		constraint = providerFilesOwnerConstraint{file.AccountID, file.Principal}
		changed = true
		target[field] = prefix + file.RemoteID
		return nil
	})
	if err != nil {
		return ctx, nil, err
	}
	if !changed {
		return ctx, body, nil
	}
	rewritten, err := json.Marshal(root)
	if err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, providerFilesOwnerContextKey{}, constraint), rewritten, nil
}

// ProviderFileSnapshotMatches is shared with the SQL owner CAS. Neither digest
// nor any credential is stored in the public metadata response.
func ProviderFileSnapshotMatches(expected, current *Account) bool {
	if expected == nil || current == nil || !expected.CreatedAt.Equal(current.CreatedAt) {
		return false
	}
	before, e1 := providerFileRequestSnapshotDigest(expected)
	after, e2 := providerFileRequestSnapshotDigest(current)
	return e1 == nil && e2 == nil && before == after
}

// Keep Files' administrative admission snapshot local to this capability.
// Changing a status or saved expiry fences an old upload/cleanup result; the
// existing credential generation contracts for other providers stay unchanged.
func providerFileRequestSnapshotDigest(account *Account) (string, error) {
	owner, err := cnControlOwnerFingerprint(account)
	if err != nil {
		return "", err
	}
	proxyStatus := ""
	var proxyExpiry *time.Time
	if account.ProxyID != nil && account.Proxy != nil {
		proxyStatus, proxyExpiry = account.Proxy.Status, account.Proxy.ExpiresAt
	}
	raw, err := json.Marshal(struct {
		Owner, AccountStatus, ProxyStatus string
		CreatedAt                         time.Time
		ProxyExpiresAt                    *time.Time
	}{owner, account.Status, proxyStatus, account.CreatedAt, proxyExpiry})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// Start is called only by production wiring. Constructors stay inert for
// deterministic tests. Cancellation stops active provider I/O; Stop is idempotent.
func (s *ProviderFilesService) Start() {
	if s == nil || s.repo == nil || s.gateway == nil {
		return
	}
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-s.recoveryCtx.Done():
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(s.recoveryCtx, 30*time.Second)
					_, err := s.RecoverPending(ctx)
					cancel()
					if err != nil && s.recoveryCtx.Err() == nil {
						logger.L().Warn("provider_files.cleanup_failed", zap.String("error_code", "provider_files_cleanup_unavailable"))
					}
				}
			}
		}()
	})
}
func (s *ProviderFilesService) Stop() {
	if s != nil {
		s.stopOnce.Do(func() {
			if s.stop != nil {
				s.stop()
			}
		})
	}
}

// RecoverPending never uploads, lists provider storage or guesses an unknown
// remote ID. Only durable known-ID intents with the exact original owner/route
// snapshot can be dispatched, under the same cross-instance mutation lease.
func (s *ProviderFilesService) RecoverPending(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.gateway == nil || s.gateway.accountRepo == nil {
		return 0, ErrProviderFileStore
	}
	files, err := s.repo.ListCleanup(ctx, 20)
	if err != nil {
		return 0, ErrProviderFileStore
	}
	completed := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		if file.Platform != PlatformDeepseek || file.RemoteID == "" || file.RequestSnapshotDigest == "" {
			continue
		}
		owner, err := s.gateway.accountRepo.GetByID(ctx, file.AccountID)
		if err != nil || owner == nil {
			continue
		}
		principal, err := providerFilePrincipal(owner)
		if err != nil || principal != file.Principal {
			continue
		}
		owner, err = snapshotCNControlAccount(owner)
		if err != nil {
			continue
		}
		digest, err := providerFileRequestSnapshotDigest(owner)
		if err != nil || digest != file.RequestSnapshotDigest {
			continue
		}
		token := uuid.NewString()
		leased, acquired, err := s.repo.BeginDelete(ctx, file.PublicID, token, file.RequestSnapshotDigest)
		if err != nil {
			return completed, ErrProviderFileStore
		}
		if !acquired {
			continue
		}
		if leased.RemoteID != file.RemoteID || leased.Principal != file.Principal || leased.RequestSnapshotDigest != file.RequestSnapshotDigest {
			_ = s.repo.FinishDelete(ctx, file.PublicID, token, false)
			continue
		}
		slot, err := s.gateway.tryAcquireAccountSlot(ctx, owner.ID, maxInt(owner.Concurrency, 1))
		if err != nil || slot == nil || !slot.Acquired {
			_ = s.repo.FinishDelete(ctx, file.PublicID, token, false)
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = s.deleteRemote(attemptCtx, owner, file.RemoteID)
		cancel()
		if slot.ReleaseFunc != nil {
			slot.ReleaseFunc()
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finishErr := s.repo.FinishDelete(finishCtx, file.PublicID, token, err == nil)
		finishCancel()
		if finishErr != nil {
			return completed, ErrProviderFileStore
		}
		if err == nil {
			completed++
		}
	}
	return completed, nil
}
