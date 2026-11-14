package service

import (
	"context"
	"strings"
)

// PlatformDefaultModelIDs returns the built-in catalog a platform serves when no
// model mapping is configured. It is the same candidate list the group model
// allowlist editor offers for that platform.
func PlatformDefaultModelIDs(platform string) []string {
	return defaultModelsListCandidateIDs(strings.TrimSpace(platform))
}

// AccountPublicModels returns the model IDs an account exposes to listings.
//
// A configured model_mapping is authoritative: its keys are the public names the
// account serves. Passthrough routing ignores the mapping, so those accounts
// expose the platform catalog. An account without a mapping (every OAuth
// account) falls back to its platform catalog, which keeps OAuth-only groups
// listable without any manual model configuration.
//
// listingPlatform scopes accounts pulled in through mixed scheduling to the
// models that belong to the listed platform.
func AccountPublicModels(account *Account, listingPlatform string) []string {
	if account == nil {
		return nil
	}
	if account.Platform == PlatformOpenAI && account.IsOpenAIPassthroughEnabled() {
		return filterPublicModelsForListing(account, listingPlatform, PlatformDefaultModelIDs(PlatformOpenAI))
	}
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		ids := make([]string, 0, len(mapping))
		for id := range mapping {
			ids = append(ids, id)
		}
		return filterPublicModelsForListing(account, listingPlatform, ids)
	}
	// StepFun narrows its catalog to live discovery when a snapshot exists.
	// Other platforms stay silent here: the group-level fallback keeps their
	// catalog visible, and an unmapped account must not widen a group whose
	// accounts already declare their models.
	if account.Platform == PlatformStepFun {
		if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
			ids := make([]string, 0, len(snapshot.Models))
			for id := range snapshot.Models {
				ids = append(ids, id)
			}
			return filterPublicModelsForListing(account, listingPlatform, ids)
		}
	}
	return nil
}

// groupDefaultModelsForListing returns the platform catalog only when the group
// itself runs on that platform. Composite groups enumerate linked platforms,
// which must not contribute defaults of their own, and CN vendors publish none.
func (s *GatewayService) groupDefaultModelsForListing(ctx context.Context, groupID *int64, platform string) []string {
	if s == nil || s.groupRepo == nil || groupID == nil || !platformHasStaticDefaultModels(platform) {
		return nil
	}
	group, err := s.groupRepo.GetByIDLite(ctx, *groupID)
	if err != nil || group == nil {
		return nil
	}
	if strings.TrimSpace(group.Platform) != strings.TrimSpace(platform) {
		return nil
	}
	return PlatformDefaultModelIDs(platform)
}

// platformHasStaticDefaultModels reports whether the platform publishes a
// maintained catalog that can stand in for missing account configuration. CN
// vendors have none: their catalogs come from live discovery per account, so a
// group-wide default would invent models the upstream may not serve.
func platformHasStaticDefaultModels(platform string) bool {
	switch platform {
	case PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu, PlatformStepFun:
		return false
	default:
		return len(PlatformDefaultModelIDs(platform)) > 0
	}
}

func filterPublicModelsForListing(account *Account, listingPlatform string, ids []string) []string {
	if account == nil || len(ids) == 0 {
		return nil
	}
	listingPlatform = strings.TrimSpace(listingPlatform)
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if listingPlatform != "" && account.Platform != listingPlatform && !mixedListingModelAllowed(listingPlatform, id) {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	return out
}
