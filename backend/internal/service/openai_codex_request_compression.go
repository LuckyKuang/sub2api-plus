package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type codexRequestCompressionKey struct{}
type codexRequestCompression struct {
	target  string
	encoded bool
}

var codexRequestEncoder = sync.OnceValues(func() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)), zstd.WithEncoderConcurrency(1))
})

// PrepareCodexRequestCompression selects compression from the credential owner,
// never from inbound headers.
// Agent Identity is excluded: its signed request cannot be changed at send time.
func PrepareCodexRequestCompression(req *http.Request, account *Account) {
	if req == nil || req.URL == nil {
		return
	}
	state := codexRequestCompression{}
	if account.IsOpenAIOAuthLike() && !account.IsOpenAIAgentIdentity() && strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") && req.Method == http.MethodPost &&
		strings.HasSuffix(req.URL.Path, "/responses") && strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		state.target = req.URL.String()
		if existing, ok := req.Context().Value(codexRequestCompressionKey{}).(codexRequestCompression); ok && existing.target == state.target {
			state.encoded = existing.encoded
		}
	}
	*req = *req.WithContext(context.WithValue(req.Context(), codexRequestCompressionKey{}, state))
}

// ApplyCodexRequestCompression encodes the final OAuth Responses body at the
// transport boundary, after JSON adapters, before HTTP or plugin transmission.
// Official Codex 1b6e0d8145 uses Zstd level 3 for these HTTP requests. WS, API
// keys, unary endpoints and signed Agent Identity requests keep their encoding.
func ApplyCodexRequestCompression(req *http.Request) error {
	if req == nil || req.URL == nil {
		return nil
	}
	state, _ := req.Context().Value(codexRequestCompressionKey{}).(codexRequestCompression)
	if state.target == "" || state.target != req.URL.String() || state.encoded || req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if req.Header.Get("Content-Encoding") != "" {
		return fmt.Errorf("codex request compression conflicts with existing content encoding")
	}
	encoder, err := codexRequestEncoder()
	if err != nil {
		return fmt.Errorf("initialize codex request compression: %w", err)
	}
	reader := req.Body
	if req.GetBody != nil {
		reader, err = req.GetBody()
		if err != nil {
			return fmt.Errorf("read codex request for compression: %w", err)
		}
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		return fmt.Errorf("read codex request for compression: %w", err)
	}
	encoded := encoder.EncodeAll(body, nil)
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(encoded))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(encoded)), nil }
	req.ContentLength = int64(len(encoded))
	req.Header.Del("Content-Length")
	req.Header.Set("Content-Encoding", "zstd")
	state.encoded = true
	*req = *req.WithContext(context.WithValue(req.Context(), codexRequestCompressionKey{}, state))
	return nil
}
