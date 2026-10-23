package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateEnvPassword rewrites a credential the user may have typed (or that was
// generated) into .env.local, so it must go through the shared writer: the old
// raw line replacement wrote the password verbatim, and a password containing a
// quote, a space or a "$" made the file read back as something else — or stop
// parsing altogether (#2148).
func TestUpdateEnvPasswordGoesThroughTheSharedWriter(t *testing.T) {
	installDir := t.TempDir()
	configDir := filepath.Join(installDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	envPath := filepath.Join(configDir, ".env.local")
	require.NoError(t, os.WriteFile(envPath, []byte("POSTGRES_PASSWORD=old\nOTHER=keep\n"), 0o644))

	// A bare value stops at " #" (godotenv reads that as an inline comment), so the
	// old raw writer silently truncated the password to "p@ss". The shared writer
	// single-quotes it and it reads back whole.
	const password = `p@ss #1`
	require.NoError(t, updateEnvPassword(installDir, password))

	parsed, err := godotenv.Read(envPath)
	require.NoError(t, err, "the file must still parse")
	assert.Equal(t, password, parsed["POSTGRES_PASSWORD"], "the password must read back exactly")
	assert.Equal(t, "keep", parsed["OTHER"], "other keys must be preserved")

	info, err := os.Stat(envPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the credential file must be 0600")
}

// A missing key is appended rather than silently ignored: the function exists to
// make the file agree with the database password.
func TestUpdateEnvPasswordAppendsAMissingKey(t *testing.T) {
	installDir := t.TempDir()
	configDir := filepath.Join(installDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	envPath := filepath.Join(configDir, ".env.local")
	require.NoError(t, os.WriteFile(envPath, []byte("OTHER=keep\n"), 0o600))

	require.NoError(t, updateEnvPassword(installDir, "rotated"))

	parsed, err := godotenv.Read(envPath)
	require.NoError(t, err)
	assert.Equal(t, "rotated", parsed["POSTGRES_PASSWORD"])
	assert.Equal(t, "keep", parsed["OTHER"])
}

// A value no quoting style can represent is refused with the file untouched,
// rather than written raw and left unreadable — the exact class the shared
// writer's guard exists for.
func TestUpdateEnvPasswordRefusesAnUnrepresentableValue(t *testing.T) {
	installDir := t.TempDir()
	configDir := filepath.Join(installDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	envPath := filepath.Join(configDir, ".env.local")
	const before = "POSTGRES_PASSWORD=old\n"
	require.NoError(t, os.WriteFile(envPath, []byte(before), 0o600))

	err := updateEnvPassword(installDir, `p@ss'w0rd $HOME`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be represented")

	content, readErr := os.ReadFile(envPath)
	require.NoError(t, readErr)
	assert.Equal(t, before, string(content), "a refusal must leave the file byte-identical")
}
