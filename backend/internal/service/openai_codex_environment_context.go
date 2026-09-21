package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

// CodexEnvironmentTimezoneExtraKey is the account-level extra key holding the
// target IANA timezone for the model-visible environment_context rewrite. It
// takes precedence over the global openai_codex_environment_timezone setting;
// an empty value means "follow the global default", and no valid value on
// either level disables the rewrite.
const CodexEnvironmentTimezoneExtraKey = "codex_environment_timezone"

// CodexEgressCountryExtraKey is the account-level extra key holding the
// annotated egress country (ISO 3166-1 alpha-2). It takes precedence over the
// egress proxy annotation and the global openai_codex_egress_country setting;
// an empty value means "follow the next source".
const CodexEgressCountryExtraKey = "egress_country"

// Default global values applied on fresh installs and when the setting key is
// absent from the store (never-saved deployments). An explicitly stored empty
// value still means "feature off / not declared".
const (
	DefaultOpenAICodexEnvironmentTimezone = "America/Los_Angeles"
	DefaultOpenAICodexEgressCountry       = "US"
)

var assignedISO3166Alpha2Codes = func() map[string]struct{} {
	const codes = `AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW`
	result := make(map[string]struct{}, 249)
	for _, code := range strings.Fields(codes) {
		result[code] = struct{}{}
	}
	return result
}()

var (
	openAICodexEnvironmentTimezoneTagPattern = regexp.MustCompile(`(?s)<timezone>[^<]*</timezone>`)
	openAICodexEnvironmentDateTagPattern     = regexp.MustCompile(`(?s)<current_date>[^<]*</current_date>`)
)

// NormalizeOpenAICodexEnvironmentTimezone validates and trims the configured
// IANA timezone. An empty value is valid and means "feature off".
func NormalizeOpenAICodexEnvironmentTimezone(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if _, err := time.LoadLocation(trimmed); err != nil {
		return "", fmt.Errorf("must be a valid IANA timezone (e.g. America/New_York): %w", err)
	}
	return trimmed, nil
}

// NormalizeOpenAICodexEgressCountry validates and trims the configured egress
// country code. The value is upper-cased and must be a two-letter ISO 3166-1
// alpha-2 code; an empty value is valid and means "not declared".
func NormalizeOpenAICodexEgressCountry(value string) (string, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		return "", nil
	}
	if _, ok := assignedISO3166Alpha2Codes[trimmed]; !ok {
		return "", fmt.Errorf("must be a two-letter ISO 3166-1 alpha-2 country code (e.g. US)")
	}
	return trimmed, nil
}

