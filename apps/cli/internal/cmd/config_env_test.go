package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/testutil"
	"github.com/joho/godotenv"
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

// captureStderr runs fn and returns whatever it wrote to os.Stderr. The
// duplicate-definition notice is a note, not an error: the command still
// succeeds, so the stderr stream is the only place it can surface.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	capture := testutil.CaptureOutput()
	err := fn()
	_, stderr, readErr := capture.Read()
	capture.Restore()
	require.NoError(t, readErr)
	return stderr, err
}

// godotenv and `docker compose --env-file` are both last-wins. Rewriting only
// the first occurrence used to leave the service reading a stale duplicate
// while the command reported success (#2119), so every assignment is rewritten
// and the user is told how many there were.
func TestUpdateEnvFileKeyRewritesEveryDuplicate(t *testing.T) {
	const before = `# standalone config, hand-maintained
GOOGLE_API_KEY=first-stale-value
POSTGRES_USER=emergent
GOOGLE_API_KEY=second-stale-value
#GOOGLE_API_KEY=commented-out-is-not-an-assignment
export GOOGLE_API_KEY=exported-stale-value
`
	const want = `# standalone config, hand-maintained
GOOGLE_API_KEY=new-value
POSTGRES_USER=emergent
GOOGLE_API_KEY=new-value
#GOOGLE_API_KEY=commented-out-is-not-an-assignment
export GOOGLE_API_KEY=new-value
`

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o644)

	stderr, err := captureStderr(t, func() error {
		return updateEnvFileKey(envPath, "GOOGLE_API_KEY", "new-value")
	})
	require.NoError(t, err)

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content), "every assignment rewritten, comments untouched")

	// Exactly one notice, naming the key the count and the file. The commented
	// line is not an assignment, so the count is 3, not 4.
	assert.Equal(t, 1, strings.Count(stderr, "\n"), "one stderr line, got: %q", stderr)
	assert.Contains(t, stderr, "GOOGLE_API_KEY")
	assert.Contains(t, stderr, "3 times")
	assert.Contains(t, stderr, envPath)

	info, err := os.Stat(envPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// The common, unambiguous case must stay quiet: a notice on every set would
// train users to ignore it.
func TestUpdateEnvFileKeyNoDuplicateNotice(t *testing.T) {
	envPath := writeEnvLocalFixture(t, t.TempDir(), "GOOGLE_API_KEY=old\n", 0o600)

	stderr, err := captureStderr(t, func() error {
		return updateEnvFileKey(envPath, "GOOGLE_API_KEY", "new")
	})
	require.NoError(t, err)
	assert.Empty(t, stderr)
}

// A duplicate that already holds the new value is still reported: the point of
// the notice is the duplicate, not the rewrite.
func TestUpdateEnvFileKeyReportsDuplicatesEvenWhenValuesMatch(t *testing.T) {
	envPath := writeEnvLocalFixture(t, t.TempDir(), "GOOGLE_API_KEY=same\nGOOGLE_API_KEY=same\n", 0o600)

	stderr, err := captureStderr(t, func() error {
		return updateEnvFileKey(envPath, "GOOGLE_API_KEY", "same")
	})
	require.NoError(t, err)
	assert.Contains(t, stderr, "2 times")
}

// The split/join rewrite must not invent or drop a final newline, for both the
// rewrite and the append path.
func TestUpdateEnvFileKeyPreservesTrailingNewline(t *testing.T) {
	tests := []struct {
		name   string
		before string
		key    string
		want   string
	}{
		{
			name:   "rewrite keeps a trailing newline",
			before: "GOOGLE_API_KEY=old\n",
			key:    "GOOGLE_API_KEY",
			want:   "GOOGLE_API_KEY=new\n",
		},
		{
			name:   "rewrite keeps the absence of a trailing newline",
			before: "GOOGLE_API_KEY=old",
			key:    "GOOGLE_API_KEY",
			want:   "GOOGLE_API_KEY=new",
		},
		{
			name:   "append keeps a trailing newline",
			before: "POSTGRES_USER=emergent\n",
			key:    "GOOGLE_API_KEY",
			want:   "POSTGRES_USER=emergent\nGOOGLE_API_KEY=new\n",
		},
		{
			name:   "append keeps the absence of a trailing newline",
			before: "POSTGRES_USER=emergent",
			key:    "GOOGLE_API_KEY",
			want:   "POSTGRES_USER=emergent\nGOOGLE_API_KEY=new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envPath := writeEnvLocalFixture(t, t.TempDir(), tt.before, 0o600)

			require.NoError(t, updateEnvFileKey(envPath, tt.key, "new"))

			content, err := os.ReadFile(envPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(content))
		})
	}
}

