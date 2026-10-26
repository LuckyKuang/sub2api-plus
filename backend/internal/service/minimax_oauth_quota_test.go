//go:build unit

package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Protocol fixtures follow MiniMax Code 5821f7c9, matrix-account-client.ts:
// personal workspace owns X-Group-Id; the first model is primary; usage_count
// means remaining, and status=3 means unlimited. Credits are independent.
const miniMaxQuotaFixture = `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"MiniMax-M2.7","current_interval_remaining_percent":25,"end_time":1793516400000,"current_weekly_remaining_percent":80,"weekly_end_time":1794096000000},{"model_name":"video","current_interval_remaining_percent":0}]}`

func miniMaxQuotaTestService(a *Account, send func(*http.Request) (*http.Response, error)) (*CNProviderQuotaService, *cnQuotaRecoveryRepo, *CNOAuthService) {
	repo := &cnQuotaRecoveryRepo{refreshAPIAccountRepo: refreshAPIAccountRepo{account: a}}
	oauth := NewCNOAuthService(nil, repo, nil)
	oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	svc := NewCNProviderQuotaService(repo, nil, &cnRecoveryUpstream{send: send}, cnProbeAllowlistConfig("agent.minimaxi.com", "www.minimaxi.com", "agent.minimax.io", "platform.minimax.io"))
	svc.cnOAuthService = oauth
	return svc, repo, oauth
}

func miniMaxQuotaTestReply(req *http.Request) (*http.Response, error) {
	switch req.URL.Path {
	case "/v1/api/user/info":
		return cnRecoveryResponse(200, `{"statusInfo":{"code":0},"data":{"userInfo":{"realUserID":"user~* +"}}}`), nil
	case "/matrix/api/v1/user/get_user_extra_info":
		return cnRecoveryResponse(200, `{"workspaces":[{"workspace_type":1,"workspace_id":11,"op_group_id":"team-group","has_token_plan":true},{"workspace_type":0,"workspace_id":42,"op_group_id":"personal-group","has_token_plan":true}]}`), nil
	case "/matrix/api/v1/commerce/get_membership_info":
		return cnRecoveryResponse(200, `{"has_token_plan":true,"op_group_id":"other-group","op_credit_summary":{"total_remaining_amount":"0"}}`), nil
	case miniMaxOAuthQuotaPath:
		return cnRecoveryResponse(200, miniMaxQuotaFixture), nil
	default:
		return nil, fmt.Errorf("unexpected path")
	}
}

