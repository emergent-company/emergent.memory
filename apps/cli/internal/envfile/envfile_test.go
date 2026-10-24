package envfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		{
			// Single quotes cannot hold a value that begins with one, and the
			// bare form would be read back starting at the quote, so the value
			// used to be refused outright. Double quotes hold it: there is no
			// "$", '"' or backslash for the parser to expand (#2164).
			name:   "value starting with a single quote is double quoted",
			before: "OPENAI_API_KEY=old\n",
			value:  "'abc",
			want:   "OPENAI_API_KEY=\"'abc\"\n",
		},
		{
			// Same rescue when the value replaces a quoted one and another
			// assignment follows on the line.
			name:   "value starting with a single quote before another assignment",
			before: "OPENAI_API_KEY='old'OTHER=keep\n",
			value:  "'abc",
			want:   "OPENAI_API_KEY=\"'abc\"OTHER=keep\n",
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

// The double-quote fallback must not weaken the "$" rule: the parser expands
// variables inside double quotes, so a value single quotes cannot hold and that
// also contains a "$" has no safe form and is still refused (#2164).
func TestRewriteEnvValuesRefusesVariableInDoubleQuoteFallback(t *testing.T) {
	const key = "OPENAI_API_KEY"
	before := []byte("OPENAI_API_KEY=old\n")

	_, rewritten, err := rewriteEnvValues(before, key, "'$HOME")
	require.Error(t, err, "a value with a leading quote and a variable must be refused, not expanded")
	require.Zero(t, rewritten)
	assert.Contains(t, err.Error(), "cannot be represented")
	assert.Contains(t, err.Error(), key)
}

// A new key goes through the same fallback order, so a value that used to be
// refused on append is written in a form the parser reads back verbatim.
func TestAppendEnvAssignmentUsesDoubleQuotesWhenSingleQuotesCannot(t *testing.T) {
	got, err := appendEnvAssignment([]byte("A=1\n"), "NEW", "'abc")
	require.NoError(t, err)
	assert.Equal(t, "A=1\nNEW=\"'abc\"\n", string(got))

	parsed, err := godotenv.Unmarshal(string(got))
	require.NoError(t, err)
	assert.Equal(t, "'abc", parsed["NEW"])
	assert.Equal(t, "1", parsed["A"])
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

// TestUpdateAllWritesEveryKey: the install wizard edits several provider keys in
// one step, so they must land together (one rewrite, deterministic order).
func TestUpdateAllWritesEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	require.NoError(t, os.WriteFile(path, []byte("GOOGLE_API_KEY=old\nOTHER=keep\n"), 0o600))

	require.NoError(t, UpdateAll(path, map[string]string{
		"OPENAI_MODEL":   "openai/gpt-4o",
		"GOOGLE_API_KEY": "new-google",
	}, 0o600))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "GOOGLE_API_KEY=new-google\nOTHER=keep\nOPENAI_MODEL=openai/gpt-4o\n", string(content))
}

// TestUpdateAllIsAllOrNothing: a value the writer cannot represent must refuse
// the whole update. The keys before it in sort order have already been applied to
// the candidate in memory, and none of them may reach the file (#2148).
func TestUpdateAllIsAllOrNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	const before = "GOOGLE_API_KEY=old\nOTHER=keep\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0o600))

	err := UpdateAll(path, map[string]string{
		"GOOGLE_API_KEY": "new-google", // sorts first, would succeed on its own
		"OPENAI_API_KEY": "a\nb",       // unrepresentable
	}, 0o600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be represented")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, string(content), "a refused UpdateAll must leave the file byte-identical")

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "a refusal must not leave a temp file behind")
}

