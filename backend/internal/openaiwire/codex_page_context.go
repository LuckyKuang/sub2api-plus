package openaiwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"
)

// NormalizeCodexPageContext preserves the same page-snapshot attribution for
// HTTP, flat response.create frames and nested Responses envelopes.
func NormalizeCodexPageContext(body []byte) ([]byte, bool) {
	if !mayContainCodexCallOutputCandidate(body, "codex_apps_open_page", "codex_apps_selected_page_part_", "browser_info", "goal_objective", "untrusted_context", "untrusted_content", "ide_context", "proposed_plan") || !hasUniqueJSONMembers(body) {
		return body, false
	}
	normalized, changed := normalizeCodexPageContextInput(body)
	var root map[string]json.RawMessage
	if json.Unmarshal(normalized, &root) != nil {
		return body, false
	}
	if response, exists := root["response"]; exists {
		if nested, nestedChanged := normalizeCodexPageContextInput(response); nestedChanged {
			root["response"] = nested
			encoded, err := json.Marshal(root)
			if err != nil {
				return body, false
			}
			return encoded, true
		}
	}
	return normalized, changed
}

var codexSelectedPageName = regexp.MustCompile(`^codex_apps_selected_page_part_([1-9][0-9]*)_of_([1-9][0-9]*)$`)

// Codex injects external context (page snapshots, browser info, goal objective and
// untrusted context) as standalone outputs, without a model tool call. Keep them
// as untrusted user text, including on history replay and mixed bootstrap turns.
func normalizeCodexPageContextInput(body []byte) ([]byte, bool) {
	if !mayContainCodexCallOutputCandidate(body, "codex_apps_open_page", "codex_apps_selected_page_part_", "browser_info", "goal_objective", "untrusted_context", "untrusted_content", "ide_context", "proposed_plan") || !hasUniqueJSONMembers(body) {
		return body, false
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil {
		return body, false
	}
	var input []json.RawMessage
	if json.Unmarshal(request["input"], &input) != nil {
		return body, false
	}
	changed := false
	for i, raw := range input {
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		readString := func(key string) string {
			var value string
			_ = json.Unmarshal(item[key], &value)
			return value
		}
		if readString("type") != "function_call_output" || readString("namespace") != "agent" {
			continue
		}
		if callID, exists := item["call_id"]; exists {
			var value string
			if bytes.Equal(bytes.TrimSpace(callID), []byte("null")) || json.Unmarshal(callID, &value) != nil || strings.TrimSpace(value) != "" {
				continue
			}
		}
		name := readString("name")
		var output *string
		if json.Unmarshal(item["output"], &output) != nil || output == nil || !validCodexUntrustedContext(name, *output) {
			continue
		}
		// Page snapshots keep their own line framing; the other external context
		// wrappers stay inline so the payload is not altered.
		text := "<external_" + name + ">" + *output + "</external_" + name + ">"
		if validCodexPageContext(name, *output) {
			text = "<external_" + name + ">\n" + *output + "\n</external_" + name + ">"
		}
		message := map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]string{{
				"type": "input_text",
				"text": text,
			}},
		}
		encoded, err := json.Marshal(message)
		if err != nil {
			return body, false
		}
		input[i] = encoded
		changed = true
	}
	if !changed {
		return body, false
	}
	encodedInput, err := json.Marshal(input)
	if err != nil {
		return body, false
	}
	request["input"] = encodedInput
	encoded, err := json.Marshal(request)
	if err != nil {
		return body, false
	}
	return encoded, true
}

func validCodexUntrustedContext(name, output string) bool {
	switch name {
	case "browser_info", "goal_objective", "untrusted_context", "ide_context":
		return strings.TrimSpace(output) != ""
	case "untrusted_content":
		return true
	case "proposed_plan":
		return output != ""
	default:
		return validCodexPageContext(name, output)
	}
}

func validCodexPageContext(name, output string) bool {
	if name == "codex_apps_open_page" {
		return validCodexPageSnapshot(output)
	}
	parts := codexSelectedPageName.FindStringSubmatch(name)
	if len(parts) != 3 {
		return false
	}
	part, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	total, err := strconv.Atoi(parts[2])
	if err != nil || part > total {
		return false
	}
	header, snapshot, ok := strings.Cut(output, "\n")
	if !ok || strings.TrimSuffix(header, "\r") != fmt.Sprintf("Companion Page snapshot part %d of %d:", part, total) || strings.TrimSpace(snapshot) == "" {
		return false
	}
	// A multi-part snapshot may split inside a JSON string; preserve each chunk verbatim.
	return total > 1 || validCodexPageSnapshot(snapshot)
}

func validCodexPageSnapshot(text string) bool {
	if !hasUniqueJSONMembers([]byte(text)) {
		return false
	}
	var snapshot map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &snapshot) != nil {
		return false
	}
	id, exists := snapshot["page_id"]
	if !exists {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
		return true
	}
	var value string
	return json.Unmarshal(id, &value) == nil && strings.TrimSpace(value) != ""
}

// MayContainCodexCallOutputCandidate reports whether a request body names any of
// the given Codex tool outputs, without walking the full JSON payload.
func MayContainCodexCallOutputCandidate(body []byte, toolNames ...string) bool {
	return mayContainCodexCallOutputCandidate(body, toolNames...)
}

func mayContainCodexCallOutputCandidate(body []byte, toolNames ...string) bool {
	if !jsonMayEscapePlainASCII(body) && !containsCodexCallOutputLiteral(body, toolNames) {
		return false
	}
	if !rawJSONView(body).Get("input").IsArray() {
		// 嵌套信封（response.create / response）没有顶层 input：交由归一化器
		// 在解包后再做精确判定，避免在此处误杀。
		return true
	}
	return ResponsesInputHasFunctionCallOutputNamed(body, toolNames...)
}

// jsonMayEscapePlainASCII reports whether any ASCII character may be written as
// a JSON \u escape, which defeats plain byte matching.
func jsonMayEscapePlainASCII(body []byte) bool {
	return bytes.Contains(body, []byte(`\u`))
}

func containsCodexCallOutputLiteral(body []byte, toolNames []string) bool {
	if !bytes.Contains(body, []byte("function_call_output")) {
		return false
	}
	for _, name := range toolNames {
		if bytes.Contains(body, []byte(name)) {
			return true
		}
	}
	return false
}

// ResponsesInputHasFunctionCallOutputNamed reports whether the top-level input
// array holds an object item whose type is "function_call_output" and whose
// name starts with one of namePrefixes. The payload is read through a zero-copy
// view: keys and simple string values are compared without allocating.
func ResponsesInputHasFunctionCallOutputNamed(body []byte, namePrefixes ...string) bool {
	if len(body) == 0 {
		return false
	}
	input := rawJSONView(body).Get("input")
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if !item.IsObject() || item.Get("type").String() != "function_call_output" {
			return true
		}
		name := item.Get("name").String()
		for _, prefix := range namePrefixes {
			if strings.HasPrefix(name, prefix) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func rawJSONView(raw []byte) gjson.Result {
	if len(raw) == 0 {
		return gjson.Result{}
	}
	return gjson.Parse(unsafe.String(unsafe.SliceData(raw), len(raw)))
}
