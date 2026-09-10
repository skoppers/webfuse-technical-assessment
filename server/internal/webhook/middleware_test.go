package webhook

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serve runs RequireSignature(testKey) over a handler that records Body(ctx)
// and returns the response plus whether downstream ran.
func serve(t *testing.T, body []byte, sig string) (*httptest.ResponseRecorder, bool, []byte) {
	t.Helper()
	called := false
	var got []byte
	h := RequireSignature(testKey)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		got = Body(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest("POST", "/webhooks", bytes.NewReader(body))
	r.Header.Set("Content-Encoding", "gzip")
	if sig != "" {
		r.Header.Set("Webhook-Signature", sig)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w, called, got
}

func TestRequireSignaturePlain(t *testing.T) {
	plain := []byte(payloadStarted)
	w, called, got := serve(t, gz(t, plain), hex.EncodeToString(sign(testKey, plain)))
	if w.Code != http.StatusOK || !called {
		t.Fatalf("status %d called %v: %s", w.Code, called, w.Body)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Body(ctx) = %q", got)
	}
}

func TestRequireSignatureRaw(t *testing.T) {
	plain := []byte(payloadStarted)
	raw := gz(t, plain)
	w, called, got := serve(t, raw, hex.EncodeToString(sign(testKey, raw)))
	if w.Code != http.StatusOK || !called {
		t.Fatalf("status %d called %v: %s", w.Code, called, w.Body)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Body(ctx) = %q", got)
	}
}

func TestRequireSignatureBad(t *testing.T) {
	plain := []byte(payloadStarted)
	w, called, _ := serve(t, gz(t, plain), hex.EncodeToString(sign("wrong", plain)))
	if w.Code != http.StatusUnauthorized || called {
		t.Fatalf("status %d called %v", w.Code, called)
	}
	if w.Body.String() != "{\"error\":\"invalid signature\"}\n" {
		t.Fatalf("body %q", w.Body)
	}
}

func TestRequireSignatureMissing(t *testing.T) {
	w, called, _ := serve(t, gz(t, []byte(payloadStarted)), "")
	if w.Code != http.StatusUnauthorized || called {
		t.Fatalf("status %d called %v", w.Code, called)
	}
}

func TestRequireSignatureOverMax(t *testing.T) {
	big := make([]byte, MaxBody+1)
	w, called, _ := serve(t, big, hex.EncodeToString(sign(testKey, big)))
	if w.Code != http.StatusBadRequest || called {
		t.Fatalf("status %d called %v", w.Code, called)
	}
}

func TestBodyOutsideMiddleware(t *testing.T) {
	if b := Body(httptest.NewRequest("GET", "/", nil).Context()); b != nil {
		t.Fatalf("got %q", b)
	}
}
