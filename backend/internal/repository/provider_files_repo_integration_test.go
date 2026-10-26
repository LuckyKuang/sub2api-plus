//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func providerFilesPGFixture(t *testing.T) (*service.Account, *service.ProviderFile) {
	t.Helper()
	ctx := context.Background()
	account := &service.Account{Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "isolated-files-key", "api_protocol": service.APIProtocolAnthropic}}
	credentials, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	err = integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,status) VALUES($1,$2,$3,$4,'active') RETURNING id,created_at`, "provider-files-"+uuid.NewString(), account.Platform, account.Type, credentials).Scan(&account.ID, &account.CreatedAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM provider_files WHERE account_id=$1`, account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id=$1`, account.ID)
	})
	file := &service.ProviderFile{PublicID: service.ProviderFileIDPrefix + uuid.NewString(), UserID: account.ID, APIKeyID: 100, GroupID: 200, Platform: account.Platform, AccountID: account.ID, Principal: "isolated-principal", State: service.ProviderFileUploading, Filename: "test.png", MIMEType: "image/png", Bytes: 108, ContentDigest: "received-byte-digest", IdempotencyDigest: uuid.NewString(), ParameterDigest: "full-parameter-digest", CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	return account, file
}

type providerFilesPGAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *providerFilesPGAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}

type providerFilesPGUpstream struct{ deletes atomic.Int64 }

func (u *providerFilesPGUpstream) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	status := 200
	body := `{"id":"file-api-pg","type":"file"}`
	if request.Method == http.MethodDelete {
		if u.deletes.Add(1) == 1 {
			status = 503
			body = `{"error":{"type":"overloaded_error"}}`
		} else {
			body = `{"id":"file-api-pg","type":"file_deleted"}`
		}
	} else {
		_, _ = io.Copy(io.Discard, request.Body)
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}
func (u *providerFilesPGUpstream) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func TestProviderFilesPersistentUploadReferenceAndRestartCleanup(t *testing.T) {
	ctx := context.Background()
	account, _ := providerFilesPGFixture(t)
	upstream := new(providerFilesPGUpstream)
	gateway := service.NewOpenAIGatewayService(&providerFilesPGAccountRepo{account: account}, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	first := service.NewProviderFilesService(NewProviderFilesRepository(integrationDB), gateway)
	second := service.NewProviderFilesService(NewProviderFilesRepository(integrationDB), gateway)
	groupID := int64(200)
	key := &service.APIKey{ID: 100, UserID: account.ID, GroupID: &groupID}
	input, err := service.ValidateProviderFileUpload(account.Platform, "test.png", "image/png", "", 0, bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)))
	require.NoError(t, err)
	file, err := first.Upload(ctx, key, account, input, "persistent-file")
	require.NoError(t, err)
	require.NotEmpty(t, file.RequestSnapshotDigest)
	_, rewritten, err := second.ResolveReferences(ctx, key, account.Platform, []byte(`{"messages":[{"content":[{"type":"image","source":{"type":"file","file_id":"`+file.PublicID+`"}}]}]}`))
	require.NoError(t, err)
	require.Contains(t, string(rewritten), `"file_id":"file-api-pg"`)
	require.ErrorIs(t, first.Delete(ctx, key, account.Platform, file.PublicID), service.ErrProviderFileUpstream)
	stored, err := NewProviderFilesRepository(integrationDB).Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileDeletePending, stored.State)
	require.Equal(t, file.RequestSnapshotDigest, stored.RequestSnapshotDigest)
	_, err = integrationDB.ExecContext(ctx, `UPDATE provider_files SET updated_at=NOW()-INTERVAL '2 minutes' WHERE public_id=$1`, file.PublicID)
	require.NoError(t, err)
	type result struct {
		completed int
		err       error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, worker := range []*service.ProviderFilesService{first, second} {
		wg.Add(1)
		go func(worker *service.ProviderFilesService) {
			defer wg.Done()
			<-start
			n, err := worker.RecoverPending(ctx)
			results <- result{n, err}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(results)
	completed := 0
	for result := range results {
		require.NoError(t, result.err)
		completed += result.completed
	}
	require.Equal(t, 1, completed)
	require.EqualValues(t, 2, upstream.deletes.Load(), "one failed foreground delete and one successful durable recovery")
	stored, err = NewProviderFilesRepository(integrationDB).Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileDeleted, stored.State)
	n, err := second.RecoverPending(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	require.EqualValues(t, 2, upstream.deletes.Load())
}

func TestProviderFilesRepositoryPersistenceIdempotencyAndFencing(t *testing.T) {
	ctx := context.Background()
	account, file := providerFilesPGFixture(t)
	first := NewProviderFilesRepository(integrationDB)
	second := NewProviderFilesRepository(integrationDB)
	reserved, created, err := first.Reserve(ctx, file, account)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, file.PublicID, reserved.PublicID)
	duplicate := *file
	duplicate.PublicID = service.ProviderFileIDPrefix + uuid.NewString()
	again, created, err := second.Reserve(ctx, &duplicate, account)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, file.PublicID, again.PublicID)
	file.RemoteID = "file-api-isolated"
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{api_key}','"replacement-key"') WHERE id=$1`, account.ID)
	require.NoError(t, err)
	require.ErrorIs(t, first.Publish(ctx, file, account), service.ErrProviderFileOwnerChanged)
	reloaded, err := second.Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileUploading, reloaded.State)
	require.Empty(t, reloaded.RemoteID, "old owner response must not publish a remote ID")
	credentials, err := json.Marshal(account.Credentials)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=$2 WHERE id=$1`, account.ID, credentials)
	require.NoError(t, err)
	require.NoError(t, second.Publish(ctx, file, account))
	reloaded, err = first.Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileReady, reloaded.State)
	require.Equal(t, file.RemoteID, reloaded.RemoteID)
	var persisted string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT to_jsonb(f)::text FROM provider_files f WHERE public_id=$1`, file.PublicID).Scan(&persisted))
	require.NotContains(t, persisted, "isolated-files-key")
	require.NotContains(t, persisted, "replacement-key")
	require.NotContains(t, persisted, "file_data")
	scope := service.ProviderFileScope{UserID: file.UserID, APIKeyID: file.APIKeyID, GroupID: file.GroupID, Platform: file.Platform}
	listed, err := first.List(ctx, scope, "", "", 20)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	scope.APIKeyID++
	listed, err = second.List(ctx, scope, "", "", 20)
	require.NoError(t, err)
	require.Empty(t, listed)
	_, acquired, err := first.BeginDelete(ctx, file.PublicID, "lease-a", "snapshot-a")
	require.NoError(t, err)
	require.True(t, acquired)
	_, acquired, err = second.BeginDelete(ctx, file.PublicID, "lease-b", "snapshot-b")
	require.NoError(t, err)
	require.False(t, acquired, "a second instance cannot dispatch the same deletion concurrently")
	require.ErrorIs(t, second.FinishDelete(ctx, file.PublicID, "lease-b", true), service.ErrProviderFileConflict)
	require.NoError(t, first.FinishDelete(ctx, file.PublicID, "lease-a", false))
	reloaded, err = second.Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileDeletePending, reloaded.State)
	_, acquired, err = second.BeginDelete(ctx, file.PublicID, "lease-b", "snapshot-b")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, second.FinishDelete(ctx, file.PublicID, "lease-b", true))
	_, acquired, err = first.BeginDelete(ctx, file.PublicID, "lease-c", "snapshot-c")
	require.NoError(t, err)
	require.False(t, acquired)
	reloaded, err = first.Get(ctx, file.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.ProviderFileDeleted, reloaded.State)
}

