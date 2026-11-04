package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials represents stored OAuth credentials.
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserEmail    string    `json:"user_email,omitempty"`
	IssuerURL    string    `json:"issuer_url,omitempty"`
}

// IsExpired checks if the access token is expired or close to expiring.
func (c *Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return true
	}

	// 5 minute buffer to refresh before actual expiration
	bufferTime := 5 * time.Minute
	return time.Now().Add(bufferTime).After(c.ExpiresAt)
}

// LoadCredentials loads credentials from a file.
func LoadCredentials(path string) (*Credentials, error) {
	// Check file permissions
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	perms := info.Mode().Perm()
	if perms&0077 != 0 {
		fmt.Fprintf(os.Stderr, "Warning: credentials file %s has insecure permissions %o (should be 0600)\n", path, perms)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	creds := &Credentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return nil, err
	}

	return creds, nil
}

// SaveCredentials saves credentials to a file with secure permissions.
func SaveCredentials(creds *Credentials, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
