//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type cnAPIKeyObservationIntegrationUpstream struct {
	before func()
	body   string
}

func (u cnAPIKeyObservationIntegrationUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if u.before != nil {
		u.before()
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func (u cnAPIKeyObservationIntegrationUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

// Exercise the public service and actual SQL writer together: an independent
// administrator transaction may replace an API key while its provider responds.
// The old result has no authority to stamp the new owner's balance/quota fresh.
func TestCNAPIKeyControlServicePersistsOnlyCurrentOwnerInPostgres(t *testing.T) {
	for _, probe := range []struct {
		name, platform, mode, base, body string
		quota                            bool
	}{
		{"deepseek-payg", "deepseek", "payg", "https://api.deepseek.com", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.5"}]}`, false},
		{"kimi-payg", "kimi", "payg", "https://api.moonshot.cn/v1", `{"code":0,"status":true,"data":{"available_balance":12.5}}`, false},
		{"kimi-coding", "kimi", "coding", "https://api.kimi.com/coding/v1", `{"usage":{"limit":100,"remaining":75}}`, true},
		{"zhipu-coding", "zhipu", "coding", "https://open.bigmodel.cn/api/coding/paas/v4", `{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}]}}`, true},
		{"minimax-coding", "minimax", "coding", "https://api.minimaxi.com/v1", `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":75}]}`, true},
	} {
		for _, mutation := range []string{"same-owner", "api-key", "nested-credentials"} {
			t.Run(probe.name+"/"+mutation, func(t *testing.T) {
				repo, initial, cache := newCNControlSnapshotFixture(t)
				credentials, err := json.Marshal(map[string]any{"api_key": "captured-control-key", "account_mode": probe.mode, "base_url": probe.base, "nested": map[string]any{"owner": "original"}})
				require.NoError(t, err)
				_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET platform=$2,type='apikey',credentials=$3::jsonb WHERE id=$1`, initial.ID, probe.platform, string(credentials))
				require.NoError(t, err)
				captured, err := repo.GetByID(context.Background(), initial.ID)
				require.NoError(t, err)
				var before string
				require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", initial.ID).Scan(&before))
				upstream := cnAPIKeyObservationIntegrationUpstream{body: probe.body, before: func() {
					switch mutation {
					case "api-key":
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"api_key":"replacement-key"}'::jsonb WHERE id=$1`, initial.ID)
					case "nested-credentials":
						_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET credentials=credentials || '{"nested":{"owner":"replacement"}}'::jsonb WHERE id=$1`, initial.ID)
					}
					require.NoError(t, err)
				}}
				var success, persisted bool
				if probe.quota {
					result, queryErr := service.NewCNProviderQuotaService(repo, nil, upstream, &config.Config{}).QueryUsageForAccount(context.Background(), captured)
					require.NoError(t, queryErr)
					success, persisted = result.Success, result.Persisted
				} else {
					result, queryErr := service.NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalanceForAccount(context.Background(), captured)
					require.NoError(t, queryErr)
					success, persisted = result.Success, result.Persisted
				}
				got, err := repo.GetByID(context.Background(), initial.ID)
				require.NoError(t, err)
				require.Equal(t, "base", got.Extra["retained"])
				if mutation == "same-owner" {
					require.True(t, success)
					require.True(t, persisted)
					require.Equal(t, captured.Credentials, got.Credentials)
					require.Empty(t, cache.setAccounts, "a committed CAS must not republish a possibly superseded credential snapshot")
					require.Equal(t, []int64{captured.ID}, cache.deleteIDs)
				} else {
					require.False(t, success)
					require.False(t, persisted)
					var after string
					require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT extra::text FROM accounts WHERE id=$1", initial.ID).Scan(&after))
					require.JSONEq(t, before, after)
					require.Zero(t, cnControlOutboxCount(t, initial.ID))
					require.Empty(t, cache.setAccounts)
					if mutation == "api-key" {
						require.Equal(t, "replacement-key", got.GetCredential("api_key"))
					}
				}
			})
		}
	}
}
