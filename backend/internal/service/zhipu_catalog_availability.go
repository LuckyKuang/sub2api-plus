package service

import "strings"

// zhipuCodingPlanOnlyModels lists catalog entries whose presence in the shared
// platform catalog is not evidence that a pay-as-you-go or trial credential can
// invoke them. Admin candidates still include these IDs; public listings and
// cross-group selection must prove the entitlement through the credential's own
// plan, an explicit mapping, or a live model snapshot.
var zhipuCodingPlanOnlyModels = map[string]struct{}{
	"glm-5.4": {},
}

// IsZhipuCodingPlanCredential reports whether the account's own plan establishes
// coding-plan access. Start-plan and off-peak credentials are excluded: their
// entitlements must come from a current observation or model snapshot.
func (a *Account) IsZhipuCodingPlanCredential() bool {
	if a == nil || a.Platform != PlatformZhipu {
		return false
	}
	switch strings.TrimSpace(a.GetCredential("plan_kind")) {
	case ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan:
		return true
	case ZhipuPlanStartPlan, ZhipuPlanOffPeak:
		return false
	}
	return a.GetAccountMode() == AccountModeCoding
}

// FilterStaticCatalogModels removes coding-plan-only entries that the shared
// catalog alone cannot prove for this credential. Apply it only to the static
// default path; explicit mappings and live metadata snapshots remain
// authoritative and are never filtered here.
func FilterStaticCatalogModels(account *Account, ids []string) []string {
	if account == nil || account.Platform != PlatformZhipu || account.IsZhipuCodingPlanCredential() {
		return ids
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, restricted := zhipuCodingPlanOnlyModels[strings.ToLower(strings.TrimSpace(id))]; restricted {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered
}
