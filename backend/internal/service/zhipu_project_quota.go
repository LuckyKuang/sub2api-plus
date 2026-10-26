package service

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/tidwall/gjson"
)

// OAuth monitor requests use the selected project's current PAT, not its public
// Key ID or a historical copied secret. Monitor authorization has no Bearer prefix.
func (s *CNProviderQuotaService) queryZhipuProjectUsage(ctx context.Context, account *Account) (*CNProviderQuotaProbeResult, error) {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	a, err := snapshotCNControlAccount(owner)
	if err != nil {
		return nil, err
	}
	team := a.GetCredential("plan_kind") == ZhipuPlanTeamCodingPlan
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		switch a.GetCredential("zhipu_project_kind") {
		case "personal":
		case "team":
			team = true
		default:
			return nil, errors.New("off-peak project scope is unknown; relink the account")
		}
	}
	if team && (strings.TrimSpace(a.GetCredential("zhipu_organization")) == "" || strings.TrimSpace(a.GetCredential("zhipu_project")) == "") {
		return nil, errors.New("team project scope is missing; relink the account")
	}
	var target string
	switch zcode.NormalizeProvider(a.GetCredential("oauth_provider")) {
	case "bigmodel":
		target = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
	case "zai":
		target = "https://api.z.ai/api/monitor/usage/quota/limit"
	default:
		return nil, errors.New("project quota provider is invalid")
	}
	if team {
		target += "?type=2"
	}
	target, err = cnValidateProbeURL(s.cfg, target)
	if err != nil {
		return nil, err
	}
	// Reject policy/scope errors before issuing or sending any project credential.
	tokenCtx := WithAccountOutboundIdentity(ctx, a)
	token, err := s.projectTokens.Token(tokenCtx, a)
	if err != nil {
		return nil, errors.New("project quota credential is unavailable")
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(WithAccountOutboundIdentity(tokenCtx, a)), cnQuotaUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("project quota request could not be built")
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")
	if team {
		req.Header.Set("bigmodel-organization", a.GetCredential("zhipu_organization"))
		req.Header.Set("bigmodel-project", a.GetCredential("zhipu_project"))
	}
	prepareAccountOutboundRequest(req, a)
	if identity, ok := outboundidentity.FromContext(req.Context()); ok {
		*req = *req.WithContext(outboundidentity.WithIdentity(req.Context(), zcode.ControlIdentity(identity)))
		outboundidentity.ApplyContext(req)
	}
	proxyURL, err := a.StrictProxyURL()
	if err != nil {
		return nil, err
	}
	if err = withCNQuotaRequestOwner(req, a); err != nil {
		return nil, err
	}
	recordTransmittedRequestCredentialOwner(req, a, a.GetOpenAIProtocolAPIKey())
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		used := req.Header.Get("Authorization")
		resp, err = s.httpUpstream.Do(req, proxyURL, a.ID, maxInt(a.Concurrency, 1))
		if err != nil || resp == nil || resp.Body == nil {
			return nil, errors.New("project quota request failed")
		}
		if resp.StatusCode != http.StatusUnauthorized {
			break
		}
		s.projectTokens.reject(tokenCtx, a, used)
		if attempt == 1 {
			break
		}
		token, err = s.projectTokens.Token(req.Context(), a)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		req.Header.Set("Authorization", token)
	}
	defer func() { _ = resp.Body.Close() }()
	a = cnQuotaRequestOwner(req, a)
	now := time.Now().UTC()
	result := &CNProviderQuotaProbeResult{Provider: PlatformZhipu, Source: "coding_plan", StatusCode: resp.StatusCode, FetchedAt: now.Unix()}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Error = "Project quota authentication failed"
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = "Project quota response unavailable"
		return result, nil
	}
	result.CredentialValid = true
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	if err != nil || len(body) > cnQuotaMaxBodyBytes {
		result.Error = "Project quota response unavailable"
		return result, nil
	}
	tiers, level, ok := parseZhipuProjectQuota(body)
	if !ok {
		result.Error = "Project quota contains no valid observation"
		return result, nil
	}
	result.Tiers, result.PlanLevel = tiers, level
	updates := map[string]any{
		cnExtraKey(PlatformZhipu, cnExtraSuffix5hUsed):       nil,
		cnExtraKey(PlatformZhipu, cnExtraSuffix5hReset):      nil,
		cnExtraKey(PlatformZhipu, cnExtraSuffixWeeklyUsed):   nil,
		cnExtraKey(PlatformZhipu, cnExtraSuffixWeeklyReset):  nil,
		cnExtraKey(PlatformZhipu, cnExtraSuffixMonthlyUsed):  nil,
		cnExtraKey(PlatformZhipu, cnExtraSuffixMonthlyReset): nil,
	}
	for key, value := range cnQuotaExtraUpdates(PlatformZhipu, tiers, now) {
		updates[key] = value
	}
	cnQuotaBoundExtraUpdates(a, updates)
	result.Persisted, err = persistCNControlExtra(ctx, s.accountRepo, a, updates)
	if err != nil || !result.Persisted {
		result.Error = "Project quota snapshot owner changed or could not be saved"
		return result, nil
	}
	result.Success = true
	return result, nil
}