// The rewrite keeps the prefix it was given — indentation, "export ", the
// separator and the spacing around it — and only replaces the value. The colon
// form matters: godotenv's locateKeyName accepts ':' as an assignment
// separator, so leaving a "KEY: stale" line alone rewrote the earlier "=" line
// and left the stale colon line last, which is the exact stale-value bug this
// change exists to remove (#2119).
func TestUpdateEnvFileKeyMatchesGodotenvGrammar(t *testing.T) {
	const before = `  export OPENAI_API_KEY = old-indented
OPENAI_API_KEY: stale-colon
`
	const want = `  export OPENAI_API_KEY = new
OPENAI_API_KEY: new
`

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o600)

	stderr, err := captureStderr(t, func() error {
		return updateEnvFileKey(envPath, "OPENAI_API_KEY", "new")
	})
	require.NoError(t, err)

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content))

	// Both forms are assignments, so the duplicate notice fires: before this
	// fix the count was 1 and the colon line silently won.
	assert.Contains(t, stderr, "2 times", "the '=' line and the ':' line are both assignments")
}

// The oracle: rewrite, then read the file back with the parser the CLI actually
// uses. Asserting the parsed value — rather than a table of expected bytes —
// catches grammar drift without enumerating syntax, and its absence is what let
// the colon form ship broken.
//
// It also pins the *rest* of the parsed file: a rewrite must not change any
// other key's value, and must not make a key disappear. Asserting only the
// target key and the line count is how the quoted-span regression slipped
// through — `KEY="old"OTHER=x` became `KEY=NEWVALUEOTHER=x`, which keeps both
// counts and the target value's shape while silently deleting OTHER. Diffing
// the before/after maps reds on that.
func TestUpdateEnvFileKeyParityWithGodotenv(t *testing.T) {
	const key = "OPENAI_API_KEY"
	fixtures := []struct{ name, before string }{
		{name: "plain", before: "OPENAI_API_KEY=old\n"},
		{name: "colon separator", before: "OPENAI_API_KEY: old\n"},
		{name: "spaces around equals", before: "OPENAI_API_KEY = old\n"},
		{name: "spaces around colon", before: "OPENAI_API_KEY : old\n"},
		{name: "export", before: "export OPENAI_API_KEY=old\n"},
		{name: "indented export", before: "  export OPENAI_API_KEY = old\n"},
		{name: "tabs around export and separator", before: "\texport\tOPENAI_API_KEY\t=\told\n"},
		{name: "vertical tab and form feed indent", before: "\v\fOPENAI_API_KEY=old\n"},
		{name: "CRLF", before: "OPENAI_API_KEY=old\r\n"},
		{name: "CRLF among LF lines", before: "OTHER=1\nOPENAI_API_KEY=old\r\n"},
		{name: "colon line after rewritten equals line", before: "export OPENAI_API_KEY = old\nOPENAI_API_KEY: stale\n"},
		{name: "KEY_2 is not KEY", before: "OPENAI_API_KEY_2=keep\nOPENAI_API_KEY=old\n"},
		{name: "commented out line", before: "# OPENAI_API_KEY=old\nOPENAI_API_KEY=old\n"},
		{name: "inline comment", before: "OPENAI_API_KEY=old # keep this\n"},
		{name: "single quoted", before: "OPENAI_API_KEY='old'\n"},
		{
			name:   "double quoted value immediately followed by a comment",
			before: "OPENAI_API_KEY=\"old\"#prod-secret-tag\nOTHER=keep\n",
		},
		{
			name:   "single quoted value immediately followed by a comment",
			before: "OPENAI_API_KEY='old'#prod-secret-tag\nOTHER=keep\n",
		},
		{
			name:   "double quoted value immediately followed by another assignment",
			before: "OPENAI_API_KEY=\"old\"OTHER=keep\n",
		},
		{
			name:   "single quoted value immediately followed by another assignment",
			before: "OPENAI_API_KEY='old'OTHER=keep\n",
		},
		{
			name:   "quoted value followed by whitespace and a comment",
			before: "OPENAI_API_KEY=\"old\" # keep this\nOTHER=keep\n",
		},
		{
			// godotenv skips a quote preceded by a backslash, so the escaped
			// quote is value text and the closing quote ends the value.
			name:   "escaped quote inside a double quoted value",
			before: "OPENAI_API_KEY=\"a\\\"b\"\nOTHER=keep\n",
		},
		{
			name:   "multi-line quoted value containing an assignment-looking line",
			before: "PROMPT=\"line one\nOPENAI_API_KEY=inside the value\nline three\"\nOPENAI_API_KEY=old\n",
		},
		{name: "empty value", before: "OPENAI_API_KEY=\n"},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			envPath := writeEnvLocalFixture(t, t.TempDir(), tt.before, 0o600)

			before, err := godotenv.Unmarshal(tt.before)
			require.NoError(t, err, "the fixture itself must parse before the rewrite")

			require.NoError(t, updateEnvFileKey(envPath, key, "new-value"))

			content, err := os.ReadFile(envPath)
			require.NoError(t, err)
			parsed, err := godotenv.Unmarshal(string(content))
			require.NoError(t, err, "the rewritten file must still parse")

			assert.Equal(t, "new-value", parsed[key], "the parser must read the value that was written")
			// Every fixture already assigns the key, so a correct rewrite edits
			// the existing assignment in place. A missed form (the old colon
			// bug) appends a second assignment instead, and last-wins would then
			// hide the stale one from the assertion above.
			assert.Equal(t, strings.Count(tt.before, "\n"), strings.Count(string(content), "\n"),
				"an existing assignment must be rewritten, not appended around")

			// Nothing else in the parsed file may change: no other value and no
			// other key. Deleting the target key from both maps turns a dropped
			// assignment or a re-parsed trailing comment into a map diff.
			delete(before, key)
			delete(parsed, key)
			assert.Equal(t, before, parsed, "every key other than %q must survive unchanged", key)
		})
	}
}

