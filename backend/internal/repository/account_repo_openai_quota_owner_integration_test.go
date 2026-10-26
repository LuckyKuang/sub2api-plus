//go:build integration

package repository

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type integrationOpenAIQuotaOwnerWriter interface {
	UpdateOpenAIQuotaCredentialsIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error)
}

// Both persistence entrypoints cross the same boundary. Before the fix the
// actual service uses UpdateCredentials and overwrites the external relink.
// Afterward its CAS entrypoint runs the identical external transaction hook.
type openAIQuotaOwnerRaceRepository struct {
	service.AccountRepository
	base          *accountRepository
	beforePersist func(context.Context) error
	persistCalls  int
}

func (r *openAIQuotaOwnerRaceRepository) before(ctx context.Context) error {
	r.persistCalls++
	if r.beforePersist == nil {
		return nil
	}
	hook := r.beforePersist
	r.beforePersist = nil
	return hook(ctx)
}

func (r *openAIQuotaOwnerRaceRepository) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	if err := r.before(ctx); err != nil {
		return err
	}
	return r.base.UpdateCredentials(ctx, id, credentials)
}

func (r *openAIQuotaOwnerRaceRepository) UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *service.Account, credentials map[string]any) (bool, error) {
	if err := r.before(ctx); err != nil {
		return false, err
	}
	writer, ok := any(r.base).(integrationOpenAIQuotaOwnerWriter)
	if !ok {
		return false, errors.New("atomic quota owner persistence is unavailable")
	}
	return writer.UpdateOpenAIQuotaCredentialsIfUnchanged(ctx, expected, credentials)
}

type openAIQuotaOwnerTokenCache struct {
	service.GeminiTokenCache
	tokens []string
}

func (*openAIQuotaOwnerTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return "", nil
}
func (c *openAIQuotaOwnerTokenCache) SetAccessToken(_ context.Context, _ string, token string, _ time.Duration) error {
	c.tokens = append(c.tokens, token)
	return nil
}
func (*openAIQuotaOwnerTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (*openAIQuotaOwnerTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

type openAIQuotaOwnerRefresh struct{ calls int }

func (*openAIQuotaOwnerRefresh) CanRefresh(*service.Account) bool                  { return true }
func (*openAIQuotaOwnerRefresh) NeedsRefresh(*service.Account, time.Duration) bool { return true }
func (*openAIQuotaOwnerRefresh) CacheKey(a *service.Account) string {
	return service.OpenAITokenCacheKey(a)
}
func (r *openAIQuotaOwnerRefresh) Refresh(_ context.Context, a *service.Account) (map[string]any, error) {
	r.calls++
	credentials := make(map[string]any, len(a.Credentials))
	for key, value := range a.Credentials {
		credentials[key] = value
	}
	credentials["access_token"], credentials["refresh_token"] = "rotated-original-access", "rotated-original-refresh"
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	return credentials, nil
}

type openAIQuotaOwnerWire struct {
	agent   bool
	headers []http.Header
	routes  []string
}

func (w *openAIQuotaOwnerWire) factory(route string) (*req.Client, error) {
	client := req.C()
	client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
		return func(request *http.Request) (*http.Response, error) {
			w.headers = append(w.headers, request.Header.Clone())
			w.routes = append(w.routes, route)
			status, body := http.StatusOK, `{"code":"ok","windows_reset":1}`
			if w.agent && len(w.headers) == 1 {
				status, body = http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}
	})
	return client, nil
}

func newOpenAIQuotaOwnerFixture(t *testing.T, agent bool) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	repo, original, cache := newCNControlSnapshotFixture(t)
	credentials := map[string]any{
		"chatgpt_account_id": "original-chatgpt-owner", "access_token": "original-access", "refresh_token": "original-refresh",
		"expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339), "_token_version": 1,
		"user_agent": "codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color",
	}
	if agent {
		key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
		der, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		credentials["auth_mode"], credentials["agent_runtime_id"], credentials["task_id"] = service.OpenAIAuthModeAgentIdentity, "original-runtime", "original-task"
		credentials["agent_private_key"] = base64.StdEncoding.EncodeToString(der)
	}
	raw, err := json.Marshal(credentials)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='openai',type='oauth',status='active',credentials=$2::jsonb WHERE id=$1`, original.ID, string(raw))
	require.NoError(t, err)
	expected, err := repo.GetByID(context.Background(), original.ID)
	require.NoError(t, err)
	return repo, expected, cache
}

func openAIQuotaOwnerRelink(ctx context.Context, id int64) error {
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-login-access","refresh_token":"replacement-login-refresh","chatgpt_account_id":"replacement-chatgpt-owner","agent_runtime_id":"replacement-runtime","task_id":"replacement-task","_token_version":9000}'::jsonb WHERE id=$1`, id)
	return err
}