func TestMiniMaxOAuthQuotaFixedRegionPersonalGrantAndControlIdentity(t *testing.T) {
	for _, region := range []string{"cn", "global"} {
		t.Run(region, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformMiniMax)
			a.Credentials["oauth_region"] = region
			a.Credentials["base_url"] = "https://attacker.invalid"
			a.Credentials["api_key"] = "forbidden-api-key"
			a.Credentials["header_overrides"] = map[string]any{"Authorization": "forbidden", "X-Group-Id": "team-group", "User-Agent": "forbidden"}
			var paths []string
			svc, repo, _ := miniMaxQuotaTestService(a, func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.URL.Path)
				controlHost, quotaHost := "agent.minimaxi.com", "www.minimaxi.com"
				if region == "global" {
					controlHost, quotaHost = "agent.minimax.io", "platform.minimax.io"
				}
				assert.Equal(t, "Bearer old", req.Header.Get("Authorization"))
				assert.Empty(t, req.Header.Get("X-Stainless-Package-Version"))
				assert.Empty(t, req.Header.Get("Originator"))
				assert.Empty(t, req.Header.Get("Version"))
				assert.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				assert.Equal(t, "https", req.URL.Scheme)
				if req.URL.Path == miniMaxOAuthQuotaPath {
					assert.Equal(t, "MiniMaxAgent", req.UserAgent())
					assert.Equal(t, quotaHost, req.URL.Host)
					assert.Equal(t, http.MethodGet, req.Method)
					assert.Empty(t, req.URL.RawQuery)
					assert.Equal(t, "personal-group", req.Header.Get("X-Group-Id"))
					assert.Empty(t, req.Header.Get("yy"))
				} else {
					assert.Equal(t, "MiniMaxCode", req.UserAgent())
					assert.Equal(t, controlHost, req.URL.Host)
					assert.Empty(t, req.Header.Get("X-Group-Id"))
					q := req.URL.Query()
					assert.Equal(t, "22201", q.Get("version_code"))
					assert.Equal(t, "linux", q.Get("os_name"))
					assert.Equal(t, "0", q.Get("timezone_offset"))
					language := "zh"
					if region == "global" {
						language = "en"
					}
					assert.Equal(t, language, q.Get("lang"))
					payload := "{}"
					if req.URL.Path == "/v1/api/user/info" {
						assert.Equal(t, http.MethodGet, req.Method)
						assert.Equal(t, "0", q.Get("user_id"))
					} else {
						assert.Equal(t, http.MethodPost, req.Method)
						assert.Contains(t, req.URL.RawQuery, "user_id=user%7E*+%2B&client=mcode")
						body, err := io.ReadAll(req.Body)
						assert.NoError(t, err)
						payload = string(body)
						if req.URL.Path == "/matrix/api/v1/commerce/get_membership_info" {
							assert.JSONEq(t, `{"workspace_id":42}`, payload)
						} else {
							assert.Equal(t, "{}", payload)
						}
					}
					// Independently verify the official attribution formula over the captured
					// wire bytes. These expectations do not use the signing implementation.
					digest := func(value string) string { v := md5.Sum([]byte(value)); return hex.EncodeToString(v[:]) }
					encoded := url.QueryEscape(req.URL.EscapedPath() + "?" + req.URL.RawQuery)
					encoded = strings.ReplaceAll(encoded, "+", "%20")
					for from, to := range map[string]string{"%21": "!", "%27": "'", "%28": "(", "%29": ")", "%2A": "*"} {
						encoded = strings.ReplaceAll(encoded, from, to)
					}
					assert.Equal(t, digest(encoded+"_"+payload+digest(q.Get("unix"))+"ooui"), req.Header.Get("yy"))
					ms, err := strconv.ParseInt(q.Get("unix"), 10, 64)
					assert.NoError(t, err)
					seconds := strconv.FormatInt(ms/1000, 10)
					signature := seconds + "I*7Cf%WZ#S&%1RlZJ&C2"
					if req.Method == http.MethodPost {
						signature += payload
					}
					assert.Equal(t, seconds, req.Header.Get("x-timestamp"))
					assert.Equal(t, digest(signature), req.Header.Get("x-signature"))
				}
				return miniMaxQuotaTestReply(req)
			})
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Equal(t, []string{"/v1/api/user/info", "/matrix/api/v1/user/get_user_extra_info", "/matrix/api/v1/commerce/get_membership_info", miniMaxOAuthQuotaPath}, paths)
			require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 75, ResetAt: "2026-11-01T07:00:00Z"}, {Window: "weekly", UsedPercent: 20, ResetAt: "2026-11-08T00:00:00Z"}}, result.Tiers)
			require.Equal(t, 75.0, repo.updates["minimax_5h_used_percent"])
			require.Equal(t, cnQuotaGeneration(a), repo.updates["minimax_quota_generation"])
			require.Nil(t, result.Balance, "Credits must never become a plan balance")
		})
	}
}

