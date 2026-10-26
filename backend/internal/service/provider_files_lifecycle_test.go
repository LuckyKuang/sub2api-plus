//go:build unit || !integration

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/deepseek"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type providerFilesTestUpstream struct {
	calls   []*http.Request
	proxies []string
	send    func(*http.Request) (*http.Response, error)
}

func (u *providerFilesTestUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.calls = append(u.calls, r)
	u.proxies = append(u.proxies, proxy)
	if u.send != nil {
		return u.send(r)
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return providerFilesReply(`{"id":"file-api-uploaded","type":"file","size_bytes":108,"created_at":"2026-01-01T00:00:00Z","filename":"test.png","mime_type":"image/png"}`), nil
}
func (u *providerFilesTestUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func providerFilesUploadFixture(t *testing.T) (*ProviderFilesService, *providerFilesTestStore, *providerFilesTestUpstream, *APIKey, *Account, *ProviderFileUpload) {
	t.Helper()
	store := &providerFilesTestStore{records: map[string]*ProviderFile{}}
	upstream := new(providerFilesTestUpstream)
	gateway := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	key := &APIKey{ID: 3, UserID: 2, GroupID: int64Ptr(4)}
	account := &Account{ID: 11, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "secret-file-owner", "api_protocol": APIProtocolAnthropic}}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
	input, err := ValidateProviderFileUpload(PlatformDeepseek, "test.png", "image/png", "", 3600, bytes.NewReader(png))
	require.NoError(t, err)
	return NewProviderFilesService(store, gateway), store, upstream, key, account, input
}

func TestProviderFilesUploadWireAndIdempotentReuse(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(kind, func(t *testing.T) {
			svc, store, upstream, key, account, input := providerFilesUploadFixture(t)
			account.Type = kind
			if kind == AccountTypeOAuth {
				account.Credentials = map[string]any{"oauth_provider": PlatformDeepseek, "oauth_region": "cn", "oauth_login_id": "login-a", "access_token": "secret-native-owner"}
			}
			upstream.send = func(r *http.Request) (*http.Response, error) {
				if kind == AccountTypeAPIKey {
					require.Equal(t, "/anthropic/v1/files", r.URL.Path)
					require.Equal(t, "secret-file-owner", r.Header.Get("X-Api-Key"))
					require.Empty(t, r.Header.Get("X-Dsh-Auth-Token"))
				} else {
					require.Equal(t, "/anthropic/v1/files", r.URL.Path)
					require.Equal(t, "secret-native-owner", r.Header.Get("X-Dsh-Auth-Token"))
					require.Empty(t, r.Header.Get("X-Api-Key"))
				}
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Cookie"))
				require.Equal(t, "files-api-2025-04-14", r.Header.Get("Anthropic-Beta"))
				require.Equal(t, "2023-06-01", r.Header.Get("Anthropic-Version"))
				require.Equal(t, deepseek.UserAgent(deepseek.ResolveVersion()), r.Header.Get("User-Agent"))
				identity, ok := outboundidentity.FromContext(r.Context())
				require.True(t, ok)
				require.Equal(t, "deepseek-harness", identity.Originator)
				require.NotEmpty(t, identity.Version)
				form, err := r.MultipartReader()
				require.NoError(t, err)
				fields := map[string]string{}
				for {
					part, err := form.NextPart()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					data, err := io.ReadAll(part)
					require.NoError(t, err)
					if part.FormName() == "file" {
						require.Equal(t, "test.png", part.FileName())
						require.Equal(t, input.Bytes, int64(len(data)))
						require.Equal(t, "image/png", part.Header.Get("Content-Type"))
					} else {
						fields[part.FormName()] = string(data)
					}
				}
				require.Equal(t, map[string]string{"expires_after[anchor]": "created_at", "expires_after[seconds]": "3600"}, fields)
				return providerFilesReply(`{"id":"file-api-uploaded","type":"file","size_bytes":108,"created_at":"2026-01-01T00:00:00Z","filename":"test.png","mime_type":"image/png"}`), nil
			}
			file, err := svc.Upload(context.Background(), key, account, input, "same-key")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(file.PublicID, ProviderFileIDPrefix))
			require.Equal(t, "file-api-uploaded", file.RemoteID)
			require.Len(t, upstream.calls, 1)
			again, err := svc.Upload(context.Background(), key, account, input, "same-key")
			require.NoError(t, err)
			require.Equal(t, file.PublicID, again.PublicID)
			require.Len(t, upstream.calls, 1, "verified replay must not repeat upstream upload")
			differentBytes := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)
			differentBytes[32] = 1
			different, validationErr := ValidateProviderFileUpload(PlatformDeepseek, "test.png", "image/png", "", 3600, bytes.NewReader(differentBytes))
			require.NoError(t, validationErr)
			_, err = svc.Upload(context.Background(), key, account, different, "same-key")
			require.ErrorIs(t, err, ErrProviderFileConflict)
			require.Len(t, upstream.calls, 1)
			store.records[file.PublicID].ExpiresAt = time.Now().Add(-time.Second)
			_, err = svc.Upload(context.Background(), key, account, input, "same-key")
			require.ErrorIs(t, err, ErrProviderFileConflict)
			require.Len(t, upstream.calls, 1)
		})
	}
}

