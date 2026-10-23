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

func TestInitWriteProjectEnvLocal(t *testing.T) {
	localOnly := map[string]string{
		"MEMORY_SERVER_URL":      "https://memory.example.com",
		"MEMORY_ACCOUNT_API_KEY": "oauth-access-token",
	}

	tests := []struct {
		name             string
		existing         []byte
		localServerScope bool
		localServerURL   string
		oauthAccessToken string
		wantExtra        map[string]string
	}{
		{
			name: "creates a fresh file with mode 0600",
		},
		{
			name:     "keeps other keys and tightens an existing 0644 file",
			existing: []byte("MEMORY_PROJECT_API_KEY=emt_old\nOTHER_KEY=keep-me\n"),
		},
		{
			name:             "local server scope adds the local-only keys",
			localServerScope: true,
			localServerURL:   localOnly["MEMORY_SERVER_URL"],
			oauthAccessToken: localOnly["MEMORY_ACCOUNT_API_KEY"],
			wantExtra:        localOnly,
		},
		{
			name:             "global server scope writes no local-only keys",
			localServerScope: false,
			localServerURL:   "https://ignored.example.com",
			oauthAccessToken: "ignored-token",
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
			got, err := initWriteProjectEnvLocal(&out, dir, envTestProjectID, envTestProjectName, envTestToken,
				tt.localServerScope, tt.localServerURL, tt.oauthAccessToken)
			assert.NoError(t, err)
			assert.True(t, filepath.IsAbs(got), "the returned path must be absolute")
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
			if len(tt.wantExtra) == 0 {
				_, hasServerURL := env["MEMORY_SERVER_URL"]
				assert.False(t, hasServerURL)
				_, hasAccountKey := env["MEMORY_ACCOUNT_API_KEY"]
				assert.False(t, hasAccountKey)
			}
			for key, want := range tt.wantExtra {
				assert.Equal(t, want, env[key])
			}

			assert.Equal(t, 1, strings.Count(out.String(), got), "the absolute path must be printed exactly once")
			assert.Contains(t, out.String(), got+" (mode 0600)")
		})
	}
}

// memory init must keep advertising .env.local to git even though the file is
// now written 0600: the mode addresses a shared machine, not version control.
func TestEnsureGitignoreAddsEnvLocal(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{name: "creates .gitignore when it does not exist", want: ".env.local\n"},
		{name: "appends the entry to an existing file", existing: "node_modules/\n", want: "node_modules/\n.env.local\n"},
		{name: "adds the missing trailing newline before appending", existing: "node_modules/", want: "node_modules/\n.env.local\n"},
		{name: "leaves an existing entry untouched", existing: "node_modules/\n.env.local\n", want: "node_modules/\n.env.local\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if tt.existing != "" {
				assert.NoError(t, os.WriteFile(".gitignore", []byte(tt.existing), 0o644))
			}

			ensureGitignore()

			data, err := os.ReadFile(".gitignore")
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}
