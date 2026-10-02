package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	SystemOnePath  = "/v1/systemone"
	JevLatestModel = "jev-latest"
)

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type SystemOneResponse struct {
	Body  []byte
	Model string
	Usage Usage
}

func NewSystemOneRequest(ctx context.Context, baseURL, key string, body []byte) (*http.Request, error) {
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), SystemOnePath)
	if err != nil {
		return nil, errors.New("typesafe invalid endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("typesafe invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// MaxSystemOneResponseBytes bounds a buffered System One response body.
const MaxSystemOneResponseBytes = 4 << 20

var ErrSystemOneResponseTooLarge = errors.New("typesafe response exceeds size limit")

func DecodeSystemOneResponse(r io.Reader) (*SystemOneResponse, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxSystemOneResponseBytes+1))
	if err != nil {
		return nil, errors.New("typesafe invalid response")
	}
	// A truncated body would otherwise surface as a misleading "invalid JSON".
	if len(body) > MaxSystemOneResponseBytes {
		return nil, ErrSystemOneResponseTooLarge
	}
	if !json.Valid(body) {
		return nil, errors.New("typesafe invalid response")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, errors.New("typesafe invalid response")
	}
	// The upstream already answered (and charged); an unexpected model or usage
	// shape must not discard the answer, so both are decoded leniently.
	var model string
	_ = json.Unmarshal(envelope["model"], &model)
	var usage map[string]json.RawMessage
	_ = json.Unmarshal(envelope["usage"], &usage)
	return &SystemOneResponse{
		Body:  body,
		Model: model,
		Usage: Usage{
			InputTokens:  systemOneTokenCount(usage["input_tokens"]),
			OutputTokens: systemOneTokenCount(usage["output_tokens"]),
		},
	}, nil
}

// maxSystemOneTokenCount bounds a reported token count before int conversion.
const maxSystemOneTokenCount = 1 << 40

// systemOneTokenCount accepts integer, float, or numeric-string token counts.
func systemOneTokenCount(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0
		}
		raw = json.RawMessage(strings.TrimSpace(text))
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return 0
	}
	value, err := number.Float64()
	if err != nil || math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value > maxSystemOneTokenCount {
		value = maxSystemOneTokenCount
	}
	return int(math.Round(value))
}
