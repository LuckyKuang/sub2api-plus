//go:build unit || !integration

package service

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseOpenAIWSDeclaredMaxMessageBytes(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		set     bool
		want    int
		wantOK  bool
		comment string
	}{
		{name: "absent", set: false},
		{name: "empty", header: "", set: true},
		{name: "whitespace", header: "   ", set: true},
		{name: "padded", header: " 1048576 ", set: true, want: 1048576, wantOK: true},
		{name: "zero", header: "0", set: true},
		{name: "negative", header: "-1", set: true},
		{name: "non numeric", header: "1mb", set: true},
		{name: "float", header: "1.5", set: true},
		{name: "overflow bound", header: "67108865", set: true},
		{name: "at bound", header: "67108864", set: true, want: 67108864, wantOK: true},
		{name: "too long", header: "00000000000000001", set: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.set {
				headers.Set(openAIWSMaxMessageBytesHeader, tt.header)
			}
			got, ok := parseOpenAIWSDeclaredMaxMessageBytes(headers)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseOpenAIWSDeclaredMaxMessageBytesNilHeaders(t *testing.T) {
	got, ok := parseOpenAIWSDeclaredMaxMessageBytes(nil)
	require.False(t, ok)
	require.Zero(t, got)
}

func TestOpenAIWSDeclaredLimitCacheExpiresAndForgets(t *testing.T) {
	cache := newOpenAIWSDeclaredLimitCache()
	now := time.Unix(1_700_000_000, 0)
	cache.now = func() time.Time { return now }

	cache.remember("42", 1024)
	limit, ok := cache.lookup("42")
	require.True(t, ok)
	require.Equal(t, 1024, limit)

	now = now.Add(openAIWSDeclaredLimitCacheTTL - time.Second)
	limit, ok = cache.lookup("42")
	require.True(t, ok)
	require.Equal(t, 1024, limit)

	now = now.Add(2 * time.Second)
	_, ok = cache.lookup("42")
	require.False(t, ok)

	cache.remember("42", 2048)
	cache.forget("42")
	_, ok = cache.lookup("42")
	require.False(t, ok)
}

func TestOpenAIWSDeclaredLimitCacheBounded(t *testing.T) {
	cache := newOpenAIWSDeclaredLimitCache()
	cache.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	for i := 0; i < openAIWSDeclaredLimitEntries+32; i++ {
		cache.remember(strconv.Itoa(i), 1024+i)
	}
	cache.mu.Lock()
	size := len(cache.entries)
	cache.mu.Unlock()
	require.LessOrEqual(t, size, openAIWSDeclaredLimitEntries)
}

func TestRememberOpenAIWSDeclaredMaxMessageBytesIgnoresMissingHeader(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	rememberOpenAIWSDeclaredMaxMessageBytes(7, http.Header{})
	_, ok := openAIWSDeclaredLimits.lookup("7")
	require.False(t, ok)

	headers := http.Header{}
	headers.Set(openAIWSMaxMessageBytesHeader, "4096")
	rememberOpenAIWSDeclaredMaxMessageBytes(7, headers)
	limit, ok := openAIWSDeclaredLimits.lookup("7")
	require.True(t, ok)
	require.Equal(t, 4096, limit)

	// An account without a declaration keeps no entry.
	rememberOpenAIWSDeclaredMaxMessageBytes(0, headers)
	_, ok = openAIWSDeclaredLimits.lookup("0")
	require.False(t, ok)
}

func TestOpenAIWSDeclaredLimitBlocksPayload(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 11}

	require.False(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1<<30), "no declaration must never block")

	headers := http.Header{}
	headers.Set(openAIWSMaxMessageBytesHeader, "1024")
	rememberOpenAIWSDeclaredMaxMessageBytes(account.ID, headers)

	require.False(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1024), "the declared size itself fits")
	require.True(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 1025))
}

func TestCheckOpenAIWSDeclaredMessageLimitBuildsFallbackError(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 23}
	lease := &openAIWSConnLease{conn: &openAIWSConn{handshakeHeaders: http.Header{
		openAIWSMaxMessageBytesHeader: []string{"2048"},
	}}}

	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 2048))

	err := svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 2049)
	require.Error(t, err)

	var fallbackErr *openAIWSFallbackError
	require.True(t, errors.As(err, &fallbackErr))
	require.Equal(t, openAIWSReasonRequestTooLarge, fallbackErr.Reason)
	require.False(t, fallbackErr.RequestSubmitted, "nothing was written upstream")

	var limitErr *openAIWSDeclaredLimitExceededError
	require.True(t, errors.As(err, &limitErr))
	require.Equal(t, 2048, limitErr.limit)
	require.Equal(t, 2049, limitErr.length)
	require.Equal(t, 2048, openAIWSDeclaredLimitFromError(err))
	require.True(t, isOpenAIWSDeclaredLimitExceeded(err))

	// The declaration is remembered for the account so the next attempt can skip
	// the socket entirely.
	limit, ok := svc.openAIWSDeclaredMessageLimitForAccount(account)
	require.True(t, ok)
	require.Equal(t, 2048, limit)
	require.True(t, svc.openAIWSDeclaredLimitBlocksPayload(account, 4096))
}

func TestCheckOpenAIWSDeclaredMessageLimitWithoutDeclaration(t *testing.T) {
	openAIWSDeclaredLimits.reset()
	t.Cleanup(openAIWSDeclaredLimits.reset)

	svc := &OpenAIGatewayService{}
	account := &Account{ID: 29}
	lease := &openAIWSConnLease{conn: &openAIWSConn{}}

	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, lease, 64<<20))
	require.NoError(t, svc.checkOpenAIWSDeclaredMessageLimit(account, nil, 64<<20))
	_, ok := svc.openAIWSDeclaredMessageLimitForAccount(account)
	require.False(t, ok)
}

func TestIsOpenAIWSDeclaredLimitFallbackReason(t *testing.T) {
	require.True(t, isOpenAIWSDeclaredLimitFallbackReason(openAIWSReasonRequestTooLarge))
	require.True(t, isOpenAIWSDeclaredLimitFallbackReason(" prewarm_message_too_big "))
	require.False(t, isOpenAIWSDeclaredLimitFallbackReason("upgrade_required"))
	require.False(t, isOpenAIWSDeclaredLimitFallbackReason(""))
}
