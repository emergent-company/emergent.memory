package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ~/.memory/config/.env.local holds billable provider keys and, on a
// standalone install, the account key. os.WriteFile only applies its mode on
// create, so an existing over-permissive file written by an older CLI or by
// the user's umask stayed world-readable while gaining new secrets (#2110).
// These tests assert the mode on disk rather than on a mock, like
// TestWriteTokenToEnvLocal; apps/cli tests run on Linux, so no GOOS skip.

// writeEnvLocalFixture creates home/.memory/config/.env.local with content and
// an explicit mode. The chmod is not redundant: os.WriteFile's mode is masked
// by the umask.
func writeEnvLocalFixture(t *testing.T, home, content string, mode os.FileMode) string {
	t.Helper()

	envPath := filepath.Join(home, ".memory", "config", ".env.local")
	require.NoError(t, os.MkdirAll(filepath.Dir(envPath), 0o700))
	require.NoError(t, os.WriteFile(envPath, []byte(content), mode))
	require.NoError(t, os.Chmod(envPath, mode))
	return envPath
}

func TestUpdateEnvFileKeyMode(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "fresh install file at 0600 is left at 0600", mode: 0o600},
		{name: "existing 0644 file is tightened to 0600", mode: 0o644},
		{name: "existing 0640 file is tightened to 0600", mode: 0o640},
		{name: "existing 0666 file is tightened to 0600", mode: 0o666},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envPath := writeEnvLocalFixture(t, t.TempDir(), "GOOGLE_API_KEY=old\n", tt.mode)

			require.NoError(t, updateEnvFileKey(envPath, "GOOGLE_API_KEY", "new"))

			info, err := os.Stat(envPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			content, err := os.ReadFile(envPath)
			require.NoError(t, err)
			assert.Equal(t, "GOOGLE_API_KEY=new\n", string(content))
		})
	}
}

// The line-preserving rewrite is the reason this path does not go through
// writeEnvLocal: comments, blank lines and key order survive byte-for-byte,
// apart from the one value that changed.
func TestUpdateEnvFileKeyPreservesLayout(t *testing.T) {
	const before = `# Memory standalone config, hand-maintained
POSTGRES_USER=emergent
GOOGLE_API_KEY=old-google-key
GOOGLE_API_KEY_BACKUP=keep-me
OPENAI_API_KEY="old-openai-key"
OPENAI_MODEL=gemini-2.0-flash

# billing
LLM_MODEL=gemini-pro
`
	const want = `# Memory standalone config, hand-maintained
POSTGRES_USER=emergent
GOOGLE_API_KEY=new-google-key
GOOGLE_API_KEY_BACKUP=keep-me
OPENAI_API_KEY="old-openai-key"
OPENAI_MODEL=gemini-2.0-flash

# billing
LLM_MODEL=gemini-pro
`

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o644)

	require.NoError(t, updateEnvFileKey(envPath, "GOOGLE_API_KEY", "new-google-key"))

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content), "only the targeted value may change")
}

// A prefix rewrite never parses the file, so it cannot drop lines it does not
// understand: the "refusing to overwrite an unparseable file" guard in
// writeEnvLocal does not apply here. This pins that reasoning.
func TestUpdateEnvFileKeyKeepsUnparseableLines(t *testing.T) {
	const before = "WEIRD=\"unterminated\nGOOGLE_API_KEY=old\n# trailing comment\n"
	const want = "WEIRD=\"unterminated\nGOOGLE_API_KEY=new\n# trailing comment\n"

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o600)

	require.NoError(t, updateEnvFileKey(envPath, "GOOGLE_API_KEY", "new"))

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content))
}

// The command path: `memory config set google_api_key` must tighten the file it
// rewrites and keep the rest of it, including an unrelated key that shares a
// prefix with the targeted one.
func TestConfigSetStandaloneKeyTightensEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	envPath := writeEnvLocalFixture(t, home, "GOOGLE_API_KEY=old-google\nGOOGLE_API_KEY_BACKUP=keep-me\n", 0o644)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"google_api_key", "new-google"})

	capture := testutil.CaptureOutput()
	require.NoError(t, cmd.Execute())
	stdout, _, err := capture.Read()
	require.NoError(t, err)
	capture.Restore()

	assert.Contains(t, stdout, "google_api_key updated in .env.local")
	assert.Contains(t, stdout, "Restart services to apply: memory ctl restart")

	info, err := os.Stat(envPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, "GOOGLE_API_KEY=new-google\nGOOGLE_API_KEY_BACKUP=keep-me\n", string(content))
}

// A missing .env.local is still refused with the standalone-installation hint;
// this change must not start creating the file.
func TestConfigSetStandaloneKeyMissingEnvFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"openai_api_key", "whatever"})

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	_, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "This setting is only available for standalone installations")
}
