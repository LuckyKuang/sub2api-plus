package handler

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnmodels"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type nativeAccessModel struct {
	Name                string   `json:"name"`
	Platform            string   `json:"platform"`
	Endpoints           []string `json:"endpoints"`
	ConversionEndpoints []string `json:"conversion_endpoints"`
}

// GetNativeAccess projects only the caller's authorized group and public models.
// Credentials and upstream account identifiers never leave this boundary.
func (h *APIKeyHandler) GetNativeAccess(c *gin.Context) {
	subject, ok := middleware2.GetReadSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid group ID")
		return
	}
	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var group *service.Group
	for i := range groups {
		if groups[i].ID == id {
			group = &groups[i]
			break
		}
	}
	if group == nil {
		response.Forbidden(c, "Group is not available")
		return
	}
	accounts, err := h.accountService.ListByGroup(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	catalog, err := h.modelPlazaService.ListAvailableCatalog(c.Request.Context(), []service.Group{*group})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	routeModels, err := h.compositeResolver.ListExactPublicModels(c.Request.Context(), id, "", true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	candidates := nativeAccessCandidates(group, accounts, catalog, routeModels)
	models := []nativeAccessModel{}
	endpoints := map[string]bool{}
	paths := map[string]string{service.APIProtocolResponses: "/v1/responses", service.APIProtocolChatCompletions: "/v1/chat/completions", service.APIProtocolAnthropic: "/v1/messages", "gemini": "/v1beta", "systemone": "/v1/systemone"}
	metadataCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	for _, offer := range candidates {
		available := map[string]bool{}
		convertible := map[string]bool{}
		for _, account := range accounts {
			if account.Platform != offer.Platform || account.Status != service.StatusActive {
				continue
			}
			for protocol, path := range paths {
				upstreamModel := offer.Name
				if group.Platform == service.PlatformComposite && h.compositeResolver != nil {
					domain := protocol
					if protocol == service.APIProtocolAnthropic {
						domain = service.CompositeRouteEndpointMessages
					}
					decision, routeErr := h.compositeResolver.Resolve(c.Request.Context(), id, offer.Name, domain)
					if routeErr != nil {
						response.ErrorFrom(c, routeErr)
						return
					}
					if !decision.Matched || decision.TargetPlatform != account.Platform {
						continue
					}
					upstreamModel = decision.UpstreamModel
				}
				upstreamModel = h.compositeResolver.NativeRoutingModel(c.Request.Context(), group.ID, account.Platform, upstreamModel)
				if !account.IsModelSupported(upstreamModel) {
					continue
				}
				if service.CompositePlatformConversionSupported(account.Platform, protocol) && len(account.NativeProtocols(account.GetMappedModel(upstreamModel))) > 0 {
					convertible[path] = true
				}
				native := false
				for _, supported := range h.openaiGateway.NativeAccessProtocols(metadataCtx, &account, account.GetMappedModel(upstreamModel)) {
					if supported == protocol {
						native = true
						break
					}
				}
				if !native {
					continue
				}
				available[path] = true
				endpoints[path] = true
			}
		}
		model := nativeAccessModel{Name: offer.Name, Platform: offer.Platform, Endpoints: []string{}, ConversionEndpoints: []string{}}
		for path := range convertible {
			model.ConversionEndpoints = append(model.ConversionEndpoints, path)
		}
		sort.Strings(model.ConversionEndpoints)
		for path := range available {
			model.Endpoints = append(model.Endpoints, path)
		}
		sort.Strings(model.Endpoints)
		if len(model.Endpoints) > 0 || len(model.ConversionEndpoints) > 0 {
			models = append(models, model)
		}
	}
	all := []string{}
	for endpoint := range endpoints {
		all = append(all, endpoint)
	}
	sort.Strings(all)
	response.Success(c, gin.H{"endpoints": all, "conversion_endpoints": compositeConversionEndpoints(models), "models": models})
}

func compositeConversionEndpoints(models []nativeAccessModel) []string {
	common := []string{}
	if len(models) == 0 {
		return common
	}
	for _, endpoint := range service.CompositeConversionEndpoints() {
		if !slices.ContainsFunc(models, func(model nativeAccessModel) bool { return !slices.Contains(model.ConversionEndpoints, endpoint) }) {
			common = append(common, endpoint)
		}
	}
	return common
}

// Capability discovery also works before a channel price directory exists.
// Candidate IDs come from the same maintained models, account mappings and
// explicit public routes as gateway listings, then respect the group allowlist.
func nativeAccessCandidates(group *service.Group, accounts []service.Account, catalog []service.CatalogGroup, routeModels []string) []nativeAccessModel {
	byID := map[string]nativeAccessModel{}
	add := func(platform, name string) {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, "*") {
			return
		}
		if group.ModelAllowlistEnabled() && len(group.ModelAllowlist.FilterForListing([]string{name})) == 0 {
			return
		}
		byID[platform+"\x00"+name] = nativeAccessModel{Name: name, Platform: platform}
	}
	for _, entry := range catalog {
		for _, model := range entry.Models {
			add(model.Platform, model.Name)
		}
	}
	for i := range accounts {
		account := &accounts[i]
		if account.Status != service.StatusActive {
			continue
		}
		mapping := account.GetModelMapping()
		for name := range mapping {
			add(account.Platform, name)
		}
		if len(mapping) == 0 {
			defaults := defaultModelIDsForPlatform(account.Platform)
			if service.IsMultiProtocolAPIKeyProvider(account.Platform) {
				defaults = cnmodels.DefaultModelIDs(account.Platform)
			}
			if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
				defaults = nil
				for name := range snapshot.Models {
					defaults = append(defaults, name)
				}
			}
			for _, name := range defaults {
				add(account.Platform, name)
			}
		}
		for _, name := range routeModels {
			add(account.Platform, name)
		}
	}
	out := make([]nativeAccessModel, 0, len(byID))
	for _, model := range byID {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Name < out[j].Name
	})
	return out
}