func TestMiniMaxOAuthQuotaRefreshRestartsEntireBillingScope(t *testing.T) {
	for _, stage := range []string{"proactive", "/v1/api/user/info", "/matrix/api/v1/user/get_user_extra_info", "/matrix/api/v1/commerce/get_membership_info", miniMaxOAuthQuotaPath, "semantic-401"} {
		t.Run(stage, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformMiniMax)
			if stage == "proactive" {
				a.Credentials["expires_at"] = time.Now().Add(time.Second).UTC().Format(time.RFC3339)
			}
			rejected := false
			var paths []string
			svc, repo, oauth := miniMaxQuotaTestService(a, func(req *http.Request) (*http.Response, error) {
				fresh := req.Header.Get("Authorization") == "Bearer new"
				paths = append(paths, req.Header.Get("Authorization")+" "+req.URL.Path)
				if !fresh && !rejected && (req.URL.Path == stage || stage == "semantic-401" && req.URL.Path == "/matrix/api/v1/user/get_user_extra_info") {
					rejected = true
					if stage == "semantic-401" {
						return cnRecoveryResponse(200, `{"statusInfo":{"code":1000048}}`), nil
					}
					return cnRecoveryResponse(401, `{}`), nil
				}
				if fresh && req.URL.Path == "/matrix/api/v1/user/get_user_extra_info" {
					return cnRecoveryResponse(200, `{"workspaces":[{"workspace_type":0,"workspace_id":99,"op_group_id":"new-group","has_token_plan":true}]}`), nil
				}
				if fresh && req.URL.Path == "/matrix/api/v1/commerce/get_membership_info" {
					body, _ := io.ReadAll(req.Body)
					require.JSONEq(t, `{"workspace_id":99}`, string(body))
				}
				if req.URL.Path == miniMaxOAuthQuotaPath {
					require.True(t, fresh)
					require.Equal(t, "new-group", req.Header.Get("X-Group-Id"))
				}
				return miniMaxQuotaTestReply(req)
			})
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				refreshes++
				return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
			})}
			oldGeneration := cnQuotaGeneration(a)
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.True(t, result.Persisted)
			require.Equal(t, 1, refreshes)
			var freshPaths []string
			for _, path := range paths {
				if strings.HasPrefix(path, "Bearer new ") {
					freshPaths = append(freshPaths, strings.TrimPrefix(path, "Bearer new "))
				}
			}
			require.Equal(t, []string{"/v1/api/user/info", "/matrix/api/v1/user/get_user_extra_info", "/matrix/api/v1/commerce/get_membership_info", miniMaxOAuthQuotaPath}, freshPaths)
			require.NotEqual(t, oldGeneration, repo.updates["minimax_quota_generation"])
			require.Equal(t, cnQuotaGeneration(repo.account), repo.updates["minimax_quota_generation"])
		})
	}
}

func TestMiniMaxOAuthQuotaUnknownOrFailedNeverPenalizesAccount(t *testing.T) {
	for _, scenario := range []string{"missing-personal", "missing-group", "no-plan", "invalid-json", "business-error", "forbidden", "redirect", "transport", "nil-response", "bad-quota", "unrecognized-quota", "allowlist", "proxy"} {
		t.Run(scenario, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformMiniMax)
			a.Schedulable = true
			a.Extra = map[string]any{"minimax_5h_used_percent": 10.0, "minimax_5h_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
			bindCNQuotaTestSnapshot(a)
			calls := 0
			svc, repo, _ := miniMaxQuotaTestService(a, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Path == "/v1/api/user/info" {
					switch scenario {
					case "invalid-json":
						return cnRecoveryResponse(200, `{`), nil
					case "business-error":
						return cnRecoveryResponse(200, `{"statusInfo":{"code":99}}`), nil
					case "forbidden":
						return cnRecoveryResponse(403, `{}`), nil
					case "redirect":
						return cnRecoveryResponse(302, `{}`), nil
					case "transport":
						return nil, errors.New("offline")
					case "nil-response":
						return nil, nil
					}
				}
				if req.URL.Path == "/matrix/api/v1/user/get_user_extra_info" {
					switch scenario {
					case "missing-personal":
						return cnRecoveryResponse(200, `{"workspaces":[{"workspace_type":1,"workspace_id":1,"op_group_id":"team"}]}`), nil
					case "missing-group":
						return cnRecoveryResponse(200, `{"workspaces":[{"workspace_type":0,"workspace_id":1}]}`), nil
					case "no-plan":
						return cnRecoveryResponse(200, `{"workspaces":[{"workspace_type":0,"workspace_id":1,"op_group_id":"personal","has_token_plan":false}]}`), nil
					}
				}
				if scenario == "no-plan" && req.URL.Path == "/matrix/api/v1/commerce/get_membership_info" {
					return cnRecoveryResponse(200, `{"has_token_plan":false}`), nil
				}
				if req.URL.Path == miniMaxOAuthQuotaPath {
					switch scenario {
					case "bad-quota":
						return cnRecoveryResponse(200, `{"base_resp":{"status_code":2056},"model_remains":[{"current_interval_remaining_percent":0}]}`), nil
					case "unrecognized-quota":
						return cnRecoveryResponse(200, `{"model_remains":[{"current_interval_remaining_percent":"NaN"}]}`), nil
					}
				}
				return miniMaxQuotaTestReply(req)
			})
			if scenario == "allowlist" {
				svc.cfg = cnProbeAllowlistConfig("www.minimaxi.com")
			}
			if scenario == "proxy" {
				id := int64(99)
				a.ProxyID = &id
			}
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.True(t, err != nil || result != nil && !result.Success)
			require.Nil(t, repo.updates)
			require.Equal(t, StatusActive, a.Status)
			require.True(t, a.Schedulable)
			require.Nil(t, a.TempUnschedulableUntil)
			require.Equal(t, 10.0, a.Extra["minimax_5h_used_percent"])
			if scenario == "allowlist" || scenario == "proxy" {
				require.Zero(t, calls)
			}
		})
	}
}

