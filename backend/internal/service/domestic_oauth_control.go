package service

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/tidwall/gjson"
)

// prepareControlRequest keeps native grants on their credential issuer's fixed
// endpoints. It uses the same refresh coordinator and owner snapshot as inference.
func (s *CNOAuthService) prepareControlRequest(req *http.Request, a *Account) error {
	if _, err := a.StrictProxyURL(); err != nil {
		return err
	}
	a, err := snapshotCNControlAccount(a)
	if err != nil {
		return err
	}
	region, err := cnoauth.NormalizeRegion(a.Platform, a.GetCredential("oauth_region"))
	if err != nil {
		return err
	}
	if req.Method != http.MethodGet || req.URL.User != nil || req.URL.Fragment != "" || req.URL.RawQuery != "" {
		return errors.New("native OAuth control operation is not supported")
	}
	allowed := false
	switch a.Platform {
	case PlatformKimi:
		base := cnoauth.ModelBase(a.Platform, region)
		allowed = req.URL.String() == base+"/v1/models" || req.URL.String() == base+"/v1/usages"
	case PlatformDeepseek:
		allowed = req.URL.String() == "https://api.deepseek.com/models" || req.URL.String() == "https://platform.deepseek.com/api/v0/users/get_user_summary"
	}
	if !allowed {
		return errors.New("native OAuth control destination is not supported")
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(withOAuthRefreshRequestPath(req.Context())), a)
	if a.Platform == PlatformKimi {
		refresher := NewCNTokenRefresher(PlatformKimi)
		if !refresher.CanRefresh(a) {
			return errors.New("native OAuth refresh token is missing")
		}
		if refresher.NeedsRefresh(a, 2*time.Minute) {
			fresh, err := s.RefreshAccount(ctx, a, false)
			if err != nil {
				return err
			}
			if !fresh.IsDomesticOAuth() || fresh.Platform != a.Platform || fresh.GetCredential("oauth_region") != a.GetCredential("oauth_region") || fresh.proxyURLOrEmpty() != a.proxyURLOrEmpty() {
				return errors.New("OAuth account changed; retry request")
			}
			a = fresh
		}
	}
	ctx = WithAccountOutboundIdentity(ctx, a)
	ctx, err = withCNControlRequestOwner(ctx, a)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(a.GetCredential("access_token"))
	if token == "" {
		return errors.New("native OAuth access token is missing")
	}
	*req = *req.WithContext(ctx)
	deleteHeaderAllForms(req.Header, "Authorization")
	deleteHeaderAllForms(req.Header, "X-Api-Key")
	deleteHeaderAllForms(req.Header, "X-Dsh-Auth-Token")
	if a.Platform == PlatformDeepseek {
		req.Header.Set("X-Dsh-Auth-Token", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return nil
}

func (s *CNProviderQuotaService) queryNativeOAuthUsage(ctx context.Context, a *Account) (*CNProviderQuotaProbeResult, error) {
	target := cnoauth.ModelBase(a.Platform, a.GetCredential("oauth_region")) + "/v1/usages"
	if a.Platform == PlatformStepFun {
		target = cnoauth.ModelBase(a.Platform, a.GetCredential("oauth_region")) + "/usage"
	}
	target, err := cnValidateProbeURL(s.cfg, target)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	oauth := s.cnOAuthService
	if oauth == nil {
		oauth = NewCNOAuthService(nil, nil, nil)
	}
	queryOwner := a
	send := func(attempt *http.Request) (*http.Response, error) {
		queryOwner = cnQuotaRequestOwner(attempt, cnControlRequestOwner(attempt.Context(), a))
		proxyURL, err := queryOwner.StrictProxyURL()
		if err != nil {
			return nil, err
		}
		return s.httpUpstream.Do(prepareAccountOutboundRequest(attempt, queryOwner), proxyURL, queryOwner.ID, maxInt(queryOwner.Concurrency, 1))
	}
	var resp *http.Response
	if a.Platform == PlatformKimi {
		resp, err = oauth.roundTripNativeOAuthWithPrepare(req, a, oauth.prepareKimiQuotaRequest, send)
	} else {
		if err = oauth.prepareRequest(req, a); err != nil {
			return nil, err
		}
		resp, err = send(req)
	}
	if err != nil || resp == nil || resp.Body == nil {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("native OAuth usage request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	now := time.Now().UTC()
	result := &CNProviderQuotaProbeResult{Provider: a.Platform, Source: "native_oauth", FetchedAt: now.Unix(), StatusCode: resp.StatusCode}
	if err != nil || len(body) > cnQuotaMaxBodyBytes || resp.StatusCode < 200 || resp.StatusCode >= 300 || !gjson.ValidBytes(body) {
		result.Error = "Native usage response unavailable"
		return result, nil
	}
	result.CredentialValid = true
	validSnapshot := false
	if a.Platform == PlatformKimi {
		if gjson.GetBytes(body, "usages").IsObject() {
			result.Tiers = parseKimiUsageTiers(body)
			validSnapshot = len(result.Tiers) > 0
		}
	} else {
		result.Tiers, result.PlanLevel, validSnapshot = parseStepUsageTiers(body)
	}
	if !validSnapshot {
		result.Error = "Native usage contains no recognized quota observation"
		return result, nil
	}
	updates := cnQuotaSnapshotExtraUpdates(a.Platform, result.Tiers, now)
	if cnQuotaRequiresGeneration(queryOwner) {
		cnQuotaBoundExtraUpdates(queryOwner, updates)
	}
	result.Persisted, err = persistCNControlExtra(ctx, s.accountRepo, queryOwner, updates)
	if err != nil || !result.Persisted {
		result.Error = "Native usage owner changed or snapshot persistence unavailable"
		return result, nil
	}
	result.Success = true
	return result, nil
}

// Validate the official schema v1 coding snapshot atomically. One malformed
// sibling invalidates the snapshot; explicitly unlimited windows stay unknown.
func parseStepUsageTiers(body []byte) ([]CNQuotaTier, string, bool) {
	if !gjson.ValidBytes(body) {
		return nil, "", false
	}
	root := gjson.ParseBytes(body)
	version, observed := root.Get("schema_version"), root.Get("observed_at_ms")
	if version.Type != gjson.Number || version.Float() != 1 || root.Get("scope").Type != gjson.String || root.Get("scope").String() != "personal_account" || !stepPositiveInteger(observed, float64(time.Now().UnixMilli()+120000)) || !stepPositiveID(root.Get("account.id")) || root.Get("plan.family").String() != "coding" || root.Get("quota.state").String() != "known" {
		return nil, "", false
	}
	plan, quota := root.Get("plan"), root.Get("quota")
	if (plan.Get("state").String() != "known" && plan.Get("state").String() != "unavailable") || !stepNullableText(plan.Get("name")) || !stepNullableText(quota.Get("reason")) || (quota.Get("allowed").Type != gjson.True && quota.Get("allowed").Type != gjson.False) {
		return nil, "", false
	}
	subscription := plan.Get("subscription")
	if subscription.Raw != "null" && (!subscription.IsObject() || !stepPositiveID(subscription.Get("id")) || !stepNullableTime(subscription.Get("expires_at_ms"))) {
		return nil, "", false
	}
	if plan.Get("state").String() == "unavailable" && (plan.Get("name").Raw != "null" || subscription.Raw != "null") {
		return nil, "", false
	}
	windows, buckets := quota.Get("windows"), quota.Get("credit_buckets")
	if !windows.IsArray() || len(windows.Array()) != 2 || !buckets.IsArray() || len(buckets.Array()) != 0 {
		return nil, "", false
	}
	var tiers []CNQuotaTier
	seen := map[string]bool{}
	for _, w := range windows.Array() {
		window := ""
		switch w.Get("id").String() {
		case "five_hour":
			window = "5h"
		case "weekly":
			window = "weekly"
		default:
			return nil, "", false
		}
		if seen[window] {
			return nil, "", false
		}
		seen[window] = true
		limited, reset := w.Get("limited"), w.Get("resets_at_ms")
		value := w.Get("used_percent")
		if (limited.Type != gjson.True && limited.Type != gjson.False) || !stepNullableTime(reset) {
			return nil, "", false
		}
		if limited.Type == gjson.False {
			if value.Raw != "null" {
				return nil, "", false
			}
			continue
		}
		if value.Type != gjson.Number || math.IsInf(value.Float(), 0) || value.Float() < 0 || value.Float() > 100 {
			return nil, "", false
		}
		tiers = append(tiers, CNQuotaTier{Window: window, UsedPercent: value.Float(), ResetAt: cnNormalizeResetTime(reset.Value())})
	}
	return tiers, root.Get("plan.name").String(), true
}

func stepPositiveInteger(value gjson.Result, maximum float64) bool {
	return value.Type == gjson.Number && value.Float() > 0 && value.Float() <= maximum && math.Trunc(value.Float()) == value.Float()
}

func stepNullableTime(value gjson.Result) bool {
	return value.Raw == "null" || stepPositiveInteger(value, 8640000000000000)
}

var stepPositiveIDPattern = regexp.MustCompile(`^\d{1,19}$`)

func stepPositiveID(value gjson.Result) bool {
	if value.Type != gjson.String || !stepPositiveIDPattern.MatchString(value.String()) {
		return false
	}
	id, err := strconv.ParseInt(value.String(), 10, 64)
	return err == nil && id > 0
}

func stepNullableText(value gjson.Result) bool {
	return value.Raw == "null" || (value.Type == gjson.String && len(utf16.Encode([]rune(value.String()))) <= 1024)
}

var deepSeekWalletDecimal = regexp.MustCompile(`(?i)^-?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$`)

// The account API exposes recharge wallets and bonus wallets independently.
// Currency conversion and the interpretation of bonus expiry belong upstream.
func (s *CNProviderBalanceService) queryDeepSeekOAuthWallet(ctx context.Context, a *Account) (*CNProviderBalanceResult, error) {
	target, err := cnValidateProbeURL(s.cfg, "https://platform.deepseek.com/api/v0/users/get_user_summary")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	oauth := NewCNOAuthService(nil, nil, nil)
	if err = oauth.prepareControlRequest(req, a); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	proxyURL, err := a.StrictProxyURL()
	if err != nil {
		return nil, err
	}
	resp, err := s.httpUpstream.Do(prepareAccountOutboundRequest(req, a), proxyURL, a.ID, maxInt(a.Concurrency, 1))
	if err != nil {
		return nil, errors.New("DeepSeek account wallet request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnBalanceMaxBodyBytes+1))
	now := time.Now().UTC()
	result := &CNProviderBalanceResult{Provider: PlatformDeepseek, FetchedAt: now.Unix(), StatusCode: resp.StatusCode}
	if err != nil || len(body) > cnBalanceMaxBodyBytes || resp.StatusCode != 200 || !gjson.ValidBytes(body) {
		result.Error = "Account wallet unavailable"
		return result, nil
	}
	root := gjson.ParseBytes(body)
	if root.Get("code").Type != gjson.Number || root.Get("code").Int() != 0 || root.Get("data.biz_code").Type != gjson.Number || root.Get("data.biz_code").Int() != 0 {
		result.Error = "Account wallet request rejected"
		return result, nil
	}
	payload := root.Get("data.biz_data")
	parse := func(value gjson.Result) ([]CNProviderBalanceEntry, bool) {
		if !value.IsArray() {
			return nil, false
		}
		var entries []CNProviderBalanceEntry
		for _, w := range value.Array() {
			currency := w.Get("currency").String()
			balance := w.Get("balance")
			amount, ok := cnParseF64(balance.Value())
			if (currency != "CNY" && currency != "USD") || balance.Type != gjson.String || !deepSeekWalletDecimal.MatchString(balance.String()) || !ok {
				return nil, false
			}
			entries = append(entries, CNProviderBalanceEntry{Currency: currency, Balance: amount})
		}
		return entries, true
	}
	normal, ok := parse(payload.Get("normal_wallets"))
	bonus, bonusOK := parse(payload.Get("bonus_wallets"))
	if !ok || !bonusOK || len(normal) == 0 {
		result.Error = "Account wallet contains no valid balance observation"
		return result, nil
	}
	result.Balances = normal
	result.BonusBalances = bonus
	result.Balance = normal[0].Balance
	result.Currency = normal[0].Currency
	spendable := make(map[string]float64, len(normal)+len(bonus))
	for _, w := range append(append([]CNProviderBalanceEntry{}, normal...), bonus...) {
		spendable[w.Currency] += w.Balance
	}
	for _, balance := range spendable {
		if balance > 0 {
			result.Available = true
		}
	}
	updates := cnBalanceExtraUpdates(PlatformDeepseek, result, now)
	updates["deepseek_bonus_balances"] = bonus
	result.Persisted, err = persistCNControlExtra(ctx, s.accountRepo, cnControlRequestOwner(req.Context(), a), updates)
	if err != nil || !result.Persisted {
		result.Error = "Account wallet owner changed or snapshot persistence unavailable"
		return result, nil
	}
	result.owner = cnControlRequestOwner(req.Context(), a)
	result.Success = true
	return result, nil
}
