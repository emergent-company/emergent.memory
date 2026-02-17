package githubapp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Crypto provides AES-256-GCM encryption and decryption for GitHub App credentials.
// The encryption key is derived from the GITHUB_APP_ENCRYPTION_KEY environment variable.
type Crypto struct {
	key []byte
}

// NewCrypto creates a new Crypto service with the given hex-encoded 256-bit key.
// The key must be a 64-character hex string (32 bytes / 256 bits).
func NewCrypto(hexKey string) (*Crypto, error) {
	if hexKey == "" {
		return &Crypto{key: nil}, nil // Crypto disabled — will error on encrypt/decrypt
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid encryption key: must be 256 bits (32 bytes), got %d bytes", len(key))
	}
	return &Crypto{key: key}, nil
}

// IsConfigured returns true if an encryption key has been set.
func (c *Crypto) IsConfigured() bool {
	return len(c.key) == 32
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns nonce || ciphertext (nonce is prepended to the ciphertext).
func (c *Crypto) Encrypt(plaintext []byte) ([]byte, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("encryption key not configured: set GITHUB_APP_ENCRYPTION_KEY")
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends ciphertext to nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// Expects nonce || ciphertext format.
func (c *Crypto) Decrypt(ciphertext []byte) ([]byte, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("encryption key not configured: set GITHUB_APP_ENCRYPTION_KEY")
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// EncryptString is a convenience wrapper for encrypting string data.
func (c *Crypto) EncryptString(plaintext string) ([]byte, error) {
	return c.Encrypt([]byte(plaintext))
}

// DecryptString is a convenience wrapper for decrypting to a string.
func (c *Crypto) DecryptString(ciphertext []byte) (string, error) {
	plaintext, err := c.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
