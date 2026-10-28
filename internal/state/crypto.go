package state

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

func newCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("state: encryption key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("state: initialize encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("state: initialize GCM: %w", err)
	}
	return aead, nil
}

// seal encrypts a secret and binds it to owner — the route or delivery ID —
// as additional authenticated data, so a blob moved to another row fails to
// decrypt.
func seal(aead cipher.AEAD, plaintext, owner string) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("state: create encryption nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), []byte(owner)), nil
}

// sealLegacy encrypts without AAD binding, matching secrets written before
// owner binding existed. Retained so tests can build legacy fixtures.
func sealLegacy(aead cipher.AEAD, plaintext string) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("state: create encryption nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// openSecret decrypts a secret bound to owner. Secrets written before owner
// binding carry no AAD, so decryption falls back to the unbound form; those
// rows are upgraded whenever they are next written. Remove the fallback only
// after every stored secret has been rewritten.
func openSecret(aead cipher.AEAD, ciphertext []byte, owner string) (string, error) {
	if len(ciphertext) < aead.NonceSize() {
		return "", errors.New("state: invalid encrypted secret")
	}
	nonce := ciphertext[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, ciphertext[aead.NonceSize():], []byte(owner))
	if err == nil {
		return string(plaintext), nil
	}
	legacy, legacyErr := aead.Open(nil, nonce, ciphertext[aead.NonceSize():], nil)
	if legacyErr != nil {
		return "", errors.New("state: decrypt secret")
	}
	return string(legacy), nil
}