func TestOpenAIQuotaOwnerRefreshFinalWriteCannotOverwriteExternalRelink(t *testing.T) {
	for _, relink := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-owner-rotation", true: "external-relink-after-final-validation"}[relink], func(t *testing.T) {
			repo, expected, schedulerCache := newOpenAIQuotaOwnerFixture(t, false)
			wrapped := &openAIQuotaOwnerRaceRepository{AccountRepository: repo, base: repo}
			if relink {
				wrapped.beforePersist = func(ctx context.Context) error { return openAIQuotaOwnerRelink(ctx, expected.ID) }
			}
			tokenCache, refresh, wire := &openAIQuotaOwnerTokenCache{}, &openAIQuotaOwnerRefresh{}, &openAIQuotaOwnerWire{}
			tokens := service.NewOpenAITokenProvider(wrapped, tokenCache, nil)
			tokens.SetRefreshAPI(service.NewOAuthRefreshAPI(wrapped, nil), refresh)
			quota := service.NewOpenAIQuotaService(wrapped, nil, tokens, wire.factory)
			_, err := quota.ResetCredit(context.Background(), expected.ID)
			got, loadErr := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, loadErr)
			require.Equal(t, 1, wrapped.persistCalls, "the external write must happen after application validation at the actual persistence boundary")
			require.Equal(t, 1, refresh.calls)
			if relink {
				assert.Error(t, err)
				assert.Equal(t, "replacement-chatgpt-owner", got.GetCredential("chatgpt_account_id"))
				assert.Equal(t, "replacement-login-access", got.GetCredential("access_token"))
				assert.Equal(t, "replacement-login-refresh", got.GetCredential("refresh_token"))
				assert.Empty(t, wire.headers, "the rejected rotation cannot authorize a quota consume")
				assert.Empty(t, tokenCache.tokens)
				assert.Zero(t, cnControlOutboxCount(t, expected.ID))
				assert.Empty(t, schedulerCache.setAccounts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "original-chatgpt-owner", got.GetCredential("chatgpt_account_id"))
			require.Equal(t, "rotated-original-access", got.GetCredential("access_token"))
			require.Equal(t, []string{"rotated-original-access"}, tokenCache.tokens)
			require.Len(t, wire.headers, 1)
			require.Equal(t, "Bearer rotated-original-access", wire.headers[0].Get("Authorization"))
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, schedulerCache.setAccounts)
			require.Equal(t, []int64{expected.ID}, schedulerCache.deleteIDs)
		})
	}
}

// Exercise the real Agent auth transport over a local CONNECT proxy with a
// trusted test CA; no auth URL override or upstream network is involved.
func openAIQuotaTaskRegistrationProxy(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		DNSNames: []string{"auth.openai.com"}, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, public, private)
	require.NoError(t, err)
	caFile := filepath.Join(t.TempDir(), "quota-task-test-ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	t.Setenv("CODEX_CA_CERTIFICATE", caFile)
	calls := &atomic.Int64{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect || request.Host != "auth.openai.com:443" {
			t.Errorf("unexpected auth proxy request: %s %s", request.Method, request.Host)
			return
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack auth proxy: %v", err)
			return
		}
		defer connection.Close()
		_, _ = io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n")
		tlsConnection := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, MinVersion: tls.VersionTLS12})
		defer tlsConnection.Close()
		upstream, err := http.ReadRequest(bufio.NewReader(tlsConnection))
		if err != nil {
			t.Errorf("read local auth request: %v", err)
			return
		}
		defer upstream.Body.Close()
		_, _ = io.Copy(io.Discard, upstream.Body)
		if upstream.URL.Path != "/api/accounts/v1/agent/original-runtime/task/register" {
			t.Errorf("unexpected task registration path: %s", upstream.URL.Path)
			return
		}
		calls.Add(1)
		body := `{"task_id":"registered-original-task"}`
		_, _ = fmt.Fprintf(tlsConnection, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL, calls
}

func bindOpenAIQuotaTaskProxy(t *testing.T, repo *accountRepository, expected *service.Account, route string) *service.Account {
	t.Helper()
	u, err := url.Parse(route)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	p := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: fmt.Sprintf("quota-task-%d", expected.ID), Protocol: "http", Host: u.Hostname(), Port: port, Status: service.StatusActive})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", expected.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM proxies WHERE id=$1", p.ID)
	})
	_, err = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=$2 WHERE id=$1", expected.ID, p.ID)
	require.NoError(t, err)
	bound, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	return bound
}

