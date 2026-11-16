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
//
// The write is atomic (temp file + rename in the same directory): this file is
// the ONLY copy of the refresh token, so a crash, a full disk, or a concurrent
// reader during a truncating write would be an unrecoverable sign-out. A reader
// therefore sees either the previous file or the complete new one, never a
// partial document.
func SaveCredentials(creds *Credentials, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp credentials file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best effort: a successful rename removes the temp name, so this only
	// fires on an error path.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to restrict temp credentials permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write temp credentials: %w", err)
	}
	// Flush before the rename so a power loss cannot leave the rename durable
	// while the contents are not.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync temp credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp credentials: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace credentials file: %w", err)
	}
	return nil
}
