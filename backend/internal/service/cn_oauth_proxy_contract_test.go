//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

type cnLoginRouteRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
	calls int
}

func (r *cnLoginRouteRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.proxy, r.err
}

func cnLoginDeviceReply() *http.Response {
	return nativeControlReply(`{"device_code":"private-device","user_code":"public-code","verification_uri_complete":"https://auth.kimi.com/verify?user_code=public-code","expires_in":300,"interval":2}`)
}

func TestCNOAuthSelectedProxyRejectsUnavailableOrInvalidRouteBeforeHTTP(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		for _, failure := range []string{"missing-repository", "missing-row", "lookup-error", "invalid-id", "different-id", "missing-host", "invalid-port", "invalid-protocol"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				id := int64(71)
				repo := &cnLoginRouteRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080}}
				switch failure {
				case "missing-row":
					repo.proxy = nil
				case "lookup-error":
					repo.err = errors.New("private-database-detail")
				case "invalid-id":
					id = 0
				case "different-id":
					repo.proxy.ID = 72
				case "missing-host":
					repo.proxy.Host = ""
				case "invalid-port":
					repo.proxy.Port = 0
				case "invalid-protocol":
					repo.proxy.Protocol = "file"
				}
				var selected ProxyRepository = repo
				if failure == "missing-repository" {
					selected = nil
				}
				svc := NewCNOAuthService(selected, nil, nil)
				calls := 0
				svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
					calls++
					return nil, errors.New("unexpected issuer HTTP")
				})}
				var err error
				require.NotPanics(t, func() { _, err = svc.Start(context.Background(), 7, platform, "cn", &id, 0) })
				require.Error(t, err, "a saved proxy binding must not authorize another route")
				require.NotContains(t, err.Error(), "private-database-detail")
				require.Zero(t, calls, "route validation must finish before provider IO")
			})
		}
	}
}

func TestCNOAuthReloginKeepsHydratedProxySnapshot(t *testing.T) {
	for _, hydrated := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-route", true: "frozen-route"}[hydrated], func(t *testing.T) {
			account := nativeControlAccount(PlatformKimi)
			id := int64(71)
			account.ProxyID = &id
			if hydrated {
				account.Proxy = &Proxy{ID: id, Protocol: "http", Host: "original.example", Port: 8080}
			}
			repo := &cnLoginRouteRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "replacement.example", Port: 8888}}
			svc := NewCNOAuthService(repo, &refreshAPIAccountRepo{account: account}, nil)
			calls := 0
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { calls++; return cnLoginDeviceReply(), nil })}
			view, err := svc.Start(context.Background(), 7, PlatformKimi, "cn", nil, account.ID)
			require.Zero(t, repo.calls, "an in-flight credential owner uses its hydrated route")
			if !hydrated {
				require.Error(t, err)
				require.Zero(t, calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.NoError(t, svc.store.Update(context.Background(), view.SessionID, func(_ context.Context, session *cnoauth.Session) error {
				require.Equal(t, "http://original.example:8080", session.ProxyURL)
				require.Equal(t, id, *session.ProxyID)
				return nil
			}))
		})
	}
}

func TestCNOAuthUnboundLoginPreservesDirectOperation(t *testing.T) {
	svc := NewCNOAuthService(nil, nil, nil)
	calls := 0
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { calls++; return cnLoginDeviceReply(), nil })}
	view, err := svc.Start(context.Background(), 7, PlatformKimi, "cn", nil, 0)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.NoError(t, svc.store.Update(context.Background(), view.SessionID, func(_ context.Context, session *cnoauth.Session) error {
		require.Nil(t, session.ProxyID)
		require.Empty(t, session.ProxyURL)
		return nil
	}))
}

func TestCNOAuthPreviewUsesFrozenLoginRoute(t *testing.T) {
	for _, route := range []string{"http://owner:private-proxy-password@original.example:8080", "", "file://original.example:8080"} {
		t.Run(route, func(t *testing.T) {
			id := int64(71)
			repo := &cnLoginRouteRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "replacement.example", Port: 8888}}
			svc := NewCNOAuthService(repo, nil, nil)
			upstream := &httpUpstreamRecorder{resp: nativeControlReply(`{"data":[{"id":"step-3.7-flash","model_type":"大语言模型"}]}`)}
			svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
			sessionID := cnOAuthReady(t, svc, PlatformStepFun, 0)
			require.NoError(t, svc.store.Update(context.Background(), sessionID, func(_ context.Context, session *cnoauth.Session) error {
				session.ProxyID = &id
				session.ProxyURL = route
				return nil
			}))
			models, err := svc.PreviewModels(context.Background(), 7, PlatformStepFun, sessionID)
			require.Zero(t, repo.calls, "preview shares the login's captured route")
			if route == "" || route == "file://original.example:8080" {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"step-3.7-flash"}, models)
			require.Equal(t, route, upstream.lastProxyURL)
		})
	}
}

