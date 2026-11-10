package githubapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestCredentialCipherRoundTrip(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cipher, err := NewCredentialCipher(key)
	if err != nil {
		t.Fatalf("NewCredentialCipher: %v", err)
	}

	const secret = "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----"
	sealed, err := cipher.Seal(secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "PRIVATE KEY") {
		t.Fatal("sealed value leaks plaintext")
	}
	got, err := cipher.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != secret {
		t.Fatalf("Open = %q, want round-trip", got)
	}
}

func TestCredentialCipherWrongKey(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()
	c1, _ := NewCredentialCipher(key1)
	c2, _ := NewCredentialCipher(key2)

	sealed, err := c1.Seal("x")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := c2.Open(sealed); err == nil {
		t.Fatal("expected decrypt failure with a different key")
	}
}

func TestCredentialCipherFailsClosedWithoutKey(t *testing.T) {
	if _, err := NewCredentialCipher(""); !errors.Is(err, ErrEncryptionKeyUnset) {
		t.Fatalf("empty key error = %v, want ErrEncryptionKeyUnset", err)
	}
	var nilCipher *CredentialCipher
	if _, err := nilCipher.Seal("x"); !errors.Is(err, ErrEncryptionKeyUnset) {
		t.Fatalf("nil cipher Seal error = %v", err)
	}
	if _, err := nilCipher.Open("x"); !errors.Is(err, ErrEncryptionKeyUnset) {
		t.Fatalf("nil cipher Open error = %v", err)
	}
}

func TestCredentialCipherRejectsTampering(t *testing.T) {
	key, _ := crypto.GenerateKey()
	cipher, _ := NewCredentialCipher(key)
	sealed, _ := cipher.Seal("x")

	tampered := []byte(sealed)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	if _, err := cipher.Open(string(tampered)); err == nil {
		t.Fatal("expected tampered ciphertext to fail")
	}
}