func TestMiniMaxOAuthQuota401RecoveryBounded(t *testing.T) {
	a := cnRecoveryAccount(PlatformMiniMax)
	calls, refreshes := 0, 0
	svc, repo, oauth := miniMaxQuotaTestService(a, func(*http.Request) (*http.Response, error) { calls++; return cnRecoveryResponse(401, `{}`), nil })
	oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		refreshes++
		return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
	})}
	_, err := svc.QueryUsageForAccount(context.Background(), a)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, 1, refreshes)
	require.Nil(t, repo.updates)
	require.Equal(t, StatusActive, repo.account.Status)
}

func TestMiniMaxOAuthQuotaLegacyUnlimitedAndInvalidWindows(t *testing.T) {
	require.Equal(t, []CNQuotaTier{{Window: "5h", UsedPercent: 75}, {Window: "weekly", UsedPercent: 90}}, parseMiniMaxOAuthUsageTiers([]byte(`{"model_remains":[{"current_interval_usage_count":25,"current_interval_total_count":100,"current_weekly_usage_count":10,"current_weekly_total_count":100}]}`)))
	tiers := parseMiniMaxOAuthUsageTiers([]byte(`{"model_remains":[{"current_interval_status":3,"current_interval_remaining_percent":0,"current_weekly_remaining_percent":0}]}`))
	require.Equal(t, []CNQuotaTier{{Window: "5h", Unlimited: true}, {Window: "weekly", UsedPercent: 100}}, tiers)
	for _, body := range []string{`{}`, `{"model_remains":[]}`, `{"model_remains":[{"current_interval_usage_count":0,"current_interval_total_count":0}]}`, `{"model_remains":[{"current_interval_remaining_percent":-1}]}`, `{"model_remains":[{"current_interval_status":"3","current_interval_remaining_percent":0}]}`, `{"model_remains":[{"current_interval_remaining_percent":"NaN"}]}`} {
		require.Empty(t, parseMiniMaxOAuthUsageTiers([]byte(body)), body)
	}
	a := cnRecoveryAccount(PlatformMiniMax)
	updates := cnQuotaExtraUpdates(PlatformMiniMax, tiers, time.Now())
	cnQuotaBoundExtraUpdates(a, updates)
	require.Equal(t, true, updates["minimax_5h_unlimited"])
	require.Nil(t, updates["minimax_5h_used_percent"])
	require.Nil(t, updates["minimax_5h_reset_at"])
	require.Nil(t, updates["minimax_weekly_unlimited"])
	finite := cnQuotaExtraUpdates(PlatformMiniMax, []CNQuotaTier{{Window: "5h", UsedPercent: 25}}, time.Now())
	cnQuotaBoundExtraUpdates(a, finite)
	require.Nil(t, finite["minimax_5h_unlimited"], "a finite observation must clear an earlier unlimited marker")
}