// A "KEY=..." line inside a multi-line quoted value is value text, not an
// assignment (godotenv's extractVarValue consumes it with the quote), so the
// rewrite must leave it alone. The same logic governs the closing quote: the
// parser ends a quoted value there, so a byte that follows it on the same line
// starts a new statement — a comment or another assignment — and must not be
// absorbed into the new value.
func TestUpdateEnvFileKeyLeavesQuotedValuesAlone(t *testing.T) {
	const key = "OPENAI_API_KEY"
	tests := []struct {
		name       string
		before     string
		want       string
		wantPrompt string
	}{
		{
			name:       "single quoted value spanning lines",
			before:     "PROMPT='line one\nOPENAI_API_KEY=inside the value\nline three'\nOPENAI_API_KEY=old\n",
			want:       "PROMPT='line one\nOPENAI_API_KEY=inside the value\nline three'\nOPENAI_API_KEY=new\n",
			wantPrompt: "OPENAI_API_KEY=inside the value",
		},
		{
			// The parser skips a quote preceded by a backslash, so the escaped
			// quote does not end the value and the lines it spans stay value text.
			name:       "escaped quote in a double quoted value spanning lines",
			before:     "PROMPT=\"a\\\"b\nOPENAI_API_KEY=inside the value\nc\"\nOPENAI_API_KEY=old\n",
			want:       "PROMPT=\"a\\\"b\nOPENAI_API_KEY=inside the value\nc\"\nOPENAI_API_KEY=new\n",
			wantPrompt: "a\"b\nOPENAI_API_KEY=inside the value\nc",
		},
		{
			name:   "quoted value immediately followed by a comment",
			before: "OPENAI_API_KEY=\"old\"#prod-secret-tag\n",
			want:   "OPENAI_API_KEY=\"new\"#prod-secret-tag\n",
		},
		{
			name:   "quoted value immediately followed by another assignment",
			before: "OPENAI_API_KEY='old'OTHER=keep\n",
			want:   "OPENAI_API_KEY='new'OTHER=keep\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envPath := writeEnvLocalFixture(t, t.TempDir(), tt.before, 0o600)

			require.NoError(t, updateEnvFileKey(envPath, key, "new"))

			content, err := os.ReadFile(envPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(content))

			parsed, err := godotenv.Unmarshal(string(content))
			require.NoError(t, err)
			assert.Equal(t, "new", parsed[key])
			if tt.wantPrompt != "" {
				assert.Contains(t, parsed["PROMPT"], tt.wantPrompt)
			}
		})
	}
}

