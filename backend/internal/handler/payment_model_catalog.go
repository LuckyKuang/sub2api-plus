package handler

import (
	"context"
	"fmt"
	"sort"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type saleCatalogReader interface {
	ListSaleCatalog(context.Context, []int64) ([]service.CatalogGroup, error)
}

type saleUserRateReader interface {
	GetUserGroupRates(context.Context, int64) (map[int64]float64, error)
}

// planModelCatalog is a sale-scoped, credential-free projection. It describes
// current consumption rules; purchasing grants group access and quota, not a
// frozen historical multiplier or a guarantee of live provider availability.
type planModelCatalog struct {
	group         service.Group
	PolicyVersion int64          `json:"policy_version"`
	AsOf          time.Time      `json:"as_of"`
	Platforms     []string       `json:"platforms"`
	Models        []catalogOffer `json:"models"`
	RateMin       *float64       `json:"rate_min"`
	RateMax       *float64       `json:"rate_max"`
}

func toPlanModelCatalog(cg service.CatalogGroup, userRates map[int64]float64) planModelCatalog {
	projected := toCatalogGroup(cg, userRates)
	out := planModelCatalog{group: cg.Group, PolicyVersion: projected.PolicyVersion, AsOf: projected.AsOf, Platforms: []string{}, Models: projected.Models}
	platforms := make(map[string]struct{})
	for _, model := range out.Models {
		platforms[model.Platform] = struct{}{}
		if model.RateQuote == nil {
			continue
		}
		rate := model.RateQuote.EffectiveMultiplier
		if out.RateMin == nil || rate < *out.RateMin {
			value := rate
			out.RateMin = &value
		}
		if out.RateMax == nil || rate > *out.RateMax {
			value := rate
			out.RateMax = &value
		}
	}
	for platform := range platforms {
		out.Platforms = append(out.Platforms, platform)
	}
	sort.Strings(out.Platforms)
	return out
}

func planGroupInfoFromCatalog(catalog planModelCatalog) service.PlanGroupInfo {
	g := catalog.group
	return service.PlanGroupInfo{Platform: g.Platform, Name: g.Name, I18n: g.I18n, RateMultiplier: g.RateMultiplier,
		PeakRateEnabled: g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd, PeakRateMultiplier: g.PeakRateMultiplier,
		DailyLimitUSD: g.DailyLimitUSD, WeeklyLimitUSD: g.WeeklyLimitUSD, MonthlyLimitUSD: g.MonthlyLimitUSD, FiveHourLimitUSD: g.FiveHourLimitUSD, ModelScopes: g.SupportedModelScopes}
}

func (h *PaymentHandler) saleCatalogForPlans(ctx context.Context, plans []*dbent.SubscriptionPlan, userID int64) (map[int64]planModelCatalog, error) {
	out := make(map[int64]planModelCatalog)
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, plan := range plans {
		if plan == nil || !plan.ForSale || seen[plan.GroupID] {
			continue
		}
		seen[plan.GroupID] = true
		ids = append(ids, plan.GroupID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	if h.modelCatalog == nil {
		return nil, fmt.Errorf("sale model catalog is unavailable")
	}
	if userID <= 0 || h.userRates == nil {
		return nil, fmt.Errorf("sale personal rates are unavailable")
	}
	rates, err := h.userRates.GetUserGroupRates(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sale personal rates unavailable: %w", err)
	}
	groups, err := h.modelCatalog.ListSaleCatalog(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if seen[group.Group.ID] {
			out[group.Group.ID] = toPlanModelCatalog(group, rates)
		}
	}
	return out, nil
}
