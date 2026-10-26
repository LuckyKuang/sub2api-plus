package service

import (
	"context"
	"crypto/sha256"
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

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/tidwall/gjson"
)

const zhipuStartPlanExtraKey = "zhipu_start_plan_entitlement"
const zhipuStartPlanObservationTTL = 15 * time.Minute

// ZhipuStartPlanEntitlement is an observation of this grant's Start product,
// never a coding/off-peak quota or an invented login refresh credential.
// ModelsKnown distinguishes an absent model contract from an explicit empty
// capability list. Unknown transport/schema results never replace a valid row.
type ZhipuStartPlanEntitlement struct {
	State              string   `json:"state"`
	Reason             string   `json:"reason,omitempty"`
	ModelsKnown        bool     `json:"models_known"`
	Models             []string `json:"models"`
	EffectiveAt        int64    `json:"effective_at,omitempty"`
	ExpiresAt          int64    `json:"expires_at,omitempty"`
	ServerTime         int64    `json:"server_time"`
	ObservedAt         int64    `json:"observed_at"`
	OwnerGeneration    string   `json:"owner_generation,omitempty"`
	IdentityGeneration string   `json:"identity_generation,omitempty"`
}

func isZhipuStartPlan(account *Account) bool {
	return account != nil && account.Platform == PlatformZhipu && account.Type == AccountTypeOAuth &&
		account.GetCredential("plan_kind") == ZhipuPlanStartPlan &&
		(zcode.NormalizeProvider(account.GetCredential("oauth_provider")) == zcode.ProviderZai || zcode.NormalizeProvider(account.GetCredential("oauth_provider")) == zcode.ProviderBigModel)
}

func cloneStartPlanEntitlement(value *ZhipuStartPlanEntitlement) *ZhipuStartPlanEntitlement {
	if value == nil {
		return nil
	}
	copy := *value
	if value.Models != nil {
		copy.Models = append([]string{}, value.Models...)
	}
	return &copy
}

func startPlanIdentityGeneration(ctx context.Context, account *Account) string {
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
	if !ok || identity.Preset != zcode.Preset {
		return ""
	}
	// Device declaration is installation state. Ensure it before hashing the
	// same immutable control identity used for the physical request.
	identity, err := zcode.EnsureDeviceIdentity(ctx, identity)
	if err != nil {
		return ""
	}
	identity = zcode.ControlIdentity(identity)
	payload, err := json.Marshal(struct {
		UserAgent, Originator, Version string
		Headers                        map[string]string
	}{identity.UserAgent, identity.Originator, identity.Version, identity.Headers})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (a *Account) startPlanEntitlement() *ZhipuStartPlanEntitlement {
	if !isZhipuStartPlan(a) || a.Extra == nil {
		return nil
	}
	raw, err := json.Marshal(a.Extra[zhipuStartPlanExtraKey])
	if err != nil {
		return nil
	}
	var state ZhipuStartPlanEntitlement
	if json.Unmarshal(raw, &state) != nil || state.ObservedAt <= 0 || state.OwnerGeneration == "" || state.OwnerGeneration != startPlanCredentialGeneration(a) {
		return nil
	}
	switch state.State {
	case "available", "unavailable", "pending":
	default:
		return nil
	}
	return &state
}

// Control snapshots and persisted JSON credentials use the same canonical map
// representation. Equivalent typed identity declarations must retain the same
// owner digest when compared with their hydrated JSON form. Preserve every
// credential and route dimension, including a precomputed scheduler digest.
func startPlanCredentialGeneration(account *Account) string {
	if account == nil || account.CredentialGeneration != "" {
		return credentialStateGeneration(account)
	}
	credentials, err := cloneAccountJSONMap(account.Credentials)
	if err != nil {
		return ""
	}
	snapshot := *account
	snapshot.Credentials = credentials
	return credentialStateGeneration(&snapshot)
}

// A transient observation failure leaves the established permission untouched.
// An absent observation retains the existing request policy (mapping + upstream
// verdict), while it cannot publish a speculative public model catalog.
func startPlanSupportsModel(account *Account, requestedModel string) bool {
	if !isZhipuStartPlan(account) {
		return true
	}
	state := account.startPlanEntitlement()
	if state == nil {
		return true
	}
	if state.State != "available" {
		return false
	}
	serverNow := state.ServerTime + time.Now().Unix() - state.ObservedAt
	if state.ExpiresAt > 0 && serverNow >= state.ExpiresAt {
		return false
	}
	if !state.ModelsKnown {
		return true
	}
	target := resolveOpenAIAccountUpstreamModelForRequest(account, requestedModel, false)
	for _, id := range state.Models {
		if strings.EqualFold(strings.TrimSpace(target), id) {
			return true
		}
	}
	return false
}

func startPlanCatalogObservation(ctx context.Context, account *Account) *ZhipuStartPlanEntitlement {
	state := account.startPlanEntitlement()
	if state == nil || state.State != "available" || !state.ModelsKnown || time.Now().Unix()-state.ObservedAt > int64(zhipuStartPlanObservationTTL/time.Second) {
		return nil
	}
	if state.IdentityGeneration == "" || state.IdentityGeneration != startPlanIdentityGeneration(ctx, account) {
		return nil
	}
	return state
}

func startPlanPublicModelIDs(ctx context.Context, account *Account) []string {
	state := startPlanCatalogObservation(ctx, account)
	if state == nil {
		return []string{}
	}
	ids := make([]string, 0, len(state.Models))
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		for _, id := range state.Models {
			if startPlanSupportsModel(account, id) {
				ids = append(ids, id)
			}
		}
		return ids
	}
	for alias := range mapping {
		if !strings.Contains(alias, "*") && account.IsModelSupported(alias) {
			ids = append(ids, alias)
		}
	}
	return ids
}

