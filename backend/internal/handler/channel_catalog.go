package handler

import (
	"log/slog"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type channelCatalog struct {
	Groups         []catalogGroup `json:"groups"`
	UserRateStatus string         `json:"user_rate_status"`
}
type catalogGroup struct {
	modelPlazaGroup
	PeakTimezone string         `json:"peak_timezone"`
	Models       []catalogOffer `json:"models"`
}
type catalogSource struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type catalogMediaTier struct {
	Label string   `json:"label"`
	Unit  string   `json:"unit"`
	Price *float64 `json:"price"`
}
type catalogServiceTier struct {
	Name    string                     `json:"name"`
	Pricing *userSupportedModelPricing `json:"pricing"`
}
type catalogOffer struct {
	modelPlazaModel
	OfferKey     string               `json:"offer_key"`
	BillingMode  *string              `json:"billing_mode"`
	BillingUnit  string               `json:"billing_unit"`
	PriceStatus  string               `json:"price_status"`
	PriceReason  string               `json:"price_reason,omitempty"`
	Source       catalogSource        `json:"source"`
	MediaTiers   []catalogMediaTier   `json:"media_tiers,omitempty"`
	ServiceTiers []catalogServiceTier `json:"service_tier_pricing,omitempty"`
}

func catalogView(c *gin.Context) bool {
	values := c.Request.URL.Query()["view"]
	return len(values) == 1 && values[0] == "catalog"
}

func emptyChannelCatalog() channelCatalog {
	return channelCatalog{Groups: []catalogGroup{}, UserRateStatus: "not_requested"}
}

func (h *AvailableChannelHandler) listCatalog(c *gin.Context, userID int64) {
	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := emptyChannelCatalog()
	if len(groups) == 0 {
		response.Success(c, out)
		return
	}
	catalog, err := h.plazaService.ListAvailableCatalog(c.Request.Context(), groups)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	rates, rateErr := h.apiKeyService.GetUserGroupRates(c.Request.Context(), userID)
	out.UserRateStatus = "loaded"
	if rateErr != nil {
		out.UserRateStatus = "unavailable"
		rates = nil
		slog.WarnContext(c.Request.Context(), "catalog user rates unavailable", "endpoint", "/api/v1/channels/available", "reason", "user_rate_dependency_failed")
	}
	for _, group := range catalog {
		out.Groups = append(out.Groups, toCatalogGroup(group, rates))
	}
	response.Success(c, out)
}

func toCatalogGroup(cg service.CatalogGroup, rates map[int64]float64) catalogGroup {
	g := cg.Group
	out := catalogGroup{
		modelPlazaGroup: modelPlazaGroup{
			ID: g.ID, Name: g.Name, Description: g.Description, Platform: g.Platform,
			SubscriptionType: g.SubscriptionType, IsExclusive: g.IsExclusive, RateMultiplier: g.RateMultiplier,
			PeakRateEnabled: g.IsSubscriptionType() && g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd, PeakRateMultiplier: g.PeakRateMultiplier,
			ImageRateIndependent: g.ImageRateIndependent, ImageRateMultiplier: g.ImageRateMultiplier,
			VideoRateIndependent: g.VideoRateIndependent, VideoRateMultiplier: g.VideoRateMultiplier,
			LongContextPricingEnabled: g.LongContextPricingEnabled,
		},
		PeakTimezone: timezone.Name(), Models: make([]catalogOffer, 0, len(cg.Models)),
	}
	if rate, ok := rates[g.ID]; ok {
		out.UserRateMultiplier = &rate
	}
	for _, m := range cg.Models {
		o := catalogOffer{
			modelPlazaModel: modelPlazaModel{Name: m.Name, Platform: m.Platform, Pricing: toUserPricing(m.Pricing), OfficialPricing: toModelPlazaOfficialPricing(m.OfficialPricing), LongContextBasis: string(m.LongContextBasis), TimePricing: toModelPlazaTimePricing(m.TimePricing)},
			OfferKey:        m.OfferKey, BillingUnit: m.BillingUnit, PriceStatus: m.PriceStatus, PriceReason: m.PriceReason,
			Source: catalogSource{Name: m.Source.Name, Description: m.Source.Description},
		}
		if m.BillingMode != "" {
			mode := string(m.BillingMode)
			o.BillingMode = &mode
		}
		for _, tier := range m.MediaTiers {
			o.MediaTiers = append(o.MediaTiers, catalogMediaTier{Label: tier.Label, Unit: tier.Unit, Price: tier.Price})
		}
		for _, tier := range m.ServiceTiers {
			o.ServiceTiers = append(o.ServiceTiers, catalogServiceTier{Name: tier.Name, Pricing: toUserPricing(tier.Pricing)})
		}
		out.Models = append(out.Models, o)
	}
	return out
}
