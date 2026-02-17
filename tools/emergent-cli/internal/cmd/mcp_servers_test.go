package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvVars(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		expected  map[string]any
		expectErr bool
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: nil,
		},
		{
			name:  "single env var",
			input: []string{"API_KEY=abc123"},
			expected: map[string]any{
				"API_KEY": "abc123",
			},
		},
		{
			name:  "multiple env vars",
			input: []string{"API_KEY=abc123", "SECRET=xyz"},
			expected: map[string]any{
				"API_KEY": "abc123",
				"SECRET":  "xyz",
			},
		},
		{
			name:  "value with equals sign",
			input: []string{"URL=http://example.com?foo=bar"},
			expected: map[string]any{
				"URL": "http://example.com?foo=bar",
			},
		},
		{
			name:  "empty value",
			input: []string{"EMPTY="},
			expected: map[string]any{
				"EMPTY": "",
			},
		},
		{
			name:      "invalid format - no equals",
			input:     []string{"INVALID"},
			expectErr: true,
		},
		{
			name:      "invalid format mixed",
			input:     []string{"GOOD=val", "BAD"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseEnvVars(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestMCPServersCommandStructure(t *testing.T) {
	// Test that all subcommands are registered
	subcommands := mcpServersCmd.Commands()
	subcommandNames := make(map[string]bool)
	for _, cmd := range subcommands {
		subcommandNames[cmd.Name()] = true
	}

	expected := []string{"list", "get", "create", "delete", "sync", "inspect", "tools"}
	for _, name := range expected {
		assert.True(t, subcommandNames[name], "expected subcommand %q to be registered", name)
	}
}

func TestMCPServersGetRequiresArg(t *testing.T) {
	cmd := getMCPServerCmd
	assert.NotNil(t, cmd.Args, "get command should require args")
}

func TestMCPServersDeleteRequiresArg(t *testing.T) {
	cmd := deleteMCPServerCmd
	assert.NotNil(t, cmd.Args, "delete command should require args")
}

func TestMCPServersSyncRequiresArg(t *testing.T) {
	cmd := syncMCPServerCmd
	assert.NotNil(t, cmd.Args, "sync command should require args")
}

func TestMCPServersInspectRequiresArg(t *testing.T) {
	cmd := inspectMCPServerCmd
	assert.NotNil(t, cmd.Args, "inspect command should require args")
}

func TestMCPServersToolsRequiresArg(t *testing.T) {
	cmd := toolsMCPServerCmd
	assert.NotNil(t, cmd.Args, "tools command should require args")
}

func TestMCPServersCreateFlags(t *testing.T) {
	cmd := createMCPServerCmd

	// Test that required flags are defined
	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag, "name flag should be defined")

	typeFlag := cmd.Flags().Lookup("type")
	require.NotNil(t, typeFlag, "type flag should be defined")

	urlFlag := cmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag, "url flag should be defined")

	commandFlag := cmd.Flags().Lookup("command")
	require.NotNil(t, commandFlag, "command flag should be defined")

	argsFlag := cmd.Flags().Lookup("args")
	require.NotNil(t, argsFlag, "args flag should be defined")

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag, "env flag should be defined")

	enabledFlag := cmd.Flags().Lookup("enabled")
	require.NotNil(t, enabledFlag, "enabled flag should be defined")
}

func TestMCPServersRootCommand(t *testing.T) {
	// Verify the mcp-servers command is registered on root
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "mcp-servers" {
			found = true
			break
		}
	}
	assert.True(t, found, "mcp-servers command should be registered on root")
}
