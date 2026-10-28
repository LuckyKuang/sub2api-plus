package handler

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
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
	PeakTimezone  string             `json:"peak_timezone"`
	Models        []catalogOffer     `json:"models"`
	PolicyVersion int64              `json:"policy_version"`
	AsOf          time.Time          `json:"as_of"`
	RateSummary   catalogRateSummary `json:"rate_summary"`
}
type catalogRateSummary struct {
	Status        string                    `json:"status"`
	PolicyVersion int64                     `json:"policy_version"`
	AsOf          time.Time                 `json:"as_of"`
	Entries       []catalogRateSummaryEntry `json:"entries"`
}
type catalogRateSummaryEntry struct {
	Platform           string                    `json:"platform"`
	BillingUnit        string                    `json:"billing_unit"`
	DefaultQuote       *service.BillingRateQuote `json:"default_quote"`
	RateMin            *float64                  `json:"rate_min"`
	RateMax            *float64                  `json:"rate_max"`
	ModelCount         int                       `json:"model_count"`
	HasModelExceptions bool                      `json:"has_model_exceptions"`
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
	OfferKey            string                    `json:"offer_key"`
	BillingMode         *string                   `json:"billing_mode"`
	BillingUnit         string                    `json:"billing_unit"`
	PriceStatus         string                    `json:"price_status"`
	PriceReason         string                    `json:"price_reason,omitempty"`
	Source              catalogSource             `json:"source"`
	MediaTiers          []catalogMediaTier        `json:"media_tiers,omitempty"`
	ServiceTiers        []catalogServiceTier      `json:"service_tier_pricing,omitempty"`
	RateQuote           *service.BillingRateQuote `json:"rate_quote"`
	Endpoints           []string                  `json:"endpoints,omitempty"`
	PriceTimeMultiplier float64                   `json:"price_time_multiplier"`
}

func catalogView(c *gin.Context) bool {
	values := c.Request.URL.Query()["view"]
	return len(values) == 1 && values[0] == "catalog"
}

func keyQuotesView(c *gin.Context) bool {
	values := c.Request.URL.Query()["view"]
	return len(values) == 1 && values[0] == "key_quotes"
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
		localized := toCatalogGroupWithRateStatus(group, rates, out.UserRateStatus != "unavailable")
		localized.Name, localized.Description = group.Group.I18n.Localize(
			c.GetHeader("Accept-Language"), localized.Name, localized.Description)
		out.Groups = append(out.Groups, localized)
	}
	response.Success(c, out)
}

func toCatalogGroup(cg service.CatalogGroup, rates map[int64]float64) catalogGroup {
	return toCatalogGroupWithRateStatus(cg, rates, true)
}

