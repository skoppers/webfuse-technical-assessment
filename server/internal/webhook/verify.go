package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func Verify(key, header string, body []byte) bool {
	header = strings.TrimSpace(header)
	if len(header) >= 7 && strings.EqualFold(header[:7], "sha256=") {
		header = strings.TrimSpace(header[7:])
	}
	if header == "" {
		return false
	}
	sig, err := hex.DecodeString(header)
	if err != nil || len(sig) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}