func TestCNOAuthRefreshBoundRouteMustValidateBeforeIssuerHTTP(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformStepFun} {
		for _, failure := range []string{"missing-row", "different-id", "invalid-protocol", "invalid-port"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				a := nativeControlAccount(platform)
				id := int64(71)
				a.ProxyID = &id
				a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080}
				switch failure {
				case "missing-row":
					a.Proxy = nil
				case "different-id":
					a.Proxy.ID++
				case "invalid-protocol":
					a.Proxy.Protocol = "file"
				case "invalid-port":
					a.Proxy.Port = 0
				}
				if platform == PlatformStepFun {
					a.Credentials["step_account_refresh_token"] = "private-refresh"
					a.Credentials["step_account_access_token"] = "private-access"
					a.Credentials["step_account_device_id"] = "device"
					a.Credentials["step_account_app_id"] = "app"
				}
				calls := 0
				refresher := &CNTokenRefresher{platform: platform, client: &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected issuer HTTP") })}}
				_, err := refresher.Refresh(context.Background(), a)
				require.Error(t, err)
				require.Zero(t, calls, "an unavailable saved route cannot degrade to host networking")
			})
		}
	}
}

func TestCNOAuthLateRefreshAfterProxyEditCannotPersistRotation(t *testing.T) {
	for _, changeID := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-row-url-change", true: "same-url-other-row"}[changeID], func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			id := int64(71)
			a.ProxyID = &id
			a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080}
			a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: a}
			svc := NewCNOAuthService(nil, repo, nil)
			svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				if changeID {
					replacement := int64(72)
					a.ProxyID = &replacement
					copy := *a.Proxy
					copy.ID = replacement
					a.Proxy = &copy
				} else {
					a.Proxy.Host = "replacement.example"
				}
				return nativeControlReply(`{"access_token":"late-old-access","refresh_token":"late-old-refresh","expires_in":3600}`), nil
			})}
			fresh, err := svc.RefreshAccount(withOAuthRefreshRequestPath(context.Background()), a, true)
			require.Error(t, err, "proxy edits change the owner of an in-flight issuer operation")
			require.Nil(t, fresh)
			require.Zero(t, repo.updateCredentialsCalls)
			require.NotEqual(t, "late-old-access", repo.account.GetCredential("access_token"))
		})
	}
}

func TestCNOAuthLateRefreshCannotChangeCapturedProxyExpiry(t *testing.T) {
	a := nativeControlAccount(PlatformKimi)
	id := int64(71)
	expires := time.Now().Add(time.Hour)
	a.ProxyID = &id
	a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "owner.example", Port: 8080, Status: StatusActive, ExpiresAt: &expires}
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	repo := &refreshAPIAccountRepo{account: a}
	svc := NewCNOAuthService(nil, repo, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		// Editing the pointee must not also change the issuer attempt's frozen
		// owner; the saved proxy row belongs to a later administrative edit.
		*a.Proxy.ExpiresAt = expires.Add(time.Hour)
		return nativeControlReply(`{"access_token":"late-old-access","refresh_token":"late-old-refresh","expires_in":3600}`), nil
	})}
	fresh, err := svc.RefreshAccount(withOAuthRefreshRequestPath(context.Background()), a, true)
	require.Error(t, err)
	require.Nil(t, fresh)
	require.Zero(t, repo.updateCredentialsCalls)
	require.Equal(t, "request-owner", repo.account.GetCredential("access_token"))
}

func TestCNOAuthReloginLatePollAfterProxyEditCannotReplaceCredentials(t *testing.T) {
	for _, changeID := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-row-route-edit", true: "same-url-new-binding"}[changeID], func(t *testing.T) {
			a := nativeControlAccount(PlatformKimi)
			id := int64(71)
			a.ProxyID = &id
			a.Proxy = &Proxy{ID: id, Protocol: "http", Host: "original.example", Port: 8080, Status: StatusActive}
			originalAccess, originalRefresh := a.GetCredential("access_token"), a.GetCredential("refresh_token")
			repo := &refreshAPIAccountRepo{account: a}
			proxyRepo := &cnLoginRouteRepo{proxy: a.Proxy}
			admin := &cnOAuthAdminStub{}
			svc := NewCNOAuthService(proxyRepo, repo, admin)
			calls := 0
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return cnLoginDeviceReply(), nil
				}
				// The administrative route edit commits while the old-route poll is
				// in flight; its late token cannot replace the current account owner.
				if changeID {
					next := int64(72)
					a.ProxyID = &next
					copy := *a.Proxy
					copy.ID = next
					a.Proxy = &copy
				} else {
					a.Proxy.Host = "replacement.example"
				}
				return nativeControlReply(`{"access_token":"late-old-login","refresh_token":"late-old-refresh","expires_in":3600}`), nil
			})}
			view, err := svc.Start(context.Background(), 7, PlatformKimi, "cn", nil, a.ID)
			require.NoError(t, err)
			require.NoError(t, svc.store.Update(context.Background(), view.SessionID, func(_ context.Context, session *cnoauth.Session) error {
				session.NextPoll = time.Now().Add(-time.Second)
				return nil
			}))
			_, pollErr := svc.Advance(context.Background(), 7, PlatformKimi, view.SessionID, "", false)
			if pollErr == nil {
				_, err = svc.Complete(context.Background(), 7, PlatformKimi, view.SessionID, CNOAuthCompleteInput{})
			} else {
				err = pollErr
			}
			require.Error(t, err, "a completed proxy edit fences a late login poll before account mutation")
			require.Zero(t, admin.writes)
			require.Zero(t, repo.updateCredentialsCalls)
			require.Equal(t, originalAccess, a.GetCredential("access_token"))
			require.Equal(t, originalRefresh, a.GetCredential("refresh_token"))
		})
	}
}