func toCatalogGroupWithRateStatus(cg service.CatalogGroup, rates map[int64]float64, ratesAvailable bool) catalogGroup {
	g := cg.Group
	ratesAvailable = ratesAvailable && g.BillingRatePolicy.Validate() == nil
	asOf := cg.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	out := catalogGroup{
		modelPlazaGroup: modelPlazaGroup{
			ID: g.ID, Name: g.Name, Description: g.Description, Platform: g.Platform,
			SubscriptionType: g.SubscriptionType, IsExclusive: g.IsExclusive, RateMultiplier: g.RateMultiplier,
			PeakRateEnabled: g.IsSubscriptionType() && g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd, PeakRateMultiplier: g.PeakRateMultiplier,
			ImageRateIndependent: g.ImageRateIndependent, ImageRateMultiplier: g.ImageRateMultiplier,
			VideoRateIndependent: g.VideoRateIndependent, VideoRateMultiplier: g.VideoRateMultiplier,
			LongContextPricingEnabled: g.LongContextPricingEnabled,
		},
		PeakTimezone: timezone.Name(), Models: make([]catalogOffer, 0, len(cg.Models)), PolicyVersion: g.BillingPolicyVersion, AsOf: asOf,
	}
	if rate, ok := rates[g.ID]; ok && ratesAvailable {
		out.UserRateMultiplier = &rate
	}
	for _, m := range cg.Models {
		o := catalogOffer{
			modelPlazaModel: modelPlazaModel{Name: m.Name, Platform: m.Platform, Pricing: toUserPricing(m.Pricing), OfficialPricing: toModelPlazaOfficialPricing(m.OfficialPricing), LongContextBasis: string(m.LongContextBasis), TimePricing: toModelPlazaTimePricing(m.TimePricing)},
			OfferKey:        m.OfferKey, BillingUnit: m.BillingUnit, PriceStatus: m.PriceStatus, PriceReason: m.PriceReason,
			Source:              catalogSource{Name: m.Source.Name, Description: m.Source.Description},
			Endpoints:           m.Endpoints,
			PriceTimeMultiplier: m.PriceTimeMultiplier,
		}
		if o.PriceTimeMultiplier == 0 {
			o.PriceTimeMultiplier = 1
		}
		if ratesAvailable && m.BillingUnit != "" && m.BillingUnit != "unknown" {
			quote := m.ResolveBillingRateQuote(&g, out.UserRateMultiplier, asOf)
			if validCatalogRateQuote(quote) {
				o.RateQuote = &quote
			}
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
	out.RateSummary = buildCatalogRateSummary(cg, out, ratesAvailable)
	return out
}

func validCatalogRateQuote(quote service.BillingRateQuote) bool {
	return !math.IsNaN(quote.EffectiveMultiplier) && !math.IsInf(quote.EffectiveMultiplier, 0) && quote.EffectiveMultiplier >= 0 && quote.EffectiveMultiplier <= domain.MaxBillingRateMultiplier
}

// Summaries describe configured public offers only. A pricing rule alone cannot
// establish that a supplier or model can be used by this group.
func buildCatalogRateSummary(cg service.CatalogGroup, public catalogGroup, available bool) catalogRateSummary {
	summary := catalogRateSummary{Status: "empty", PolicyVersion: public.PolicyVersion, AsOf: public.AsOf, Entries: []catalogRateSummaryEntry{}}
	if !available {
		summary.Status = "unavailable"
	} else if len(cg.Models) > 0 {
		summary.Status = "available"
	}
	type bucket struct {
		entry             catalogRateSummaryEntry
		models            map[string]bool
		defaultSet        bool
		defaultCompatible bool
	}
	buckets := make(map[string]*bucket)
	defaultGroup := cg.Group
	defaultGroup.BillingRatePolicy = cg.Group.BillingRatePolicy.Clone()
	defaultGroup.BillingRatePolicy.ModelMultipliers = nil
	for i, offer := range cg.Models {
		if !domain.IsConcretePlatform(offer.Platform) || offer.BillingUnit == "" || offer.BillingUnit == "unknown" {
			continue
		}
		key := offer.Platform + "\x00" + offer.BillingUnit
		b := buckets[key]
		if b == nil {
			b = &bucket{entry: catalogRateSummaryEntry{Platform: offer.Platform, BillingUnit: offer.BillingUnit}, models: make(map[string]bool), defaultCompatible: true}
			buckets[key] = b
		}
		b.models[strings.ToLower(offer.Name)] = true
		if !available {
			continue
		}
		quote := public.Models[i].RateQuote
		if quote != nil {
			rate := quote.EffectiveMultiplier
			if b.entry.RateMin == nil || rate < *b.entry.RateMin {
				value := rate
				b.entry.RateMin = &value
			}
			if b.entry.RateMax == nil || rate > *b.entry.RateMax {
				value := rate
				b.entry.RateMax = &value
			}
			b.entry.HasModelExceptions = b.entry.HasModelExceptions || quote.Source == "model"
		}
		def := offer.ResolveBillingRateQuote(&defaultGroup, public.UserRateMultiplier, public.AsOf)
		def.Model = ""
		if !validCatalogRateQuote(def) {
			b.defaultCompatible = false
		} else if !b.defaultSet {
			b.entry.DefaultQuote, b.defaultSet = &def, true
		} else if current := b.entry.DefaultQuote; current.BaseMultiplier != def.BaseMultiplier || current.BaseSource != def.BaseSource || current.EffectiveMultiplier != def.EffectiveMultiplier {
			// Request-priced image/video offers can use different independent
			// media bases. Their model quotes remain authoritative.
			b.defaultCompatible = false
		}
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := buckets[key]
		b.entry.ModelCount = len(b.models)
		if !b.defaultCompatible {
			b.entry.DefaultQuote = nil
		}
		summary.Entries = append(summary.Entries, b.entry)
	}
	return summary
}
