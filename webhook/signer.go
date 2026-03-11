package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the HMAC-SHA256 hex signature of payload using secret.
// Format: "sha256=<hex>"
func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature returns true if sig matches the expected HMAC of payload.
// Uses constant-time comparison to prevent timing attacks.
func VerifySignature(payload []byte, secret, sig string) bool {
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(sig), []byte(expected))
}
