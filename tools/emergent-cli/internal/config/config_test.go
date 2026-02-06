package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigStruct(t *testing.T) {
	cfg := &Config{
		ServerURL: "http://localhost:3000",
		Email:     "test@example.com",
		OrgID:     "org-123",
		ProjectID: "proj-456",
		Debug:     true,
	}

	assert.Equal(t, "http://localhost:3000", cfg.ServerURL)
	assert.Equal(t, "test@example.com", cfg.Email)
	assert.Equal(t, "org-123", cfg.OrgID)
	assert.Equal(t, "proj-456", cfg.ProjectID)
	assert.True(t, cfg.Debug)
}

func TestConfigLoad_File(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `server_url: http://localhost:3001
email: loaded@example.com
org_id: org-loaded
project_id: proj-loaded
debug: true
`

	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:3001", cfg.ServerURL)
	assert.Equal(t, "loaded@example.com", cfg.Email)
	assert.Equal(t, "org-loaded", cfg.OrgID)
	assert.Equal(t, "proj-loaded", cfg.ProjectID)
	assert.True(t, cfg.Debug)
}

func TestConfigLoad_Defaults(t *testing.T) {
	nonExistentPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	cfg, err := Load(nonExistentPath)
	require.NoError(t, err)

	assert.NotEmpty(t, cfg.ServerURL, "should have default server URL")
	assert.False(t, cfg.Debug, "debug should default to false")
}

func TestConfigSave(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &Config{
		ServerURL: "http://saved.example.com",
		Email:     "saved@example.com",
		OrgID:     "org-saved",
		ProjectID: "proj-saved",
		Debug:     false,
	}

	err := Save(cfg, configPath)
	require.NoError(t, err)

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm(), "should have 0644 permissions")

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "server_url: http://saved.example.com")
	assert.Contains(t, content, "email: saved@example.com")
	assert.Contains(t, content, "org_id: org-saved")
}

func TestDiscoverPath_FlagProvided(t *testing.T) {
	tempDir := t.TempDir()
	flagPath := filepath.Join(tempDir, "flag-config.yaml")

	err := os.WriteFile(flagPath, []byte("test: flag"), 0644)
	require.NoError(t, err)

	discovered := DiscoverPath(flagPath)
	assert.Equal(t, flagPath, discovered, "should use flag-provided path")
}

func TestDiscoverPath_EnvVar(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "env-config.yaml")

	err := os.WriteFile(envPath, []byte("test: env"), 0644)
	require.NoError(t, err)

	t.Setenv("EMERGENT_CONFIG", envPath)

	discovered := DiscoverPath("")
	assert.Equal(t, envPath, discovered, "should use EMERGENT_CONFIG env var")
}

func TestDiscoverPath_Default(t *testing.T) {
	discovered := DiscoverPath("")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	expectedDefault := filepath.Join(homeDir, ".emergent", "config.yaml")
	assert.Equal(t, expectedDefault, discovered, "should fallback to default path")
}

func TestDiscoverPath_Precedence(t *testing.T) {
	tempDir := t.TempDir()

	flagPath := filepath.Join(tempDir, "flag.yaml")
	envPath := filepath.Join(tempDir, "env.yaml")

	err := os.WriteFile(flagPath, []byte("test: flag"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(envPath, []byte("test: env"), 0644)
	require.NoError(t, err)

	t.Setenv("EMERGENT_CONFIG", envPath)

	discovered := DiscoverPath(flagPath)
	assert.Equal(t, flagPath, discovered, "flag should take precedence over env var")
}

func TestLoadFromEnv_ServerURL(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `server_url: http://file.example.com
email: file@example.com
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	t.Setenv("EMERGENT_SERVER_URL", "http://env.example.com")

	cfg, err := LoadWithEnv(configPath)
	require.NoError(t, err)

	assert.Equal(t, "http://env.example.com", cfg.ServerURL, "env var should override file")
	assert.Equal(t, "file@example.com", cfg.Email, "non-overridden values should come from file")
}

func TestLoadFromEnv_Email(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `server_url: http://file.example.com
email: file@example.com
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	t.Setenv("EMERGENT_EMAIL", "env@example.com")

	cfg, err := LoadWithEnv(configPath)
	require.NoError(t, err)

	assert.Equal(t, "env@example.com", cfg.Email, "env var should override file")
	assert.Equal(t, "http://file.example.com", cfg.ServerURL, "non-overridden values should come from file")
}

func TestLoadFromEnv_Precedence(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	content := `server_url: http://file.example.com
email: file@example.com
debug: false
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	t.Setenv("EMERGENT_SERVER_URL", "http://env.example.com")
	t.Setenv("EMERGENT_DEBUG", "true")

	cfg, err := LoadWithEnv(configPath)
	require.NoError(t, err)

	assert.Equal(t, "http://env.example.com", cfg.ServerURL, "env should override file")
	assert.Equal(t, "file@example.com", cfg.Email, "file value when no env var")
	assert.True(t, cfg.Debug, "env var should override file for bool")
}