func TestProviderFilesUncertainAndCompensation(t *testing.T) {
	t.Run("lost response never replays", func(t *testing.T) {
		svc, store, upstream, key, account, input := providerFilesUploadFixture(t)
		upstream.send = func(r *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, r.Body)
			return nil, errors.New("lost response includes secret-file-owner")
		}
		_, err := svc.Upload(context.Background(), key, account, input, "lost")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-file-owner")
		_, err = svc.Upload(context.Background(), key, account, input, "lost")
		require.ErrorIs(t, err, ErrProviderFileConflict)
		require.Len(t, upstream.calls, 1)
		for _, record := range store.records {
			require.Equal(t, ProviderFileUncertain, record.State)
			require.Empty(t, record.RemoteID)
		}
	})
	t.Run("mapping failure cannot publish and cleans known remote", func(t *testing.T) {
		svc, store, upstream, key, account, input := providerFilesUploadFixture(t)
		store.publishErr = errors.New("storage failed")
		upstream.send = func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost {
				_, _ = io.Copy(io.Discard, r.Body)
				return providerFilesReply(`{"id":"file-api-uploaded","type":"file"}`), nil
			}
			require.Equal(t, http.MethodDelete, r.Method)
			require.Equal(t, "/anthropic/v1/files/file-api-uploaded", r.URL.Path)
			require.Equal(t, "secret-file-owner", r.Header.Get("X-Api-Key"))
			return providerFilesReply(`{"id":"file-api-uploaded","type":"file_deleted"}`), nil
		}
		result, err := svc.Upload(context.Background(), key, account, input, "save-fails")
		require.ErrorIs(t, err, ErrProviderFileStore)
		require.Nil(t, result)
		require.Len(t, upstream.calls, 2)
		for _, record := range store.records {
			require.Equal(t, ProviderFileDeleted, record.State)
		}
	})
}

func TestProviderFilesMiniMaxBusinessFailureAndChatReference(t *testing.T) {
	svc, store, upstream, key, account, _ := providerFilesUploadFixture(t)
	account.Platform = PlatformMiniMax
	account.Credentials = map[string]any{"api_key": "minimax-key", "api_protocol": APIProtocolChatCompletions, "base_url": "https://api.minimax.io/v1"}
	video := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)
	input, err := ValidateProviderFileUpload(PlatformMiniMax, "test.mp4", "video/mp4", "video_understanding", 0, bytes.NewReader(video))
	require.NoError(t, err)
	upstream.send = func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		require.Equal(t, "https://api.minimax.io/v1/files/upload", r.URL.String())
		require.Equal(t, "Bearer minimax-key", r.Header.Get("Authorization"))
		return providerFilesReply(`{"file":{"file_id":"must-not-publish"},"base_resp":{"status_code":1008,"status_msg":"quota"}}`), nil
	}
	result, err := svc.Upload(context.Background(), key, account, input, "business-fails")
	require.Error(t, err)
	require.Nil(t, result)
	for _, f := range store.records {
		require.Equal(t, ProviderFileUncertain, f.State)
	}
	upstream.send = func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		return providerFilesReply(`{"file":{"file_id":1234567},"base_resp":{"status_code":0}}`), nil
	}
	_, _ = input.Data.Seek(0, io.SeekStart)
	file, err := svc.Upload(context.Background(), key, account, input, "successful-video")
	require.NoError(t, err)
	require.Equal(t, "1234567", file.RemoteID)
	body := []byte(`{"model":"MiniMax-M3","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"mm_file://` + file.PublicID + `"}}]}]}`)
	ctx, rewritten, err := svc.ResolveReferences(context.Background(), key, PlatformMiniMax, body)
	require.NoError(t, err)
	require.Contains(t, string(rewritten), `mm_file://1234567`)
	require.True(t, providerFilesAccountAllowed(ctx, account))
}

type providerFilesAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *providerFilesAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}
func (r *providerFilesAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]Account, error) {
	if r.account == nil || r.account.Platform != platform {
		return nil, nil
	}
	return []Account{*r.account}, nil
}

func TestProviderFilesDeleteFailureRecoveryDoesNotRepeat(t *testing.T) {
	svc, store, upstream, key, account, input := providerFilesUploadFixture(t)
	svc.gateway.accountRepo = &providerFilesAccountRepo{account: account}
	file, err := svc.Upload(context.Background(), key, account, input, "delete")
	require.NoError(t, err)
	attempts := 0
	upstream.send = func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodDelete, r.Method)
		attempts++
		if attempts == 1 {
			return nil, errors.New("offline")
		}
		return providerFilesReply(`{"id":"file-api-uploaded","type":"file_deleted"}`), nil
	}
	require.Error(t, svc.Delete(context.Background(), key, PlatformDeepseek, file.PublicID))
	require.Equal(t, ProviderFileDeletePending, store.records[file.PublicID].State)
	_, _, err = svc.ResolveReferences(context.Background(), key, PlatformDeepseek, []byte(`{"messages":[{"content":[{"type":"file","file_id":"`+file.PublicID+`"}]}]}`))
	require.ErrorIs(t, err, ErrProviderFileNotFound)
	require.NoError(t, svc.Delete(context.Background(), key, PlatformDeepseek, file.PublicID))
	require.Equal(t, ProviderFileDeleted, store.records[file.PublicID].State)
	require.NoError(t, svc.Delete(context.Background(), key, PlatformDeepseek, file.PublicID))
	require.Equal(t, 2, attempts, "failure then recovery is exactly two attempts; successful deletion does not dispatch again")
}

func providerFilesReply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestProviderFilesDeepSeekUploadInferenceReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, upstream, key, account, input := providerFilesUploadFixture(t)
	file, err := svc.Upload(context.Background(), key, account, input, "inference")
	require.NoError(t, err)
	refs := []byte(`{"model":"deepseek-flash","max_tokens":32,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"` + file.PublicID + `"}},{"type":"text","text":"inspect"}]}]}`)
	upstream.send = func(r *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(data), `"file_id":"file-api-uploaded"`)
		require.NotContains(t, string(data), file.PublicID)
		require.Equal(t, "secret-file-owner", r.Header.Get("X-Api-Key"))
		require.Equal(t, "files-api-2025-04-14", r.Header.Get("Anthropic-Beta"))
		return providerFilesReply(`{"id":"msg-files","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":2}}`), nil
	}
	for i := 0; i < 2; i++ {
		ctx, rewritten, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, refs)
		require.NoError(t, err)
		request, _ := gin.CreateTestContext(httptest.NewRecorder())
		request.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(refs)).WithContext(ctx)
		result, err := svc.gateway.ForwardAsAnthropic(ctx, request, account, rewritten, "", "")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.EqualValues(t, 4, result.Usage.InputTokens)
		require.EqualValues(t, 2, result.Usage.OutputTokens)
	}
	require.Len(t, upstream.calls, 3, "one upload and two inference requests reuse the persistent mapping")
}

func TestProviderFilesMiniMaxUploadActualChatInference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _, upstream, key, account, _ := providerFilesUploadFixture(t)
	account.Platform = PlatformMiniMax
	account.Credentials = map[string]any{"api_key": "minimax-key", "api_protocol": APIProtocolChatCompletions, "base_url": "https://api.minimax.io/v1"}
	video := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)
	input, err := ValidateProviderFileUpload(PlatformMiniMax, "test.mp4", "video/mp4", "video_understanding", 0, bytes.NewReader(video))
	require.NoError(t, err)
	upstream.send = func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		return providerFilesReply(`{"file":{"file_id":"remote-video"},"base_resp":{"status_code":0}}`), nil
	}
	file, err := svc.Upload(context.Background(), key, account, input, "video-chat")
	require.NoError(t, err)
	refs := []byte(`{"model":"MiniMax-M3","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"mm_file://` + file.PublicID + `"}},{"type":"text","text":"inspect"}]}]}`)
	upstream.send = func(r *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Contains(t, string(data), `mm_file://remote-video`)
		require.NotContains(t, string(data), file.PublicID)
		require.Equal(t, "Bearer minimax-key", r.Header.Get("Authorization"))
		return providerFilesReply(`{"id":"chat-video","object":"chat.completion","model":"MiniMax-M3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`), nil
	}
	for i := 0; i < 2; i++ {
		ctx, rewritten, err := svc.ResolveReferences(context.Background(), key, PlatformMiniMax, refs)
		require.NoError(t, err)
		request, _ := gin.CreateTestContext(httptest.NewRecorder())
		request.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(refs)).WithContext(ctx)
		result, err := svc.gateway.ForwardAsChatCompletions(ctx, request, account, rewritten, "", "")
		require.NoError(t, err)
		require.NotNil(t, result)
		require.EqualValues(t, 4, result.Usage.InputTokens)
		require.EqualValues(t, 2, result.Usage.OutputTokens)
	}
	require.Len(t, upstream.calls, 3)
}