// resolveOpenAICodexEgressCountry resolves the annotated egress country for a
// Codex account. Account extra wins, then the egress proxy's annotation, then
// the global setting. Misconfigured values never block traffic: they degrade
// to the next source, and "no valid value on any level" means "not declared".
// Nil ctx is treated as Background because the setting getter is cache-backed.
//
//nolint:unused // retained for the egress country resolution chain; consumed by unit-tagged tests pending request-path wiring
func resolveOpenAICodexEgressCountry(ctx context.Context, account *Account, settingService *SettingService) string {
	if account == nil || !account.IsOpenAI() || !account.UsesOpenAICodexProtocol() {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if country := parseOpenAICodexEgressCountry(account.getExtraString(CodexEgressCountryExtraKey)); country != "" {
		return country
	}
	if account.Proxy != nil {
		if country := parseOpenAICodexEgressCountry(account.Proxy.EgressCountry); country != "" {
			return country
		}
	}
	if settingService == nil {
		return ""
	}
	return parseOpenAICodexEgressCountry(settingService.GetOpenAICodexEgressCountry(ctx))
}

//nolint:unused // retained for the egress country resolution chain; consumed by unit-tagged tests pending request-path wiring
func parseOpenAICodexEgressCountry(value string) string {
	normalized, err := NormalizeOpenAICodexEgressCountry(value)
	if err != nil {
		slog.Debug("openai_codex_egress_country_invalid", "value", strings.TrimSpace(value), "error", err)
		return ""
	}
	return normalized
}

// ValidateEgressCountryExtra validates and normalizes the account-level
// egress country extra key: it must be a two-letter ISO 3166-1 alpha-2 code;
// an empty value is removed from extra (meaning "follow the next source").
func ValidateEgressCountryExtra(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	raw, ok := extra[CodexEgressCountryExtraKey]
	if !ok || raw == nil {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return errors.BadRequest("INVALID_EGRESS_COUNTRY",
			"egress_country must be a two-letter ISO 3166-1 alpha-2 country code")
	}
	normalized, err := NormalizeOpenAICodexEgressCountry(value)
	if err != nil {
		return errors.BadRequest("INVALID_EGRESS_COUNTRY",
			"egress_country "+err.Error())
	}
	if normalized == "" {
		delete(extra, CodexEgressCountryExtraKey)
		return nil
	}
	extra[CodexEgressCountryExtraKey] = normalized
	return nil
}

// resolveOpenAICodexEnvironmentTimezone resolves the target location for the
// environment_context rewrite. Account extra wins, then the egress proxy's
// annotated timezone (the account is bound to that proxy, so the visible time
// follows the actual exit), then the global setting. Misconfigured values
// never block traffic: they degrade to the next source, and "no valid value on
// any level" disables the rewrite. Nil ctx is treated as Background because
// the setting getter is cache-backed.
func resolveOpenAICodexEnvironmentTimezone(ctx context.Context, account *Account, settingService *SettingService) *time.Location {
	if account == nil || !account.IsOpenAI() || !account.UsesOpenAICodexProtocol() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if loc := parseOpenAICodexEnvironmentTimezone(account.getExtraString(CodexEnvironmentTimezoneExtraKey)); loc != nil {
		return loc
	}
	// Egress proxy annotation: the hot path preloads account.Proxy, so this is
	// a field read, not a query. A missing edge or an unannotated/invalid
	// timezone falls through to the global default.
	if account.Proxy != nil {
		if loc := parseOpenAICodexEnvironmentTimezone(account.Proxy.EgressTimezone); loc != nil {
			return loc
		}
	}
	if settingService == nil {
		return nil
	}
	return parseOpenAICodexEnvironmentTimezone(settingService.GetOpenAICodexEnvironmentTimezone(ctx))
}

func parseOpenAICodexEnvironmentTimezone(value string) *time.Location {
	normalized, err := NormalizeOpenAICodexEnvironmentTimezone(value)
	if err != nil {
		slog.Debug("openai_codex_environment_timezone_invalid", "value", strings.TrimSpace(value), "error", err)
		return nil
	}
	if normalized == "" {
		return nil
	}
	loc, err := time.LoadLocation(normalized)
	if err != nil {
		return nil
	}
	return loc
}

// rewriteOpenAICodexEnvironmentContextText rewrites the model-visible timezone
// and current date inside a standalone <environment_context> block. The two
// declarations are always written as one consistent pair in the official
// order (current_date before timezone); a missing tag is injected right
// before the closing tag. Text that is not exactly one environment_context
// block — quoted logs, markdown examples, mixed prose — is returned
// unchanged.
func rewriteOpenAICodexEnvironmentContextText(text string, loc *time.Location) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "<environment_context>") || !strings.HasSuffix(trimmed, "</environment_context>") {
		return text, false
	}
	timezone := loc.String()
	currentDate := time.Now().In(loc).Format("2006-01-02")

	updated := text
	hasTimezone := openAICodexEnvironmentTimezoneTagPattern.MatchString(updated)
	hasDate := openAICodexEnvironmentDateTagPattern.MatchString(updated)
	if !hasTimezone && !hasDate {
		// The client declares no time environment at all: inventing one would
		// not match any official client shape, so the block stays untouched.
		return text, false
	}
	if hasDate {
		updated = openAICodexEnvironmentDateTagPattern.ReplaceAllString(updated, "<current_date>"+currentDate+"</current_date>")
	}
	if hasTimezone {
		updated = openAICodexEnvironmentTimezoneTagPattern.ReplaceAllString(updated, "<timezone>"+timezone+"</timezone>")
	}
	if !hasTimezone || !hasDate {
		// Keep the pair consistent: inject the missing declaration before the
		// closing tag, mirroring the official two-space-indented layout.
		injection := ""
		if !hasDate {
			injection += "  <current_date>" + currentDate + "</current_date>\n"
		}
		if !hasTimezone {
			injection += "  <timezone>" + timezone + "</timezone>\n"
		}
		idx := strings.LastIndex(updated, "</environment_context>")
		prefix := updated[:idx]
		if !strings.HasSuffix(prefix, "\n") {
			injection = "\n" + injection
		}
		updated = prefix + injection + updated[idx:]
	}
	return updated, updated != text
}

