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
	cmd := serverUpgradeCmd

	assert.Equal(t, "upgrade", cmd.Use)
	assert.Contains(t, cmd.Short, "standalone server")
	assert.Contains(t, cmd.Long, "Docker images")

	dirFlag := cmd.Flag("dir")
	require.NotNil(t, dirFlag)

	forceFlag := cmd.Flag("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestServerCommand_HasUpgradeSubcommand(t *testing.T) {
	found := false
	for _, subcmd := range serverCmd.Commands() {
		if subcmd.Name() == "upgrade" {
			found = true
			break
		}
	}
	assert.True(t, found, "server should have 'upgrade' subcommand")
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

func TestIsMainReleaseTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"v0.44.0", true},
		{"v0.43.0", true},
		{"v0.41.149", true},
		{"v0.1.0-rc.1", false},
		{"v0.1.0+meta", false},
		{"apps/server/pkg/sdk/v0.41.149", false},
		{"cli-v1.0.0", false},
		{"0.44.0", false},
		{"v1.0", false},
		{"v1.0.0.1", false},
		{"v1.0.x", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isMainReleaseTag(c.tag), "isMainReleaseTag(%q)", c.tag)
	}
}

func TestPickHighestTag(t *testing.T) {
	assert.Equal(t, "v0.44.0", pickHighestTag([]string{"v0.41.149", "v0.43.0", "v0.44.0", "v0.42.0"}))
	assert.Equal(t, "v0.43.0", pickHighestTag([]string{"v0.43.0", "v0.41.149"}))
	assert.Equal(t, "v0.41.149", pickHighestTag([]string{"v0.41.149"}))
	assert.Equal(t, "", pickHighestTag(nil))
}
