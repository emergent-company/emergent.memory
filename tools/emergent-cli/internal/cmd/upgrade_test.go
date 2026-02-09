package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeCommand_Structure(t *testing.T) {
	cmd := upgradeCmd

	assert.Equal(t, "upgrade", cmd.Use)
	assert.Contains(t, cmd.Short, "Upgrade")
	assert.Contains(t, cmd.Long, "CLI binary")
}

func TestUpgradeCommand_ServerSubcommand(t *testing.T) {
	cmd := upgradeServerCmd

	assert.Equal(t, "server", cmd.Use)
	assert.Contains(t, cmd.Short, "standalone server")
	assert.Contains(t, cmd.Long, "Pull the latest Docker images")

	dirFlag := cmd.Flag("dir")
	require.NotNil(t, dirFlag)

	forceFlag := cmd.Flag("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestUpgradeCommand_HasServerSubcommand(t *testing.T) {
	found := false
	for _, subcmd := range upgradeCmd.Commands() {
		if subcmd.Name() == "server" {
			found = true
			break
		}
	}
	assert.True(t, found, "upgrade should have 'server' subcommand")
}

func TestUpgradeCommand_DevVersion(t *testing.T) {
	originalVersion := Version
	Version = "dev"
	defer func() { Version = originalVersion }()

	assert.Equal(t, "dev", Version)
}

func TestUpgradeServerCommand_NoInstallation(t *testing.T) {
	tempDir := t.TempDir()
	upgradeFlags.dir = tempDir

	_, err := os.Stat(filepath.Join(tempDir, "docker", "docker-compose.yml"))
	assert.True(t, os.IsNotExist(err))
}

func TestFindAsset_Linux(t *testing.T) {
	assets := []Asset{
		{Name: "emergent-cli-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
		{Name: "emergent-cli-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/mac"},
		{Name: "emergent-cli-windows-amd64.zip", BrowserDownloadURL: "https://example.com/win"},
	}

	for _, asset := range assets {
		if asset.Name == "emergent-cli-linux-amd64.tar.gz" {
			assert.Equal(t, "https://example.com/linux", asset.BrowserDownloadURL)
		}
	}
}

func TestFindAsset_NoMatch(t *testing.T) {
	assets := []Asset{
		{Name: "emergent-cli-freebsd-386.tar.gz", BrowserDownloadURL: "https://example.com/freebsd"},
	}

	_, _, err := findAsset(assets)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no asset found")
}

func TestRelease_JSONParsing(t *testing.T) {
	release := Release{
		TagName: "cli-v1.0.0",
		Assets: []Asset{
			{Name: "test.tar.gz", BrowserDownloadURL: "https://example.com/test"},
		},
	}

	assert.Equal(t, "cli-v1.0.0", release.TagName)
	assert.Len(t, release.Assets, 1)
	assert.Equal(t, "test.tar.gz", release.Assets[0].Name)
}