// rewriteOpenAICodexEnvironmentContextBytes rewrites environment_context blocks
// inside the Responses request body's input array (user messages only).
// Idempotent: re-running with the same or a different account's timezone
// replaces the pair wholesale, so failover never leaves a stale timezone.
// Any parse or mutation error keeps the original body — the client's own
// timezone/date pair is self-consistent, and the request must never fail
// because of this cosmetic alignment.
func (s *OpenAIGatewayService) rewriteOpenAICodexEnvironmentContextBytes(ctx context.Context, account *Account, body []byte) []byte {
	if len(body) == 0 || account == nil {
		return body
	}
	loc := resolveOpenAICodexEnvironmentTimezone(ctx, account, s.settingService)
	if loc == nil {
		return body
	}
	inputResult := gjson.GetBytes(body, "input")
	if !inputResult.IsArray() {
		return body
	}

	// 收集补丁：gjson 在完整 body 上按全路径查询，Result.Index 即原始字节偏移。
	// 替换字面量用 json.Encoder(SetEscapeHTML=false) 生成，保持 `<`/`>` 原始
	// 字节形态，避免 sjson 默认转义把整个 body 的 XML 标签变成 \u003c 漂移。
	type environmentContextPatch struct {
		start   int
		raw     string
		newText string
	}
	var patches []environmentContextPatch
	appendPatch := func(path, value string) {
		res := gjson.GetBytes(body, path)
		if !res.Exists() || res.Index < 0 {
			return
		}
		newText, changed := rewriteOpenAICodexEnvironmentContextText(value, loc)
		if !changed {
			return
		}
		patches = append(patches, environmentContextPatch{start: res.Index, raw: res.Raw, newText: newText})
	}
	for i, item := range inputResult.Array() {
		if item.Get("role").String() != "user" {
			continue
		}
		content := item.Get("content")
		if content.Type == gjson.String {
			appendPatch(fmt.Sprintf("input.%d.content", i), content.String())
		} else if content.IsArray() {
			for j, part := range content.Array() {
				if part.Get("type").String() != "input_text" {
					continue
				}
				appendPatch(fmt.Sprintf("input.%d.content.%d.text", i, j), part.Get("text").String())
			}
		}
	}
	if len(patches) == 0 {
		return body
	}
	sort.Slice(patches, func(a, b int) bool { return patches[a].start > patches[b].start })

	updated := body
	for _, patch := range patches {
		literal, err := marshalEnvironmentContextJSONString(patch.newText)
		if err != nil {
			slog.Debug("openai_codex_environment_context_rewrite_failed", "error", err)
			return body
		}
		start := patch.start
		end := start + len(patch.raw)
		if end > len(updated) || string(updated[start:end]) != patch.raw {
			slog.Debug("openai_codex_environment_context_rewrite_span_mismatch")
			return body
		}
		next := make([]byte, 0, len(updated)-(end-start)+len(literal))
		next = append(next, updated[:start]...)
		next = append(next, literal...)
		next = append(next, updated[end:]...)
		updated = next
	}
	return updated
}

// marshalEnvironmentContextJSONString encodes text as a JSON string literal
// with HTML escaping disabled, so `<`/`>` keep their raw byte form.
func marshalEnvironmentContextJSONString(text string) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(text); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// rewriteOpenAICodexEnvironmentContextMap is the map form of the rewrite used
// by the WS response.create hot path, where the payload is already decoded.
// It mutates the nested user-message content in place; see the bytes form for
// the full contract.
func (s *OpenAIGatewayService) rewriteOpenAICodexEnvironmentContextMap(ctx context.Context, account *Account, payload map[string]any) map[string]any {
	if payload == nil || account == nil {
		return payload
	}
	loc := resolveOpenAICodexEnvironmentTimezone(ctx, account, s.settingService)
	if loc == nil {
		return payload
	}
	input, ok := payload["input"].([]any)
	if !ok {
		return payload
	}
	for _, item := range input {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if role != "user" {
			continue
		}
		switch content := message["content"].(type) {
		case string:
			if newText, changedOne := rewriteOpenAICodexEnvironmentContextText(content, loc); changedOne {
				message["content"] = newText
			}
		case []any:
			for _, part := range content {
				partMap, ok := part.(map[string]any)
				if !ok {
					continue
				}
				partType, _ := partMap["type"].(string)
				if partType != "input_text" {
					continue
				}
				text, _ := partMap["text"].(string)
				if newText, changedOne := rewriteOpenAICodexEnvironmentContextText(text, loc); changedOne {
					partMap["text"] = newText
				}
			}
		}
	}
	return payload
}
