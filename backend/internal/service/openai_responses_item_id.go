package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openAIResponsesInputItemIDPrefixes lists every prefix a replayed item of this
// type may legitimately carry. Since Codex 0.170 the client allocates item IDs
// from a per-kind prefix (protocol::ResponseItem::id_prefix), so the legacy
// single-prefix inference is no longer enough: `local_shell_call` IDs now arrive
// as lsh_*, not fc_*. Legacy prefixes stay accepted so history replayed by older
// clients is never rewritten.
func openAIResponsesInputItemIDPrefixes(itemType string) ([]string, bool) {
	switch strings.TrimSpace(itemType) {
	case "message":
		return []string{"msg"}, true
	case "reasoning":
		return []string{"rs"}, true
	case "web_search_call":
		return []string{"ws"}, true
	case "local_shell_call":
		return []string{"lsh", "fc"}, true
	case "custom_tool_call":
		return []string{"ctc"}, true
	case "tool_search_call":
		return []string{"tsc"}, true
	case "custom_tool_call_output":
		// Although the client allocates ctco IDs, OpenAI validates replayed
		// custom call output item IDs against the generic fc namespace, so only
		// fc survives upstream replay. The official ctco shape is accepted
		// separately, because 0.170 emits it and upstream must accept it for the
		// official client to work.
		return []string{"fc"}, true
	default:
		if isCodexToolCallInputType(itemType) {
			// function_call keeps fc_; function_call_output/programmatic call
			// kinds accept fco_ as well because "fc" is a prefix of "fco".
			return []string{openAIResponsesToolCallIDPrefix(itemType), "fco"}, true
		}
		return nil, false
	}
}

func openAIResponsesToolCallIDPrefix(itemType string) string {
	switch strings.TrimSpace(itemType) {
	case "custom_tool_call", "custom_tool_call_output":
		return "ctc"
	case "tool_search_call", "tool_search_output":
		return "tsc"
	default:
		return "fc"
	}
}

// Invalid replayed IDs are removed rather than rewritten because a fabricated
// item ID may point at a different upstream object. The prefix is part of the
// Responses item contract and differs per item kind, so an assigned ID from any
// accepted prefix is preserved byte for byte.
func shouldStripOpenAIResponsesInputItemID(itemType, id string) bool {
	prefixes, constrained := openAIResponsesInputItemIDPrefixes(itemType)
	if !constrained {
		return false
	}
	if id == "" || len(id) > 64 {
		return true
	}
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(id, prefix) {
			return false
		}
	}
	if acceptsOpenAIResponsesOfficialItemIDAlias(itemType, id) {
		return false
	}
	return true
}

// acceptsOpenAIResponsesOfficialItemIDAlias covers kinds where the official
// allocation prefix is not the prefix upstream validates. Only the exact
// model-facing shape is accepted (e.g. ctco_item004): an opaque ID such as
// ctco_bad_output can still belong to another object and stays stripped.
func acceptsOpenAIResponsesOfficialItemIDAlias(itemType, id string) bool {
	switch strings.TrimSpace(itemType) {
	case "custom_tool_call_output":
		return strings.HasPrefix(id, "ctco") && isOpenAIResponsesModelFacingItemID(id)
	default:
		return false
	}
}

// isOpenAIResponsesModelFacingItemID matches the IDs Codex allocates itself:
// `<kind>_<salt><digits>` with zero-padded decimal digits, optionally followed by
// a provider suffix (`_2`). Server IDs are UUIDs or opaque strings instead.
func isOpenAIResponsesModelFacingItemID(id string) bool {
	body := id
	// A provider suffix is a short decimal tail appended to an allocated ID.
	if cut := strings.LastIndexByte(body, '_'); cut > 0 {
		if tail := body[cut+1:]; len(tail) > 0 && len(tail) <= 3 && isOpenAIResponsesDecimalDigits(tail) {
			body = body[:cut]
		}
	}
	digitStart := len(body)
	for digitStart > 0 && body[digitStart-1] >= '0' && body[digitStart-1] <= '9' {
		digitStart--
	}
	if len(body)-digitStart < 3 {
		return false
	}
	stem := strings.TrimSuffix(body[:digitStart], "_")
	if stem == "" {
		return false
	}
	for _, part := range strings.Split(stem, "_") {
		if len(part) < 2 || len(part) > 16 || !isOpenAIResponsesLowerAlpha(part) {
			return false
		}
	}
	return true
}

func isOpenAIResponsesDecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func isOpenAIResponsesLowerAlpha(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 'a' || value[i] > 'z' {
			return false
		}
	}
	return true
}

func shouldStripOpenAIResponsesNonPairCallID(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "message", "reasoning", "image_generation_call":
		return true
	default:
		return false
	}
}

func sanitizeOpenAIResponsesInputItemIDs(body []byte) ([]byte, bool, error) {
	input := parseRawJSONView(body).Get("input")
	if !input.IsArray() {
		return body, false, nil
	}

	type inputItem struct {
		raw         string
		stripID     bool
		stripCallID bool
	}

	items := make([]inputItem, 0)
	input.ForEach(func(_, item gjson.Result) bool {
		parsed := inputItem{raw: item.Raw}
		if item.IsObject() {
			itemType := item.Get("type")
			id := item.Get("id")
			trimmedItemType := strings.TrimSpace(itemType.String())
			parsed.stripCallID = item.Get("call_id").Exists() && shouldStripOpenAIResponsesNonPairCallID(trimmedItemType)
			if id.Type == gjson.String {
				parsed.stripID = shouldStripOpenAIResponsesInputItemID(trimmedItemType, id.String())
			}
		}
		items = append(items, parsed)
		return true
	})
	hasSanitization := false
	for _, item := range items {
		if item.stripID || item.stripCallID {
			hasSanitization = true
			break
		}
	}
	if !hasSanitization {
		return body, false, nil
	}

	rebuiltItems := make([]string, 0, len(items))
	for index, item := range items {
		if !item.stripID && !item.stripCallID {
			rebuiltItems = append(rebuiltItems, item.raw)
			continue
		}
		itemBody := []byte(item.raw)
		if item.stripID {
			var err error
			itemBody, err = sjson.DeleteBytes(itemBody, "id")
			if err != nil {
				return nil, false, fmt.Errorf("delete input.%d.id: %w", index, err)
			}
		}
		if item.stripCallID {
			var err error
			itemBody, err = sjson.DeleteBytes(itemBody, "call_id")
			if err != nil {
				return nil, false, fmt.Errorf("delete input.%d.call_id: %w", index, err)
			}
		}
		rebuiltItems = append(rebuiltItems, string(itemBody))
	}
	return replaceOpenAIRawInput(body, input, rebuiltItems), true, nil
}
