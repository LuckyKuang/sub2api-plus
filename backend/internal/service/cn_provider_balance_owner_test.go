//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type cnBalanceOwnerUpstream struct {
	send func(*http.Request, string) (*http.Response, error)
}

func (u *cnBalanceOwnerUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.send(req, proxy)
}

func (u *cnBalanceOwnerUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func cnBalanceOwnerHTTP(t *testing.T, handler http.Handler) *cnBalanceOwnerUpstream {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	return &cnBalanceOwnerUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		forwarded := req.Clone(req.Context())
		endpoint := *req.URL
		endpoint.Scheme, endpoint.Host = target.Scheme, target.Host
		forwarded.URL = &endpoint
		return server.Client().Do(forwarded)
	}}
}

func cnBalanceOwnerAccount(platform string) *Account {
	proxyID := int64(83)
	return &Account{ID: 778, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "balance-owner-G1", "account_mode": AccountModePayG},
		Extra:       map[string]any{"keep": "G1"}, ProxyID: &proxyID,
		Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "proxy-G1.invalid", Port: 8080, Username: "G1", Password: "G1-password", Status: StatusActive}}
}

func writeCNBalanceOwnerResponse(w http.ResponseWriter, platform string, balance int) {
	w.Header().Set("Content-Type", "application/json")
	if platform == PlatformKimi {
		_, _ = fmt.Fprintf(w, `{"code":0,"status":true,"data":{"available_balance":%d}}`, balance)
	} else {
		_, _ = fmt.Fprintf(w, `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"%d"}]}`, balance)
	}
}

type cnBalanceOwnerQueryResult struct {
	result *CNProviderBalanceResult
	err    error
}

func (r *cnBalanceProbeRepo) replace(account *Account) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.account = snapshotRequestCredentialAccount(account)
}

func TestCNBalanceOwnerReplacementSeparatesFlightsAndRejectsLateState(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformDeepseek} {
		for _, change := range []string{"credentials", "proxy-endpoint", "proxy-credentials", "proxy-status"} {
			for _, oldBalance := range []int{0, 100} {
				t.Run(fmt.Sprintf("%s/%s/old=%d", platform, change, oldBalance), func(t *testing.T) {
					old := cnBalanceOwnerAccount(platform)
					if oldBalance > 0 {
						until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
						old.TempUnschedulableUntil, old.TempUnschedulableReason = &until, cnBalanceLowReason("old-observation")
					}
					repo := &cnBalanceProbeRepo{account: snapshotRequestCredentialAccount(old)}
					started, release := make(chan struct{}), make(chan struct{})
					var releaseOnce sync.Once
					defer releaseOnce.Do(func() { close(release) })
					upstream := cnBalanceOwnerHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						if req.Header.Get("X-Balance-Generation") != "G2" {
							close(started)
							select {
							case <-release:
							case <-req.Context().Done():
								return
							}
							writeCNBalanceOwnerResponse(w, platform, oldBalance)
							return
						}
						writeCNBalanceOwnerResponse(w, platform, 80)
					}))
					// A literal request label differentiates the two real HTTP
					// observations even when only the same proxy row changed.
					originalSend := upstream.send
					upstream.send = func(req *http.Request, proxy string) (*http.Response, error) {
						owner, ok := requestCredentialOwner(req.Context(), old)
						if !ok {
							return nil, errors.New("missing balance credential owner")
						}
						if owner.GetCredential("api_key") == "balance-owner-G2" || owner.Proxy.Host == "proxy-G2.invalid" || owner.Proxy.Password == "G2-password" || owner.Proxy.Status == StatusDisabled {
							req.Header.Set("X-Balance-Generation", "G2")
						}
						return originalSend(req, proxy)
					}
					svc := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{})
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					oldDone := make(chan cnBalanceOwnerQueryResult, 1)
					go func() {
						result, err := svc.QueryBalance(ctx, old.ID)
						oldDone <- cnBalanceOwnerQueryResult{result, err}
					}()
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					current := snapshotRequestCredentialAccount(old)
					switch change {
					case "credentials":
						current.Credentials["api_key"] = "balance-owner-G2"
					case "proxy-endpoint":
						current.Proxy.Host = "proxy-G2.invalid"
					case "proxy-credentials":
						current.Proxy.Password = "G2-password"
					case "proxy-status":
						current.Proxy.Status = StatusDisabled
					}
					later := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
					current.TempUnschedulableUntil, current.TempUnschedulableReason = &later, "G2-newer-cooldown"
					current.Extra = map[string]any{"keep": "G2", cnExtraKey(platform, cnBalanceExtraSuffixLow): true}
					repo.replace(current)
					newDone := make(chan cnBalanceOwnerQueryResult, 1)
					go func() {
						result, err := svc.QueryBalance(ctx, current.ID)
						newDone <- cnBalanceOwnerQueryResult{result, err}
					}()
					select {
					case got := <-newDone:
						require.NoError(t, got.err)
						require.True(t, got.result.Success)
						require.True(t, got.result.Persisted)
						require.Equal(t, float64(80), got.result.Balance, "G2 must complete its own probe while G1 is blocked")
					case <-ctx.Done():
						t.Fatal("G2 query joined G1's in-flight balance probe")
					}
					before, err := repo.GetByID(ctx, old.ID)
					require.NoError(t, err)
					releaseOnce.Do(func() { close(release) })
					var late cnBalanceOwnerQueryResult
					select {
					case late = <-oldDone:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					require.NoError(t, late.err)
					require.False(t, late.result.Success, "a stale owner cannot publish success for the replacement grant")
					require.False(t, late.result.Persisted, "G1's late response cannot update G2's extra")
					check := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}
					require.Equal(t, cnBalanceNoChange, check.applyBalanceResult(ctx, old, late.result, 5))
					after, err := repo.GetByID(ctx, old.ID)
					require.NoError(t, err)
					require.Equal(t, before, after, "late low balance cannot pause G2; late healthy balance cannot clear its newer cooldown")
					require.Len(t, repo.stateWrites, 1, "only G2's own snapshot may commit")
					require.Zero(t, repo.unconditionalStateCalls+repo.unconditionalExtraCalls)
				})
			}
		}
	}
}