func TestProviderFilesRepositoryCrossInstanceUploadReservation(t *testing.T) {
	ctx := context.Background()
	account, file := providerFilesPGFixture(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	type result struct {
		file    *service.ProviderFile
		created bool
		err     error
	}
	results := make(chan result, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			candidate := *file
			candidate.PublicID = service.ProviderFileIDPrefix + uuid.NewString()
			repo := NewProviderFilesRepository(integrationDB)
			f, created, err := repo.Reserve(ctx, &candidate, account)
			results <- result{f, created, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	createdCount := 0
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.file)
		if winner == "" {
			winner = result.file.PublicID
		}
		require.Equal(t, winner, result.file.PublicID)
		if result.created {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount, "exactly one instance may send the upload for the same explicit key")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM provider_files WHERE idempotency_digest=$1`, file.IdempotencyDigest).Scan(&count))
	require.Equal(t, 1, count)
}

func TestProviderFilesRepositoryPaginationCursorCannotReadAnotherScope(t *testing.T) {
	ctx := context.Background()
	account, prototype := providerFilesPGFixture(t)
	repo := NewProviderFilesRepository(integrationDB)
	scope := service.ProviderFileScope{UserID: prototype.UserID, APIKeyID: prototype.APIKeyID, GroupID: prototype.GroupID, Platform: prototype.Platform}
	baseTime := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
	save := func(owner *service.Account, candidate *service.ProviderFile) {
		t.Helper()
		_, created, err := repo.Reserve(ctx, candidate, owner)
		require.NoError(t, err)
		require.True(t, created)
		candidate.RemoteID = "file-api-pagination-" + uuid.NewString()
		require.NoError(t, repo.Publish(ctx, candidate, owner))
	}
	owned := make([]*service.ProviderFile, 0, 3)
	for index := range 3 {
		file := *prototype
		file.PublicID = service.ProviderFileIDPrefix + uuid.NewString()
		file.IdempotencyDigest = uuid.NewString()
		file.CreatedAt = baseTime.Add(time.Duration(index) * time.Hour)
		save(account, &file)
		owned = append(owned, &file)
	}
	// Usable same-scope cursors retain ordinary chronological pagination.
	after, err := repo.List(ctx, scope, owned[1].PublicID, "", 20)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, owned[2].PublicID, after[0].PublicID)
	before, err := repo.List(ctx, scope, "", owned[1].PublicID, 20)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, owned[0].PublicID, before[0].PublicID)

	for _, dimension := range []string{"user", "api-key", "group", "platform", "unknown"} {
		t.Run(dimension, func(t *testing.T) {
			cursor := service.ProviderFileIDPrefix + uuid.NewString()
			if dimension != "unknown" {
				foreignOwner, foreign := providerFilesPGFixture(t)
				foreign.PublicID = cursor
				foreign.UserID, foreign.APIKeyID, foreign.GroupID, foreign.Platform = scope.UserID, scope.APIKeyID, scope.GroupID, scope.Platform
				foreign.CreatedAt = owned[1].CreatedAt
				switch dimension {
				case "user":
					foreign.UserID = foreignOwner.ID
				case "api-key":
					foreign.APIKeyID++
				case "group":
					foreign.GroupID++
				case "platform":
					foreign.Platform, foreignOwner.Platform = service.PlatformMiniMax, service.PlatformMiniMax
					_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET platform=$2 WHERE id=$1`, foreignOwner.ID, foreignOwner.Platform)
					require.NoError(t, err)
				}
				save(foreignOwner, foreign)
			}
			for _, direction := range []string{"after", "before"} {
				t.Run(direction, func(t *testing.T) {
					afterID, beforeID := cursor, ""
					if direction == "before" {
						afterID, beforeID = "", cursor
					}
					files, err := repo.List(ctx, scope, afterID, beforeID, 20)
					require.NoError(t, err)
					require.Empty(t, files, "an inaccessible cursor must behave like an unknown ID; its existence or timestamp cannot change the caller's page")
				})
			}
		})
	}
}