func (s *CNProviderQuotaService) queryZhipuStartPlanUsage(ctx context.Context, account *Account) (*CNProviderQuotaProbeResult, error) {
	state, status, owner, err := fetchZhipuStartPlanEntitlement(ctx, account, s.httpUpstream, s.cfg)
	if err != nil {
		return nil, err
	}
	result := &CNProviderQuotaProbeResult{Provider: PlatformZhipu, Source: "start_plan", StatusCode: status, FetchedAt: state.ObservedAt, StartPlan: state}
	if state.State == "unknown" {
		result.Error = "Start Plan observation unavailable"
		return result, nil
	}
	result.CredentialValid = state.Reason != "auth_failed" && state.Reason != "reauthorization_required"
	result.Persisted, err = persistCNControlExtra(ctx, s.accountRepo, owner, map[string]any{zhipuStartPlanExtraKey: state})
	if err != nil || !result.Persisted {
		result.Error = "Start Plan snapshot owner changed or could not be saved"
		return result, nil
	}
	result.Success = true
	return result, nil
}

func fetchZhipuStartPlanEntitlement(ctx context.Context, account *Account, upstream HTTPUpstream, cfg *config.Config) (*ZhipuStartPlanEntitlement, int, *Account, error) {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return nil, 0, nil, err
	}
	a, err := snapshotCNControlAccount(owner)
	if err != nil {
		return nil, 0, nil, err
	}
	if !isZhipuStartPlan(a) {
		return nil, 0, nil, errors.New("Start Plan credential owner is required")
	}
	now := time.Now().UTC()
	unknown := &ZhipuStartPlanEntitlement{State: "unknown", Reason: "request_failed", ObservedAt: now.Unix(), ServerTime: now.Unix()}
	jwt := strings.TrimSpace(a.GetCredential("zcode_jwt_token"))
	if jwt == "" {
		unavailable := &ZhipuStartPlanEntitlement{State: "unavailable", Reason: "reauthorization_required", ModelsKnown: true, Models: []string{}, ObservedAt: now.Unix(), ServerTime: now.Unix()}
		bindStartPlanObservation(ctx, a, unavailable)
		return unavailable, 0, a, nil
	}
	identityCtx := WithAccountOutboundIdentity(ctx, a)
	identity, ok := outboundidentity.FromContext(identityCtx)
	if !ok || identity.Preset != zcode.Preset || !zcode.IsSupportedVersion(identity.Version) {
		return nil, 0, a, errors.New("Start Plan trusted identity is unavailable")
	}
	target, err := cnValidateProbeURL(cfg, "https://zcode.z.ai/api/v1/zcode-plan/billing/balance?app_version="+url.QueryEscape(identity.Version))
	if err != nil {
		return nil, 0, a, errors.New("Start Plan balance endpoint is not allowed")
	}
	proxy, err := a.StrictProxyURL()
	if err != nil {
		return nil, 0, a, err
	}
	callCtx, cancel := context.WithTimeout(WithHTTPUpstreamRedirectsDisabled(identityCtx), cnQuotaUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, a, errors.New("Start Plan request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/json")
	prepareAccountOutboundRequest(req, a)
	// Account overrides never select the Start token or project scope. The
	// linked Start contract requires its JWT and no project/API-key credential.
	req.Header.Set("Authorization", "Bearer "+jwt)
	for _, name := range []string{"X-Api-Key", "bigmodel-organization", "bigmodel-project", "X-Coding-Plan-Api-Key"} {
		deleteHeaderAllForms(req.Header, name)
	}
	if err = withCNQuotaRequestOwner(req, a); err != nil {
		return nil, 0, a, err
	}
	recordTransmittedRequestCredentialOwner(req, a, jwt)
	if upstream == nil {
		return nil, 0, a, errors.New("Start Plan HTTP client is unavailable")
	}
	resp, err := upstream.Do(req, proxy, a.ID, maxInt(a.Concurrency, 1))
	if err != nil || resp == nil || resp.Body == nil {
		return unknown, 0, a, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		state := &ZhipuStartPlanEntitlement{State: "unavailable", Reason: "auth_failed", ModelsKnown: true, Models: []string{}, ObservedAt: now.Unix(), ServerTime: now.Unix()}
		bindStartPlanObservation(req.Context(), a, state)
		return state, resp.StatusCode, a, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return unknown, resp.StatusCode, a, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes+1))
	if err != nil || len(body) > cnQuotaMaxBodyBytes {
		return unknown, resp.StatusCode, a, nil
	}
	state := parseZhipuStartPlanEntitlement(body, resp.Header.Get("Date"), now)
	bindStartPlanObservation(req.Context(), a, state)
	return state, resp.StatusCode, a, nil
}

