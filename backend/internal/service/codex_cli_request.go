package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
)

type codexCLITaskContextKey struct{}

// WithCodexCLITask is set only by the separate authenticated task route.
func WithCodexCLITask(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexCLITaskContextKey{}, true)
}

func IsCodexCLITask(ctx context.Context) bool {
	value, _ := ctx.Value(codexCLITaskContextKey{}).(bool)
	return value
}

type CodexCLITaskRequest struct {
	Model           string
	Input           string
	Instructions    string
	ReasoningEffort string
	Stream          bool
}

var codexCLITaskModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

// ParseCodexCLITaskRequest defines a deliberately small task API. A request
// cannot silently lose continuation, tools, media or generation parameters.
func ParseCodexCLITaskRequest(body []byte) (CodexCLITaskRequest, error) {
	var request CodexCLITaskRequest
	invalid := errors.New("codex CLI task requests support model, nonempty string input, string instructions, stream and reasoning.effort only")
	fields, objectErr := decodeCodexCLITaskObject(body)
	if objectErr != nil {
		return request, invalid
	}
	for key, value := range fields {
		switch key {
		case "model":
			if json.Unmarshal(value, &request.Model) != nil || !codexCLITaskModelPattern.MatchString(request.Model) {
				return request, invalid
			}
		case "input":
			if json.Unmarshal(value, &request.Input) != nil || strings.TrimSpace(request.Input) == "" || strings.ContainsRune(request.Input, '\x00') {
				return request, invalid
			}
		case "instructions":
			if string(value) == "null" || json.Unmarshal(value, &request.Instructions) != nil || strings.ContainsRune(request.Instructions, '\x00') {
				return request, invalid
			}
		case "stream":
			if string(value) == "null" || json.Unmarshal(value, &request.Stream) != nil {
				return request, invalid
			}
		case "reasoning":
			reasoning, err := decodeCodexCLITaskObject(value)
			if err != nil || len(reasoning) != 1 {
				return request, invalid
			}
			effort, exists := reasoning["effort"]
			if !exists || json.Unmarshal(effort, &request.ReasoningEffort) != nil {
				return request, invalid
			}
			switch request.ReasoningEffort {
			case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			default:
				return request, invalid
			}
		default:
			return request, invalid
		}
	}
	if request.Model == "" || request.Input == "" {
		return request, invalid
	}
	if len(request.Input)+len(request.Instructions) > codexcli.MaxTaskPromptBytes {
		return request, invalid
	}
	return request, nil
}

func decodeCodexCLITaskObject(body []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	invalid := errors.New("task fields must be an object with unique keys")
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, invalid
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, invalid
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, invalid
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, invalid
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, invalid
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, invalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, invalid
	}
	return fields, nil
}

func CodexCLITaskAccountEnabled(account *Account) bool {
	if validateCodexCLITaskAccount(account) != nil {
		return false
	}
	configuration, err := CodexCLIConfigurationForAccount(account)
	return err == nil && configuration.Enabled
}
