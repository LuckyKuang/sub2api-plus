package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// A recovery signal is internal to Forward. Stream readers may withhold a
// terminal only when the caller has already proved a safe replacement exists.
type openAIEncryptedRecoverySignal struct{}

func (e *openAIEncryptedRecoverySignal) Error() string { return "invalid_encrypted_content" }

func prepareOpenAIEncryptedRecoveryBody(body []byte) ([]byte, error) {
	if len(collectOpenAIEncryptedContentDigestsRaw(body)) == 0 {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if !trimOpenAIEncryptedReasoningItems(decoded) {
		return nil, nil
	}
	// Never submit an empty reconstruction after removing opaque history.
	switch input := decoded["input"].(type) {
	case []any:
		if !openAIEncryptedRecoveryArrayUsable(input) {
			return nil, nil
		}
	case string:
		if input == "" {
			return nil, nil
		}
	case map[string]any:
		if len(input) == 0 {
			return nil, nil
		}
	default:
		return nil, nil
	}
	return marshalOpenAIUpstreamJSON(decoded)
}

// Reasoning IDs without readable content are not a reconstructed request.
// Other items retain the endpoint's basic-validation contract, including tools.
func openAIEncryptedRecoveryArrayUsable(input []any) bool {
	for _, value := range input {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if item["type"] != "reasoning" {
			if len(item) > 0 {
				return true
			}
			continue
		}
		for _, key := range []string{"summary", "content"} {
			if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
			if parts, ok := item[key].([]any); ok {
				for _, part := range parts {
					if content, ok := part.(map[string]any); ok {
						if text, ok := content["text"].(string); ok && strings.TrimSpace(text) != "" {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func openAIEncryptedStreamRecoverySignal(payload []byte, allowed []bool) error {
	if len(allowed) == 0 || !allowed[0] {
		return nil
	}
	code := gjson.GetBytes(payload, "response.error.code").String()
	if code == "" {
		code = gjson.GetBytes(payload, "error.code").String()
	}
	if code == "" {
		code = gjson.GetBytes(payload, "code").String()
	}
	if code != "invalid_encrypted_content" {
		return nil
	}
	return &openAIEncryptedRecoverySignal{}
}

func (s *OpenAIGatewayService) recordOpenAIEncryptedRecovery(c *gin.Context, account *Account, resp *http.Response, entryBody []byte, passthrough bool) {
	if digests := collectOpenAIEncryptedContentDigestsRaw(entryBody); len(digests) > 0 {
		s.markOpenAIWSInvalidEncryptedContentLineage(getOpenAIGroupIDFromContext(c), s.openAIWSLineageSessionHashFromContext(c, entryBody), digests)
	}
	event := OpsUpstreamErrorEvent{
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: http.StatusBadRequest, UpstreamRequestID: resp.Header.Get("x-request-id"),
		Kind: "retry", Reason: "invalid_encrypted_content", Passthrough: passthrough,
		Message: "Encrypted history rejected; retrying once with preserved readable context",
	}
	snapshotOpsOpenAIStream(c, &event)
	appendOpsUpstreamError(c, event)
}
