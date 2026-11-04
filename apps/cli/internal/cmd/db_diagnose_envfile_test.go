package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readDiagDSNFromEnvLocal builds a DSN for psql from the installer's env file, so
// it must read the credentials the way the service does. A password the installer
// quoted because it contains a "#" used to reach the DSN with its quotes on and
// the password truncated at the hash (#2174).
func TestReadDiagDSNFromEnvLocalUnquotesThePassword(t *testing.T) {
	installDir := t.TempDir()
	configDir := filepath.Join(installDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env.local"), []byte(
		"POSTGRES_USER=emergent\n"+
			"POSTGRES_PASSWORD='p@ss #1'\n"+
			"POSTGRES_DB=emergent\n"+
			"POSTGRES_PORT=5432\n"), 0o600))

	previous := dbDiagnoseFlags.installDir
	dbDiagnoseFlags.installDir = installDir
	t.Cleanup(func() { dbDiagnoseFlags.installDir = previous })

	dsn := readDiagDSNFromEnvLocal()

	require.NotEmpty(t, dsn)
	assert.True(t, strings.Contains(dsn, "p@ss #1@"), "the password must be whole and unquoted, got %q", dsn)
	assert.False(t, strings.Contains(dsn, "'"), "no delimiter may survive into the DSN, got %q", dsn)
}

func TestReadDiagDSNFromEnvLocalDefaultsAndMissingPassword(t *testing.T) {
	installDir := t.TempDir()
	configDir := filepath.Join(installDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	previous := dbDiagnoseFlags.installDir
	dbDiagnoseFlags.installDir = installDir
	t.Cleanup(func() { dbDiagnoseFlags.installDir = previous })

	assert.Empty(t, readDiagDSNFromEnvLocal(), "no password means no DSN")

	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env.local"),
		[]byte("export POSTGRES_USER=app # the app role\nPOSTGRES_PASSWORD=secret\n"), 0o600))
	assert.Equal(t, "postgres://app:secret@localhost:5432/emergent?sslmode=disable",
		readDiagDSNFromEnvLocal(), "the export prefix and the inline comment are not part of the value")
}
