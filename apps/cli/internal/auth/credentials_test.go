package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialsLoad(t *testing.T) {
	tempDir := t.TempDir()
	credPath := filepath.Join(tempDir, "credentials.json")

	content := `{
  "access_token": "test-access-token",
  "refresh_token": "test-refresh-token",
  "expires_at": "2025-12-31T23:59:59Z"
}`

	err := os.WriteFile(credPath, []byte(content), 0600)
	require.NoError(t, err)

	creds, err := Load(credPath)
	require.NoError(t, err)

	assert.Equal(t, "test-access-token", creds.AccessToken)
	assert.Equal(t, "test-refresh-token", creds.RefreshToken)
	assert.False(t, creds.ExpiresAt.IsZero())
}

func TestCredentialsSave(t *testing.T) {
	tempDir := t.TempDir()
	credPath := filepath.Join(tempDir, "credentials.json")

	expiresAt, _ := time.Parse(time.RFC3339, "2025-12-31T23:59:59Z")
	creds := &Credentials{
		AccessToken:  "saved-access-token",
		RefreshToken: "saved-refresh-token",
		ExpiresAt:    expiresAt,
	}

	err := Save(creds, credPath)
	require.NoError(t, err)

	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "should have 0600 permissions")

	data, err := os.ReadFile(credPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "saved-access-token")
	assert.Contains(t, content, "saved-refresh-token")
}

func TestCredentialsPermissionCheck(t *testing.T) {
	tempDir := t.TempDir()
	credPath := filepath.Join(tempDir, "credentials.json")

	content := `{"access_token": "test"}`
	err := os.WriteFile(credPath, []byte(content), 0644)
	require.NoError(t, err)

	_, err = Load(credPath)
	require.NoError(t, err)
}

func TestCredentialsNotFound(t *testing.T) {
	tempDir := t.TempDir()
	credPath := filepath.Join(tempDir, "does-not-exist.json")

	_, err := Load(credPath)
	assert.Error(t, err, "should error when file doesn't exist")
	assert.True(t, os.IsNotExist(err), "error should be not-exist error")
}

func TestCredentialsIsExpired_Expired(t *testing.T) {
	creds := &Credentials{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}

	assert.True(t, creds.IsExpired(), "should be expired")
}

func TestCredentialsIsExpired_AboutToExpire(t *testing.T) {
	creds := &Credentials{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(3 * time.Minute),
	}

	assert.True(t, creds.IsExpired(), "should be expired (within 5-minute buffer)")
}

func TestCredentialsIsExpired_Valid(t *testing.T) {
	creds := &Credentials{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(30 * time.Minute),
	}

	assert.False(t, creds.IsExpired(), "should not be expired")
}

func TestCredentialsIsExpired_ZeroTime(t *testing.T) {
	creds := &Credentials{
		AccessToken: "test",
		ExpiresAt:   time.Time{},
	}

	assert.True(t, creds.IsExpired(), "zero time should be considered expired")
}