func TestOpenAIQuotaOwnerTaskFinalWriteCannotOverwriteExternalRelink(t *testing.T) {
	for _, relink := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-owner-task", true: "external-relink-after-final-validation"}[relink], func(t *testing.T) {
			proxy, registryCalls := openAIQuotaTaskRegistrationProxy(t)
			repo, expected, cache := newOpenAIQuotaOwnerFixture(t, true)
			expected = bindOpenAIQuotaTaskProxy(t, repo, expected, proxy)
			wrapped := &openAIQuotaOwnerRaceRepository{AccountRepository: repo, base: repo}
			if relink {
				wrapped.beforePersist = func(ctx context.Context) error { return openAIQuotaOwnerRelink(ctx, expected.ID) }
			}
			wire := &openAIQuotaOwnerWire{agent: true}
			quota := service.NewOpenAIQuotaService(wrapped, nil, nil, wire.factory)
			_, err := quota.ResetCredit(context.Background(), expected.ID)
			got, readErr := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, readErr)
			require.EqualValues(t, 1, registryCalls.Load())
			require.Equal(t, 1, wrapped.persistCalls)
			if relink {
				assert.Error(t, err)
				assert.Equal(t, "replacement-chatgpt-owner", got.GetCredential("chatgpt_account_id"))
				assert.Equal(t, "replacement-runtime", got.GetCredential("agent_runtime_id"))
				assert.Equal(t, "replacement-task", got.GetCredential("task_id"))
				assert.Len(t, wire.headers, 1, "a task registered for the old owner cannot authorize a retry")
				assert.Zero(t, cnControlOutboxCount(t, expected.ID))
				assert.Empty(t, cache.setAccounts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "registered-original-task", got.GetCredential("task_id"))
			require.Equal(t, "original-runtime", got.GetCredential("agent_runtime_id"))
			require.Len(t, wire.headers, 2)
			encoded := strings.TrimPrefix(wire.headers[1].Get("Authorization"), "AgentAssertion ")
			raw, err := base64.RawURLEncoding.DecodeString(encoded)
			require.NoError(t, err)
			var assertion map[string]string
			require.NoError(t, json.Unmarshal(raw, &assertion))
			require.Equal(t, "registered-original-task", assertion["task_id"])
			require.Equal(t, 1, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
			require.Equal(t, []int64{expected.ID}, cache.deleteIDs)
		})
	}
}

func TestOpenAIQuotaOwnerTaskCASMissReusesConcurrentSameOwnerTask(t *testing.T) {
	proxy, registryCalls := openAIQuotaTaskRegistrationProxy(t)
	repo, expected, cache := newOpenAIQuotaOwnerFixture(t, true)
	expected = bindOpenAIQuotaTaskProxy(t, repo, expected, proxy)
	wrapped := &openAIQuotaOwnerRaceRepository{AccountRepository: repo, base: repo, beforePersist: func(ctx context.Context) error {
		_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"task_id":"concurrently-registered-task"}'::jsonb WHERE id=$1`, expected.ID)
		return err
	}}
	wire := &openAIQuotaOwnerWire{agent: true}
	quota := service.NewOpenAIQuotaService(wrapped, nil, nil, wire.factory)
	_, err := quota.ResetCredit(context.Background(), expected.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, registryCalls.Load())
	require.Equal(t, 1, wrapped.persistCalls)
	got, err := repo.GetByID(context.Background(), expected.ID)
	require.NoError(t, err)
	require.Equal(t, "concurrently-registered-task", got.GetCredential("task_id"))
	require.Equal(t, "original-runtime", got.GetCredential("agent_runtime_id"))
	require.Len(t, wire.headers, 2)
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(wire.headers[1].Get("Authorization"), "AgentAssertion "))
	require.NoError(t, err)
	var assertion map[string]string
	require.NoError(t, json.Unmarshal(raw, &assertion))
	require.Equal(t, "concurrently-registered-task", assertion["task_id"])
	require.Zero(t, cnControlOutboxCount(t, expected.ID), "the losing registration must not publish an event")
	require.Empty(t, cache.setAccounts, "the losing registration must not publish a credential snapshot")
}

