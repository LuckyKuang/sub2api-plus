//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type startPlanIntegrationUpstream struct {
	before func()
	body   string
}

func (u *startPlanIntegrationUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	if u.before != nil {
		u.before()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func (u *startPlanIntegrationUpstream) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, c)
}

const startPlanIntegrationBody = `{"data":{"plans":[{"plan_id":"start-plan","status":"active"}],"balances":[{"capabilities":["model:GLM-5.3"]}]}}`

func newStartPlanIntegrationAccount(t *testing.T) (*accountRepository, *service.Account, *schedulerCacheRecorder) {
	t.Helper()
	repo, initial, cache := newCNControlSnapshotFixture(t)
	credentials := map[string]any{"oauth_provider": "zai", "account_mode": service.AccountModeCoding, "plan_kind": service.ZhipuPlanStartPlan, "zcode_jwt_token": "linked-start-jwt", "api_key": "linked-start-jwt",
		service.SettingKeyOutboundIdentity: service.OutboundIdentitySelection{Preset: zcode.Preset, Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"}}}
	payload, err := json.Marshal(credentials)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform='zhipu',credentials=$2::jsonb WHERE id=$1`, initial.ID, string(payload))
	require.NoError(t, err)
	a, err := repo.GetByID(context.Background(), initial.ID)
	require.NoError(t, err)
	return repo, a, cache
}

func startPlanIntegrationService(repo service.AccountRepository, upstream service.HTTPUpstream) *service.CNProviderQuotaService {
	return service.NewCNProviderQuotaService(repo, nil, upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}})
}

func TestZhipuStartPlanPostgresCommitOutboxAndCrossInstanceSelection(t *testing.T) {
	repo, a, cache := newStartPlanIntegrationAccount(t)
	result, err := startPlanIntegrationService(repo, &startPlanIntegrationUpstream{body: startPlanIntegrationBody}).QueryUsageForAccount(context.Background(), a)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.True(t, result.Persisted)
	require.Equal(t, "available", result.StartPlan.State)
	require.Equal(t, 1, cnControlOutboxCount(t, a.ID))
	require.Equal(t, []int64{a.ID}, cache.deleteIDs)
	// A new repository instance must enforce the persisted grant independently
	// of the probing process's memory. Unrelated coding quota remains untouched.
	other := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	got, err := other.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	require.True(t, got.IsModelSupported("GLM-5.3"))
	require.False(t, got.IsModelSupported("GLM-4.7"))
	require.Equal(t, a.Extra["kimi_weekly_used_percent"], got.Extra["kimi_weekly_used_percent"])
	require.Equal(t, "base", got.Extra["retained"])
	require.Equal(t, a.Credentials, got.Credentials)
	// Redis slim candidate metadata must carry the product discriminator and
	// owner-bound observation, without retaining the Start business JWT.
	rdb := testRedis(t)
	redisCache := NewSchedulerCache(rdb)
	bucket := service.SchedulerBucket{GroupID: a.ID, Platform: service.PlatformZhipu, Mode: service.SchedulerModeSingle}
	token, err := redisCache.CaptureBucketWriteToken(context.Background(), bucket)
	require.NoError(t, err)
	require.NoError(t, redisCache.SetSnapshot(context.Background(), bucket, token, []service.Account{*got}))
	secondCache := NewSchedulerCache(rdb)
	list, hit, err := secondCache.GetSnapshot(context.Background(), bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, list, 1)
	require.Equal(t, service.ZhipuPlanStartPlan, list[0].GetCredential("plan_kind"))
	require.Empty(t, list[0].GetCredential("zcode_jwt_token"))
	require.Empty(t, list[0].GetCredential("api_key"))
	require.True(t, list[0].IsModelSupported("GLM-5.3"))
	require.False(t, list[0].IsModelSupported("GLM-4.7"))
}

func TestZhipuStartPlanPostgresLateObservationCannotReplaceNewObservation(t *testing.T) {
	repo, a, _ := newStartPlanIntegrationAccount(t)
	arrived, release := make(chan struct{}), make(chan struct{})
	first := startPlanIntegrationService(repo, &startPlanIntegrationUpstream{body: startPlanIntegrationBody, before: func() { close(arrived); <-release }})
	type outcome struct {
		result *service.CNProviderQuotaProbeResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := first.QueryUsageForAccount(context.Background(), a)
		done <- outcome{result, err}
	}()
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("first instance did not reach balance")
	}
	otherRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	secondOwner, err := otherRepo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	negative := startPlanIntegrationService(otherRepo, &startPlanIntegrationUpstream{body: `{"data":{"plans":[]}}`})
	second, err := negative.QueryUsageForAccount(context.Background(), secondOwner)
	require.NoError(t, err)
	require.True(t, second.Success)
	require.Equal(t, "unavailable", second.StartPlan.State)
	close(release)
	select {
	case firstResult := <-done:
		require.NoError(t, firstResult.err)
		require.False(t, firstResult.result.Success)
		require.False(t, firstResult.result.Persisted)
	case <-time.After(3 * time.Second):
		t.Fatal("late observation did not finish")
	}
	got, err := otherRepo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	require.False(t, got.IsModelSupported("GLM-5.3"), "late available response cannot erase the committed negative entitlement")
	require.Equal(t, 1, cnControlOutboxCount(t, a.ID), "failed observation CAS cannot emit another outbox event")
}

func TestZhipuStartPlanPostgresOwnerAndOutboxFailuresPreserveObservation(t *testing.T) {
	for _, mode := range []string{"relink", "estate", "plan", "outbox"} {
		t.Run(mode, func(t *testing.T) {
			repo, a, cache := newStartPlanIntegrationAccount(t)
			upstream := &startPlanIntegrationUpstream{body: startPlanIntegrationBody}
			if mode == "outbox" {
				repo.sql = &failAtomicSchedulerOutboxSQLExecutor{sqlExecutor: integrationDB}
			} else {
				upstream.before = func() {
					patch := map[string]string{"relink": `{"zcode_jwt_token":"new-login"}`, "estate": `{"oauth_provider":"bigmodel"}`, "plan": `{"plan_kind":"off-peak"}`}[mode]
					_, err := integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || $2::jsonb WHERE id=$1`, a.ID, patch)
					require.NoError(t, err)
				}
			}
			result, err := startPlanIntegrationService(repo, upstream).QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.False(t, result.Persisted)
			got, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			require.Equal(t, a.Extra, got.Extra)
			require.Zero(t, cnControlOutboxCount(t, a.ID))
			require.Empty(t, cache.deleteIDs)
			require.Empty(t, cache.setAccounts)
		})
	}
}
