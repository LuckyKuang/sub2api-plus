package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
)

// kimi-code packages/oauth/src/managed-kimi-code.ts accepts unified reasoning
// objects. Keep public/PAYG boolean catalogs readable as well.
func parseUpstreamReasoning(raw json.RawMessage) (*bool, []string, string) {
	var boolean bool
	if json.Unmarshal(raw, &boolean) == nil && string(raw) != "null" {
		return &boolean, nil, ""
	}
	var value struct {
		Type   string `json:"type"`
		Effort struct {
			Valid   []string `json:"valid"`
			Default string   `json:"default"`
		} `json:"effort"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return nil, nil, ""
	}
	switch value.Type {
	case "only", "both":
		boolean = true
	case "none":
		boolean = false
	default:
		return nil, nil, ""
	}
	if !boolean {
		return &boolean, nil, ""
	}
	levels := normalizeReasoningLevels(value.Effort.Valid)
	defaultLevel := normalizeReasoningLevel(value.Effort.Default)
	if !slices.Contains(levels, defaultLevel) {
		defaultLevel = ""
	}
	return &boolean, levels, defaultLevel
}

func (s *AccountTestService) buildDomesticOAuthModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	region, err := cnoauth.NormalizeRegion(account.Platform, account.GetCredential("oauth_region"))
	if err != nil {
		return nil, err
	}
	// Native OAuth tokens only reach the platform's fixed inference origin.
	base := cnoauth.ModelBase(account.Platform, region)
	target := strings.TrimRight(base, "/") + "/v1/models"
	header := "Authorization"
	if account.Platform == PlatformDeepseek {
		u, _ := url.Parse(base)
		u.Path = "/models"
		target = u.String()
		header = "x-dsh-auth-token"
	} else {
		refresher := NewCNTokenRefresher(account.Platform)
		if refresher.NeedsRefresh(account, 2*time.Minute) {
			if s.cnOAuthService == nil || s.cnOAuthService.refreshAPI == nil {
				return nil, errors.New("native OAuth refresh service unavailable")
			}
			fresh, refreshErr := s.cnOAuthService.RefreshAccount(ctx, account, false)
			if refreshErr != nil {
				return nil, refreshErr
			}
			if fresh == nil || fresh.ID != account.ID || fresh.Platform != account.Platform || fresh.GetCredential("oauth_region") != account.GetCredential("oauth_region") || fresh.proxyURLOrEmpty() != account.proxyURLOrEmpty() {
				return nil, errors.New("OAuth account changed; retry discovery")
			}
			account = fresh
		}
	}
	if _, err := s.validateUpstreamBaseURL(target); err != nil {
		return nil, newUpstreamModelSyncConfigError("Native OAuth model destination is not allowed", err)
	}
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return nil, errors.New("native OAuth model credential is missing")
	}
	if header == "Authorization" {
		token = "Bearer " + token
	}
	ctx = WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(ctx), account)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(header, token)
	return req, nil
}

// OAuth account listing follows Harness listing.ts/resolve-catalog.ts. API-key
// /models remains a gateway discovery extension; no account token goes there.
func extractDeepSeekHarnessCatalog(body []byte, version string) ([]byte, map[string]UpstreamModelMetadata, error) {
	var listing struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || listing.Data == nil {
		return nil, nil, fmt.Errorf("invalid DeepSeek Harness model listing")
	}
	offered := make([]map[string]json.RawMessage, 0)
	metadata := map[string]UpstreamModelMetadata{}
	seen := map[string]bool{}
	for _, rawRow := range listing.Data {
		var row map[string]json.RawMessage
		if json.Unmarshal(rawRow, &row) != nil || row == nil {
			continue
		}
		var id string
		if json.Unmarshal(row["id"], &id) != nil || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		// An empty version is used only to decode an already filtered listing.
		if version != "" && !deepSeekModelVersionAllowed(row, id, version) {
			continue
		}
		var capabilities map[string]json.RawMessage
		if json.Unmarshal(row["api_capabilities"], &capabilities) != nil {
			continue
		}
		var messages map[string]json.RawMessage
		if json.Unmarshal(capabilities["anthropic_messages"], &messages) != nil || messages == nil {
			continue
		}
		input := deepSeekListedModalities(row, "input_modalities")
		output := deepSeekListedModalities(row, "output_modalities")
		if !slices.Contains(output, "text") {
			continue
		}
		if len(input) == 0 {
			continue
		}
		capacity := func(name string) (int64, bool) {
			raw, ok := row[name]
			if !ok {
				return 0, true
			}
			var n float64
			if json.Unmarshal(raw, &n) != nil || n <= 0 || n > 9007199254740991 || math.Trunc(n) != n {
				return 0, false
			}
			return int64(n), true
		}
		contextWindow, ok := capacity("context_window")
		if !ok {
			continue
		}
		maxOutput, ok := capacity("max_output_tokens")
		if !ok {
			continue
		}
		var name string
		_ = json.Unmarshal(row["name"], &name)
		metadata[id] = UpstreamModelMetadata{ID: id, DisplayName: name, InputModalities: input, ContextWindow: contextWindow, MaxContextWindow: contextWindow, MaxOutputTokens: maxOutput}
		offered = append(offered, row)
	}
	if len(seen) == 0 {
		return nil, nil, fmt.Errorf("deepseek harness model listing contains no model entry")
	}
	filtered, err := json.Marshal(map[string]any{"data": offered})
	return filtered, metadata, err
}

// listing.ts ignores unknown array values while retaining exact known strings
// in listing order; an absent declaration defaults to text.
func deepSeekListedModalities(row map[string]json.RawMessage, field string) []string {
	raw, exists := row[field]
	if !exists {
		return []string{"text"}
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var modalities []string
	for _, entry := range entries {
		var value string
		if json.Unmarshal(entry, &value) == nil && (value == "text" || value == "image") && !slices.Contains(modalities, value) {
			modalities = append(modalities, value)
		}
	}
	return modalities
}

func deepSeekModelVersionAllowed(row map[string]json.RawMessage, id, version string) bool {
	known := id == "deepseek-flash" || id == "deepseek-v4-pro"
	raw, exists := row["client_requirements"]
	if !exists {
		return known
	}
	var requirements map[string]json.RawMessage
	if json.Unmarshal(raw, &requirements) != nil || requirements == nil {
		return false
	}
	raw, exists = requirements["deepseek-harness"]
	if !exists {
		return known
	}
	var own map[string]json.RawMessage
	if json.Unmarshal(raw, &own) != nil || own == nil {
		return false
	}
	raw, exists = own["min_version"]
	if !exists {
		return known
	}
	var minimum string
	if json.Unmarshal(raw, &minimum) != nil {
		return false
	}
	minimum = strings.TrimPrefix(strings.TrimSpace(minimum), "v")
	if !IsStrictSemanticVersion(minimum) || !IsStrictSemanticVersion(version) {
		return false
	}
	return CompareVersions(version, minimum) >= 0
}
