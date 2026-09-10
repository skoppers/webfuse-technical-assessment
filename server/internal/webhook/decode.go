// Package webhook is the security boundary for the Space lifecycle webhooks
// Webfuse delivers: it reads and gunzips the body, verifies the HMAC-SHA256
// signature, and parses the event envelope and its per-category data. It
// owns the webhook wire shapes (Envelope, SessionData, ParticipantData) and
// nothing else: it stores nothing and publishes nothing.
package webhook

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// gzipMagic is the two-byte header every gzip stream starts with.
var gzipMagic = []byte{0x1f, 0x8b}

// ReadBody reads at most max bytes of r's body. If the body is gzipped
// (Content-Encoding: gzip, or it starts with the gzip magic) plain is the
// decompressed bytes, also capped at max; otherwise plain is raw. Both are
// returned because the upstream docs do not state which bytes are signed.
func ReadBody(r *http.Request, max int64) (raw, plain []byte, err error) {
	raw, err = io.ReadAll(http.MaxBytesReader(nil, r.Body, max))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, nil, fmt.Errorf("body exceeds %d bytes", max)
		}
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	if r.Header.Get("Content-Encoding") != "gzip" && !bytes.HasPrefix(raw, gzipMagic) {
		return raw, raw, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid gzip body: %w", err)
	}
	// Read one byte past max so an oversized stream is distinguishable from
	// one that is exactly max bytes.
	plain, err = io.ReadAll(io.LimitReader(zr, max+1))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid gzip body: %w", err)
	}
	if int64(len(plain)) > max {
		return nil, nil, fmt.Errorf("decompressed body exceeds %d bytes", max)
	}
	return raw, plain, nil
}
