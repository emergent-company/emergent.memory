package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCommand_Flags(t *testing.T) {
	cmd := NewRootCommand()
	require.NotNil(t, cmd, "root command should not be nil")

	// Verify global flags are registered
	serverFlag := cmd.PersistentFlags().Lookup("server")
	assert.NotNil(t, serverFlag, "--server flag should be registered")
	assert.Equal(t, "string", serverFlag.Value.Type(), "--server should be a string flag")

	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag, "--output flag should be registered")
	assert.Equal(t, "string", outputFlag.Value.Type(), "--output should be a string flag")

	debugFlag := cmd.PersistentFlags().Lookup("debug")
	assert.NotNil(t, debugFlag, "--debug flag should be registered")
	assert.Equal(t, "bool", debugFlag.Value.Type(), "--debug should be a bool flag")

	noColorFlag := cmd.PersistentFlags().Lookup("no-color")
	assert.NotNil(t, noColorFlag, "--no-color flag should be registered")
	assert.Equal(t, "bool", noColorFlag.Value.Type(), "--no-color should be a bool flag")
}

func TestRootCommand_Execution(t *testing.T) {
	cmd := NewRootCommand()
	require.NotNil(t, cmd, "root command should not be nil")

	// Execute with no arguments should not error (shows help)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err, "executing root command should not error")
}

func TestRootCommand_ViperBinding(t *testing.T) {
	cmd := NewRootCommand()
	require.NotNil(t, cmd, "root command should not be nil")

	cmd.SetArgs([]string{"--server", "https://test.example.com", "--debug"})
	err := cmd.ParseFlags([]string{"--server", "https://test.example.com", "--debug"})
	require.NoError(t, err, "parsing flags should not error")

	serverFlag := cmd.PersistentFlags().Lookup("server")
	require.NotNil(t, serverFlag)
	assert.Equal(t, "https://test.example.com", serverFlag.Value.String())

	debugFlag := cmd.PersistentFlags().Lookup("debug")
	require.NotNil(t, debugFlag)
	assert.Equal(t, "true", debugFlag.Value.String())
}
