package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/tidwall/gjson"
)

const miniMaxOAuthQuotaPath = "/v1/api/openplatform/coding_plan/remains"

func miniMaxOAuthQuotaOrigins(account *Account) (matrix, platform string, err error) {
	if account == nil || account.Platform != PlatformMiniMax || !account.IsDomesticOAuth() {
		return "", "", errors.New("MiniMax OAuth quota requires a native account")
	}
	region, err := cnoauth.NormalizeRegion(PlatformMiniMax, account.GetCredential("oauth_region"))
	if err != nil {
		return "", "", errors.New("MiniMax OAuth quota region is invalid")
	}
	if region == "global" {
		return "https://agent.minimax.io", "https://platform.minimax.io", nil
	}
	return "https://agent.minimaxi.com", "https://www.minimaxi.com", nil
}

// Each attempt resolves identity, personal workspace and membership with the
// same OAuth grant. A 401 anywhere restarts the complete chain after the shared
// coordinator refreshes; a new Bearer can never reuse an old workspace group.
func (s *CNProviderQuotaService) roundTripMiniMaxOAuthQuota(ctx context.Context, account *Account) (*http.Response, *Account, error) {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	account = snapshotRequestCredentialAccount(owner)
	_, platform, err := miniMaxOAuthQuotaOrigins(account)
	if err != nil {
		return nil, nil, err
	}
	if s.cnOAuthService == nil {
		return nil, nil, NewProviderCredentialFailure("native_oauth_service_unavailable", nil)
	}
	if _, err := requestCredentialOwnerProxyURL(ctx, account); err != nil {
		return nil, nil, NewProviderCredentialFailure("native_oauth_proxy_unavailable", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, platform+miniMaxOAuthQuotaPath, nil)
	if err != nil {
		return nil, nil, err
	}
	prepare := func(attempt *http.Request, owner *Account) error {
		if !sameCNOAuthOwner(account, owner) || !owner.IsActive() {
			return NewProviderCredentialFailure("native_oauth_owner_changed", nil)
		}
		_, expected, err := miniMaxOAuthQuotaOrigins(owner)
		if err != nil || attempt.Method != http.MethodGet || attempt.URL.String() != expected+miniMaxOAuthQuotaPath {
			return NewProviderCredentialFailure("native_oauth_destination_rejected", nil)
		}
		attemptCtx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(withOAuthRefreshRequestPath(attempt.Context())), owner)
		*attempt = *attempt.WithContext(attemptCtx)
		refresher := &CNTokenRefresher{platform: PlatformMiniMax}
		if !refresher.CanRefresh(owner) {
			return NewAccountCredentialFailure("native_oauth_refresh_token_missing", nil)
		}
		if refresher.NeedsRefresh(owner, 2*time.Minute) {
			owner, err = s.cnOAuthService.RefreshAccount(attemptCtx, owner, false)
			if err != nil {
				return err
			}
		}
		token := strings.TrimSpace(owner.GetCredential("access_token"))
		if token == "" {
			return NewAccountCredentialFailure("native_oauth_access_token_missing", nil)
		}
		attempt.Header.Set("Authorization", "Bearer "+token)
		recordTransmittedRequestCredentialOwner(attempt, owner, token)
		return withCNQuotaRequestOwner(attempt, owner)
	}
	var queryOwner *Account
	resp, err := s.cnOAuthService.roundTripNativeOAuthWithPrepare(req, account, prepare, func(attempt *http.Request) (*http.Response, error) {
		queryOwner = cnQuotaRequestOwner(attempt, account)
		return s.miniMaxOAuthQuotaAttempt(attempt.Context(), queryOwner)
	})
	return resp, queryOwner, err
}

