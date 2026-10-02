//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type catalogAccessStub struct {
	groups       []service.Group
	rates        map[int64]float64
	err, rateErr error
	users        []int64
	rateCalls    int
}

func (s *catalogAccessStub) GetAvailableGroups(_ context.Context, id int64) ([]service.Group, error) {
	s.users = append(s.users, id)
	return s.groups, s.err
}
func (s *catalogAccessStub) GetUserGroupRates(_ context.Context, id int64) (map[int64]float64, error) {
	s.users = append(s.users, id)
	s.rateCalls++
	return s.rates, s.rateErr
}

type catalogReaderStub struct {
	received []service.Group
	err      error
	calls    int
}

func (s *catalogReaderStub) ListAvailableCatalog(_ context.Context, groups []service.Group) ([]service.CatalogGroup, error) {
	s.received = groups
	s.calls++
	out := []service.CatalogGroup{}
	for _, g := range groups {
		out = append(out, service.CatalogGroup{Group: g, Models: []service.CatalogOffer{}})
	}
	return out, s.err
}
func catalogContext(query string, user bool) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/channels/available"+query, nil)
	if user {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}
	return c, w
}
func TestAvailableCatalogGateAndQueryCompatibility(t *testing.T) {
	for _, q := range []string{"", "?view=", "?view=wrong", "?view=catalog&view=catalog", "?view=catalog"} {
		t.Run(q, func(t *testing.T) {
			h := &AvailableChannelHandler{}
			c, w := catalogContext(q, false)
			h.List(c)
			require.Equal(t, 401, w.Code)
			c, w = catalogContext(q, true)
			h.List(c)
			require.Equal(t, 200, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if q == "?view=catalog" {
				data := body["data"].(map[string]any)
				require.Equal(t, "not_requested", data["user_rate_status"])
				require.Equal(t, []any{}, data["groups"])
			} else {
				require.Equal(t, []any{}, body["data"])
			}
		})
	}
}
func TestAvailableCatalogAuthorizationBeforeAssemblyAndRates(t *testing.T) {
	for _, failure := range []string{"", "auth", "catalog", "rates", "empty"} {
		t.Run(failure, func(t *testing.T) {
			access := &catalogAccessStub{groups: []service.Group{{ID: 7, Name: "Allowed"}}, rates: map[int64]float64{7: 0, 99: 10}}
			reader := &catalogReaderStub{}
			if failure == "auth" {
				access.err = errors.New("authorization failed")
			}
			if failure == "catalog" {
				reader.err = errors.New("read failed")
			}
			if failure == "rates" {
				access.rateErr = errors.New("rates failed")
			}
			if failure == "empty" {
				access.groups = nil
			}
			settings := service.NewSettingService(&settingHandlerPublicRepoStub{values: map[string]string{service.SettingKeyAvailableChannelsEnabled: "true", service.SettingKeyModelPlazaEnabled: "false"}}, &config.Config{})
			h := &AvailableChannelHandler{apiKeyService: access, plazaService: reader, settingService: settings}
			c, w := catalogContext("?view=catalog", true)
			h.List(c)
			if failure == "auth" || failure == "catalog" {
				require.Equal(t, 500, w.Code)
				require.Zero(t, access.rateCalls)
				if failure == "auth" {
					require.Zero(t, reader.calls)
				}
				return
			}
			require.Equal(t, 200, w.Code)
			var body struct {
				Data channelCatalog `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if failure == "empty" {
				require.Zero(t, reader.calls)
				require.Zero(t, access.rateCalls)
				require.Equal(t, "not_requested", body.Data.UserRateStatus)
				return
			}
			require.Equal(t, access.groups, reader.received)
			require.Equal(t, []int64{42, 42}, access.users)
			require.Len(t, body.Data.Groups, 1)
			require.Equal(t, int64(7), body.Data.Groups[0].ID)
			require.NotNil(t, body.Data.Groups[0].Models)
			if failure == "rates" {
				require.Equal(t, "unavailable", body.Data.UserRateStatus)
				require.Nil(t, body.Data.Groups[0].UserRateMultiplier)
			} else {
				require.Equal(t, "loaded", body.Data.UserRateStatus)
				require.NotNil(t, body.Data.Groups[0].UserRateMultiplier)
				require.Equal(t, 0.0, *body.Data.Groups[0].UserRateMultiplier)
			}
		})
	}
}
func TestAvailableCatalogWhitelistAndIndependentGroupFlags(t *testing.T) {
	price := 0.0
	dto := toCatalogGroup(service.CatalogGroup{Group: service.Group{ID: 7, IsExclusive: true, SubscriptionType: "subscription", PeakRateEnabled: true}, Models: []service.CatalogOffer{{
		PlazaModel: service.PlazaModel{Name: "public-id", Platform: "openai", Pricing: &service.ChannelModelPricing{ID: 98, ChannelID: 99, Models: []string{"internal-model"}, InputPrice: &price}},
		OfferKey:   "opaque", Source: service.CatalogSource{Name: "Channel"}, PriceStatus: "resolved", BillingUnit: "token",
	}}}, map[int64]float64{7: 0})
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	for _, hidden := range []string{"internal-model", "channel_id", "account_id", "credentials", "billing_model_source", "restrict_models"} {
		require.NotContains(t, string(raw), hidden)
	}
	require.True(t, dto.IsExclusive)
	require.Equal(t, "subscription", dto.SubscriptionType)
	require.Contains(t, string(raw), `"intervals":[]`)
	require.Contains(t, string(raw), `"input_price":0`)
}
