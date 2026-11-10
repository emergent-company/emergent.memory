package githubapp

import (
	"encoding/base64"
	"errors"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// ErrEncryptionKeyUnset is returned when credentials are used without
// GITHUB_APP_ENCRYPTION_KEY. Callers must fail closed.
var ErrEncryptionKeyUnset = errors.New("githubapp: GITHUB_APP_ENCRYPTION_KEY is not configured")

// gcmNonceSize is the AES-GCM standard nonce length used by pkg/crypto.
const gcmNonceSize = 12

// CredentialCipher seals App private keys and webhook secrets at rest. A nil
// cipher is valid and fails closed: reads and writes return
// ErrEncryptionKeyUnset.
type CredentialCipher struct {
	enc *crypto.Encryptor
}

// NewCredentialCipher builds the cipher from the hex-encoded AES-256 key.
func NewCredentialCipher(hexKey string) (*CredentialCipher, error) {
	if hexKey == "" {
		return nil, ErrEncryptionKeyUnset
	}
	enc, err := crypto.NewEncryptor(hexKey)
	if err != nil {
		return nil, err
	}
	return &CredentialCipher{enc: enc}, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext).
func (c *CredentialCipher) Seal(plaintext string) (string, error) {
	if c == nil || c.enc == nil {
		return "", ErrEncryptionKeyUnset
	}
	ciphertext, nonce, err := c.enc.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	sealed := make([]byte, 0, len(nonce)+len(ciphertext))
	sealed = append(sealed, nonce...)
	sealed = append(sealed, ciphertext...)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal.
func (c *CredentialCipher) Open(sealed string) (string, error) {
	if c == nil || c.enc == nil {
		return "", ErrEncryptionKeyUnset
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	if len(raw) <= gcmNonceSize {
		return "", errors.New("githubapp: sealed credential is too short")
	}
	plaintext, err := c.enc.Decrypt(raw[gcmNonceSize:], raw[:gcmNonceSize])
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
