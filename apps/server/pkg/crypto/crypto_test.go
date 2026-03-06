package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// Generate a key
	hexKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	enc, err := NewEncryptor(hexKey)
	if err != nil {
		t.Fatalf("NewEncryptor() failed: %v", err)
	}

	plaintext := []byte("my-secret-api-key-12345")

	ciphertext, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("Decrypt() failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted text does not match: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_LargePayload(t *testing.T) {
	hexKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	enc, err := NewEncryptor(hexKey)
	if err != nil {
		t.Fatalf("NewEncryptor() failed: %v", err)
	}

	// Simulate a large service account JSON
	plaintext := make([]byte, 4096)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	decrypted, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("Decrypt() failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted large payload does not match original")
	}
}

func TestNewEncryptor_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"empty key", ""},
		{"invalid hex", "not-hex-at-all"},
		{"too short", "aabbccdd"},
		{"too long", "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEncryptor(tt.key)
			if err == nil {
				t.Fatal("expected error for invalid key")
			}
		})
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	enc1, _ := NewEncryptor(key1)
	enc2, _ := NewEncryptor(key2)

	plaintext := []byte("secret data")
	ciphertext, nonce, err := enc1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() failed: %v", err)
	}

	// Decrypting with a different key should fail
	_, err = enc2.Decrypt(ciphertext, nonce)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	hexKey, _ := GenerateKey()
	enc, _ := NewEncryptor(hexKey)

	plaintext := []byte("secret data")
	ciphertext, nonce, _ := enc.Encrypt(plaintext)

	// Tamper with ciphertext
	ciphertext[0] ^= 0xff

	_, err := enc.Decrypt(ciphertext, nonce)
	if err == nil {
		t.Fatal("expected error decrypting tampered ciphertext")
	}
}

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	if len(key1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(key1))
	}

	if key1 == key2 {
		t.Fatal("two generated keys should not be equal")
	}
}
