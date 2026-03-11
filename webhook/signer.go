package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(payload []byte, secret, sig string) bool {
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(sig), []byte(expected))
}
