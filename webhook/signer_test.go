package webhook_test

import (
	"testing"

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
