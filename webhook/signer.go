package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// DefaultSignatureTolerance bounds the accepted clock skew between signing
// and verification when no explicit tolerance is supplied.
const DefaultSignatureTolerance = 5 * time.Minute

// Sign returns the HMAC-SHA256 hex signature of payload using secret.
// Format: "sha256=<hex>"
//
// This is the legacy payload-only scheme: it carries no freshness field, so
// a captured request verifies forever. New integrations should verify with
// VerifyTimestampedSignature instead; Sign remains for receivers still fed
// by senders that predate timestamped signing.
func Sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignTimestamped returns the HMAC-SHA256 hex signature of
// "{unix_ts}.{payload}" using secret. The timestamp is sent alongside the
// delivery in the X-MXHook-Timestamp header.
// Format: "sha256=<hex>"
func SignTimestamped(payload []byte, secret string, ts time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts.Unix(), 10) + "."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature returns true if sig matches the expected HMAC of payload.
// Uses constant-time comparison to prevent timing attacks.
//
// This is the explicit legacy verification path for the payload-only scheme
// (no replay protection). Deliveries carrying an X-MXHook-Timestamp header
// must be verified with VerifyTimestampedSignature.
func VerifySignature(payload []byte, secret, sig string) bool {
	expected := Sign(payload, secret)
	return hmac.Equal([]byte(sig), []byte(expected))
}

// VerifyTimestampedSignature returns true if sig matches the HMAC of
// "{timestamp}.{payload}" and timestamp (unix seconds, as received in the
// X-MXHook-Timestamp header) is within tolerance of the current time.
// A tolerance <= 0 applies DefaultSignatureTolerance. Uses constant-time
// signature comparison.
func VerifyTimestampedSignature(payload []byte, secret, sig, timestamp string, tolerance time.Duration) bool {
	if tolerance <= 0 {
		tolerance = DefaultSignatureTolerance
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	skew := time.Since(time.Unix(ts, 0))
	if skew < 0 {
		skew = -skew
	}
	if skew > tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}