func bindStartPlanObservation(ctx context.Context, account *Account, state *ZhipuStartPlanEntitlement) {
	state.OwnerGeneration = startPlanCredentialGeneration(account)
	state.IdentityGeneration = startPlanIdentityGeneration(ctx, account)
}

type startPlanContractPlan struct {
	node            gjson.Result
	id, instance    string
	active, expired bool
	end             int64
}

func parseZhipuStartPlanEntitlement(body []byte, date string, observed time.Time) *ZhipuStartPlanEntitlement {
	state := &ZhipuStartPlanEntitlement{State: "unknown", Reason: "invalid_response", ObservedAt: observed.Unix(), ServerTime: observed.Unix()}
	if !gjson.ValidBytes(body) {
		return state
	}
	root := gjson.ParseBytes(body)
	if !root.IsObject() {
		return state
	}
	if success := root.Get("success"); success.Exists() && success.Type != gjson.True && success.Type != gjson.False {
		return state
	}
	if code := root.Get("code"); code.Exists() && (code.Type != gjson.Number || code.Float() != float64(code.Int())) {
		return state
	}
	if root.Get("success").Type == gjson.False || (root.Get("code").Exists() && root.Get("code").Int() != 0 && root.Get("code").Int() != 200) {
		state.State, state.Reason, state.ModelsKnown, state.Models = "unavailable", "auth_failed", true, []string{}
		return state
	}
	data := root.Get("data")
	plans := data.Get("plans")
	if !data.IsObject() || !plans.IsArray() {
		return state
	}
	if remote := data.Get("server_time"); remote.Exists() {
		v, valid := startPlanEpoch(remote)
		if remote.Type != gjson.Number || !valid {
			return state
		}
		state.ServerTime = v
	}
	if remote, err := http.ParseTime(date); err == nil {
		state.ServerTime = remote.Unix()
	}
	var all []startPlanContractPlan
	var active []startPlanContractPlan
	hadExpired := false
	for _, node := range plans.Array() {
		if !node.IsObject() {
			return state
		}
		for _, field := range []string{"plan_id", "user_plan_id", "name", "status"} {
			if value := node.Get(field); value.Exists() && value.Type != gjson.String {
				return state
			}
		}
		status := strings.ToLower(strings.TrimSpace(node.Get("status").String()))
		if status == "" {
			return state
		}
		plan := startPlanContractPlan{node: node, id: node.Get("plan_id").String(), instance: node.Get("user_plan_id").String()}
		if value := node.Get("ends_at"); value.Exists() && value.Type != gjson.Null {
			end, ok := startPlanEpoch(value)
			if !ok {
				return state
			}
			plan.end = end
			plan.expired = end > 0 && end <= state.ServerTime
		}
		if value := node.Get("starts_at"); value.Exists() && value.Type != gjson.Null {
			if _, ok := startPlanEpoch(value); !ok {
				return state
			}
		}
		name := strings.ToLower(plan.id + " " + node.Get("name").String())
		isStart := strings.Contains(name, "start-plan") || strings.Contains(name, "start plan")
		if plan.id == "" && strings.TrimSpace(node.Get("name").String()) == "" {
			return state
		}
		plan.active = status == "active" && !plan.expired && isStart
		all = append(all, plan)
		if isStart && plan.expired {
			hadExpired = true
		}
		if plan.active {
			active = append(active, plan)
		}
	}
	if len(active) == 0 {
		state.State, state.Reason, state.ModelsKnown, state.Models = "unavailable", "not_entitled", true, []string{}
		if hadExpired {
			state.Reason = "expired"
		}
		return state
	}
	// The earliest active expiration is a conservative observation boundary;
	// crossing it requires a new authoritative balance rather than a guess.
	for _, plan := range active {
		if plan.end > 0 && (state.ExpiresAt == 0 || plan.end < state.ExpiresAt) {
			state.ExpiresAt = plan.end
		}
	}
	state.Models = []string{}
	balances := data.Get("balances")
	if balances.Exists() && !balances.IsArray() {
		return invalidStartPlanObservation(state)
	}
	seen := map[string]bool{}
	for _, balance := range balances.Array() {
		if !balance.IsObject() {
			return invalidStartPlanObservation(state)
		}
		for _, field := range []string{"plan_id", "user_plan_id", "show_name"} {
			if value := balance.Get(field); value.Exists() && value.Type != gjson.String {
				return invalidStartPlanObservation(state)
			}
		}
		instance, id := balance.Get("user_plan_id").String(), balance.Get("plan_id").String()
		matched, owned := false, false
		for _, plan := range all {
			match := id != "" && id == plan.id
			if instance != "" && plan.instance != "" {
				match = instance == plan.instance
			}
			if match {
				matched = true
				if plan.active {
					owned = true
				}
			}
		}
		if matched && !owned {
			continue
		}
		// Explicitly foreign scope is diagnostic information, not proof of
		// this active product's model permission.
		if !matched && (instance != "" || id != "") {
			continue
		}
		for _, field := range []string{"expires_at", "period_end"} {
			if value := balance.Get(field); value.Exists() && value.Type != gjson.Null {
				end, ok := startPlanEpoch(value)
				if !ok {
					return invalidStartPlanObservation(state)
				}
				if end > 0 && end <= state.ServerTime {
					owned = false
					matched = true
					// An explicitly expired model bucket is evidence that its
					// models are no longer authorized, not missing model data.
					if balance.Get("capabilities").IsArray() || strings.TrimSpace(balance.Get("show_name").String()) != "" {
						state.ModelsKnown = true
					}
				}
				if end > state.ServerTime && (state.ExpiresAt == 0 || end < state.ExpiresAt) {
					state.ExpiresAt = end
				}
			}
		}
		if matched && !owned {
			continue
		}
		capabilities := balance.Get("capabilities")
		if capabilities.Exists() && !capabilities.IsArray() {
			return invalidStartPlanObservation(state)
		}
		var candidates []string
		for _, capability := range capabilities.Array() {
			if capability.Type != gjson.String {
				return invalidStartPlanObservation(state)
			}
			value := strings.TrimSpace(capability.String())
			if strings.HasPrefix(strings.ToLower(value), "model:") {
				model := strings.TrimSpace(value[len("model:"):])
				if model == "" {
					return invalidStartPlanObservation(state)
				}
				candidates = append(candidates, model)
			}
		}
		if len(candidates) == 0 {
			if name := strings.TrimSpace(balance.Get("show_name").String()); name != "" {
				candidates = append(candidates, name)
			}
		}
		if len(candidates) > 0 || capabilities.IsArray() {
			state.ModelsKnown = true
		}
		for _, candidate := range candidates {
			id := canonicalStartPlanModelID(candidate)
			key := strings.ToLower(id)
			if id != "" && !seen[key] {
				state.Models = append(state.Models, id)
				seen[key] = true
			}
		}
	}
	future, allFuture := int64(0), true
	for _, plan := range active {
		entitlements := plan.node.Get("entitlements")
		if entitlements.Exists() && !entitlements.IsArray() {
			return invalidStartPlanObservation(state)
		}
		if len(entitlements.Array()) == 0 {
			value, ok := startPlanEpoch(plan.node.Get("starts_at"))
			if !ok || value <= state.ServerTime {
				allFuture = false
			} else if future == 0 || value < future {
				future = value
			}
			continue
		}
		for _, entitlement := range entitlements.Array() {
			if !entitlement.IsObject() {
				return invalidStartPlanObservation(state)
			}
			field := entitlement.Get("effective_at")
			if field.Exists() && field.Type != gjson.Null {
				value, ok := startPlanEpoch(field)
				if !ok {
					return invalidStartPlanObservation(state)
				}
				if value <= state.ServerTime {
					allFuture = false
				} else if future == 0 || value < future {
					future = value
				}
			} else {
				allFuture = false
			}
		}
	}
	state.State, state.Reason = "available", ""
	if len(state.Models) == 0 && allFuture && future > state.ServerTime {
		state.State, state.Reason, state.EffectiveAt, state.ModelsKnown = "pending", "not_effective", future, true
	}
	return state
}

