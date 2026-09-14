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

func TestVerifyHex(t *testing.T) {
	body := []byte(`{"category":"space.session.started"}`)
	sig := hex.EncodeToString(sign(testKey, body))
	cases := []struct{ name, header string }{
		{"lower", sig},
		{"upper", strings.ToUpper(sig)},
		{"prefix", "sha256=" + sig},
		{"prefix upper", "SHA256=" + sig},
		{"whitespace", "  " + sig + "\n"},
		{"prefix and whitespace", " sha256= " + sig + " "},
	}
	for _, c := range cases {
		if !Verify(testKey, c.header, body) {
			t.Errorf("%s: not verified", c.name)
		}
	}
}

func TestVerifyRejects(t *testing.T) {
	body := []byte("body")
	raw := sign(testKey, body)
	good := hex.EncodeToString(raw)
	cases := []struct {
		name, key, header string
		body              []byte
	}{
		{"wrong key", "other", good, body},
		{"tampered body", testKey, good, []byte("body!")},
		{"empty header", testKey, "", body},
		{"whitespace only", testKey, "  ", body},
		{"prefix only", testKey, "sha256=", body},
		{"garbage header", testKey, "not a signature!!", body},
		{"hex wrong length", testKey, good[:62], body},
		{"hex too long", testKey, good + "ab", body},
		{"base64", testKey, base64.StdEncoding.EncodeToString(raw), body},
		{"base64 raw", testKey, base64.RawStdEncoding.EncodeToString(raw), body},
		{"base64url", testKey, base64.URLEncoding.EncodeToString(raw), body},
		{"base64url raw", testKey, base64.RawURLEncoding.EncodeToString(raw), body},
	}
	for _, c := range cases {
		if Verify(c.key, c.header, c.body) {
			t.Errorf("%s: verified", c.name)
		}
	}
}
