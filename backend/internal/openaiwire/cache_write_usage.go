package openaiwire

import "github.com/tidwall/gjson"

// CacheWriteTokenDetails selects the first reported duration breakdown. An
// invalid canonical breakdown must not fall through to a lower-priority alias.
func CacheWriteTokenDetails(usage gjson.Result) gjson.Result {
	for _, path := range []string{"input_tokens_details", "prompt_tokens_details"} {
		if details := usage.Get(path + ".cache_write_tokens_details"); details.Exists() {
			return details
		}
	}
	return gjson.Result{}
}

// CacheWriteTTLTokens classifies only a complete, nonnegative integer split
// equal to the reported aggregate. Unknown/invalid details keep legacy billing;
// request TTLs cannot prove cache writes. See docs/CHANNEL_PRICING.md.
func CacheWriteTTLTokens(details gjson.Result, aggregate int) (int, int) {
	short, long := details.Get("ttl_30m_tokens"), details.Get("ttl_12h_tokens")
	if short.Type != gjson.Number || long.Type != gjson.Number {
		return 0, 0
	}
	a, b := short.Int(), long.Int()
	if a < 0 || b < 0 || float64(a) != short.Float() || float64(b) != long.Float() ||
		a > int64(aggregate) || b != int64(aggregate)-a {
		return 0, 0
	}
	return int(a), int(b)
}