func (s *CNProviderQuotaService) miniMaxOAuthQuotaAttempt(ctx context.Context, owner *Account) (*http.Response, error) {
	matrix, platform, err := miniMaxOAuthQuotaOrigins(owner)
	if err != nil {
		return nil, err
	}
	identity, response, err := s.miniMaxOAuthQuotaJSON(ctx, owner, matrix, http.MethodGet, "/v1/api/user/info", nil, "")
	if err != nil || response != nil {
		return response, err
	}
	user := identity.Get("data.userInfo")
	for _, path := range []string{"data.user_info", "userInfo", "user_info"} {
		if !user.IsObject() {
			user = identity.Get(path)
		}
	}
	userID := miniMaxQuotaString(user.Get("realUserID"))
	if userID == "" {
		userID = miniMaxQuotaString(user.Get("real_user_id"))
	}
	if userID == "" {
		return nil, errors.New("MiniMax quota identity response has no account ID")
	}
	extra, response, err := s.miniMaxOAuthQuotaJSON(ctx, owner, matrix, http.MethodPost, "/matrix/api/v1/user/get_user_extra_info", []byte("{}"), userID)
	if err != nil || response != nil {
		return response, err
	}
	workspaces := extra.Get("workspaces")
	if !workspaces.IsArray() {
		workspaces = extra.Get("data.workspaces")
	}
	if !workspaces.IsArray() {
		return nil, errors.New("MiniMax quota workspace response is invalid")
	}
	var personal gjson.Result
	workspaces.ForEach(func(_, workspace gjson.Result) bool {
		kind := workspace.Get("workspace_type")
		if kind.Type == gjson.Number && kind.Float() == 0 && miniMaxQuotaWorkspaceID(workspace.Get("workspace_id")) != nil {
			personal = workspace
			return false
		}
		return true
	})
	if !personal.IsObject() {
		return nil, errors.New("MiniMax quota has no personal workspace")
	}
	group := miniMaxQuotaString(personal.Get("op_group_id"))
	if group == "" || strings.ContainsAny(group, "\r\n") {
		return nil, errors.New("MiniMax quota personal workspace has no billing group")
	}
	payload, err := json.Marshal(map[string]any{"workspace_id": miniMaxQuotaWorkspaceID(personal.Get("workspace_id"))})
	if err != nil {
		return nil, err
	}
	membership, response, err := s.miniMaxOAuthQuotaJSON(ctx, owner, matrix, http.MethodPost, "/matrix/api/v1/commerce/get_membership_info", payload, userID)
	if err != nil || response != nil {
		return response, err
	}
	hasPersonal := personal.Get("has_token_plan")
	hasScoped := membership.Get("has_token_plan")
	if !hasScoped.Exists() {
		hasScoped = membership.Get("data.has_token_plan")
	}
	if hasPersonal.Type != gjson.True && hasScoped.Type != gjson.True && (hasPersonal.Type == gjson.False || hasScoped.Type == gjson.False) {
		return nil, errors.New("MiniMax account has no Token Plan subscription")
	}
	req, err := s.newMiniMaxOAuthQuotaRequest(ctx, owner, http.MethodGet, platform+miniMaxOAuthQuotaPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Group-Id", group)
	proxyURL, err := requestCredentialOwnerProxyURL(req.Context(), owner)
	if err != nil {
		return nil, err
	}
	return s.httpUpstream.Do(req, proxyURL, owner.ID, maxInt(owner.Concurrency, 1))
}

// Matrix attribution is public first-party protocol, independent of credential
// authentication. Apply the selected trusted identity before signing and retain
// it in context so send-time rendering cannot replace its declarations.
func (s *CNProviderQuotaService) miniMaxOAuthQuotaJSON(ctx context.Context, owner *Account, origin, method, path string, payload []byte, userID string) (gjson.Result, *http.Response, error) {
	now := time.Now()
	identity, _ := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, owner))
	language := "zh"
	if owner.GetCredential("oauth_region") == "global" {
		language = "en"
	}
	query := [][2]string{
		{"device_platform", "mcode"}, {"biz_id", "3"}, {"app_id", "3001"}, {"version_code", "22201"},
		{"unix", strconv.FormatInt(now.UnixMilli(), 10)}, {"timezone_offset", strconv.Itoa(identity.TimezoneOffset(now))},
		{"sys_language", language}, {"lang", language}, {"device_id", "0"}, {"os_name", "linux"},
		{"browser_name", "mcode"}, {"user_id", userID}, {"client", "mcode"},
	}
	if userID == "" {
		query[11][1] = "0"
	}
	var encoded []string
	for _, field := range query {
		encoded = append(encoded, miniMaxQuotaSearchEscape(field[0])+"="+miniMaxQuotaSearchEscape(field[1]))
	}
	req, err := s.newMiniMaxOAuthQuotaRequest(ctx, owner, method, origin+path+"?"+strings.Join(encoded, "&"), payload)
	if err != nil {
		return gjson.Result{}, nil, err
	}
	requestTime := strconv.FormatInt(now.UnixMilli(), 10)
	second := strconv.FormatInt(now.Unix(), 10)
	body := "{}"
	if len(payload) > 0 {
		body = string(payload)
	}
	pathWithSearch := req.URL.EscapedPath() + "?" + req.URL.RawQuery
	req.Header.Set("yy", miniMaxQuotaMD5(miniMaxQuotaEncodeURIComponent(pathWithSearch)+"_"+body+miniMaxQuotaMD5(requestTime)+"ooui"))
	req.Header.Set("x-timestamp", second)
	signature := second + "I*7Cf%WZ#S&%1RlZJ&C2"
	if method == http.MethodPost {
		signature += body
	}
	req.Header.Set("x-signature", miniMaxQuotaMD5(signature))
	req.Header.Set("Content-Type", "application/json")
	proxyURL, err := requestCredentialOwnerProxyURL(req.Context(), owner)
	if err != nil {
		return gjson.Result{}, nil, err
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, owner.ID, maxInt(owner.Concurrency, 1))
	if err != nil {
		return gjson.Result{}, nil, err
	}
	if resp == nil || resp.Body == nil {
		return gjson.Result{}, nil, errors.New("MiniMax quota control response is missing")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gjson.Result{}, resp, nil
	}
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	_ = resp.Body.Close()
	if readErr != nil || len(bodyBytes) > cnQuotaMaxBodyBytes || !gjson.ValidBytes(bodyBytes) {
		return gjson.Result{}, nil, errors.New("MiniMax quota control response is invalid")
	}
	value := gjson.ParseBytes(bodyBytes)
	if !value.IsObject() {
		return gjson.Result{}, nil, errors.New("MiniMax quota control response is invalid")
	}
	for _, codePath := range []string{"statusInfo.code", "base_resp.status_code"} {
		code := value.Get(codePath)
		if !code.Exists() {
			continue
		}
		if code.Type != gjson.Number || code.Float() != 0 {
			if codePath == "statusInfo.code" && code.Type == gjson.Number && code.Float() == 1000048 {
				resp.StatusCode = http.StatusUnauthorized
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return gjson.Result{}, resp, nil
			}
			return gjson.Result{}, nil, errors.New("MiniMax quota control request was rejected")
		}
	}
	return value, nil, nil
}

