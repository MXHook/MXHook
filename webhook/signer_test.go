package webhook_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/mxhook/mxhook/webhook"
)

func TestSign_ProducesConsistentSignature(t *testing.T) {
	payload := []byte(`{"id":"msg-1","subject":"test"}`)
	sig1 := webhook.Sign(payload, "secret")
	sig2 := webhook.Sign(payload, "secret")
	if sig1 != sig2 {
		t.Errorf("signatures differ: %q vs %q", sig1, sig2)
	}
	if sig1 == "" {
		t.Error("expected non-empty signature")
	}
}

func TestVerifySignature_ValidSig(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	sig := webhook.Sign(payload, "mysecret")
	if !webhook.VerifySignature(payload, "mysecret", sig) {
		t.Error("valid signature rejected")
	}
}

func TestVerifySignature_InvalidSig(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	if webhook.VerifySignature(payload, "mysecret", "sha256=badhash") {
		t.Error("invalid signature accepted")
	}
}

func TestVerifySignature_WrongSecret(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	sig := webhook.Sign(payload, "secret1")
	if webhook.VerifySignature(payload, "secret2", sig) {
		t.Error("wrong-secret signature accepted")
	}
}

func TestSignTimestamped_CoversTimestampAndPayload(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	ts := time.Unix(1700000000, 0)
	got := webhook.SignTimestamped(payload, "secret", ts)
	want := webhook.Sign([]byte(fmt.Sprintf("%d.%s", ts.Unix(), payload)), "secret")
	if got != want {
		t.Errorf("SignTimestamped = %q, want HMAC over \"{unix_ts}.{payload}\" (%q)", got, want)
	}
	if got == webhook.Sign(payload, "secret") {
		t.Error("timestamped signature must differ from the legacy payload-only signature")
	}
}

func TestVerifyTimestampedSignature_ValidWithinTolerance(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	ts := time.Now()
	sig := webhook.SignTimestamped(payload, "mysecret", ts)
	header := strconv.FormatInt(ts.Unix(), 10)
	if !webhook.VerifyTimestampedSignature(payload, "mysecret", sig, header, 5*time.Minute) {
		t.Error("fresh timestamped signature rejected")
	}
	if !webhook.VerifyTimestampedSignature(payload, "mysecret", sig, header, 0) {
		t.Error("fresh signature rejected under default tolerance")
	}
}

func TestVerifyTimestampedSignature_StaleTimestampRejected(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	ts := time.Now().Add(-10 * time.Minute)
	sig := webhook.SignTimestamped(payload, "mysecret", ts)
	header := strconv.FormatInt(ts.Unix(), 10)
	if webhook.VerifyTimestampedSignature(payload, "mysecret", sig, header, 5*time.Minute) {
		t.Error("stale signature accepted: replay window not enforced")
	}
}

func TestVerifyTimestampedSignature_FutureSkew(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	near := time.Now().Add(time.Minute)
	sig := webhook.SignTimestamped(payload, "mysecret", near)
	if !webhook.VerifyTimestampedSignature(payload, "mysecret", sig, strconv.FormatInt(near.Unix(), 10), 5*time.Minute) {
		t.Error("small future clock skew rejected")
	}
	far := time.Now().Add(10 * time.Minute)
	sig = webhook.SignTimestamped(payload, "mysecret", far)
	if webhook.VerifyTimestampedSignature(payload, "mysecret", sig, strconv.FormatInt(far.Unix(), 10), 5*time.Minute) {
		t.Error("far-future timestamp accepted")
	}
}

func TestVerifyTimestampedSignature_TamperedTimestampRejected(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	ts := time.Now().Add(-10 * time.Minute)
	sig := webhook.SignTimestamped(payload, "mysecret", ts)
	fresh := strconv.FormatInt(time.Now().Unix(), 10)
	if webhook.VerifyTimestampedSignature(payload, "mysecret", sig, fresh, 5*time.Minute) {
		t.Error("attacker-freshened timestamp accepted: timestamp is not bound to signature")
	}
}

func TestVerifyTimestampedSignature_MalformedTimestampRejected(t *testing.T) {
	payload := []byte(`{"id":"msg-1"}`)
	sig := webhook.SignTimestamped(payload, "mysecret", time.Now())
	for _, bad := range []string{"", "not-a-number", "17e9"} {
		if webhook.VerifyTimestampedSignature(payload, "mysecret", sig, bad, 5*time.Minute) {
			t.Errorf("malformed timestamp %q accepted", bad)
		}
	}
}
