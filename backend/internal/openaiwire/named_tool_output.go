package openaiwire

import (
	"strings"

	"github.com/tidwall/gjson"
)

// IsNamedUnpairedFunctionCallOutput recognizes the official Responses context
// output shape, which names its source instead of replying to a tool call.
// Source: openai/codex 6d8f311b00e823052622eac2b463919ad86a666d,
// context-fragments/src/additional_context.rs and protocol/src/models.rs's
// named_unpaired_function_call_output_round_trips_without_call_id contract.
// A malformed or blank call_id remains an ordinary invalid paired output.
func IsNamedUnpairedFunctionCallOutput(item map[string]any) bool {
	typ, _ := item["type"].(string)
	name, _ := item["name"].(string)
	namespace, namespaceExists := item["namespace"]
	namespaceString, namespaceIsString := namespace.(string)
	validNamespace := !namespaceExists || namespace == nil ||
		(namespaceIsString && validOutputName(namespaceString))
	_, textOutput := item["output"].(string)
	_, arrayOutput := item["output"].([]any)
	return isNamedUnpairedOutput(typ, item["call_id"] == nil, name, validNamespace, textOutput || arrayOutput)
}

// IsNamedUnpairedFunctionCallOutputJSON provides the same classification without
// decoding large output bodies. Duplicate discriminator fields are never used
// to bypass pairing validation, regardless of a JSON parser's key preference.
func IsNamedUnpairedFunctionCallOutputJSON(item gjson.Result) bool {
	if !item.IsObject() {
		return false
	}
	var typ, callID, name, namespace, output gjson.Result
	seen, duplicate := uint8(0), false
	item.ForEach(func(key, value gjson.Result) bool {
		var target *gjson.Result
		var bit uint8
		switch key.Str {
		case "type":
			target, bit = &typ, 1
		case "call_id":
			target, bit = &callID, 2
		case "name":
			target, bit = &name, 4
		case "namespace":
			target, bit = &namespace, 8
		case "output":
			target, bit = &output, 16
		default:
			return true
		}
		if seen&bit != 0 {
			duplicate = true
			return false
		}
		seen |= bit
		*target = value
		return true
	})
	if duplicate || typ.Type != gjson.String || name.Type != gjson.String {
		return false
	}
	validNamespace := namespace.Type == gjson.Null ||
		(namespace.Type == gjson.String && validOutputName(namespace.Str))
	return isNamedUnpairedOutput(typ.Str, callID.Type == gjson.Null, name.Str,
		validNamespace, output.Type == gjson.String || output.IsArray())
}

func isNamedUnpairedOutput(typ string, callIDAbsent bool, name string, validNamespace, validOutput bool) bool {
	return typ == "function_call_output" && callIDAbsent && validOutputName(name) && validNamespace && validOutput
}

func validOutputName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name
}
