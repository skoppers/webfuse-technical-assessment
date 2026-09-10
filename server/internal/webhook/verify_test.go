package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const testKey = "whsec_test_key"

func sign(key string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return mac.Sum(nil)
}

func TestVerifyEncodings(t *testing.T) {
	body := []byte(`{"category":"space.session.started"}`)
	sig := sign(testKey, body)
	cases := []struct {
		name, header, encoding string
	}{
		{"hex", hex.EncodeToString(sig), "hex"},
		{"hex upper", strings.ToUpper(hex.EncodeToString(sig)), "hex"},
		{"base64", base64.StdEncoding.EncodeToString(sig), "base64"},
		{"base64 raw", base64.RawStdEncoding.EncodeToString(sig), "base64"},
		{"base64url", base64.URLEncoding.EncodeToString(sig), "base64url"},
		{"base64url raw", base64.RawURLEncoding.EncodeToString(sig), "base64url"},
	}
	for _, c := range cases {
		for _, prefix := range []string{"", "sha256=", "SHA256=", " sha256= "} {
			h := prefix + c.header
			if prefix != "" {
				h += " "
			}
			m, ok := Verify(testKey, h, body)
			if !ok {
				t.Errorf("%s with prefix %q: not verified", c.name, prefix)
				continue
			}
			if m.Candidate != 0 || m.Encoding != c.encoding {
				t.Errorf("%s with prefix %q: got %+v", c.name, prefix, m)
			}
		}
	}
}

func TestVerifySecondCandidate(t *testing.T) {
	first := []byte("plain")
	second := []byte("raw")
	h := hex.EncodeToString(sign(testKey, second))
	m, ok := Verify(testKey, h, first, second)
	if !ok || m.Candidate != 1 || m.Encoding != "hex" {
		t.Fatalf("got %+v ok=%v", m, ok)
	}
}

func TestVerifyRejects(t *testing.T) {
	body := []byte("body")
	good := hex.EncodeToString(sign(testKey, body))
	cases := []struct {
		name, key, header string
		body              []byte
	}{
		{"wrong key", "other", good, body},
		{"tampered body", testKey, good, []byte("body!")},
		{"empty header", testKey, "", body},
		{"prefix only", testKey, "sha256=", body},
		{"garbage header", testKey, "not a signature!!", body},
		{"hex wrong length", testKey, good[:62], body},
		{"hex too long", testKey, good + "ab", body},
	}
	for _, c := range cases {
		if m, ok := Verify(c.key, c.header, c.body); ok {
			t.Errorf("%s: verified as %+v", c.name, m)
		}
	}
	if _, ok := Verify(testKey, good); ok {
		t.Error("no candidates: verified")
	}
}

func TestMatchString(t *testing.T) {
	if got := (Match{Candidate: 1, Encoding: "hex"}).String(); got != "hex over candidate 1" {
		t.Fatalf("got %q", got)
	}
}