func TestCNBalanceSameGrantCannotReplaceCooldownInstalledDuringProbe(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformDeepseek} {
		for _, scenario := range []string{"pause", "clear", "new-cooldown-before-pause", "new-deadline-before-clear", "new-reason-before-clear"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				owner := cnBalanceOwnerAccount(platform)
				balance := 0
				if strings.Contains(scenario, "clear") {
					balance = 100
					until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
					owner.TempUnschedulableUntil, owner.TempUnschedulableReason = &until, cnBalanceLowReason("observed-before-probe")
				}
				repo := &cnBalanceProbeRepo{account: snapshotRequestCredentialAccount(owner)}
				upstream := cnBalanceOwnerHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					current := snapshotRequestCredentialAccount(owner)
					switch scenario {
					case "new-cooldown-before-pause":
						until := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
						current.TempUnschedulableUntil, current.TempUnschedulableReason = &until, "other-subsystem"
					case "new-deadline-before-clear":
						until := current.TempUnschedulableUntil.Add(time.Hour)
						current.TempUnschedulableUntil = &until
					case "new-reason-before-clear":
						current.TempUnschedulableReason = "same-deadline-new-failure"
					}
					if strings.HasPrefix(scenario, "new-") {
						current.Extra[cnExtraKey(platform, cnBalanceExtraSuffixLow)] = true
					}
					repo.replace(current)
					writeCNBalanceOwnerResponse(w, platform, balance)
				}))
				svc := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{})
				result, err := svc.QueryBalance(t.Context(), owner.ID)
				require.NoError(t, err)
				require.True(t, result.Success)
				require.Equal(t, !strings.HasPrefix(scenario, "new-"), result.Persisted, "newer cooldown also fences balance_low metadata")
				before, err := repo.GetByID(t.Context(), owner.ID)
				require.NoError(t, err)
				check := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}
				outcome := check.applyBalanceResult(t.Context(), owner, result, 5)
				after, err := repo.GetByID(t.Context(), owner.ID)
				require.NoError(t, err)
				switch scenario {
				case "pause":
					require.Equal(t, cnBalancePaused, outcome)
					require.NotNil(t, after.TempUnschedulableUntil)
					require.Contains(t, after.TempUnschedulableReason, cnBalanceLowReasonPrefix)
				case "clear":
					require.Equal(t, cnBalanceCleared, outcome)
					require.Nil(t, after.TempUnschedulableUntil)
					require.Empty(t, after.TempUnschedulableReason)
				default:
					require.Equal(t, cnBalanceNoChange, outcome)
					require.Equal(t, before, after, "same-grant probe must preserve newer cooldown state")
					require.Equal(t, true, after.Extra[cnExtraKey(platform, cnBalanceExtraSuffixLow)])
					require.Empty(t, repo.extraWrites, "the late probe cannot clear the newer reactive balance marker")
				}
				require.Zero(t, repo.unconditionalStateCalls+repo.unconditionalExtraCalls)
			})
		}
	}
}

