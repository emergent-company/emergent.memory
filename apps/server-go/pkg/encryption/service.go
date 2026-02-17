// Package encryption provides encryption and decryption for sensitive data
// using PostgreSQL pgcrypto extension.
//
// This matches the NestJS EncryptionService implementation which uses
// pgp_sym_encrypt/pgp_sym_decrypt functions.
package encryption

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/logger"
)

// Common errors
var (
	ErrKeyNotConfigured = errors.New("encryption key not configured")
	ErrDecryptionFailed = errors.New("failed to decrypt data")
)

// Service provides encryption and decryption using PostgreSQL pgcrypto.
// It uses pgp_sym_encrypt/pgp_sym_decrypt for AES-256 encryption.
type Service struct {
	db  *bun.DB
	log *slog.Logger
	key string
}

// NewService creates a new encryption service.
// It reads the encryption key from INTEGRATION_ENCRYPTION_KEY environment variable.
func NewService(db *bun.DB, log *slog.Logger) *Service {
	key := os.Getenv("INTEGRATION_ENCRYPTION_KEY")
	svc := &Service{
		db:  db,
		log: log.With(logger.Scope("encryption")),
		key: key,
	}

	// Validate encryption key
	env := os.Getenv("GO_ENV")
	if key == "" {
		if env == "production" {
			svc.log.Error("INTEGRATION_ENCRYPTION_KEY is required in production")
		} else if env != "test" {
			svc.log.Warn("INTEGRATION_ENCRYPTION_KEY not set - credentials will NOT be encrypted")
		}
	} else if len(key) < 32 {
		if env == "production" {
			svc.log.Error("INTEGRATION_ENCRYPTION_KEY is too short for AES-256",
				slog.Int("length", len(key)))
		} else {
			svc.log.Warn("INTEGRATION_ENCRYPTION_KEY is short for AES-256",
				slog.Int("length", len(key)))
		}
	}

	return svc
}

// IsConfigured returns true if encryption is properly configured
func (s *Service) IsConfigured() bool {
	return s.key != "" && len(s.key) >= 32
}

// Encrypt encrypts a map of settings using PostgreSQL pgcrypto.
// Returns base64-encoded encrypted data.
func (s *Service) Encrypt(ctx context.Context, settings map[string]interface{}) (string, error) {
	if s.key == "" {
		s.log.Warn("Encryption key not set - storing as plain JSON (INSECURE)")
		data, err := json.Marshal(settings)
		if err != nil {
			return "", fmt.Errorf("failed to marshal settings: %w", err)
		}
		return string(data), nil
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("failed to marshal settings: %w", err)
	}

	var encrypted string
	err = s.db.NewRaw(`
		SELECT encode(
			pgp_sym_encrypt(?::text, ?::text),
			'base64'
		) as encrypted
	`, string(settingsJSON), s.key).Scan(ctx, &encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt: %w", err)
	}

	return encrypted, nil
}

// Decrypt decrypts encrypted settings using PostgreSQL pgcrypto.
// Supports base64-encoded encrypted data from pgp_sym_encrypt.
func (s *Service) Decrypt(ctx context.Context, encryptedData string) (map[string]interface{}, error) {
	if encryptedData == "" {
		return make(map[string]interface{}), nil
	}

	if s.key == "" {
		// No encryption key - data is stored as plain JSON
		s.log.Debug("Decrypting without key - assuming plain JSON")
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(encryptedData), &settings); err != nil {
			s.log.Warn("Failed to parse unencrypted settings as JSON",
				slog.String("error", err.Error()))
			return make(map[string]interface{}), nil
		}
		return settings, nil
	}

	// Try pgp_sym_decrypt first (new method)
	var decrypted string
	err := s.db.NewRaw(`
		SELECT pgp_sym_decrypt(decode(?, 'base64'), ?::text) as decrypted
	`, encryptedData, s.key).Scan(ctx, &decrypted)

	if err != nil {
		// Try legacy decrypt method for backwards compatibility
		s.log.Warn("pgp_sym_decrypt failed, trying legacy method",
			slog.String("error", err.Error()))

		err = s.db.NewRaw(`
			SELECT convert_from(
				decrypt(decode(?, 'base64'), digest(?, 'sha256'), 'aes-cbc'),
				'utf-8'
			) as decrypted
		`, encryptedData, s.key).Scan(ctx, &decrypted)

		if err != nil {
			s.log.Error("Failed to decrypt with both methods",
				slog.String("error", err.Error()))
			return nil, ErrDecryptionFailed
		}
		s.log.Debug("Successfully decrypted using legacy method")
	}

	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decrypted settings: %w", err)
	}

	return settings, nil
}

// DecryptBytes decrypts encrypted settings stored as bytes/bytea.
// This handles the case where PostgreSQL returns BYTEA directly.
func (s *Service) DecryptBytes(ctx context.Context, data []byte) (map[string]interface{}, error) {
	if len(data) == 0 {
		return make(map[string]interface{}), nil
	}

	// Convert to string and use standard decrypt
	return s.Decrypt(ctx, string(data))
}

// EncryptJSON encrypts any JSON-serializable value
func (s *Service) EncryptJSON(ctx context.Context, value interface{}) (string, error) {
	settings := make(map[string]interface{})
	
	// If value is already a map, use it directly
	if m, ok := value.(map[string]interface{}); ok {
		settings = m
	} else {
		// Otherwise, wrap it
		settings["value"] = value
	}
	
	return s.Encrypt(ctx, settings)
}

// Module provides the fx module for the encryption service
func Module() interface{} {
	return NewService
}

// Ensure Service implements a decrypter interface
type Decrypter interface {
	Decrypt(ctx context.Context, encryptedData string) (map[string]interface{}, error)
	IsConfigured() bool
}

var _ Decrypter = (*Service)(nil)

// NullService is a no-op encryption service for testing
type NullService struct{}

// NewNullService creates a null encryption service
func NewNullService() *NullService {
	return &NullService{}
}

// Encrypt returns the settings as JSON (no encryption)
func (n *NullService) Encrypt(ctx context.Context, settings map[string]interface{}) (string, error) {
	data, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Decrypt parses JSON settings (no decryption)
func (n *NullService) Decrypt(ctx context.Context, data string) (map[string]interface{}, error) {
	if data == "" {
		return make(map[string]interface{}), nil
	}
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return make(map[string]interface{}), nil
	}
	return settings, nil
}

// IsConfigured always returns false for NullService
func (n *NullService) IsConfigured() bool {
	return false
}

// Ensure NullService implements Decrypter
var _ Decrypter = (*NullService)(nil)

// TransactionDecrypt decrypts within an existing transaction
func (s *Service) TransactionDecrypt(ctx context.Context, tx bun.Tx, encryptedData string) (map[string]interface{}, error) {
	if encryptedData == "" {
		return make(map[string]interface{}), nil
	}

	if s.key == "" {
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(encryptedData), &settings); err != nil {
			return make(map[string]interface{}), nil
		}
		return settings, nil
	}

	var decrypted string
	err := tx.NewRaw(`
		SELECT pgp_sym_decrypt(decode(?, 'base64'), ?::text) as decrypted
	`, encryptedData, s.key).Scan(ctx, &decrypted)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return make(map[string]interface{}), nil
		}
		return nil, ErrDecryptionFailed
	}

	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decrypted settings: %w", err)
	}

	return settings, nil
}