func TestMiniMaxOAuthQuotaExhaustionEligibility(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name      string
		mutate    func(*Account)
		exhausted bool
	}{
		{"exhausted", func(*Account) {}, true},
		{"below", func(a *Account) { a.Extra["minimax_5h_used_percent"] = 99.9 }, false},
		{"reset", func(a *Account) { a.Extra["minimax_5h_reset_at"] = now.Add(-time.Second).Format(time.RFC3339) }, false},
		{"reset-boundary", func(a *Account) { a.Extra["minimax_5h_reset_at"] = now.Format(time.RFC3339Nano) }, false},
		{"unknown-reset", func(a *Account) { delete(a.Extra, "minimax_5h_reset_at") }, false},
		{"old-grant", func(a *Account) { a.Credentials["access_token"] = "replacement" }, false},
		{"unbound", func(a *Account) { delete(a.Extra, "minimax_quota_generation") }, false},
		{"unlimited", func(a *Account) { a.Extra["minimax_5h_unlimited"] = true }, false},
		{"weekly", func(a *Account) {
			a.Extra["minimax_5h_used_percent"] = 10.0
			a.Extra["minimax_weekly_used_percent"] = 100.0
			a.Extra["minimax_weekly_reset_at"] = now.Add(24 * time.Hour).Format(time.RFC3339)
		}, true},
		{"apikey", func(a *Account) { a.Type = AccountTypeAPIKey; bindCNQuotaTestSnapshot(a) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformMiniMax)
			a.Schedulable = true
			a.Extra = map[string]any{"minimax_5h_used_percent": 100.0, "minimax_5h_reset_at": now.Add(time.Hour).UTC().Format(time.RFC3339)}
			bindCNQuotaTestSnapshot(a)
			tc.mutate(a)
			require.Equal(t, tc.exhausted, a.miniMaxOAuthPlanExhausted(now))
			require.Equal(t, !tc.exhausted, a.IsSchedulable())
			require.Equal(t, StatusActive, a.Status)
			require.True(t, a.Schedulable)
			require.Nil(t, a.TempUnschedulableUntil)
		})
	}
}

func TestMiniMaxUnlimitedQuotaMonitorPropagation(t *testing.T) {
	a := cnRecoveryAccount(PlatformMiniMax)
	svc, _, _ := miniMaxQuotaTestService(a, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == miniMaxOAuthQuotaPath {
			return cnRecoveryResponse(200, `{"model_remains":[{"current_interval_status":3,"current_weekly_remaining_percent":90}]}`), nil
		}
		return miniMaxQuotaTestReply(req)
	})
	fetcher := &ChannelMonitorQuotaFetcher{cnQuota: svc}
	snapshot := fetcher.fetchCNQuota(context.Background(), a, time.Now())
	require.True(t, snapshot.Success)
	require.Equal(t, []domain.MonitorQuotaTier{{Window: "5h", Unlimited: true}, {Window: "weekly", UsedPercent: 10}}, snapshot.Tiers)
	require.Empty(t, quotaDegradedHint(&domain.MonitorQuotaSnapshot{Tiers: []domain.MonitorQuotaTier{{Window: "5h", Unlimited: true, UsedPercent: 100}}}))
}