func (s *CNProviderQuotaService) newMiniMaxOAuthQuotaRequest(ctx context.Context, owner *Account, method, target string, payload []byte) (*http.Request, error) {
	validated, err := cnValidateProbeURL(s.cfg, target)
	if err != nil {
		return nil, err
	}
	ctx = WithHTTPUpstreamRedirectsDisabled(WithAccountOutboundIdentity(ctx, owner))
	identity, _ := outboundidentity.FromContext(ctx)
	if identity.Preset == minimax.Preset {
		identity = minimax.ControlIdentityForURL(identity, validated)
	}
	ctx = outboundidentity.WithIdentity(ctx, identity)
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, validated, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(owner.GetCredential("access_token")))
	outboundidentity.ApplyContext(req)
	return req, nil
}

func miniMaxQuotaString(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func miniMaxQuotaWorkspaceID(value gjson.Result) any {
	if value.Type == gjson.String {
		if id := miniMaxQuotaString(value); id != "" {
			return id
		}
	}
	if value.Type == gjson.Number && value.Float() >= 0 && !math.IsInf(value.Float(), 0) && math.Trunc(value.Float()) == value.Float() {
		return json.Number(value.Raw)
	}
	return nil
}

func miniMaxQuotaMD5(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}

// URLSearchParams uses application/x-www-form-urlencoded, whose unescaped
// set differs from Go's QueryEscape for '*' and '~'. Sign the actual wire query.
func miniMaxQuotaSearchEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value), "~", "%7E"), "%2A", "*")
}

