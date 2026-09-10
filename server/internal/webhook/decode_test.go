package webhook

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"strings"
	"testing"
)

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadBodyPlain(t *testing.T) {
	body := []byte(`{"a":1}`)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	raw, plain, err := ReadBody(r, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, body) || !bytes.Equal(plain, body) {
		t.Fatalf("raw=%q plain=%q", raw, plain)
	}
}

func TestReadBodyGzipHeader(t *testing.T) {
	body := []byte(`{"a":1}`)
	z := gz(t, body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(z))
	r.Header.Set("Content-Encoding", "gzip")
	raw, plain, err := ReadBody(r, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, z) {
		t.Fatalf("raw should stay gzipped")
	}
	if !bytes.Equal(plain, body) {
		t.Fatalf("plain=%q", plain)
	}
}

func TestReadBodyGzipSniffed(t *testing.T) {
	body := []byte(`{"a":1}`)
	z := gz(t, body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(z))
	raw, plain, err := ReadBody(r, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, z) || !bytes.Equal(plain, body) {
		t.Fatalf("raw=%q plain=%q", raw, plain)
	}
}

func TestReadBodyOverMax(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 11)))
	if _, _, err := ReadBody(r, 10); err == nil {
		t.Fatal("want error for raw body over max")
	}
}

func TestReadBodyDecompressedOverMax(t *testing.T) {
	// 4 KiB of zeros gzips to well under 100 bytes.
	z := gz(t, make([]byte, 4096))
	if len(z) > 100 {
		t.Fatalf("fixture gzips to %d bytes", len(z))
	}
	r := httptest.NewRequest("POST", "/", bytes.NewReader(z))
	if _, _, err := ReadBody(r, 100); err == nil {
		t.Fatal("want error for decompressed body over max")
	}
}

func TestReadBodyCorruptGzip(t *testing.T) {
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte{0x1f, 0x8b, 0xff, 0xff}))
	if _, _, err := ReadBody(r, 1024); err == nil {
		t.Fatal("want error for corrupt gzip")
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader("not gzip"))
	r.Header.Set("Content-Encoding", "gzip")
	if _, _, err := ReadBody(r, 1024); err == nil {
		t.Fatal("want error for non-gzip body with gzip header")
	}
}