// TestUpdateCallsTheGuardBeforeWriting is the seam for issue #2148: the guard's
// own cases cannot prove it runs, because every candidate it would refuse is a
// candidate the renderer already refuses — so removing the call site would fail
// no test. This swaps the call for a refusal the renderer would never produce and
// asserts the write never happens.
func TestUpdateCallsTheGuardBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	const before = "GOOGLE_API_KEY=old\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0o600))

	refusal := errors.New("guard refused")
	calls := 0
	original := verifyRewrite
	verifyRewrite = func(before, after []byte, key, value string) error {
		calls++
		return refusal
	}
	t.Cleanup(func() { verifyRewrite = original })

	err := Update(path, "GOOGLE_API_KEY", "new", 0o600)
	require.ErrorIs(t, err, refusal)
	assert.Equal(t, 1, calls, "the write path must consult the guard")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, string(content), "a guard refusal must leave the file byte-identical")

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "a guard refusal must not write a temp file")

	// UpdateAll must consult the guard before it writes the candidate.
	calls = 0
	err = UpdateAll(path, map[string]string{"A": "1", "B": "2"}, 0o600)
	require.ErrorIs(t, err, refusal)
	assert.Equal(t, 1, calls, "UpdateAll must consult the guard before writing the candidate")
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

// Read is the read side of the writer: a caller that displays or forwards a
// value must see what the parser sees, not the bytes on that line. The cases
// that used to be re-implemented per caller are the ones that matter — a
// quoted value, an inline comment, the "export " prefix, the colon form and a
// duplicated key (#2170).
func TestReadReturnsWhatTheParserReads(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		want    string
		wantOK  bool
	}{
		{name: "bare", content: "A=plain\n", key: "A", want: "plain", wantOK: true},
		{name: "single quoted", content: "A='p@ss #1'\n", key: "A", want: "p@ss #1", wantOK: true},
		{name: "double quoted", content: "A=\"x y\"\n", key: "A", want: "x y", wantOK: true},
		{name: "inline comment", content: "A=https://x # note\n", key: "A", want: "https://x", wantOK: true},
		{name: "export prefix", content: "export A=v\n", key: "A", want: "v", wantOK: true},
		{name: "colon form", content: "A: v\n", key: "A", want: "v", wantOK: true},
		{name: "last assignment wins", content: "A=first\nA=second\n", key: "A", want: "second", wantOK: true},
		{name: "spacing around the separator", content: "  A =  value  \n", key: "A", want: "value", wantOK: true},
		{name: "absent key", content: "B=1\n", key: "A", want: "", wantOK: false},
		{name: "quoted value spanning lines", content: "A='one\ntwo'\n", key: "A", want: "one\ntwo", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Read([]byte(tt.content), tt.key)
			if ok != tt.wantOK {
				t.Fatalf("Read() ok = %v, want %v", ok, tt.wantOK)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// The values Read returns for a writer-produced file must be exactly what
// godotenv reads, for every shape the writer can emit: if these ever disagree,
// a display path is showing something the service does not see.
func TestReadAgreesWithGodotenvOnWriterOutput(t *testing.T) {
	values := map[string]string{
		"GOOGLE_API_KEY":  "AIzaSy-plain",
		"OPENAI_BASE_URL": "https://llm.example.com/a#b",
		"LLM_MODEL":       "'quote-leading",
		"SECRET":          "p@ss #1",
		"SPACED":          "  padded  ",
	}
	path := filepath.Join(t.TempDir(), ".env.local")
	require.NoError(t, os.WriteFile(path, []byte("KEEP=1\n"), 0o600))
	require.NoError(t, UpdateAll(path, values, 0o600))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	parsed, err := godotenv.Unmarshal(string(content))
	require.NoError(t, err)
	for key, want := range values {
		got, ok := Read(content, key)
		require.True(t, ok, "Read(%q) not found in %q", key, string(content))
		assert.Equal(t, want, got, "Read(%q) differs from the file's meaning", key)
		assert.Equal(t, parsed[key], got, "Read(%q) differs from godotenv", key)
	}
	assert.Equal(t, "1", parsed["KEEP"], "the rewrite must not drop unrelated keys")
}