func TestProviderFilesRepositoryOwnerCASLocksProxyBeforeAccount(t *testing.T) {
	for _, operation := range []string{"reserve", "publish"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			account, file := providerFilesPGFixture(t)
			var proxyID int64
			err := integrationDB.QueryRowContext(ctx, `INSERT INTO proxies(name,protocol,host,port) VALUES($1,'http','files-lock.example',8080) RETURNING id`, "files-lock-"+uuid.NewString()).Scan(&proxyID)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET proxy_id=NULL WHERE id=$1`, account.ID)
				_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM proxies WHERE id=$1`, proxyID)
			})
			_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET proxy_id=$2 WHERE id=$1`, account.ID, proxyID)
			require.NoError(t, err)
			account.ProxyID = &proxyID
			account.Proxy = &service.Proxy{ID: proxyID, Protocol: "http", Host: "files-lock.example", Port: 8080, Status: service.StatusActive}
			repo := NewProviderFilesRepository(integrationDB)
			if operation == "publish" {
				_, created, err := repo.Reserve(ctx, file, account)
				require.NoError(t, err)
				require.True(t, created)
				file.RemoteID = "file-api-known-lock-order"
			}

			// Proxy mutation owns its proxy before invalidating/rebinding accounts.
			// A Files owner CAS waiting there must not retain the account lock that
			// the same mutation needs; otherwise the two transactions deadlock.
			writer, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = writer.Rollback() }()
			var lockedProxyID int64
			require.NoError(t, writer.QueryRowContext(ctx, `SELECT id FROM proxies WHERE id=$1 FOR UPDATE`, proxyID).Scan(&lockedProxyID))
			var writerPID int
			require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
			done := make(chan error, 1)
			go func() {
				if operation == "publish" {
					done <- repo.Publish(ctx, file, account)
					return
				}
				_, _, err := repo.Reserve(ctx, file, account)
				done <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'
 AND $1=ANY(pg_blocking_pids(pid))
 AND query LIKE '%proxies%' AND pid<>pg_backend_pid())`, writerPID).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "Files persistence must actually wait for the held proxy writer")
			var lockedAccountID int64
			accountLockErr := writer.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT`, account.ID).Scan(&lockedAccountID)
			// Release the proxy even in the failing implementation, and wait for
			// its in-flight operation before checking the business assertion.
			require.NoError(t, writer.Rollback())
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("Files operation did not finish after the proxy lock was released")
			}
			require.NoError(t, accountLockErr, "Files waiting for a proxy must not lock the account required by that proxy's editor")
			require.Equal(t, account.ID, lockedAccountID)
		})
	}
}

func TestProviderFilesRepositoryOwnerCASRejectsChangedAdmissionSnapshot(t *testing.T) {
	for _, operation := range []string{"reserve", "publish"} {
		for _, mutation := range []string{"account-status", "proxy-status", "proxy-expiry"} {
			t.Run(operation+"/"+mutation, func(t *testing.T) {
				ctx := context.Background()
				account, file := providerFilesPGFixture(t)
				account.Status = service.StatusActive
				var proxyID int64
				err := integrationDB.QueryRowContext(ctx, `INSERT INTO proxies(name,protocol,host,port,status) VALUES($1,'http','files-state.example',8080,'active') RETURNING id`, "files-state-"+uuid.NewString()).Scan(&proxyID)
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET proxy_id=NULL WHERE id=$1`, account.ID)
					_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM proxies WHERE id=$1`, proxyID)
				})
				_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET proxy_id=$2 WHERE id=$1`, account.ID, proxyID)
				require.NoError(t, err)
				account.ProxyID = &proxyID
				account.Proxy = &service.Proxy{ID: proxyID, Protocol: "http", Host: "files-state.example", Port: 8080, Status: service.StatusActive}
				repo := NewProviderFilesRepository(integrationDB)
				if operation == "publish" {
					_, created, err := repo.Reserve(ctx, file, account)
					require.NoError(t, err)
					require.True(t, created)
					file.RemoteID = "file-api-old-admission-snapshot"
				}
				switch mutation {
				case "account-status":
					_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET status='disabled' WHERE id=$1`, account.ID)
				case "proxy-status":
					_, err = integrationDB.ExecContext(ctx, `UPDATE proxies SET status='inactive' WHERE id=$1`, proxyID)
				case "proxy-expiry":
					_, err = integrationDB.ExecContext(ctx, `UPDATE proxies SET expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, proxyID)
				}
				require.NoError(t, err)
				// The account and proxy state were accepted by scheduling when
				// captured. A later administrative mutation cannot authorize an
				// upload reservation or publish that old operation's result.
				if operation == "reserve" {
					_, created, err := repo.Reserve(ctx, file, account)
					require.ErrorIs(t, err, service.ErrProviderFileOwnerChanged)
					require.False(t, created)
					var count int
					require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM provider_files WHERE public_id=$1`, file.PublicID).Scan(&count))
					require.Zero(t, count, "a stale admission snapshot cannot reserve an upstream upload")
					return
				}
				require.ErrorIs(t, repo.Publish(ctx, file, account), service.ErrProviderFileOwnerChanged)
				persisted, err := repo.Get(ctx, file.PublicID)
				require.NoError(t, err)
				require.Equal(t, service.ProviderFileUploading, persisted.State)
				require.Empty(t, persisted.RemoteID, "a stale upload response cannot become a usable mapping")
			})
		}
	}
}

func TestProviderFilesRepositoryQuotaAndProxyCAS(t *testing.T) {
	ctx := context.Background()
	account, file := providerFilesPGFixture(t)
	var proxyID int64
	err := integrationDB.QueryRowContext(ctx, `INSERT INTO proxies(name,protocol,host,port) VALUES($1,'http','proxy.example',8080) RETURNING id`, fmt.Sprintf("files-%d", account.ID)).Scan(&proxyID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM proxies WHERE id=$1`, proxyID)
	})
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET proxy_id=$2 WHERE id=$1`, account.ID, proxyID)
	require.NoError(t, err)
	account.ProxyID = &proxyID
	account.Proxy = &service.Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080, Status: service.StatusActive}
	repo := NewProviderFilesRepository(integrationDB)
	_, created, err := repo.Reserve(ctx, file, account)
	require.NoError(t, err)
	require.True(t, created)
	_, err = integrationDB.ExecContext(ctx, `UPDATE proxies SET host='edited-proxy.example' WHERE id=$1`, proxyID)
	require.NoError(t, err)
	file.RemoteID = "file-api-known"
	require.ErrorIs(t, repo.Publish(ctx, file, account), service.ErrProviderFileOwnerChanged, "a proxy edit during upload must fence the old result")
	_, err = integrationDB.ExecContext(ctx, `UPDATE proxies SET host='proxy.example' WHERE id=$1`, proxyID)
	require.NoError(t, err)
	// Reservations, uncertain results and local-only revoked files still consume
	// the conservative quota until their expiry; they are not invisible storage.
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO provider_files(public_id,user_id,api_key_id,group_id,platform,account_id,principal,state,filename,mime_type,bytes,content_digest,idempotency_digest,parameter_digest,created_at)
 SELECT 'file-s2a-quota-'||$1::bigint::text||'-'||g,$1::bigint,100,200,'deepseek',$1::bigint,'quota','uncertain','q.png','image/png',67108864,'digest','quota-'||$1::bigint::text||'-'||g,'params',NOW() FROM generate_series(1,400) g`, account.ID)
	require.NoError(t, err)
	next := *file
	next.PublicID = service.ProviderFileIDPrefix + uuid.NewString()
	next.IdempotencyDigest = uuid.NewString()
	_, _, err = repo.Reserve(ctx, &next, account)
	require.ErrorIs(t, err, service.ErrProviderFileQuota)
}