// Only explicitly observed windows may update scheduling thresholds. Incomplete
// or unrecognized data must never become an invented zero or guessed window.
func parseZhipuProjectQuota(body []byte) ([]CNQuotaTier, string, bool) {
	if !gjson.ValidBytes(body) {
		return nil, "", false
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return nil, "", false
	}
	if success := root.Get("success"); success.Exists() && success.Type != gjson.Null && success.Type != gjson.True {
		return nil, "", false
	}
	if code := root.Get("code"); code.Exists() && code.Type != gjson.Null {
		if code.Type != gjson.Number || (code.Float() != 0 && code.Float() != 200) {
			return nil, "", false
		}
	}
	data := root.Get("data")
	limits := data.Get("limits")
	if !data.IsObject() || !limits.IsArray() {
		return nil, "", false
	}
	limitType := "CREDIT_LIMIT"
	for _, item := range limits.Array() {
		if item.Get("type").String() == "TOKENS_LIMIT" {
			limitType = "TOKENS_LIMIT"
			break
		}
	}
	byWindow := make(map[string]CNQuotaTier, 2)
	for _, item := range limits.Array() {
		if item.Get("type").String() != limitType {
			continue
		}
		percentage := item.Get("percentage")
		if percentage.Type != gjson.Number || math.IsNaN(percentage.Float()) || math.IsInf(percentage.Float(), 0) || percentage.Float() < 0 {
			return nil, "", false
		}
		unit := item.Get("unit")
		window := ""
		if unit.Type == gjson.Number {
			switch unit.Float() {
			case 3:
				window = "5h"
			case 6:
				window = "weekly"
			}
		}
		if window == "" {
			return nil, "", false
		}
		if _, exists := byWindow[window]; exists {
			return nil, "", false
		}
		tier := CNQuotaTier{Window: window, UsedPercent: percentage.Float()}
		if reset := item.Get("nextResetTime"); reset.Exists() && reset.Type != gjson.Null {
			millis, err := strconv.ParseInt(reset.Raw, 10, 64)
			if reset.Type != gjson.Number || err != nil || millis <= 0 {
				return nil, "", false
			}
			tier.ResetAt = cnMillisToRFC3339(millis)
		}
		byWindow[window] = tier
	}
	if len(byWindow) == 0 {
		return nil, "", false
	}
	var tiers []CNQuotaTier
	for _, window := range []string{"5h", "weekly"} {
		if tier, exists := byWindow[window]; exists {
			tiers = append(tiers, tier)
		}
	}
	return tiers, strings.TrimSpace(data.Get("level").String()), true
}
