package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLogoutTestServer creates a mock OIDC server with discovery and revocation
// endpoints. revokedTokens collects the tokens that were revoked. If revokedTokens
// is nil, no revocation endpoint is registered.
func newLogoutTestServer(t *testing.T, revokedTokens *[]string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]interface{}{
			"issuer":                        "https://auth.example.com",
			"device_authorization_endpoint": "https://auth.example.com/oauth/device_authorization",
			"token_endpoint":                "https://auth.example.com/oauth/token",
			"userinfo_endpoint":             "https://auth.example.com/oauth/userinfo",
			"revocation_endpoint":           ts.URL + "/oauth/v2/revoke",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(doc)
	})

	if revokedTokens != nil {
		mux.HandleFunc("/oauth/v2/revoke", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			*revokedTokens = append(*revokedTokens, r.FormValue("token_type_hint")+":"+r.FormValue("token"))
			w.WriteHeader(http.StatusOK)
		})
	}

	return ts
}

func writeCredsFile(t *testing.T, dir string, creds map[string]interface{}) string {
	t.Helper()
	credsPath := filepath.Join(dir, ".memory", "credentials.json")
	data, err := json.Marshal(creds)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(credsPath), 0700))
	require.NoError(t, os.WriteFile(credsPath, data, 0600))
	return credsPath
}

func TestLogout(t *testing.T) {
	// Reset package-level flag state
	logoutAll = false

	var revokedTokens []string
	server := newLogoutTestServer(t, &revokedTokens)
	defer server.Close()

	tempDir := t.TempDir()
	credsPath := writeCredsFile(t, tempDir, map[string]interface{}{
		"access_token":  "test-access",
		"refresh_token": "test-refresh",
		"issuer_url":    server.URL,
	})

	t.Setenv("HOME", tempDir)
	if os.PathSeparator == '\\' {
		t.Setenv("USERPROFILE", tempDir)
	}

	cmd := newLogoutCmd()

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "Logged out successfully")
	assert.Contains(t, stdout, "Tokens revoked server-side")
	assert.Contains(t, stdout, "OAuth credentials removed")

	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")

	// Verify both tokens were revoked (refresh first, then access)
	require.Len(t, revokedTokens, 2)
	assert.Equal(t, "refresh_token:test-refresh", revokedTokens[0])
	assert.Equal(t, "access_token:test-access", revokedTokens[1])
}

func TestLogoutAll(t *testing.T) {
	// Reset package-level flag state
	logoutAll = false

	var revokedTokens []string
	server := newLogoutTestServer(t, &revokedTokens)
	defer server.Close()

	tempDir := t.TempDir()
	credsPath := writeCredsFile(t, tempDir, map[string]interface{}{
		"access_token": "test-access",
		"issuer_url":   server.URL,
	})

	// Write a config file with api_key and project_token alongside other settings
	configDir := filepath.Join(tempDir, ".memory")
	configPath := filepath.Join(configDir, "config.yaml")
	configContent := `server_url: https://api.example.com
api_key: my-secret-key
project_token: emt_project123
project_id: proj-456
debug: false
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	t.Setenv("HOME", tempDir)
	t.Setenv("MEMORY_CONFIG", configPath)
	if os.PathSeparator == '\\' {
		t.Setenv("USERPROFILE", tempDir)
	}

	cmd := newLogoutCmd()
	cmd.SetArgs([]string{"--all"})

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "Logged out successfully")
	assert.Contains(t, stdout, "OAuth credentials removed")
	assert.Contains(t, stdout, "Cleared api_key from config")
	assert.Contains(t, stdout, "Cleared project_token from config")

	// Credentials file should be deleted
	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")

	// Config file should still exist but with cleared auth fields
	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	configStr := string(configData)
	// The config should retain server_url and project_id
	assert.Contains(t, configStr, "api.example.com")
	assert.Contains(t, configStr, "proj-456")
	// But api_key and project_token should be empty
	assert.NotContains(t, configStr, "my-secret-key")
	assert.NotContains(t, configStr, "emt_project123")
}

func TestLogoutNoIssuer(t *testing.T) {
	// Reset package-level flag state
	logoutAll = false

	tempDir := t.TempDir()
	// Credentials without issuer_url (like from set-token without a server)
	credsPath := writeCredsFile(t, tempDir, map[string]interface{}{
		"access_token": "test-access",
	})

	t.Setenv("HOME", tempDir)
	if os.PathSeparator == '\\' {
		t.Setenv("USERPROFILE", tempDir)
	}

	cmd := newLogoutCmd()

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	require.NoError(t, err)
	stdout, stderr, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	// Should warn about skipping revocation
	assert.Contains(t, stderr, "no issuer URL")

	// Should still delete credentials
	assert.Contains(t, stdout, "Logged out successfully")
	assert.Contains(t, stdout, "OAuth credentials removed")

	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")
}

func TestLogoutRevocationFails(t *testing.T) {
	// Reset package-level flag state
	logoutAll = false

	// Create a mock server that returns 500 for revocation
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]interface{}{
			"issuer":                        "https://auth.example.com",
			"device_authorization_endpoint": "https://auth.example.com/oauth/device_authorization",
			"token_endpoint":                "https://auth.example.com/oauth/token",
			"userinfo_endpoint":             "https://auth.example.com/oauth/userinfo",
			"revocation_endpoint":           server.URL + "/oauth/v2/revoke",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/oauth/v2/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	tempDir := t.TempDir()
	credsPath := writeCredsFile(t, tempDir, map[string]interface{}{
		"access_token":  "test-access",
		"refresh_token": "test-refresh",
		"issuer_url":    server.URL,
	})

	t.Setenv("HOME", tempDir)
	if os.PathSeparator == '\\' {
		t.Setenv("USERPROFILE", tempDir)
	}

	cmd := newLogoutCmd()

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	require.NoError(t, err)
	stdout, stderr, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	// Should warn about failed revocation
	assert.Contains(t, stderr, "failed to revoke")

	// Should still delete credentials despite revocation failure
	assert.Contains(t, stdout, "Logged out successfully")
	assert.Contains(t, stdout, "OAuth credentials removed")

	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")
}