func TestCNOAuthNativeControlBoundRouteCannotRereadOrDialDirect(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformStepFun, PlatformDeepseek} {
		for _, failure := range []string{"missing-route", "different-row"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				a := nativeControlAccount(platform)
				id := int64(71)
				a.ProxyID = &id
				if failure == "different-row" {
					a.Proxy = &Proxy{ID: 72, Protocol: "http", Host: "wrong-owner.example", Port: 8080}
				}
				repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: a}}
				proxyRepo := &cnLoginRouteRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "replacement.example", Port: 8888}}
				sends := 0
				upstream := nativeControlOwnerUpstream{respond: func(*http.Request) (*http.Response, error) {
					sends++
					switch platform {
					case PlatformKimi:
						return nativeControlReply(`{"usages":{"limit_5h":{"used_ratio":0.25}}}`), nil
					case PlatformStepFun:
						return nativeControlReply(stepCodingUsageFixture), nil
					default:
						return nativeControlReply(`{"code":0,"data":{"biz_code":0,"biz_data":{"normal_wallets":[{"currency":"CNY","balance":"3"}],"bonus_wallets":[]}}}`), nil
					}
				}}
				if platform == PlatformDeepseek {
					result, err := NewCNProviderBalanceService(repo, proxyRepo, upstream, &config.Config{}).QueryBalance(context.Background(), a.ID)
					if err == nil {
						require.False(t, result.Success)
						require.False(t, result.Persisted)
					}
				} else {
					result, err := NewCNProviderQuotaService(repo, proxyRepo, upstream, &config.Config{}).QueryUsage(context.Background(), a.ID)
					if err == nil {
						require.False(t, result.Success)
						require.False(t, result.Persisted)
					}
				}
				require.Zero(t, sends, "a missing or foreign captured proxy must stop before provider IO")
				require.Zero(t, proxyRepo.calls, "mutable row rereads cannot repair an incomplete credential-owner snapshot")
				require.Nil(t, repo.updates)
			})
		}
	}
}

func TestCNOAuthTerminalSessionDiscardsOldLoginOwner(t *testing.T) {
	for _, terminal := range []string{"cancel", "access_denied", "expired_token", "invalid_grant", "completed"} {
		t.Run(terminal, func(t *testing.T) {
			account := nativeControlAccount(PlatformKimi)
			account.Status = StatusActive
			repo := &refreshAPIAccountRepo{account: account}
			svc := NewCNOAuthService(nil, repo, &cnOAuthAdminStub{})
			owner := cnOAuthTestLoginOwner(t, account)
			session := &cnoauth.Session{OwnerID: 7, AccountID: account.ID, LoginOwner: owner, Identity: builtInOutboundIdentity(PlatformKimi),
				Flow: &cnoauth.Flow{Platform: PlatformKimi, Region: "cn", DeviceCode: "private-device", ExpiresAt: time.Now().Add(time.Minute)}}
			if terminal == "completed" {
				session.Grant = &cnoauth.Grant{AccessToken: "new-login-access", RefreshToken: "new-login-refresh", ExpiresAt: time.Now().Add(time.Hour)}
			}
			id, err := svc.store.Create(context.Background(), session)
			require.NoError(t, err)
			if terminal == "completed" {
				view, err := svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
				require.NoError(t, err)
				require.Equal(t, "completed", view.Status)
				require.Equal(t, "new-login-access", repo.account.GetCredential("access_token"))
			} else {
				encoded, err := json.Marshal(map[string]string{"error": terminal})
				require.NoError(t, err)
				svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { return nativeControlReply(string(encoded)), nil })}
				view, err := svc.Advance(context.Background(), 7, PlatformKimi, id, "", terminal == "cancel")
				require.NoError(t, err)
				require.Equal(t, "cancelled", view.Status)
				require.Zero(t, repo.updateCredentialsCalls)
			}
			require.NoError(t, svc.store.Update(context.Background(), id, func(_ context.Context, saved *cnoauth.Session) error {
				require.Empty(t, saved.LoginOwner, "a terminal receipt cannot retain the replaced or abandoned login's credentials")
				return nil
			}))
		})
	}
}
