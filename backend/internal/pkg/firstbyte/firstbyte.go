// Package firstbyte observes the first successful response-body read, before
// decompression or protocol parsing. It never reads ahead or measures headers.
package firstbyte

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

type contextKey struct{}
type responseKey struct{}

// Timing belongs to one forwarding call. Each HTTP attempt replaces its
// observation, so a discarded response cannot supply the next attempt's time.
type Timing struct {
	start   time.Time
	mu      sync.Mutex
	current *observation
}

type observation struct {
	mu sync.Mutex
	at time.Time
}

func WithStart(ctx context.Context, start time.Time) (context.Context, *Timing) {
	t := &Timing{start: start}
	return context.WithValue(ctx, contextKey{}, t), t
}

func (t *Timing) Milliseconds() *int {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	o := t.current
	t.mu.Unlock()
	if o == nil {
		return nil
	}
	o.mu.Lock()
	at := o.at
	o.mu.Unlock()
	if at.IsZero() {
		return nil
	}
	ms := int(at.Sub(t.start).Milliseconds())
	if ms < 0 {
		ms = 0
	}
	return &ms
}

// Track attaches a new observation without reading. Repeated wrapping of the
// same response is harmless, including the repository's decompression boundary.
func Track(req *http.Request, resp *http.Response) {
	if req == nil || resp == nil || resp.Body == nil {
		return
	}
	t, _ := req.Context().Value(contextKey{}).(*Timing)
	if t == nil {
		return
	}
	if resp.Request != nil && resp.Request.Context().Value(responseKey{}) != nil {
		return
	}
	o := &observation{}
	t.mu.Lock()
	t.current = o
	t.mu.Unlock()
	resp.Body = &body{ReadCloser: resp.Body, observation: o}
	resp.Request = req.WithContext(context.WithValue(req.Context(), responseKey{}, o))
}

type body struct {
	io.ReadCloser
	observation *observation
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.observation.mu.Lock()
		if b.observation.at.IsZero() {
			b.observation.at = time.Now()
		}
		b.observation.mu.Unlock()
	}
	return n, err
}

// WrapTransport takes a newly constructed transport. Reproduce net/http's
// automatic gzip negotiation and lazy decoding outside the raw body observer.
// Explicit Accept-Encoding, Range and HEAD requests retain their wire behavior.
// No identity/signing declaration or request body is changed.
func WrapTransport(base *http.Transport) http.RoundTripper {
	auto := !base.DisableCompression
	base.DisableCompression = true
	return &transport{base: base, autoGzip: auto}
}

type transport struct {
	base     *http.Transport
	autoGzip bool
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if timing, _ := req.Context().Value(contextKey{}).(*Timing); timing != nil {
		timing.mu.Lock()
		timing.current = nil
		timing.mu.Unlock()
	}
	auto := t.autoGzip && req.Header.Get("Accept-Encoding") == "" && req.Header.Get("Range") == "" && req.Method != http.MethodHead
	wire := req
	if auto {
		wire = req.Clone(req.Context())
		wire.Header.Set("Accept-Encoding", "gzip")
	}
	resp, err := t.base.RoundTrip(wire)
	if err != nil {
		return resp, err
	}
	Track(req, resp)
	if auto && resp.Header.Get("Content-Encoding") == "gzip" {
		resp.Body = &gzipBody{ReadCloser: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Uncompressed = true
	}
	return resp, nil
}

func (t *transport) CloseIdleConnections()     { t.base.CloseIdleConnections() }
func (t *transport) Unwrap() http.RoundTripper { return t.base }

type gzipBody struct {
	io.ReadCloser
	reader *gzip.Reader
	err    error
}

func (b *gzipBody) Read(p []byte) (int, error) {
	if b.reader == nil && b.err == nil {
		b.reader, b.err = gzip.NewReader(b.ReadCloser)
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.reader.Read(p)
}
