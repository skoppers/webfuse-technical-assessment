// Package httpx holds small HTTP helpers shared by every handler package.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// MaxBodyBytes caps request bodies read by ReadJSON.
const MaxBodyBytes = 1 << 20

// WriteJSON encodes v as JSON with the given status. Encoding errors are
// ignored: headers have already been sent and the caller cannot recover.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes {"error": msg} with the given status.
func Error(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// ReadJSON decodes a JSON request body into dst, limited to MaxBodyBytes and
// rejecting trailing data. The returned error is safe to show to clients.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return fmt.Errorf("body exceeds %d bytes", MaxBodyBytes)
		case errors.Is(err, io.EOF):
			return errors.New("empty body")
		default:
			return fmt.Errorf("invalid JSON: %w", err)
		}
	}
	if dec.More() {
		return errors.New("unexpected data after JSON body")
	}
	return nil
}
