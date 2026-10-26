package service

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

// filterDeepSeekNativeModelList applies the official Messages listing contract
// to the identity that actually owns this request. Both public discovery and
// admin metadata synchronization consume the same filtered response.
func filterDeepSeekNativeModelList(ctx context.Context, account *Account, body []byte) ([]byte, error) {
	if account == nil || account.Platform != PlatformDeepseek || !account.IsDomesticOAuth() {
		return body, nil
	}
	envelope, rows, err := modelCatalogEntries(body, "data")
	if err != nil {
		return nil, err
	}
	identity, _ := outboundidentity.FromContext(ctx)
	accepted := make([]json.RawMessage, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, raw := range rows {
		row, ok := deepSeekModelFields(raw)
		if !ok {
			continue
		}
		var id string
		if json.Unmarshal(row["id"], &id) != nil || strings.TrimSpace(id) == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		if !deepSeekModelRequirementSatisfied(row, identity.Version) || !deepSeekModelMessagesUsable(row) {
			continue
		}
		accepted = append(accepted, raw)
	}
	if len(accepted) == len(rows) {
		return body, nil
	}
	envelope["data"], err = json.Marshal(accepted)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

func deepSeekModelFields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func deepSeekModelRequirementSatisfied(row map[string]json.RawMessage, clientVersion string) bool {
	raw, declared := row["client_requirements"]
	if !declared {
		return true
	}
	requirements, ok := deepSeekModelFields(raw)
	if !ok {
		return false
	}
	raw, declared = requirements["deepseek-harness"]
	if !declared {
		return true
	}
	own, ok := deepSeekModelFields(raw)
	if !ok {
		return false
	}
	raw, declared = own["min_version"]
	if !declared {
		return true
	}
	var floor string
	if json.Unmarshal(raw, &floor) != nil {
		return false
	}
	// npm semver accepts an optional v prefix, but requires all three core
	// components. CompareVersions alone would also accept shorthand versions.
	if !IsStrictSemanticVersion(strings.TrimPrefix(strings.TrimSpace(floor), "v")) ||
		!IsStrictSemanticVersion(strings.TrimPrefix(strings.TrimSpace(clientVersion), "v")) {
		return false
	}
	return CompareVersions(clientVersion, floor) >= 0
}

func deepSeekModelMessagesUsable(row map[string]json.RawMessage) bool {
	capabilities, ok := deepSeekModelFields(row["api_capabilities"])
	if !ok {
		return false
	}
	if _, ok := deepSeekModelFields(capabilities["anthropic_messages"]); !ok {
		return false
	}
	return deepSeekModelModalitiesUsable(row, "input_modalities", "text", "image") &&
		deepSeekModelModalitiesUsable(row, "output_modalities", "text") &&
		deepSeekModelCapacityUsable(row, "context_window") &&
		deepSeekModelCapacityUsable(row, "max_output_tokens")
}

func deepSeekModelModalitiesUsable(row map[string]json.RawMessage, field string, supported ...string) bool {
	raw, declared := row[field]
	if !declared {
		return true // The official default is text for both directions.
	}
	var modalities []json.RawMessage
	if json.Unmarshal(raw, &modalities) != nil {
		return false
	}
	for _, raw := range modalities {
		var modality string
		if json.Unmarshal(raw, &modality) != nil {
			continue
		}
		for _, accepted := range supported {
			if modality == accepted {
				return true
			}
		}
	}
	return false
}

func deepSeekModelCapacityUsable(row map[string]json.RawMessage, field string) bool {
	raw, declared := row[field]
	if !declared {
		return true
	}
	var value float64
	return json.Unmarshal(raw, &value) == nil && value > 0 && value <= 9007199254740991 && math.Trunc(value) == value
}
