package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
)

// Match reports which candidate body and which header encoding a signature
// verified against.
type Match struct {
	// Candidate is the index into the candidates passed to Verify.
	Candidate int
	// Encoding is how the header was encoded: "hex", "base64" or "base64url".
	Encoding string
}

// String renders m as encoding and candidate index for logs.
func (m Match) String() string {
	return m.Encoding + " over candidate " + strconv.Itoa(m.Candidate)
}

// decoding is one way the signature header may be encoded.
type decoding struct {
	name   string
	decode func(string) ([]byte, error)
}

// decodings lists every header encoding tried, in order. The upstream docs
// do not state the encoding or which bytes are signed, so all variants are
// tried and the match is logged.
var decodings = []decoding{
	{"hex", hex.DecodeString},
	{"base64", base64.StdEncoding.DecodeString},
	{"base64", base64.RawStdEncoding.DecodeString},
	{"base64url", base64.URLEncoding.DecodeString},
	{"base64url", base64.RawURLEncoding.DecodeString},
}

// Verify reports whether header carries a valid HMAC-SHA256 of one of the
// candidates under key. The header may carry an optional "sha256=" prefix
// (any case) and surrounding whitespace, and may be hex, standard base64 or
// URL base64, padded or not. Every candidate is checked against every
// decoding that yields 32 bytes and the first match is returned; comparison
// is constant-time via hmac.Equal.
func Verify(key, header string, candidates ...[]byte) (Match, bool) {
	header = strings.TrimSpace(header)
	if len(header) >= 7 && strings.EqualFold(header[:7], "sha256=") {
		header = strings.TrimSpace(header[7:])
	}
	if header == "" {
		return Match{}, false
	}
	var sigs []struct {
		name string
		sig  []byte
	}
	for _, d := range decodings {
		sig, err := d.decode(header)
		if err != nil || len(sig) != sha256.Size {
			continue
		}
		sigs = append(sigs, struct {
			name string
			sig  []byte
		}{d.name, sig})
	}
	if len(sigs) == 0 {
		return Match{}, false
	}
	for i, c := range candidates {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write(c)
		want := mac.Sum(nil)
		for _, s := range sigs {
			if hmac.Equal(want, s.sig) {
				return Match{Candidate: i, Encoding: s.name}, true
			}
		}
	}
	return Match{}, false
}
