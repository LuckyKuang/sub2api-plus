//go:build unit || !integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type offPeakSegmentAdminAccountStub struct {
	service.AdminService
	account *service.Account
	err     error
	reads   int
}

func (s *offPeakSegmentAdminAccountStub) GetAccount(context.Context, int64) (*service.Account, error) {
	s.reads++
	return s.account, s.err
}

type offPeakSegmentHandlerManagerStub struct {
	current          *service.ZhipuOffPeakSegment
	err              error
	reads            int
	terminated       [][2]string
	retired          map[[2]string]bool
	requestAccountID int64
}

func (m *offPeakSegmentHandlerManagerStub) CurrentSegment(_ context.Context, account *service.Account) (*service.ZhipuOffPeakSegment, error) {
	m.reads++
	m.requestAccountID = account.ID
	return m.current, m.err
}

func (m *offPeakSegmentHandlerManagerStub) TerminateSegment(_ context.Context, account *service.Account, record, generation string) error {
	m.requestAccountID = account.ID
	m.terminated = append(m.terminated, [2]string{record, generation})
	if m.err != nil {
		return m.err
	}
	pair := [2]string{record, generation}
	if m.retired[pair] {
		return nil
	}
	if m.current == nil || m.current.RecordID != record || m.current.Generation != generation {
		return infraerrors.New(http.StatusConflict, "ZHIPU_OFFPEAK_SEGMENT_CONFLICT", "off-peak segment is no longer current")
	}
	if m.retired == nil {
		m.retired = map[[2]string]bool{}
	}
	m.retired[pair] = true
	m.current = nil
	return nil
}

func offPeakSegmentHandlerAccount() *service.Account {
	return &service.Account{ID: 501, Platform: service.PlatformZhipu, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"plan_kind": service.ZhipuPlanOffPeak, "zcode_jwt_token": "secret-zcode-jwt", "oauth_access_token": "secret-business-login"}}
}

func offPeakSegmentHandlerRouter(accounts *offPeakSegmentAdminAccountStub, manager ZhipuOffPeakSegmentManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewZhipuOAuthHandler(nil, accounts)
	h.SetOffPeakTicketManager(manager)
	router := gin.New()
	router.GET("/accounts/:id/off-peak/segment", h.GetOffPeakSegment)
	router.POST("/accounts/:id/off-peak/segment/terminate", h.TerminateOffPeakSegment)
	return router
}

func TestZhipuOffPeakSegmentHandlerOpaqueReadAndExactTermination(t *testing.T) {
	accounts := &offPeakSegmentAdminAccountStub{account: offPeakSegmentHandlerAccount()}
	manager := &offPeakSegmentHandlerManagerStub{current: &service.ZhipuOffPeakSegment{RecordID: "opaque-record-a", Generation: "opaque-generation-a", State: "active"}}
	router := offPeakSegmentHandlerRouter(accounts, manager)
	read := zhipuRequest(t, router, http.MethodGet, "/accounts/501/off-peak/segment", "")
	require.Equal(t, http.StatusOK, read.Code)
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &body))
	require.Equal(t, map[string]any{"record_id": "opaque-record-a", "generation": "opaque-generation-a", "state": "active"}, body.Data, "only gateway-owned handles and state are public")
	require.NotContains(t, read.Body.String(), "secret-")
	require.Equal(t, int64(501), manager.requestAccountID)
	terminated := zhipuRequest(t, router, http.MethodPost, "/accounts/501/off-peak/segment/terminate", `{"record_id":"opaque-record-a","generation":"opaque-generation-a"}`)
	require.Equal(t, http.StatusOK, terminated.Code)
	require.True(t, gjson.Get(terminated.Body.String(), "data.accepted").Bool(), "acceptance refers to durable retirement intent")
	require.Equal(t, [][2]string{{"opaque-record-a", "opaque-generation-a"}}, manager.terminated)
	require.Equal(t, 1, manager.reads, "termination must never inspect and substitute the latest segment")
}

