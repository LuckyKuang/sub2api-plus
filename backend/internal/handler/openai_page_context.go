package handler

import "github.com/LuckyKuang/sub2api-plus/internal/openaiwire"

func normalizeCodexPageContext(body []byte) ([]byte, bool) {
	return openaiwire.NormalizeCodexPageContext(body)
}