func TestMiniMaxOAuthQuotaLateResponseCannotOverwriteReplacementGrant(t *testing.T) {
	for _, change := range []string{"grant", "proxy", "disable"} {
		t.Run(change, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformMiniMax)
			repo := &cnQuotaGenerationRepo{accounts: map[int64]*Account{a.ID: a}}
			upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == miniMaxOAuthQuotaPath {
					replacement, err := snapshotCNQuotaAccount(a)
					require.NoError(t, err)
					replacement.Extra = map[string]any{"minimax_5h_used_percent": 10.0}
					switch change {
					case "grant":
						replacement.Credentials["access_token"] = "new-login"
					case "proxy":
						id := int64(9)
						replacement.ProxyID = &id
					case "disable":
						replacement.Status = "disabled"
					}
					repo.put(replacement)
					return cnRecoveryResponse(200, `{"model_remains":[{"current_interval_remaining_percent":0,"end_time":1793516400000}]}`), nil
				}
				return miniMaxQuotaTestReply(req)
			}}
			svc := NewCNProviderQuotaService(repo, nil, upstream, cnProbeAllowlistConfig("agent.minimaxi.com", "www.minimaxi.com"))
			svc.cnOAuthService = NewCNOAuthService(nil, repo, nil)
			result, err := svc.QueryUsageForAccount(context.Background(), a)
			require.NoError(t, err)
			require.False(t, result.Success, "a stale quota observation cannot certify the replacement owner")
			require.False(t, result.Persisted)
			require.Zero(t, repo.writes)
			current, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			require.Equal(t, 10.0, current.Extra["minimax_5h_used_percent"])
			require.Nil(t, current.TempUnschedulableUntil)
		})
	}
}

