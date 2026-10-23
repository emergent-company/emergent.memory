package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

// .env.local holds a live emt_... credential, so every writer must create it
// 0600 and tighten an existing over-permissive file (#2081). These tests assert
// the mode on disk rather than on a mocked filesystem; like the sibling
// create-token test (TestWriteTokenToEnvLocal) they assert unconditionally
// because apps/cli tests run on Linux.

const (
	envTestProjectID   = "11111111-2222-3333-4444-555555555555"
	envTestProjectName = "demo-project"
	envTestToken       = "emt_live_token_do_not_commit"
)

func TestSetProjectEnvLocal(t *testing.T) {
	tests := []struct {
		name     string
		existing []byte
	}{
		{name: "creates a fresh file with mode 0600"},
		{
			name:     "keeps other keys and tightens an existing 0644 file",
			existing: []byte("MEMORY_PROJECT_API_KEY=emt_old\nOTHER_KEY=keep-me\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".env.local")
			if tt.existing != nil {
				assert.NoError(t, os.WriteFile(path, tt.existing, 0o644))
				assert.NoError(t, os.Chmod(path, 0o644))
			}

			var out bytes.Buffer
			got, err := setProjectEnvLocal(&out, dir, envTestProjectID, envTestProjectName, envTestToken)
			assert.NoError(t, err)
			assert.True(t, filepath.IsAbs(got))
			assert.Equal(t, path, got)

			info, err := os.Stat(path)
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			env, err := godotenv.Read(path)
			assert.NoError(t, err)
			assert.Equal(t, envTestProjectID, env["MEMORY_PROJECT_ID"])
			assert.Equal(t, envTestProjectName, env["MEMORY_PROJECT_NAME"])
			assert.Equal(t, envTestToken, env["MEMORY_PROJECT_API_KEY"])

			if tt.existing != nil {
				assert.Equal(t, "keep-me", env["OTHER_KEY"], "unrelated keys must survive the rewrite")
			}

			assert.Equal(t, 1, strings.Count(out.String(), got), "the absolute path must be printed exactly once")
			assert.Contains(t, out.String(), got+" (mode 0600)")
		})
	}
}

// Both paths now share the reader, so neither can silently drop the keys of a
// file it cannot parse (#2075 decision, extended here).
func TestWriteProjectEnvLocalRefusesUnparseableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	original := []byte("KEY=\"unterminated\n")
	assert.NoError(t, os.WriteFile(path, original, 0o600))

	var out bytes.Buffer
	_, err := writeProjectEnvLocal(&out, dir, envTestProjectID, envTestProjectName, envTestToken, nil)
	assert.Error(t, err)
	assert.Empty(t, out.String(), "nothing may be announced when the write is refused")

	after, readErr := os.ReadFile(path)
	assert.NoError(t, readErr)
	assert.Equal(t, original, after, "the original file must be left untouched")
}
