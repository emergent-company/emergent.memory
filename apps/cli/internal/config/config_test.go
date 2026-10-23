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
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "should be created 0600")

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "server_url: http://saved.example.com")
	assert.Contains(t, content, "email: saved@example.com")
	assert.Contains(t, content, "org_id: org-saved")
}

// config.yaml can hold an account API key (`memory config set api_key`), so
// Save must never leave it world-readable. os.WriteFile applies its perm
// argument only on create, so an existing file needs an explicit tighten: Save
// does that for the default per-user path, and deliberately leaves an explicit
// --config path alone because that file may be shared between users.
func TestSaveFileModes(t *testing.T) {
	const existingContent = `server_url: https://old.example.com
api_key: old-key
email: old@example.com
`

	tests := []struct {
		name         string
		defaultPath  bool
		existingMode os.FileMode // 0 means the file does not exist yet
		wantMode     os.FileMode
	}{
		{name: "fresh default path is created 0600", defaultPath: true, wantMode: 0o600},
		{name: "existing 0644 default path is tightened to 0600", defaultPath: true, existingMode: 0o644, wantMode: 0o600},
		{name: "existing 0640 default path is tightened to 0600", defaultPath: true, existingMode: 0o640, wantMode: 0o600},
		{name: "existing 0600 default path stays 0600", defaultPath: true, existingMode: 0o600, wantMode: 0o600},
		{name: "fresh explicit path is created 0600", wantMode: 0o600},
		{name: "existing 0644 explicit path keeps its mode", existingMode: 0o644, wantMode: 0o644},
		{name: "existing 0600 explicit path keeps its mode", existingMode: 0o600, wantMode: 0o600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("MEMORY_CONFIG", "")

			path := filepath.Join(t.TempDir(), "shared-config.yaml")
			if tt.defaultPath {
				path = filepath.Join(home, ".memory", "config.yaml")
			}
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

			if tt.existingMode != 0 {
				require.NoError(t, os.WriteFile(path, []byte(existingContent), 0o600))
				require.NoError(t, os.Chmod(path, tt.existingMode))
			}

			// Callers Load, mutate, Save — reuse the existing file so the table
			// also covers content survival.
			cfg := &Config{}
			if tt.existingMode != 0 {
				loaded, err := Load(path)
				require.NoError(t, err)
				cfg = loaded
			}
			cfg.ServerURL = "https://new.example.com"
			cfg.APIKey = "new-key"
			require.NoError(t, Save(cfg, path))

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, info.Mode().Perm(), "mode on disk")

			loaded, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, "https://new.example.com", loaded.ServerURL)
			assert.Equal(t, "new-key", loaded.APIKey, "new content should be written")
			if tt.existingMode != 0 {
				assert.Equal(t, "old@example.com", loaded.Email, "unrelated keys should be preserved")
			}
		})
	}
}

// Tightening the default path must not drop keys Save did not touch.
func TestSaveTighteningDefaultPathPreservesContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".memory", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	existing := `server_url: https://keep.example.com
api_key: old-key
email: keep@example.com
org_id: org-keep
project_id: proj-keep
debug: true
cache:
    ttl: 15m
    enabled: false
query:
    default_limit: 25
    default_sort: name:asc
auto_update:
    enabled: false
    mode: auto
    check_interval: 1h
`
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))
	require.NoError(t, os.Chmod(path, 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	cfg.APIKey = "new-key"
	require.NoError(t, Save(cfg, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "default path should be tightened")

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "new-key", loaded.APIKey)
	assert.Equal(t, "https://keep.example.com", loaded.ServerURL)
	assert.Equal(t, "keep@example.com", loaded.Email)
	assert.Equal(t, "org-keep", loaded.OrgID)
	assert.Equal(t, "proj-keep", loaded.ProjectID)
	assert.True(t, loaded.Debug)
	assert.Equal(t, "15m", loaded.Cache.TTL)
	assert.False(t, loaded.Cache.Enabled)
	assert.Equal(t, 25, loaded.Query.DefaultLimit)
	assert.Equal(t, "name:asc", loaded.Query.DefaultSort)
	assert.False(t, loaded.AutoUpdate.Enabled)
	assert.Equal(t, "auto", loaded.AutoUpdate.Mode)
	assert.Equal(t, "1h", loaded.AutoUpdate.CheckInterval)
}

// A default-path config.yaml that is a symlink is left alone: chmod follows the
// link, so tightening it would change the mode of a target that may be shared.
func TestSaveLeavesSymlinkedDefaultPathModeAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".memory"), 0o700))

	target := filepath.Join(t.TempDir(), "shared.yaml")
	require.NoError(t, os.WriteFile(target, []byte("server_url: https://old.example.com\n"), 0o600))
	require.NoError(t, os.Chmod(target, 0o644))

	link := filepath.Join(home, ".memory", "config.yaml")
	require.NoError(t, os.Symlink(target, link))

	cfg := &Config{ServerURL: "https://new.example.com", APIKey: "new-key"}
	require.NoError(t, Save(cfg, link))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "symlink target mode should be untouched")

	loaded, err := Load(link)
	require.NoError(t, err)
	assert.Equal(t, "new-key", loaded.APIKey)
}

// The default path is compared via absolute, cleaned paths rather than raw
// strings, and never resolved through symlinks.
func TestIsDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaultPath := filepath.Join(home, ".memory", "config.yaml")

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "default path", path: defaultPath, want: true},
		{name: "default path with dot segments", path: filepath.Join(home, ".memory", ".", "..", ".memory", "config.yaml"), want: true},
		{name: "sibling file in .memory", path: filepath.Join(home, ".memory", "other.yaml"), want: false},
		{name: "same relative layout under another home", path: filepath.Join(home, "other", ".memory", "config.yaml"), want: false},
		{name: "explicit path", path: filepath.Join(t.TempDir(), "config.yaml"), want: false},
		{name: "empty path", path: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isDefaultPath(tt.path))
		})
	}
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

	t.Setenv("MEMORY_CONFIG", envPath)

	discovered := DiscoverPath("")
	assert.Equal(t, envPath, discovered, "should use MEMORY_CONFIG env var")
}

func TestDiscoverPath_Default(t *testing.T) {
	discovered := DiscoverPath("")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	expectedDefault := filepath.Join(homeDir, ".memory", "config.yaml")
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

	t.Setenv("MEMORY_CONFIG", envPath)

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

	t.Setenv("MEMORY_SERVER_URL", "http://env.example.com")

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

	t.Setenv("MEMORY_EMAIL", "env@example.com")

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

	t.Setenv("MEMORY_SERVER_URL", "http://env.example.com")
	t.Setenv("MEMORY_DEBUG", "true")

	cfg, err := LoadWithEnv(configPath)
	require.NoError(t, err)

	assert.Equal(t, "http://env.example.com", cfg.ServerURL, "env should override file")
	assert.Equal(t, "file@example.com", cfg.Email, "file value when no env var")
	assert.True(t, cfg.Debug, "env var should override file for bool")
}
