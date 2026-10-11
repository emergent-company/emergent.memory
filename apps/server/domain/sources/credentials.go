package sources

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// Credential blobs are a provider-defined map serialized to JSON and encrypted
// with the config-sourced AES-256 key (LLM_ENCRYPTION_KEY), in the same
// self-contained format used previously: [1 byte nonce length][nonce][ciphertext].

// encryptCredentialMap encrypts a provider's credential map into a blob.
func encryptCredentialMap(key []byte, creds Credentials) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("sources: encryption key is empty")
	}
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("sources: marshal credentials: %w", err)
	}
	return encryptBlob(key, plaintext)
}

// decryptCredentialMap reverses encryptCredentialMap.
func decryptCredentialMap(key []byte, blob []byte) (Credentials, error) {
	plaintext, err := decryptBlob(key, blob)
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("sources: unmarshal credentials: %w", err)
	}
	return creds, nil
}

// encryptBlob encrypts arbitrary plaintext into the self-contained blob format.
func encryptBlob(key []byte, plaintext []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("sources: encryption key is empty")
	}
	enc, err := crypto.NewEncryptor(hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("sources: init encryptor: %w", err)
	}
	ciphertext, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("sources: encrypt: %w", err)
	}
	if len(nonce) > 255 {
		return nil, errors.New("sources: nonce too long to encode")
	}
	blob := make([]byte, 0, 1+len(nonce)+len(ciphertext))
	blob = append(blob, byte(len(nonce)))
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	return blob, nil
}

// decryptBlob reverses encryptBlob.
func decryptBlob(key []byte, blob []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("sources: encryption key is empty")
	}
	if len(blob) < 2 {
		return nil, errors.New("sources: malformed credential blob")
	}
	enc, err := crypto.NewEncryptor(hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("sources: init encryptor: %w", err)
	}
	nonceLen := int(blob[0])
	if 1+nonceLen > len(blob) {
		return nil, errors.New("sources: malformed credential blob: nonce out of range")
	}
	nonce := blob[1 : 1+nonceLen]
	ciphertext := blob[1+nonceLen:]
	plaintext, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		return nil, fmt.Errorf("sources: decrypt: %w", err)
	}
	return plaintext, nil
}
