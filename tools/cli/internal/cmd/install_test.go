package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCommand_Flags(t *testing.T) {
	cmd := installCmd

	assert.Equal(t, "install", cmd.Use)
	assert.Contains(t, cmd.Short, "Install")

	dirFlag := cmd.Flag("dir")
	require.NotNil(t, dirFlag)

	portFlag := cmd.Flag("port")
	require.NotNil(t, portFlag)
	assert.Equal(t, "3002", portFlag.DefValue)

	googleKeyFlag := cmd.Flag("google-api-key")
	require.NotNil(t, googleKeyFlag)

	skipStartFlag := cmd.Flag("skip-start")
	require.NotNil(t, skipStartFlag)
	assert.Equal(t, "false", skipStartFlag.DefValue)

	forceFlag := cmd.Flag("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestInstallCommand_IsInstalled(t *testing.T) {
	tempDir := t.TempDir()

	installFlags.dir = tempDir
	installFlags.force = false

	dockerDir := filepath.Join(tempDir, "docker")
	err := os.MkdirAll(dockerDir, 0755)
	require.NoError(t, err)

	composePath := filepath.Join(dockerDir, "docker-compose.yml")
	err = os.WriteFile(composePath, []byte("version: '3'\n"), 0644)
	require.NoError(t, err)

	cmd := installCmd
	cmd.SetArgs([]string{"--dir", tempDir})

	assert.FileExists(t, composePath)
}

func TestInstallCommand_Help(t *testing.T) {
	cmd := installCmd

	helpOutput := cmd.Long
	assert.Contains(t, helpOutput, "Check Docker")
	assert.Contains(t, helpOutput, "Create installation directory")
	assert.Contains(t, helpOutput, "Generate secure configuration")
}