func TestCNBalanceFreezesOwnerAndStopsPreparationFailures(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformDeepseek} {
		t.Run(platform+"/immutable-owner", func(t *testing.T) {
			selected := cnBalanceOwnerAccount(platform)
			until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			selected.TempUnschedulableUntil, selected.TempUnschedulableReason = &until, cnBalanceLowReason("observed")
			original := snapshotRequestCredentialAccount(selected)
			repo := &cnBalanceProbeRepo{account: snapshotRequestCredentialAccount(selected)}
			upstream := &cnBalanceOwnerUpstream{send: func(req *http.Request, proxy string) (*http.Response, error) {
				require.Equal(t, "Bearer balance-owner-G1", req.Header.Get("Authorization"))
				require.Equal(t, "http://G1:G1-password@proxy-G1.invalid:8080", proxy)
				selected.Credentials["api_key"], selected.Proxy.Host = "selected-G2", "selected-G2.invalid"
				selected.TempUnschedulableReason = "selected-new-cooldown"
				selected.Platform = PlatformCommandCode
				return (&cnBalanceResponseUpstream{statusCode: http.StatusOK, body: map[string]string{PlatformKimi: `{"code":0,"status":true,"data":{"available_balance":100}}`, PlatformDeepseek: `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"100"}]}`}[platform]}).Do(req, proxy, selected.ID, 1)
			}}
			result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalanceForAccount(t.Context(), selected)
			require.NoError(t, err)
			require.True(t, result.Persisted)
			require.Equal(t, original, result.owner)
			serialized, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), "balance-owner-G1", "internal owner evidence must never enter the admin JSON response")
			check := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}
			require.Equal(t, cnBalanceCleared, check.applyBalanceResult(t.Context(), selected, result, 5))
		})
		t.Run(platform+"/preparation-failure", func(t *testing.T) {
			account := cnBalanceOwnerAccount(platform)
			repo := &cnBalanceProbeRepo{account: account}
			upstream := &recordingHTTPUpstream{}
			failure := errors.New("preparation-dependency-unavailable")
			ctx := WithOutboundPreparationError(t.Context(), failure)
			_, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(ctx, account.ID)
			require.ErrorIs(t, err, failure)
			require.Zero(t, upstream.calls)
			require.Empty(t, repo.stateWrites)
		})
	}
}

func TestCNBalanceRequiresHydratedEvidenceAndConditionalStorage(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformDeepseek} {
		for _, evidence := range []string{"missing-proxy", "wrong-proxy", "slim-owner", "missing-storage"} {
			t.Run(platform+"/"+evidence, func(t *testing.T) {
				owner := cnBalanceOwnerAccount(platform)
				switch evidence {
				case "missing-proxy":
					owner.Proxy = nil
				case "wrong-proxy":
					owner.Proxy.ID++
				case "slim-owner":
					owner.CredentialGeneration = "scheduler-digest"
				case "missing-storage":
					owner.Proxy, owner.ProxyID = nil, nil
				}
				if evidence != "missing-storage" {
					repo := &cnBalanceProbeRepo{account: owner}
					upstream := &recordingHTTPUpstream{}
					_, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(t.Context(), owner.ID)
					require.Error(t, err)
					require.Zero(t, upstream.calls)
					require.Empty(t, repo.stateWrites)
					return
				}
				repo := &fakeCNProbeAccountRepo{account: owner}
				upstream := &cnBalanceResponseUpstream{statusCode: http.StatusOK, body: map[string]string{PlatformKimi: `{"code":0,"status":true,"data":{"available_balance":0}}`, PlatformDeepseek: `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`}[platform]}
				result, err := NewCNProviderBalanceService(repo, nil, upstream, &config.Config{}).QueryBalance(t.Context(), owner.ID)
				require.NoError(t, err)
				require.False(t, result.Success, "missing conditional storage cannot certify a durable wallet observation")
				require.False(t, result.Persisted, "a missing CAS port cannot fall back to UpdateExtra")
				check := &CNProviderBalanceCheckService{accountRepo: repo, cfg: &config.Config{}}
				require.Equal(t, cnBalanceNoChange, check.applyBalanceResult(t.Context(), owner, result, 5), "a missing CAS port cannot fall back to row-ID scheduling writes")
			})
		}
	}
}