func TestMiniMaxOAuthExhaustionSwitchesStickyOwnerAndRetainsReplacement(t *testing.T) {
	// Business contract: healthy A remains sticky; once A's bound plan is spent,
	// choose B, then keep B even after A's quota recovers. No credential penalty.
	a, b := cnRecoveryAccount(PlatformMiniMax), cnRecoveryAccount(PlatformMiniMax)
	a.ID, b.ID = 71, 72
	a.Schedulable, b.Schedulable = true, true
	a.Priority, b.Priority = 0, 1
	a.Concurrency, b.Concurrency = 1, 1
	repo := schedulerTestOpenAIAccountRepo{accounts: []Account{*a, *b}}
	cache := &schedulerTestGatewayCache{}
	svc := &OpenAIGatewayService{accountRepo: repo, cache: cache, cfg: &config.Config{RunMode: config.RunModeSimple}}
	selectOwner := func(want int64) {
		t.Helper()
		selected, err := svc.selectAccountForModelWithExclusions(context.Background(), nil, PlatformMiniMax, "minimax-sticky", "", nil, false, 0, "", false)
		require.NoError(t, err)
		require.Equal(t, want, selected.ID)
	}
	selectOwner(71)
	selectOwner(71)
	repo.accounts[0].Extra = map[string]any{"minimax_5h_used_percent": 100.0, "minimax_5h_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	bindCNQuotaTestSnapshot(&repo.accounts[0])
	selectOwner(72)
	repo.accounts[0].Extra["minimax_5h_used_percent"] = 10.0
	selectOwner(72)
	require.Equal(t, StatusActive, repo.accounts[0].Status)
	require.True(t, repo.accounts[0].Schedulable)
	require.Nil(t, repo.accounts[0].TempUnschedulableUntil)
}

func TestMiniMaxReactivePlanAndCreditsErrorsNeverInvalidateCredentials(t *testing.T) {
	// Official MiniMax Code llm-error-classifier.ts UPSTREAM_STATUS_CODE_MAP.
	for _, code := range []int{2056, 2067, 1400010161} {
		for _, status := range []int{403, 429, 500} {
			t.Run(fmt.Sprintf("%d/%d", code, status), func(t *testing.T) {
				a := cnRecoveryAccount(PlatformMiniMax)
				repo := &rateLimitAccountRepoStub{}
				svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				handled := svc.HandleUpstreamError(context.Background(), a, status, http.Header{}, []byte(fmt.Sprintf(`{"base_resp":{"status_code":%d,"status_msg":"fixture"}}`, code)))
				require.True(t, handled)
				require.Zero(t, repo.setErrorCalls)
				require.Equal(t, 1, repo.tempCalls)
				if code == 1400010161 {
					require.Contains(t, repo.lastTempReason, cnBalanceLowReasonPrefix)
				} else {
					require.Contains(t, repo.lastTempReason, cnQuotaExhaustedReasonPrefix)
				}
			})
		}
	}
	// Account scope and structured type are mandatory. A request's echoed model
	// or message digits must not create an account-level quota penalty.
	for _, body := range []string{`{"message":"model 2056"}`, `{"status_code":"2056"}`, `{"status_code":2056.5}`, `{"status_code":9999}`} {
		repo := &rateLimitAccountRepoStub{}
		svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		require.False(t, svc.handleMiniMaxUsageError(context.Background(), cnRecoveryAccount(PlatformMiniMax), 500, []byte(body), ""))
		require.Zero(t, repo.tempCalls)
	}
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	require.False(t, svc.handleMiniMaxUsageError(context.Background(), cnRecoveryAccount(PlatformKimi), 500, []byte(`{"status_code":2056}`), ""))
	require.False(t, svc.handleMiniMaxUsageError(context.Background(), cnRecoveryAccount(PlatformMiniMax), 401, []byte(`{"status_code":2056}`), ""))
}

func TestMiniMaxUsageCooldownCannotPoisonAnotherGrant(t *testing.T) {
	// A late quota/Credits error must never pause or evict a replacement login.
	// A native refresh uses the final transmitted grant, not the outer selection.
	for _, code := range []int{2056, 2067, 1400010161} {
		for _, scenario := range []string{"unchanged", "reauthorized", "proxy-changed", "disabled", "write-failure", "ambiguous", "unknown-bearer", "refreshed"} {
			t.Run(fmt.Sprintf("%d/%s", code, scenario), func(t *testing.T) {
				selected := cnRecoveryAccount(PlatformMiniMax)
				current := snapshotRequestCredentialAccount(selected)
				repo := &http401CredentialCooldownRepo{http401CredentialFenceRepo: http401CredentialFenceRepo{current: current}}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				sent := selected
				token := "old"
				switch scenario {
				case "reauthorized":
					current.Credentials["refresh_token"] = "replacement-login"
				case "proxy-changed":
					id := int64(99)
					current.ProxyID = &id
				case "disabled":
					current.Status = "disabled"
				case "write-failure":
					repo.writeErr = errors.New("unavailable")
				case "unknown-bearer":
					token = "unknown"
				case "refreshed":
					current.Credentials["access_token"] = "refreshed-token"
					current.Credentials["refresh_token"] = "rotated"
					sent = current
					token = "refreshed-token"
				}
				recordRequestCredentialOwner(ctx, sent, token)
				if scenario == "ambiguous" {
					other := snapshotRequestCredentialAccount(selected)
					other.Credentials["refresh_token"] = "different-grant"
					recordRequestCredentialOwner(ctx, other, token)
				}
				// Unknown bearer evidence is intentionally not recorded for a valid owner.
				if scenario == "unknown-bearer" {
					ctx = WithOutboundIdentityScope(context.Background(), nil)
				}
				ctx = withRequestCredentialOwner(ctx, selected, token)
				blocker := &runtimeBlockRecorder{}
				invalidator := &tokenCacheInvalidatorRecorder{}
				svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				svc.SetAccountRuntimeBlocker(blocker)
				svc.SetTokenCacheInvalidator(invalidator)
				require.True(t, svc.HandleUpstreamError(ctx, selected, 500, http.Header{}, []byte(fmt.Sprintf(`{"status_code":%d}`, code))))
				require.Zero(t, repo.unconditionalCooldowns)
				require.Zero(t, repo.unconditionalCalls)
				require.Zero(t, repo.casCalls, "quota cannot revoke credentials")
				require.Empty(t, invalidator.accounts, "quota cannot evict cached OAuth tokens")
				if scenario == "unchanged" || scenario == "refreshed" {
					require.NotNil(t, current.TempUnschedulableUntil)
					require.Equal(t, sent.Credentials, repo.attempted.Credentials)
					require.Len(t, blocker.accounts, 1)
				} else {
					require.Nil(t, current.TempUnschedulableUntil)
					require.Empty(t, blocker.accounts)
				}
			})
		}
	}
}