func miniMaxQuotaEncodeURIComponent(value string) string {
	const digits = "0123456789ABCDEF"
	encoded := make([]byte, 0, len(value))
	for _, b := range []byte(value) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-_.!~*'()", rune(b)) {
			encoded = append(encoded, b)
		} else {
			encoded = append(encoded, '%', digits[b>>4], digits[b&15])
		}
	}
	return string(encoded)
}

// OAuth quota reports remaining percentages, not consumed counts. The legacy
// *_usage_count fields also mean remaining amounts, despite their names.
func parseMiniMaxOAuthUsageTiers(body []byte) []CNQuotaTier {
	if !gjson.ValidBytes(body) {
		return nil
	}
	models := gjson.GetBytes(body, "model_remains")
	if !models.IsArray() {
		return nil
	}
	// MiniMax Code uses the first object as the primary Token Plan. Model
	// names are not a stable discriminator (the primary may be a model name).
	var primary gjson.Result
	models.ForEach(func(_, model gjson.Result) bool {
		if model.IsObject() {
			primary = model
			return false
		}
		return true
	})
	if !primary.IsObject() {
		return nil
	}
	var tiers []CNQuotaTier
	for _, spec := range []struct{ prefix, window, reset string }{
		{"current_interval", "5h", "end_time"},
		{"current_weekly", "weekly", "weekly_end_time"},
	} {
		status := primary.Get(spec.prefix + "_status")
		if status.Exists() && (status.Type != gjson.Number || status.Float() != math.Trunc(status.Float())) {
			continue
		}
		if status.Type == gjson.Number && status.Int() == 3 {
			tiers = append(tiers, CNQuotaTier{Window: spec.window, Unlimited: true})
			continue
		}
		remaining, ok := miniMaxQuotaFiniteNumber(primary.Get(spec.prefix + "_remaining_percent"))
		if !ok {
			count, countOK := miniMaxQuotaFiniteNumber(primary.Get(spec.prefix + "_usage_count"))
			total, totalOK := miniMaxQuotaFiniteNumber(primary.Get(spec.prefix + "_total_count"))
			if !countOK || !totalOK || count < 0 || total <= 0 {
				continue
			}
			remaining = count / total * 100
		}
		if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 {
			continue
		}
		remaining = math.Min(100, remaining)
		tiers = append(tiers, CNQuotaTier{Window: spec.window, UsedPercent: 100 - remaining, ResetAt: minimaxResetTime(primary.Get(spec.reset))})
	}
	return tiers
}

func miniMaxQuotaFiniteNumber(value gjson.Result) (float64, bool) {
	if value.Type != gjson.Number {
		return 0, false
	}
	number := value.Float()
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

// Native MiniMax can automatically bill Credits after Token Plan exhaustion.
// A bound, finite, unreset exhausted window excludes this owner even when the
// optional early-pause threshold is disabled. No account/credential mutation or
// network call occurs during selection; the reset makes it eligible again.
func (a *Account) miniMaxOAuthPlanExhausted(now time.Time) bool {
	return a != nil && a.Platform == PlatformMiniMax && a.IsDomesticOAuth() && a.IsCodingPlan() &&
		pickLatestResetSchedulingCandidate(cnProviderThresholdCandidates(a, PlatformMiniMax), 100, now) != nil
}