func TestZhipuOffPeakSegmentHandlerValidatesBeforeControlSideEffects(t *testing.T) {
	for _, profile := range []struct{ platform, accountType, plan string }{
		{service.PlatformZhipu, service.AccountTypeOAuth, service.ZhipuPlanStartPlan},
		{service.PlatformZhipu, service.AccountTypeOAuth, service.ZhipuPlanIndividualCodingPlan},
		{service.PlatformStepFun, service.AccountTypeOAuth, service.ZhipuPlanOffPeak},
		{service.PlatformZhipu, service.AccountTypeUpstream, service.ZhipuPlanOffPeak},
	} {
		t.Run(profile.platform+"/"+profile.accountType+"/"+profile.plan, func(t *testing.T) {
			account := offPeakSegmentHandlerAccount()
			account.Platform, account.Type, account.Credentials["plan_kind"] = profile.platform, profile.accountType, profile.plan
			accounts := &offPeakSegmentAdminAccountStub{account: account}
			manager := &offPeakSegmentHandlerManagerStub{}
			router := offPeakSegmentHandlerRouter(accounts, manager)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path := "/accounts/501/off-peak/segment"
				if method == http.MethodPost {
					path += "/terminate"
				}
				result := zhipuRequest(t, router, method, path, `{"record_id":"opaque-a","generation":"generation-a"}`)
				require.Equal(t, http.StatusBadRequest, result.Code)
				require.Equal(t, "ZHIPU_OFFPEAK_ACCOUNT_INVALID", gjson.Get(result.Body.String(), "reason").String())
			}
			require.Zero(t, manager.reads)
			require.Empty(t, manager.terminated)
		})
	}
	accounts := &offPeakSegmentAdminAccountStub{account: offPeakSegmentHandlerAccount()}
	manager := &offPeakSegmentHandlerManagerStub{}
	router := offPeakSegmentHandlerRouter(accounts, manager)
	for _, id := range []string{"invalid", "0", "-1", "9223372036854775808"} {
		result := zhipuRequest(t, router, http.MethodGet, "/accounts/"+id+"/off-peak/segment", "")
		require.Equal(t, http.StatusBadRequest, result.Code)
	}
	for _, body := range []string{`{`, `{}`, `{"record_id":"record"}`, `{"record_id":" ","generation":"gen"}`, `{"record_id":"record","generation":" "}`} {
		result := zhipuRequest(t, router, http.MethodPost, "/accounts/501/off-peak/segment/terminate", body)
		require.Equal(t, http.StatusBadRequest, result.Code)
	}
	require.Zero(t, accounts.reads, "invalid paths/bodies cannot load account credentials")
	require.Zero(t, manager.reads)
	require.Empty(t, manager.terminated)
}

func TestZhipuOffPeakSegmentHandlerReportsDisabledMissingAndStoreFailure(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		reason string
		nilMgr bool
	}{
		{"disabled-persistence", http.StatusNotImplemented, "ZHIPU_OFFPEAK_DURABLE_REQUIRED", false},
		{"manager-unavailable", http.StatusNotImplemented, "ZHIPU_OFFPEAK_DURABLE_REQUIRED", true},
		{"missing-segment", http.StatusNotFound, "ZHIPU_OFFPEAK_SEGMENT_NOT_FOUND", false},
		{"stale-generation", http.StatusConflict, "ZHIPU_OFFPEAK_SEGMENT_CONFLICT", false},
		{"database-unavailable", http.StatusServiceUnavailable, "ZHIPU_OFFPEAK_STORE_UNAVAILABLE", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			accounts := &offPeakSegmentAdminAccountStub{account: offPeakSegmentHandlerAccount()}
			manager := &offPeakSegmentHandlerManagerStub{err: infraerrors.New(scenario.status, scenario.reason, "off-peak segment unavailable")}
			var port ZhipuOffPeakSegmentManager = manager
			if scenario.nilMgr {
				port = nil
			}
			router := offPeakSegmentHandlerRouter(accounts, port)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path := "/accounts/501/off-peak/segment"
				if method == http.MethodPost {
					path += "/terminate"
				}
				result := zhipuRequest(t, router, method, path, `{"record_id":"record","generation":"generation"}`)
				require.Equal(t, scenario.status, result.Code)
				require.Equal(t, scenario.reason, gjson.Get(result.Body.String(), "reason").String())
				require.NotContains(t, result.Body.String(), "secret-")
				require.False(t, gjson.Get(result.Body.String(), "data.accepted").Bool())
			}
		})
	}
	accounts := &offPeakSegmentAdminAccountStub{err: errors.New("database read unavailable")}
	manager := &offPeakSegmentHandlerManagerStub{}
	result := zhipuRequest(t, offPeakSegmentHandlerRouter(accounts, manager), http.MethodGet, "/accounts/501/off-peak/segment", "")
	require.Equal(t, http.StatusInternalServerError, result.Code)
	require.Zero(t, manager.reads)
}

func TestZhipuOffPeakSegmentHandlerDuplicateOldHandleCannotTerminateNewSegment(t *testing.T) {
	accounts := &offPeakSegmentAdminAccountStub{account: offPeakSegmentHandlerAccount()}
	manager := &offPeakSegmentHandlerManagerStub{current: &service.ZhipuOffPeakSegment{RecordID: "record-old", Generation: "generation-old", State: "active"}}
	router := offPeakSegmentHandlerRouter(accounts, manager)
	body := `{"record_id":"record-old","generation":"generation-old"}`
	first := zhipuRequest(t, router, http.MethodPost, "/accounts/501/off-peak/segment/terminate", body)
	require.Equal(t, http.StatusOK, first.Code)
	manager.current = &service.ZhipuOffPeakSegment{RecordID: "record-new", Generation: "generation-new", State: "ready"}
	duplicate := zhipuRequest(t, router, http.MethodPost, "/accounts/501/off-peak/segment/terminate", body)
	require.Equal(t, http.StatusOK, duplicate.Code)
	require.Equal(t, [][2]string{{"record-old", "generation-old"}, {"record-old", "generation-old"}}, manager.terminated)
	require.Equal(t, "record-new", manager.current.RecordID)
	require.Zero(t, manager.reads, "the endpoint must forward the explicit pair instead of resolving the latest one")
	wrong := zhipuRequest(t, router, http.MethodPost, "/accounts/501/off-peak/segment/terminate", `{"record_id":"record-new","generation":"generation-old"}`)
	require.Equal(t, http.StatusConflict, wrong.Code)
	require.Equal(t, "record-new", manager.current.RecordID)
	require.False(t, manager.retired[[2]string{"record-new", "generation-new"}])
}