// The rewrite itself must keep a quoted value's delimiters, so the failure is
// localised here rather than only visible through the parser oracle. The bytes
// the parser places after the closing quote are a separate statement and stay
// outside the rewritten span.
func TestRewriteEnvValuesPreservesQuoting(t *testing.T) {
	const key = "OPENAI_API_KEY"
	tests := []struct {
		name   string
		before string
		value  string
		want   string
	}{
		{
			name:   "double quoted value before a comment",
			before: "OPENAI_API_KEY=\"old\"#prod-secret-tag\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY=\"NEWVALUE\"#prod-secret-tag\n",
		},
		{
			name:   "single quoted value before a comment",
			before: "OPENAI_API_KEY='old'#prod-secret-tag\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY='NEWVALUE'#prod-secret-tag\n",
		},
		{
			name:   "double quoted value before another assignment",
			before: "OPENAI_API_KEY=\"old\"OTHER=x\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY=\"NEWVALUE\"OTHER=x\n",
		},
		{
			name:   "single quoted value before another assignment",
			before: "OPENAI_API_KEY='old'OTHER=x\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY='NEWVALUE'OTHER=x\n",
		},
		{
			name:   "quoted value before whitespace and a comment keeps working",
			before: "OPENAI_API_KEY=\"old\" # keep this\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY=\"NEWVALUE\" # keep this\n",
		},
		{
			name:   "quoted value at end of line",
			before: "OPENAI_API_KEY='old'\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY='NEWVALUE'\n",
		},
		{
			name:   "unquoted value stays bare",
			before: "OPENAI_API_KEY=old # keep this\n",
			value:  "NEWVALUE",
			want:   "OPENAI_API_KEY=NEWVALUE # keep this\n",
		},
		{
			// A quote in the new value cannot go inside the original quotes; the
			// fallback writes it bare but keeps the comment separate with one
			// space instead of gluing it to the value.
			name:   "value that cannot live in the original quotes gets a separator",
			before: "OPENAI_API_KEY=\"old\"#prod-secret-tag\n",
			value:  "has\"quote",
			want:   "OPENAI_API_KEY=has\"quote #prod-secret-tag\n",
		},
		{
			// A "$" is expanded by godotenv in the double-quoted *and* the bare
			// branch, so keeping the quotes and writing the value bare both
			// change it. Only the single-quoted form is literal.
			name:   "value with a variable reference is single quoted",
			before: "OPENAI_API_KEY=\"old\"#prod-secret-tag\n",
			value:  "$HOME-key",
			want:   "OPENAI_API_KEY='$HOME-key'#prod-secret-tag\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rewritten, err := rewriteEnvValues([]byte(tt.before), key, tt.value)
			require.NoError(t, err)
			require.Equal(t, 1, rewritten)
			assert.Equal(t, tt.want, string(got))

			// The expected bytes are not the contract — the parse is. This
			// case used to encode "$HOME-key" written bare, which the parser
			// expands to "-key".
			parsed, err := godotenv.Unmarshal(string(got))
			require.NoError(t, err)
			assert.Equal(t, tt.value, parsed[key], "the rewrite must read back as the value it was given")
		})
	}
}