func TestProviderFilesRejectCredentialReplacementAtActualSend(t *testing.T) {
	account := &Account{ID: 11, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "original-key", "api_protocol": APIProtocolAnthropic}}
	principal, err := providerFilePrincipal(account)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), providerFilesOwnerContextKey{}, providerFilesOwnerConstraint{AccountID: account.ID, Principal: principal})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/anthropic/v1/messages", strings.NewReader(`{"model":"deepseek-flash"}`))
	require.NoError(t, err)
	account.Credentials["api_key"] = "replacement-key"
	prepareAccountOutboundRequest(request, account)
	require.ErrorIs(t, OutboundPreparationError(request.Context()), ErrProviderFileOwnerChanged, "send preparation must recheck the credential owner after scheduling")
}

func TestProviderFilesRestartCleanupUsesKnownOriginalSnapshot(t *testing.T) {
	for _, change := range []string{"same-owner", "proxy-changed", "credential-changed", "unknown-remote", "account-disabled", "proxy-disabled", "proxy-expiry-changed"} {
		t.Run(change, func(t *testing.T) {
			svc, store, upstream, key, account, input := providerFilesUploadFixture(t)
			if change == "proxy-disabled" || change == "proxy-expiry-changed" {
				proxyID := int64(8)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "original-proxy.example", Port: 8080, Status: StatusActive}
			}
			svc.gateway.accountRepo = &providerFilesAccountRepo{account: account}
			file, err := svc.Upload(context.Background(), key, account, input, "restart-cleanup")
			require.NoError(t, err)
			file.State = ProviderFileUncertain
			if change == "proxy-changed" {
				proxyID := int64(8)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: 8, Protocol: "http", Host: "replacement-proxy.example", Port: 8080}
			}
			if change == "credential-changed" {
				account.Credentials["api_key"] = "replacement-key"
			}
			if change == "unknown-remote" {
				file.RemoteID = ""
			}
			switch change {
			case "account-disabled":
				account.Status = StatusDisabled
			case "proxy-disabled":
				account.Proxy.Status = "inactive"
			case "proxy-expiry-changed":
				expires := time.Now().Add(-time.Minute)
				account.Proxy.ExpiresAt = &expires
			}
			attempts := 0
			upstream.send = func(r *http.Request) (*http.Response, error) {
				attempts++
				require.Equal(t, http.MethodDelete, r.Method)
				require.Equal(t, "file-api-uploaded", file.RemoteID)
				require.Equal(t, "secret-file-owner", r.Header.Get("X-Api-Key"))
				return providerFilesReply(`{"id":"file-api-uploaded","type":"file_deleted"}`), nil
			}
			// New service object shares only durable metadata, not an in-memory
			// upload/cleanup closure. No original bytes are passed to recovery.
			restarted := NewProviderFilesService(store, svc.gateway)
			completed, err := restarted.RecoverPending(context.Background())
			require.NoError(t, err)
			if change == "same-owner" {
				require.Equal(t, 1, completed)
				require.Equal(t, 1, attempts)
				require.Equal(t, ProviderFileDeleted, file.State)
			} else {
				require.Zero(t, completed)
				require.Zero(t, attempts)
				require.Equal(t, ProviderFileUncertain, file.State)
			}
			_, err = restarted.RecoverPending(context.Background())
			require.NoError(t, err)
			if change == "same-owner" {
				require.Equal(t, 1, attempts, "acknowledged cleanup is not dispatched after restart again")
			}
			restarted.Stop()
			restarted.Stop()
		})
	}
}

