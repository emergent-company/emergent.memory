package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCtlCommand_Structure(t *testing.T) {
	cmd := ctlCmd

	assert.Equal(t, "ctl", cmd.Use)
	assert.Contains(t, cmd.Short, "Control")

	dirFlag := cmd.PersistentFlags().Lookup("dir")
	require.NotNil(t, dirFlag)
}

func TestCtlCommand_Subcommands(t *testing.T) {
	subcommands := []string{"start", "stop", "restart", "status", "logs", "health", "shell", "pull"}

	for _, name := range subcommands {
		t.Run(name, func(t *testing.T) {
			found := false
			for _, subcmd := range ctlCmd.Commands() {
				if subcmd.Use == name || subcmd.Name() == name {
					found = true
					break
				}
			}
			assert.True(t, found, "subcommand %s should exist", name)
		})
	}
}

func TestCtlStartCommand(t *testing.T) {
	cmd := ctlStartCmd
	assert.Equal(t, "start", cmd.Use)
	assert.Contains(t, cmd.Short, "Start")
}

func TestCtlStopCommand(t *testing.T) {
	cmd := ctlStopCmd
	assert.Equal(t, "stop", cmd.Use)
	assert.Contains(t, cmd.Short, "Stop")
}

func TestCtlRestartCommand(t *testing.T) {
	cmd := ctlRestartCmd
	assert.Equal(t, "restart", cmd.Use)
	assert.Contains(t, cmd.Short, "Restart")
}

func TestCtlStatusCommand(t *testing.T) {
	cmd := ctlStatusCmd
	assert.Equal(t, "status", cmd.Use)
	assert.Contains(t, cmd.Short, "status")
}

func TestCtlLogsCommand(t *testing.T) {
	cmd := ctlLogsCmd
	assert.Equal(t, "logs [service]", cmd.Use)
	assert.Contains(t, cmd.Short, "logs")

	followFlag := cmd.Flag("follow")
	require.NotNil(t, followFlag)
	assert.Equal(t, "f", followFlag.Shorthand)

	linesFlag := cmd.Flag("lines")
	require.NotNil(t, linesFlag)
	assert.Equal(t, "n", linesFlag.Shorthand)
	assert.Equal(t, "100", linesFlag.DefValue)
}

func TestCtlHealthCommand(t *testing.T) {
	cmd := ctlHealthCmd
	assert.Equal(t, "health", cmd.Use)
	assert.Contains(t, cmd.Short, "health")
}

func TestCtlShellCommand(t *testing.T) {
	cmd := ctlShellCmd
	assert.Equal(t, "shell", cmd.Use)
	assert.Contains(t, cmd.Short, "shell")
}

func TestCtlPullCommand(t *testing.T) {
	cmd := ctlPullCmd
	assert.Equal(t, "pull", cmd.Use)
	assert.Contains(t, cmd.Short, "Pull")
}

func TestCtlCommand_NoInstallation(t *testing.T) {
	tempDir := t.TempDir()
	ctlFlags.dir = tempDir

	_, err := os.Stat(filepath.Join(tempDir, "docker", "docker-compose.yml"))
	assert.True(t, os.IsNotExist(err))
}

func TestCtlCommand_Help(t *testing.T) {
	cmd := ctlCmd

	helpOutput := cmd.Long
	assert.Contains(t, helpOutput, "start/stop/restart")
	assert.Contains(t, helpOutput, "view service status")
	assert.Contains(t, helpOutput, "check server health")
}
