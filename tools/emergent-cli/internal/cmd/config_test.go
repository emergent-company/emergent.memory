package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigSetServer(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := config.Config{
		ServerURL: "https://old.example.com",
		Email:     "test@example.com",
	}
	err := config.Save(&cfg, configPath)
	require.NoError(t, err)

	cmd := newConfigSetServerCmd()
	cmd.SetArgs([]string{"https://new.example.com", "--config", configPath})

	capture := testutil.CaptureOutput()
	err = cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "https://new.example.com")

	loadedCfg, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", loadedCfg.ServerURL)
	assert.Equal(t, "test@example.com", loadedCfg.Email)
}

func TestConfigSetCredentials(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := config.Config{
		ServerURL: "https://api.example.com",
		Email:     "old@example.com",
	}
	err := config.Save(&cfg, configPath)
	require.NoError(t, err)

	cmd := newConfigSetCredentialsCmd()
	cmd.SetArgs([]string{"new@example.com", "--config", configPath})

	capture := testutil.CaptureOutput()
	err = cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "new@example.com")

	loadedCfg, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", loadedCfg.ServerURL)
	assert.Equal(t, "new@example.com", loadedCfg.Email)
}

func TestConfigShow(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := config.Config{
		ServerURL: "https://api.example.com",
		Email:     "user@example.com",
		OrgID:     "org-123",
		ProjectID: "proj-456",
		Debug:     true,
	}
	err := config.Save(&cfg, configPath)
	require.NoError(t, err)

	cmd := newConfigShowCmd()
	cmd.SetArgs([]string{"--config", configPath})

	capture := testutil.CaptureOutput()
	err = cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "https://api.example.com")
	assert.Contains(t, stdout, "user@example.com")
	assert.Contains(t, stdout, "org-123")
	assert.Contains(t, stdout, "proj-456")
}

func TestConfigLogout(t *testing.T) {
	tempDir := t.TempDir()
	credsPath := filepath.Join(tempDir, "credentials.json")

	err := os.WriteFile(credsPath, []byte(`{"access_token":"test"}`), 0600)
	require.NoError(t, err)

	require.FileExists(t, credsPath)

	cmd := newConfigLogoutCmd()
	cmd.SetArgs([]string{"--credentials-path", credsPath})

	capture := testutil.CaptureOutput()
	err = cmd.Execute()
	require.NoError(t, err)
	stdout, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	assert.Contains(t, stdout, "Logged out")

	_, err = os.Stat(credsPath)
	assert.True(t, os.IsNotExist(err), "credentials file should be deleted")
}