func TestOpenAIQuotaOwnerPersistenceFailureStopsQuotaAndCache(t *testing.T) {
	for _, mode := range []string{"refresh", "agent-task"} {
		for _, fault := range []string{"storage", "outbox"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				agent := mode == "agent-task"
				repo, expected, schedulerCache := newOpenAIQuotaOwnerFixture(t, agent)
				if agent {
					proxy, _ := openAIQuotaTaskRegistrationProxy(t)
					expected = bindOpenAIQuotaTaskProxy(t, repo, expected, proxy)
				}
				if fault == "storage" {
					repo.sql = &openAIQuotaOwnerStorageFaultSQL{sqlExecutor: integrationDB}
				} else {
					repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
				}
				wrapped := &openAIQuotaOwnerRaceRepository{AccountRepository: repo, base: repo}
				tokenCache, refresh, wire := &openAIQuotaOwnerTokenCache{}, &openAIQuotaOwnerRefresh{}, &openAIQuotaOwnerWire{agent: agent}
				tokens := service.NewOpenAITokenProvider(wrapped, tokenCache, nil)
				tokens.SetRefreshAPI(service.NewOAuthRefreshAPI(wrapped, nil), refresh)
				quota := service.NewOpenAIQuotaService(wrapped, nil, tokens, wire.factory)
				_, err := quota.ResetCredit(context.Background(), expected.ID)
				require.Error(t, err, "a persistence failure cannot authorize consume or a task retry")
				require.Equal(t, 1, wrapped.persistCalls)
				got, err := repo.GetByID(context.Background(), expected.ID)
				require.NoError(t, err)
				require.Equal(t, expected.Credentials, got.Credentials)
				if agent {
					require.Len(t, wire.headers, 1)
				} else {
					require.Empty(t, wire.headers)
				}
				require.Empty(t, tokenCache.tokens)
				require.Empty(t, schedulerCache.setAccounts)
				require.Zero(t, cnControlOutboxCount(t, expected.ID))
			})
		}
	}
}

type openAIQuotaOwnerStorageFaultSQL struct{ sqlExecutor }

func (e *openAIQuotaOwnerStorageFaultSQL) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("quota credential storage unavailable")
}

func TestOpenAIQuotaOwnerCASOutboxFailureAndCallerRollbackPublishNothing(t *testing.T) {
	for _, scenario := range []string{"storage-failure", "outbox-failure", "caller-rollback"} {
		t.Run(scenario, func(t *testing.T) {
			repo, expected, cache := newOpenAIQuotaOwnerFixture(t, false)
			writer, ok := any(repo).(integrationOpenAIQuotaOwnerWriter)
			require.True(t, ok, "quota persistence requires one atomic owner and outbox boundary")
			ctx := context.Background()
			var tx *dbent.Tx
			var err error
			if scenario == "storage-failure" {
				repo.sql = &openAIQuotaOwnerStorageFaultSQL{sqlExecutor: integrationDB}
			} else if scenario == "outbox-failure" {
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			} else {
				tx, err = integrationEntClient.Tx(ctx)
				require.NoError(t, err)
				defer tx.Rollback()
				ctx = dbent.NewTxContext(ctx, tx)
			}
			credentials := map[string]any{"chatgpt_account_id": "original-chatgpt-owner", "access_token": "rotation-that-must-not-publish"}
			applied, err := writer.UpdateOpenAIQuotaCredentialsIfUnchanged(ctx, expected, credentials)
			if scenario != "caller-rollback" {
				require.Error(t, err)
				require.False(t, applied)
			} else {
				require.NoError(t, err)
				require.True(t, applied)
				require.Empty(t, cache.setAccounts, "uncommitted credentials cannot enter scheduler cache")
				require.NoError(t, tx.Rollback())
			}
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			require.Equal(t, expected.Credentials, got.Credentials)
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}

func TestOpenAIQuotaOwnerCASRechecksConcurrentAccountAndProxyWriters(t *testing.T) {
	for _, mutation := range []string{"relink", "same-proxy-route-edit"} {
		t.Run(mutation, func(t *testing.T) {
			repo, expected, cache := newOpenAIQuotaOwnerFixture(t, false)
			expected = bindCNProxyOwner(t, repo, expected)
			writer, ok := any(repo).(integrationOpenAIQuotaOwnerWriter)
			require.True(t, ok)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			external, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer external.Rollback()
			if mutation == "relink" {
				_, err = external.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"chatgpt_account_id":"new-owner","access_token":"new-login"}'::jsonb WHERE id=$1`, expected.ID)
			} else {
				_, err = external.ExecContext(ctx, `UPDATE proxies SET password='new-route-secret' WHERE id=$1`, *expected.ProxyID)
			}
			require.NoError(t, err)
			type outcome struct {
				applied bool
				err     error
			}
			done := make(chan outcome, 1)
			go func() {
				applied, err := writer.UpdateOpenAIQuotaCredentialsIfUnchanged(ctx, expected, map[string]any{"access_token": "late-old-owner-token"})
				done <- outcome{applied, err}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%/* openai_quota_owner_cas */%')`).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "the actual CAS statement must wait for the cross-instance account or proxy writer")
			require.NoError(t, external.Commit())
			result := <-done
			require.NoError(t, result.err)
			require.False(t, result.applied)
			got, err := repo.GetByID(context.Background(), expected.ID)
			require.NoError(t, err)
			if mutation == "relink" {
				require.Equal(t, "new-login", got.GetCredential("access_token"))
			} else {
				require.Equal(t, expected.Credentials, got.Credentials)
			}
			require.Zero(t, cnControlOutboxCount(t, expected.ID))
			require.Empty(t, cache.setAccounts)
		})
	}
}