func invalidStartPlanObservation(state *ZhipuStartPlanEntitlement) *ZhipuStartPlanEntitlement {
	return &ZhipuStartPlanEntitlement{State: "unknown", Reason: "invalid_response", ObservedAt: state.ObservedAt, ServerTime: state.ServerTime}
}

func startPlanEpoch(value gjson.Result) (int64, bool) {
	if !value.Exists() || value.Type == gjson.Null {
		return 0, false
	}
	var raw string
	switch value.Type {
	case gjson.String:
		raw = strings.TrimSpace(value.String())
	case gjson.Number:
		raw = value.Raw
	default:
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 253402300799 {
		return 0, false
	}
	return int64(parsed), true
}

func canonicalStartPlanModelID(value string) string {
	value = strings.TrimSpace(value)
	// Fixed official-glm-model-id.ts list at the reviewed official baseline.
	for _, id := range []string{"GLM-5.3", "GLM-5.3-Flash", "GLM-5V-Turbo", "GLM-5.2", "GLM-5.1", "GLM-5.1-Highspeed", "GLM-5", "GLM-5-Turbo", "GLM-4.7", "GLM-4.7-FlashX", "GLM-4.7-Flash", "GLM-4.6", "GLM-4.5-Air", "GLM-4.5", "GLM-4.6V", "GLM-4.6V-Flash", "GLM-4.6V-FlashX", "GLM-4.1V-Thinking-FlashX", "GLM-4.1V-Thinking-Flash", "GLM-4-FlashX-250414", "GLM-4-Flash-250414", "GLM-4V-Flash"} {
		if strings.EqualFold(value, id) {
			return id
		}
	}
	return value
}

func (s *AccountTestService) syncZhipuStartPlanCatalog(ctx context.Context, account *Account) (*UpstreamModelCatalog, error) {
	state, _, owner, err := fetchZhipuStartPlanEntitlement(ctx, account, s.httpUpstream, s.cfg)
	if err != nil {
		return nil, err
	}
	if state.State == "unknown" {
		return nil, newUpstreamModelSyncUpstreamError("Start Plan entitlement is unknown; no model permission was inferred", nil)
	}
	persisted, err := persistCNControlExtra(ctx, s.accountRepo, owner, map[string]any{zhipuStartPlanExtraKey: state})
	if err != nil || !persisted {
		return nil, newUpstreamModelSyncInternalError("Start Plan snapshot owner changed or could not be saved", err)
	}
	catalog := &UpstreamModelCatalog{Models: []string{}, Metadata: map[string]UpstreamModelMetadata{}}
	if state.State == "available" && state.ModelsKnown {
		catalog.Models = append(catalog.Models, state.Models...)
		for _, id := range state.Models {
			catalog.Metadata[id] = UpstreamModelMetadata{ID: id}
		}
	} else {
		catalog.Warnings = []UpstreamModelSyncWarning{{Code: "start_plan_" + state.State, Message: "Start Plan returned no proven available model catalog"}}
	}
	return catalog, nil
}

func (s *AccountTestService) fetchZhipuStartPlanModels(ctx context.Context, account *Account) ([]string, []byte, *Account, error) {
	state, _, owner, err := fetchZhipuStartPlanEntitlement(ctx, account, s.httpUpstream, s.cfg)
	if err != nil {
		return nil, nil, owner, err
	}
	if state.State != "available" || !state.ModelsKnown {
		return nil, nil, owner, newUpstreamModelSyncUpstreamError("Start Plan returned no proven available model catalog", nil)
	}
	entries := make([]map[string]string, 0, len(state.Models))
	for _, id := range state.Models {
		entries = append(entries, map[string]string{"id": id})
	}
	body, err := json.Marshal(map[string]any{"object": "list", "data": entries})
	return append([]string{}, state.Models...), body, owner, err
}
