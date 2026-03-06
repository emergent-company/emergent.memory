package agents

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// WebhookTokenPrefix helps identify webhook tokens
	WebhookTokenPrefix = "whk_"
	// WebhookTokenBytes is the number of random bytes in a token
	WebhookTokenBytes = 32
)

// GenerateWebhookToken creates a new secure random token for a webhook hook.
// It returns the plaintext token.
func GenerateWebhookToken() (string, error) {
	bytes := make([]byte, WebhookTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return WebhookTokenPrefix + hex.EncodeToString(bytes), nil
}

// HashWebhookToken creates a bcrypt hash of the plaintext token.
func HashWebhookToken(token string) (string, error) {
	// Use bcrypt's default cost (usually 10)
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash token: %w", err)
	}
	return string(hash), nil
}

// VerifyWebhookToken checks if the provided plaintext token matches the stored hash.
func VerifyWebhookToken(token, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token))
	return err == nil
}