func TestProviderFilesMultipleOwnerReferencesFailBeforeSend(t *testing.T) {
	key := &APIKey{ID: 3, UserID: 2, GroupID: int64Ptr(4)}
	first := &ProviderFile{PublicID: ProviderFileIDPrefix + "first", UserID: 2, APIKeyID: 3, GroupID: 4, Platform: PlatformDeepseek, AccountID: 11, Principal: "owner-a", RemoteID: "remote-a", State: ProviderFileReady}
	second := *first
	second.PublicID = ProviderFileIDPrefix + "second"
	second.AccountID = 12
	second.Principal = "owner-b"
	second.RemoteID = "remote-b"
	svc := NewProviderFilesService(&providerFilesTestStore{records: map[string]*ProviderFile{first.PublicID: first, second.PublicID: &second}}, nil)
	body := []byte(`{"messages":[{"content":[{"type":"image","source":{"type":"file","file_id":"` + first.PublicID + `"}},{"type":"image","source":{"type":"file","file_id":"` + second.PublicID + `"}}]}]}`)
	_, rewritten, err := svc.ResolveReferences(context.Background(), key, PlatformDeepseek, body)
	require.ErrorIs(t, err, ErrProviderFileOwnerChanged)
	require.Nil(t, rewritten)
}

type providerFilesPoolRepo struct {
	AccountRepository
	accounts []Account
}

func (r *providerFilesPoolRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return r.accounts, nil
}
func (r *providerFilesPoolRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, ErrAccountNotFound
}

func TestProviderFilesSchedulerCannotFailoverToDifferentFileOwner(t *testing.T) {
	owner := Account{ID: 11, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 99, GroupIDs: []int64{4}, Credentials: map[string]any{"api_key": "file-owner-key", "api_protocol": APIProtocolAnthropic}}
	other := owner
	other.ID = 12
	other.Priority = 1
	other.Credentials = map[string]any{"api_key": "unrelated-key", "api_protocol": APIProtocolAnthropic}
	principal, err := providerFilePrincipal(&owner)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), providerFilesOwnerContextKey{}, providerFilesOwnerConstraint{owner.ID, principal})
	repo := &providerFilesPoolRepo{accounts: []Account{other, owner}}
	gateway := NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	groupID := int64(4)
	selection, _, err := gateway.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "deepseek-flash", nil, OpenAIUpstreamTransportHTTPSSE, "", false, false, false, PlatformDeepseek)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.EqualValues(t, 11, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	selection, _, err = gateway.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "deepseek-flash", map[int64]struct{}{11: {}}, OpenAIUpstreamTransportHTTPSSE, "", false, false, false, PlatformDeepseek)
	require.ErrorIs(t, err, ErrProviderFileOwnerChanged)
	require.Nil(t, selection, "an upstream failure cannot migrate a file request to the higher-priority unrelated account")
}

func TestProviderFilesExplicitRetryPinsOriginalOwner(t *testing.T) {
	svc, _, upstream, key, account, input := providerFilesUploadFixture(t)
	file, err := svc.Upload(context.Background(), key, account, input, "stable-retry")
	require.NoError(t, err)
	ctx, err := svc.ResolveUploadRetry(context.Background(), key, PlatformDeepseek, "stable-retry")
	require.NoError(t, err)
	require.True(t, providerFilesAccountAllowed(ctx, account))
	other := *account
	other.ID++
	require.False(t, providerFilesAccountAllowed(ctx, &other))
	require.Len(t, upstream.calls, 1)
	file.State = ProviderFileUncertain
	_, err = svc.ResolveUploadRetry(context.Background(), key, PlatformDeepseek, "stable-retry")
	require.ErrorIs(t, err, ErrProviderFileConflict)
	require.Len(t, upstream.calls, 1)
}

func TestProviderFilesMiniMaxLocalRevokeIsExplicitlyUnsupportedRemoteDelete(t *testing.T) {
	svc, _, upstream, key, account, _ := providerFilesUploadFixture(t)
	account.Platform = PlatformMiniMax
	account.Credentials = map[string]any{"api_key": "minimax-key", "api_protocol": APIProtocolChatCompletions, "base_url": "https://api.minimax.io/v1"}
	input, err := ValidateProviderFileUpload(PlatformMiniMax, "test.mp4", "video/mp4", "video_understanding", 0, bytes.NewReader(append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)))
	require.NoError(t, err)
	upstream.send = func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		return providerFilesReply(`{"file":{"file_id":"native-video"},"base_resp":{"status_code":0}}`), nil
	}
	file, err := svc.Upload(context.Background(), key, account, input, "revoke")
	require.NoError(t, err)
	require.ErrorIs(t, svc.Delete(context.Background(), key, PlatformMiniMax, file.PublicID), ErrProviderFilesUnsupported)
	_, err = svc.Get(context.Background(), key, PlatformMiniMax, file.PublicID)
	require.ErrorIs(t, err, ErrProviderFileNotFound)
	require.Len(t, upstream.calls, 1, "local revocation cannot pretend to have dispatched a supported remote delete")
}
