package brandidentity

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripOutboundHeadersIsNarrow(t *testing.T) {
	header := http.Header{
		"X-Sub2API-Trace":         {"internal"},
		GrokClientToolCacheHeader: {"prefer-cache"},
		"X-Grok-Conv-Id":          {"conversation"},
		"User-Agent":              {"sub2api-client/1"},
		"X-Organization":          {"Sub2API Plus"},
		"Authorization":           {"Bearer sub2api-user-value"},
	}

	StripOutboundHeaders(header)

	require.Empty(t, header.Values("X-Sub2API-Trace"))
	require.Empty(t, header.Values(GrokClientToolCacheHeader))
	require.Equal(t, []string{"conversation"}, header.Values("X-Grok-Conv-Id"))
	require.Empty(t, header.Values("User-Agent"))
	require.Equal(t, []string{"Sub2API Plus"}, header.Values("X-Organization"))
	require.Equal(t, []string{"Bearer sub2api-user-value"}, header.Values("Authorization"))
}
