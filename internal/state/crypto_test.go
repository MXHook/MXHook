package state

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealOpenRoundTripWithAssociatedData(t *testing.T) {
	aead, err := newCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("newCipher: %v", err)
	}
	ciphertext, err := seal(aead, "super-secret", "route-a")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	plaintext, err := openSecret(aead, ciphertext, "route-a")
	if err != nil || plaintext != "super-secret" {
		t.Fatalf("openSecret = %q, %v", plaintext, err)
	}
}

func TestOpenSecretRejectsSecretMovedToAnotherRoute(t *testing.T) {
	aead, err := newCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("newCipher: %v", err)
	}
	ciphertext, err := seal(aead, "super-secret", "route-a")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// A DB-write attacker swapping blobs between routes must not decrypt.
	if _, err := openSecret(aead, ciphertext, "route-b"); err == nil {
		t.Fatal("secret sealed for route-a decrypted under route-b: AAD not bound")
	}
}

func TestOpenSecretRejectsWrongKeyAndTamperedCiphertext(t *testing.T) {
	aead, err := newCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("newCipher: %v", err)
	}
	ciphertext, err := seal(aead, "super-secret", "route-a")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	other, err := newCipher(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatalf("newCipher (other): %v", err)
	}
	if _, err := openSecret(other, ciphertext, "route-a"); err == nil {
		t.Fatal("wrong key decrypted the secret")
	}
	tampered := append([]byte(nil), ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := openSecret(aead, tampered, "route-a"); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	if _, err := openSecret(aead, ciphertext[:aead.NonceSize()-1], "route-a"); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
}

func TestOpenSecretReadsLegacyUnboundSecrets(t *testing.T) {
	aead, err := newCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatalf("newCipher: %v", err)
	}
	// Secrets sealed before AAD binding carry no associated data; they must
	// keep decrypting so existing installs are not stranded.
	legacy, err := sealLegacy(aead, "legacy-secret")
	if err != nil {
		t.Fatalf("sealLegacy: %v", err)
	}
	plaintext, err := openSecret(aead, legacy, "route-a")
	if err != nil || plaintext != "legacy-secret" {
		t.Fatalf("legacy openSecret = %q, %v", plaintext, err)
	}
}

func TestNewCipherRejectsWrongKeySize(t *testing.T) {
	if _, err := newCipher(bytes.Repeat([]byte{1}, 16)); err == nil {
		t.Fatal("16-byte key accepted")
	} else if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}
