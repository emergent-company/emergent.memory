package gdrive

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// EncryptCredentials encrypts the credential struct using the pkg/crypto
// AES-GCM encryptor, returning a single self-contained blob of the form
// [1 byte nonce length][nonce][ciphertext]. key is the raw AES-256 key bytes;
// it is hex-encoded before being handed to pkg/crypto (whose API accepts a
// hex string key, not raw bytes).
func EncryptCredentials(key []byte, creds Credentials) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("gdrive: encryption key is empty")
	}

	enc, err := crypto.NewEncryptor(hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("gdrive: init encryptor: %w", err)
	}

	plaintext, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("gdrive: marshal credentials: %w", err)
	}

	ciphertext, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("gdrive: encrypt credentials: %w", err)
	}
	if len(nonce) > 255 {
		return nil, errors.New("gdrive: nonce too long to encode")
	}

	blob := make([]byte, 0, 1+len(nonce)+len(ciphertext))
	blob = append(blob, byte(len(nonce)))
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	return blob, nil
}

// DecryptCredentials reverses EncryptCredentials, returning the original
// Credentials struct.
func DecryptCredentials(key []byte, blob []byte) (Credentials, error) {
	if len(key) == 0 {
		return Credentials{}, errors.New("gdrive: encryption key is empty")
	}
	if len(blob) < 2 {
		return Credentials{}, errors.New("gdrive: malformed credential blob")
	}

	enc, err := crypto.NewEncryptor(hex.EncodeToString(key))
	if err != nil {
		return Credentials{}, fmt.Errorf("gdrive: init encryptor: %w", err)
	}

	nonceLen := int(blob[0])
	if 1+nonceLen > len(blob) {
		return Credentials{}, errors.New("gdrive: malformed credential blob: nonce out of range")
	}
	nonce := blob[1 : 1+nonceLen]
	ciphertext := blob[1+nonceLen:]

	plaintext, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		return Credentials{}, fmt.Errorf("gdrive: decrypt credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return Credentials{}, fmt.Errorf("gdrive: unmarshal credentials: %w", err)
	}
	return creds, nil
}