// The writer's edge cases must end in one of exactly two states: the value
// round-trips through the parser unchanged, or the command refuses and the file
// is byte-identical. A silent third state — the file still parses but holds a
// different value — is the bug class this change exists to remove, so the
// assertion below is written to fail on it rather than to accommodate it.
func TestUpdateEnvFileKeyAdversarialValues(t *testing.T) {
	const key = "GOOGLE_API_KEY"

	fixtures := []struct{ name, before string }{
		{name: "unquoted", before: "GOOGLE_API_KEY=old\nOTHER=keep\n"},
		{name: "double quoted before a comment", before: "GOOGLE_API_KEY=\"old\"#tag\nOTHER=keep\n"},
		{name: "single quoted before a comment", before: "GOOGLE_API_KEY='old'#tag\nOTHER=keep\n"},
		{name: "double quoted before an assignment", before: "GOOGLE_API_KEY=\"old\"OTHER=keep\n"},
		{name: "single quoted before an assignment", before: "GOOGLE_API_KEY='old'OTHER=keep\n"},
		{name: "unquoted before a comment", before: "GOOGLE_API_KEY=old #tag\nOTHER=keep\n"},
	}

	values := []struct{ name, value string }{
		{name: "trailing backslash", value: `abc\`},
		{name: "variable reference", value: "$HOME-key"},
		{name: "newline", value: "a\nb"},
		{name: "carriage return", value: "a\rb"},
		{name: "leading double quote", value: `"abc`},
		{name: "leading single quote", value: `'abc`},
		{name: "both quote styles", value: `a'b"c`},
		{name: "hash", value: "a#b"},
		{name: "hash and single quote", value: "it's#1"},
		{name: "single quote and variable reference", value: "it's $HOME"},
		{name: "backslash and single quote", value: `a'b\`},
		{name: "spaces", value: "  padded  "},
		{name: "empty", value: ""},
		{name: "plain", value: "new-value"},
	}

	for _, fixture := range fixtures {
		for _, tv := range values {
			t.Run(fixture.name+"/"+tv.name, func(t *testing.T) {
				envPath := writeEnvLocalFixture(t, t.TempDir(), fixture.before, 0o600)

				previous, err := godotenv.Unmarshal(fixture.before)
				require.NoError(t, err, "the fixture itself must parse before the rewrite")

				updateErr := updateEnvFileKey(envPath, key, tv.value)

				content, err := os.ReadFile(envPath)
				require.NoError(t, err)

				if updateErr != nil {
					// A refusal is only legitimate if nothing was written.
					assert.Contains(t, updateErr.Error(), key, "a refusal must name the key")
					assert.Equal(t, fixture.before, string(content),
						"a refused write must leave the file byte-identical")

					leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(envPath), "*.tmp-*"))
					require.NoError(t, err)
					assert.Empty(t, leftovers, "a refusal must not leave a temp file behind")
					return
				}

				parsed, err := godotenv.Unmarshal(string(content))
				require.NoError(t, err,
					"a written file that does not parse is a silent corruption: %q", string(content))
				require.Equal(t, tv.value, parsed[key],
					"the parser must read the value that was written, file was %q", string(content))

				// Nothing else may change, the same full-map oracle the parity
				// test uses: a dropped or absorbed assignment is a map diff.
				delete(previous, key)
				delete(parsed, key)
				assert.Equal(t, previous, parsed,
					"every key other than %q must survive unchanged", key)
			})
		}
	}
}

// The refusal path a user actually hits: `memory config set` must fail, leave
// the file exactly as it was, and leave no temp file behind. A newline cannot
// be written into an env value by any quoting style, so it is the value that
// must always be refused rather than repaired.
func TestConfigSetStandaloneKeyRefusesUnrepresentableValue(t *testing.T) {
	const before = "GOOGLE_API_KEY=old\nOTHER=keep\n"

	home := t.TempDir()
	t.Setenv("HOME", home)
	envPath := writeEnvLocalFixture(t, home, before, 0o600)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"google_api_key", "a\nb"})

	capture := testutil.CaptureOutput()
	err := cmd.Execute()
	_, _, readErr := capture.Read()
	require.NoError(t, readErr)
	capture.Restore()

	// RunE's error is what makes the process exit non-zero: cmd/main.go writes
	// it to stderr and calls os.Exit(1).
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GOOGLE_API_KEY")
	assert.Contains(t, err.Error(), "cannot be represented in this file's quoting style")
	assert.Contains(t, err.Error(), "edit that line by hand")

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, before, string(content), "a refused write must leave the file byte-identical")

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(envPath), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "a refusal must not leave a temp file behind")
}

// The round-trip guard is the last line of defence, so it is tested directly
// with candidate content a future writer change could produce: if the guard
// were removed, a wrong value or a dropped assignment would reach the file.
func TestVerifyEnvRewriteRefusesACorruptingCandidate(t *testing.T) {
	const before = "GOOGLE_API_KEY=old\nOTHER=keep\n"

	tests := []struct {
		name  string
		after string
	}{
		{
			name:  "value silently expanded",
			after: "GOOGLE_API_KEY=$HOME-key\nOTHER=keep\n",
		},
		{
			name:  "another assignment absorbed into the value",
			after: "GOOGLE_API_KEY=old OTHER=keep\n",
		},
		{
			name:  "another assignment dropped",
			after: "GOOGLE_API_KEY=new\n",
		},
		{
			name:  "value written raw with an unescaped newline",
			after: "GOOGLE_API_KEY=a\nb\nOTHER=keep\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyEnvRewrite([]byte(before), []byte(tt.after), "GOOGLE_API_KEY", "new")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "GOOGLE_API_KEY")
			assert.Contains(t, err.Error(), "cannot be represented in this file's quoting style")
			assert.Contains(t, err.Error(), "the file is unchanged")
		})
	}

	// The guard must not refuse the legitimate rewrite it exists to protect.
	require.NoError(t, verifyEnvRewrite([]byte(before),
		[]byte("GOOGLE_API_KEY='$HOME-key'\nOTHER=keep\n"), "GOOGLE_API_KEY", "$HOME-key"))
}

// A file that was already unreadable by the loader cannot be map-diffed, but a
// line-preserving rewrite must still work on it (see
// TestUpdateEnvFileKeyKeepsUnparseableLines): the guard skips only that case.
func TestVerifyEnvRewriteSkipsAnUnparseableOriginal(t *testing.T) {
	const before = "WEIRD=\"unterminated\nGOOGLE_API_KEY=old\n"

	require.NoError(t, verifyEnvRewrite([]byte(before),
		[]byte("WEIRD=\"unterminated\nGOOGLE_API_KEY=new\n"), "GOOGLE_API_KEY", "new"))
}

// A hand-maintained file may use CRLF; rewriting one assignment must not leave
// mixed endings behind (the old split/join dropped the \r of every rewritten
// line).
func TestUpdateEnvFileKeyPreservesCRLF(t *testing.T) {
	const before = "OTHER=1\r\nOPENAI_API_KEY=old\r\n# comment\r\n"
	const want = "OTHER=1\r\nOPENAI_API_KEY=new\r\n# comment\r\n"

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o600)

	require.NoError(t, updateEnvFileKey(envPath, "OPENAI_API_KEY", "new"))

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content))
	assert.Equal(t, strings.Count(string(content), "\n"), strings.Count(string(content), "\r\n"),
		"every line ending must still be CRLF")
}

// The append path follows the file's existing ending rather than switching it.
func TestUpdateEnvFileKeyAppendKeepsCRLF(t *testing.T) {
	const before = "OTHER=1\r\n"
	const want = "OTHER=1\r\nOPENAI_API_KEY=new\r\n"

	envPath := writeEnvLocalFixture(t, t.TempDir(), before, 0o600)

	require.NoError(t, updateEnvFileKey(envPath, "OPENAI_API_KEY", "new"))

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, want, string(content))
}

// The write goes through a temp file and a rename so a failure cannot truncate
// the credential file. A symlinked .env.local (a dotfiles-managed one) keeps
// being written through the link rather than replaced by a regular file.
func TestUpdateEnvFileKeyWritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", ".env.local")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("GOOGLE_API_KEY=old\n"), 0o600))

	link := filepath.Join(dir, ".env.local")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, updateEnvFileKey(link, "GOOGLE_API_KEY", "new"))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink itself must survive the rewrite")

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "GOOGLE_API_KEY=new\n", string(content))

	targetInfo, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), targetInfo.Mode().Perm())
}

// The command path: a duplicated .env.local is normalised and the stderr notice
// reaches the user rather than being swallowed by the command.
func TestConfigSetStandaloneKeyReportsDuplicates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	envPath := writeEnvLocalFixture(t, home, "GOOGLE_API_KEY=stale-one\nGOOGLE_API_KEY=stale-two\n", 0o644)

	cmd := newConfigSetCmd()
	cmd.SetArgs([]string{"google_api_key", "new-google"})

	capture := testutil.CaptureOutput()
	require.NoError(t, cmd.Execute())
	stdout, stderr, err := capture.Read()
	require.NoError(t, err)
	capture.Restore()

	assert.Contains(t, stdout, "google_api_key updated in .env.local")
	assert.Contains(t, stderr, "GOOGLE_API_KEY is defined 2 times")

	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, "GOOGLE_API_KEY=new-google\nGOOGLE_API_KEY=new-google\n", string(content))

	info, err := os.Stat(envPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// A failed atomic write must leave no temp file behind: a stray
// .env.local.tmp-* holding the new credential is worse than the failure.
func TestReplaceFileAtomicallyCleansUpTempOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".env.local")
	require.NoError(t, os.WriteFile(target, []byte("KEEP=1\n"), 0o600))

	restore := swapRename(func(string, string) error { return syscall.EBUSY })
	defer restore()

	err := replaceFileAtomically(target, []byte("NEW=2\n"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the original file is unchanged")

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "KEEP=1\n", string(content), "a failed replace must not touch the original")

	leftovers, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "a failed replace must not leave a temp file behind")
}

// The failure to create a temp file is the case a user hits on a read-only
// directory, and the old message named the temp path with a doubled dot. It must
// name the file they asked to write and the directory that has to be writable.
func TestReplaceFileAtomicallyExplainsDirectoryRequirement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".env.local")
	require.NoError(t, os.WriteFile(target, []byte("KEEP=1\n"), 0o600))

	restore := swapCreateTemp(func(string, string) (*os.File, error) { return nil, syscall.EACCES })
	defer restore()

	err := replaceFileAtomically(target, []byte("NEW=2\n"), 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), target, "the message must name the file being written")
	assert.Contains(t, err.Error(), dir, "the message must name the directory that must be writable")
	assert.Contains(t, err.Error(), "writable")
	assert.NotContains(t, err.Error(), "tmp-", "the temp name is an implementation detail, not an explanation")

	leftovers, globErr := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	require.NoError(t, globErr)
	assert.Empty(t, leftovers)
}

// rename replaces the inode, so a write over a file owned by another user would
// leave it owned by whoever ran the command. When the caller may chown (root),
// this reproduces the sudo case; when it may not, it pins the trivial path.
func TestReplaceFileAtomicallyKeepsTheOwner(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".env.local")
	require.NoError(t, os.WriteFile(target, []byte("GOOGLE_API_KEY=old\n"), 0o600))

	wantUID, wantGID := os.Getuid(), os.Getgid()
	if os.Geteuid() == 0 {
		// nobody:nogroup on Linux; the root write is the case that used to flip
		// the owner to root.
		wantUID, wantGID = 65534, 65534
		require.NoError(t, os.Chown(target, wantUID, wantGID))
	}

	require.NoError(t, replaceFileAtomically(target, []byte("GOOGLE_API_KEY=new\n"), 0o600))

	info, err := os.Stat(target)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "stat owner is available on the platforms these tests run on")
	assert.Equal(t, wantUID, int(st.Uid), "the rewritten file must keep its owner")
	assert.Equal(t, wantGID, int(st.Gid), "the rewritten file must keep its group")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// swapRename replaces the rename seam and returns its restore function.
func swapRename(fn func(string, string) error) func() {
	original := renamePath
	renamePath = fn
	return func() { renamePath = original }
}

// swapCreateTemp replaces the temp-file seam and returns its restore function.
func swapCreateTemp(fn func(string, string) (*os.File, error)) func() {
	original := createTempFile
	createTempFile = fn
	return func() { createTempFile = original }
}
