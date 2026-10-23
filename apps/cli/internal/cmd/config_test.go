package cmd

import (
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/config"
	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
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

// `config set` must not echo a credential to stdout: that output lands in
// terminal scrollback, screen shares and tee'd CI logs, so printing the value
// would defeat the 0600 config file on the command's normal path (#2123). The
// server_url/email/org_id/project_id cases are the negative control — the fix
// is a classification, not a mute, so non-secret values are still echoed.
func TestConfigSetDoesNotEchoSecretValues(t *testing.T) {
	// Deliberately implausible: .gitleaks.toml scans the repo, so a test value
	// must never look like a real credential.
	const sentinel = "test-api-key-do-not-use-0000"

	tests := []struct {
		name           string
		key            string
		value          string
		envLocalKey    bool
		mustContain    []string
		mustNotContain []string
	}{
		{
			name:           "api_key is classified as a secret",
			key:            "api_key",
			value:          sentinel,
			mustContain:    []string{"api_key updated (value hidden)", "Configuration saved to:"},
			mustNotContain: []string{sentinel},
		},
		{
			name:        "server_url still echoes its value",
			key:         "server_url",
			value:       "https://server.example.com:9999",
			mustContain: []string{"server_url updated to: https://server.example.com:9999"},
		},
		{
			name:        "email still echoes its value",
			key:         "email",
			value:       "user@example.com",
			mustContain: []string{"email updated to: user@example.com"},
		},
		{
			name:        "org_id still echoes its value",
			key:         "org_id",
			value:       "org-0000",
			mustContain: []string{"org_id updated to: org-0000"},
		},
		{
			name:        "project_id still echoes its value",
			key:         "project_id",
			value:       "proj-0000",
			mustContain: []string{"project_id updated to: proj-0000"},
		},
		{
			// The .env.local branch already named the key only; this pins that
			// the config.yaml change did not make the two branches diverge the
			// other way.
			name:           "google_api_key .env.local branch names the key only",
			key:            "google_api_key",
			value:          sentinel,
			envLocalKey:    true,
			mustContain:    []string{"google_api_key updated in .env.local"},
			mustNotContain: []string{sentinel},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			args := []string{tt.key, tt.value}
			var configPath string
			if tt.envLocalKey {
				writeEnvLocalFixture(t, home, "GOOGLE_API_KEY=old\n", 0o600)
			} else {
				configPath = filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, config.Save(&config.Config{ServerURL: "https://old.example.com"}, configPath))
				args = append(args, "--config", configPath)
			}

			cmd := newConfigSetCmd()
			cmd.SetArgs(args)

			capture := testutil.CaptureOutput()
			err := cmd.Execute()
			stdout, stderr, readErr := capture.Read()
			capture.Restore()

			require.NoError(t, readErr)
			require.NoError(t, err, "stderr: %s", stderr)

			for _, want := range tt.mustContain {
				assert.Contains(t, stdout, want)
			}
			for _, notWant := range tt.mustNotContain {
				assert.NotContains(t, stdout, notWant)
			}

			// Masking is display-only: the credential must still be stored.
			if !tt.envLocalKey {
				loadedCfg, loadErr := config.Load(configPath)
				require.NoError(t, loadErr)
				switch tt.key {
				case "api_key":
					assert.Equal(t, tt.value, loadedCfg.APIKey)
				case "server_url":
					assert.Equal(t, tt.value, loadedCfg.ServerURL)
				case "email":
					assert.Equal(t, tt.value, loadedCfg.Email)
				case "org_id":
					assert.Equal(t, tt.value, loadedCfg.OrgID)
				case "project_id":
					assert.Equal(t, tt.value, loadedCfg.ProjectID)
				}
			}
		})
	}
}

// A new settable config.yaml key must be classified deliberately. The echo
// policy is only safe while every key is either in secretConfigKeys or on the
// known-non-secret list below; a credential added to configYAMLKeys alone would
// silently start being printed again.
func TestConfigSetKeyClassificationIsComplete(t *testing.T) {
	classified := map[string]bool{}
	for key := range secretConfigKeys {
		classified[key] = true
	}
	for _, key := range []string{
		"server_url",
		"email",
		"org_id",
		"project_id",
		"auto_update_enabled",
		"auto_update_mode",
		"auto_update_check_interval",
	} {
		require.NotContains(t, classified, key, "%q cannot be both secret and non-secret", key)
		classified[key] = false
	}

	for key := range configYAMLKeys {
		assert.Contains(t, classified, key,
			"config.yaml key %q is neither in secretConfigKeys nor the non-secret list: decide whether it is a credential before it is echoed", key)
	}
	assert.Len(t, classified, len(configYAMLKeys))
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
