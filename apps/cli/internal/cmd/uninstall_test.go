package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUninstallCommand_Flags(t *testing.T) {
	cmd := uninstallCmd

	assert.Equal(t, "uninstall", cmd.Use)
	assert.Contains(t, cmd.Short, "Remove")

	dirFlag := cmd.Flag("dir")
	require.NotNil(t, dirFlag)

	keepDataFlag := cmd.Flag("keep-data")
	require.NotNil(t, keepDataFlag)
	assert.Equal(t, "false", keepDataFlag.DefValue)

	forceFlag := cmd.Flag("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestUninstallCommand_NoInstallation(t *testing.T) {
	tempDir := t.TempDir()

	uninstallFlags.dir = tempDir
	uninstallFlags.force = true

	_, err := os.Stat(filepath.Join(tempDir, "docker", "docker-compose.yml"))
	assert.True(t, os.IsNotExist(err))
}

func TestUninstallCommand_Help(t *testing.T) {
	cmd := uninstallCmd

	helpOutput := cmd.Long
	assert.Contains(t, helpOutput, "Stop and remove Docker containers")
	assert.Contains(t, helpOutput, "Remove Docker volumes")
	assert.Contains(t, helpOutput, "Remove installation directory")
}
