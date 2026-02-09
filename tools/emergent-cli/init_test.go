package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectStructure(t *testing.T) {
	projectRoot := "."

	goModPath := filepath.Join(projectRoot, "go.mod")
	_, err := os.Stat(goModPath)
	require.NoError(t, err, "go.mod should exist")

	data, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "github.com/emergent-company/emergent/tools/emergent-cli",
		"go.mod should have correct module path")

	requiredDirs := []string{
		"cmd",
		"internal",
		"internal/testutil",
		"internal/config",
		"internal/auth",
		"internal/client",
		"internal/cmd",
		"internal/installer",
	}

	for _, dir := range requiredDirs {
		dirPath := filepath.Join(projectRoot, dir)
		info, err := os.Stat(dirPath)
		require.NoError(t, err, "directory %s should exist", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}

	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	_, err = os.Stat(gitignorePath)
	require.NoError(t, err, ".gitignore should exist")

	gitignoreData, err := os.ReadFile(gitignorePath)
	require.NoError(t, err)
	gitignoreContent := string(gitignoreData)

	assert.Contains(t, gitignoreContent, "emergent-cli", "should ignore binary")
	assert.Contains(t, gitignoreContent, "*.test", "should ignore test binaries")
	assert.Contains(t, gitignoreContent, "coverage.out", "should ignore coverage files")
}
