// Package envfile is the one writer for .env files in this CLI: every surface
// that edits a key in a file a service reads goes through Update, so the quoting
// fallback, the atomic replace with ownership preservation, and the round-trip
// guard are not re-implemented per caller (and cannot drift apart).
package envfile

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/joho/godotenv"
)

//
// The .env grammar this rewrite obeys is not invented here: it is
// joho/godotenv v1.5.1's parser, which is what godotenv.Read/Unmarshal — the
// reader this CLI actually uses — runs (subosito/gotenv reaches the module only
// through viper). The rules that matter, from parser.go:
//
//   - a statement starts at the first non-whitespace character, and "#" starts
//     a comment to the end of the line (getStatementStart);
//   - an optional "export" prefix is skipped when whitespace follows it
//     (locateKeyName);
//   - a key is a run of letters, digits, '_' and '.', and either '=' or ':'
//     separates it from its value — the yaml-style colon is a real assignment
//     form (`case '=', ':'`), not a typo;
//   - whitespace around the key and the separator is ignored, and "whitespace"
//     is the parser's isSpace: tab, vertical tab, form feed, carriage return,
//     space, NEL and no-break space. So \v and \f indent an assignment, and \r
//     is not a line break to the parser (parseBytes rewrites CRLF to LF first);
//   - a quoted value can span lines, so a "KEY=..." line inside one is value
//     text, not an assignment (extractVarValue).
//
// Agreeing with the parser matters in one direction: every line the parser
// reads as an assignment of key must be rewritten, or the service keeps reading
// a stale duplicate after the command reported success (#2119). A second,
// hand-rolled grammar is how the colon form shipped broken, so the tests check
// the rewrite against godotenv.Unmarshal itself rather than against a list of
// syntax cases (TestUpdateEnvFileKeyParityWithGodotenv).

// envSpace reports whether r is whitespace to the .env parser (its isSpace),
// which differs from unicode.IsSpace by excluding line breaks.
func envSpace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return false
}

// envAssignment is one assignment found in an env file, as byte offsets into
// the file. valueStart:valueEnd covers the value text only: the prefix (leading
// whitespace, "export ", the key, the separator and the spacing around it) and
// anything the parser does not treat as part of the value (trailing spaces, an
// inline "# comment", the line ending) sit outside the span and are carried
// through byte for byte by a rewrite.
//
// A quoted value also carries its quote character, because the delimiters are
// the parser's end-of-value boundary: godotenv ends a quoted value at the
// closing quote and then treats whatever follows as a new statement
// (getStatementStart), so "#tag" after it is a comment and "OTHER=x" after it is
// another assignment. A rewrite that dropped the quotes would glue those bytes
// onto the new value instead (see rewriteEnvValues).
type envAssignment struct {
	key        string
	valueStart int
	valueEnd   int
	quote      byte // '"' or '\'' when the value is quoted, 0 when it is not
}

// scanEnvAssignments finds every assignment in content using the parser's
// statement grammar (see the comment above), reporting where each value sits.
//
// It is tolerant by contract: a statement the parser would reject ends at its
// own line and scanning resumes on the next one, so a rewrite never fails a
// file it cannot parse and never swallows the assignments after it — including
// after an unterminated quote.
func scanEnvAssignments(content []byte) []envAssignment {
	var found []envAssignment
	for i := 0; i < len(content); {
		for i < len(content) {
			r, size := utf8.DecodeRune(content[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += size
		}
		if i >= len(content) {
			break
		}
		if content[i] == '#' {
			i = nextEnvLine(content, i)
			continue
		}

		key, valueStart, ok := locateEnvAssignment(content, i)
		if !ok {
			i = nextEnvLine(content, i)
			continue
		}
		valueEnd, quote := envValueSpan(content, valueStart)
		found = append(found, envAssignment{key: key, valueStart: valueStart, valueEnd: valueEnd, quote: quote})
		// Resume after the value. For a multi-line quoted value that skips the
		// lines it contains, so a "KEY=..." line inside one is never mistaken
		// for a statement of its own.
		i = valueEnd
	}
	return found
}

// nextEnvLine returns the offset just past the end of the line starting at i.
func nextEnvLine(content []byte, i int) int {
	if nl := bytes.IndexByte(content[i:], '\n'); nl >= 0 {
		return i + nl + 1
	}
	return len(content)
}

// locateEnvAssignment locates one statement starting at i the way the parser's
// locateKeyName does, returning the key and the offset of its value. The key
// text is everything before the separator, so whitespace inside a key makes it
// a different key, exactly as the parser sees it.
func locateEnvAssignment(content []byte, i int) (key string, valueStart int, ok bool) {
	// "export" is a prefix only when whitespace follows it.
	if bytes.HasPrefix(content[i:], []byte("export")) {
		j := i + len("export")
		if j < len(content) {
			if r, _ := utf8.DecodeRune(content[j:]); envSpace(r) {
				// Skip the whitespace after "export" as the parser does, so the
				// key text does not start with it.
				for i = j; i < len(content); {
					r, size := utf8.DecodeRune(content[i:])
					if !envSpace(r) {
						break
					}
					i += size
				}
			}
		}
	}

	keyStart := i
	sep := -1
	for j := i; j < len(content) && sep < 0; {
		r, size := utf8.DecodeRune(content[j:])
		switch {
		case r == '\n':
			// A key cannot cross a line: this line is not an assignment.
			return "", 0, false
		case r == '=' || r == ':':
			sep = j
		case envSpace(r):
		case r == '_' || r == '.':
		case unicode.IsLetter(r) || unicode.IsNumber(r):
		default:
			return "", 0, false
		}
		j += size
	}
	if sep < 0 {
		return "", 0, false
	}

	key = strings.TrimRightFunc(string(content[keyStart:sep]), unicode.IsSpace)
	if key == "" {
		return "", 0, false
	}

	valueStart = sep + 1
	for valueStart < len(content) {
		r, size := utf8.DecodeRune(content[valueStart:])
		if !envSpace(r) {
			break
		}
		// A CRLF is the line ending, not leading whitespace: keep it outside the
		// value span so an empty value is not rewritten as "KEY=\rnew".
		if r == '\r' && valueStart+size < len(content) && content[valueStart+size] == '\n' {
			break
		}
		valueStart += size
	}
	return key, valueStart, true
}

// envValueSpan returns the offset one past the part of the value the parser
// keeps (extractVarValue) and, when the value is quoted, its quote character.
// The span includes the delimiters so a rewrite can see them; the bytes they
// enclose are the value. For an unquoted value the span is the text up to
// trailing whitespace and an inline " # comment". A line ending is never part
// of the value, so a CRLF assignment keeps its \r.
func envValueSpan(content []byte, valueStart int) (end int, quote byte) {
	if valueStart < len(content) && (content[valueStart] == '"' || content[valueStart] == '\'') {
		q := content[valueStart]
		for j := valueStart + 1; j < len(content); j++ {
			if content[j] != q || content[j-1] == '\\' {
				continue
			}
			// The closing quote ends the value but is not part of it: rewrite
			// replaces the bytes between the delimiters and keeps the
			// delimiters themselves, so a byte the parser placed after the
			// closing quote stays a separate statement.
			return j + 1, q
		}
		// An unterminated quote is not a value that runs to the end of the file:
		// fall back to the line, so the assignments after it stay visible.
	}

	lineEnd := len(content)
	if n := bytes.IndexAny(content[valueStart:], "\r\n"); n >= 0 {
		lineEnd = valueStart + n
	}
	end = lineEnd
	// An inline comment starts at a "#" preceded by whitespace; it is not part
	// of the value and must survive the rewrite.
	for j := end - 1; j > valueStart; j-- {
		if content[j] != '#' {
			continue
		}
		if r, _ := utf8.DecodeLastRune(content[valueStart:j]); envSpace(r) {
			end = j
			break
		}
	}
	for end > valueStart {
		r, size := utf8.DecodeLastRune(content[valueStart:end])
		if !envSpace(r) {
			break
		}
		end -= size
	}
	return end, 0
}

// rewriteEnvValues replaces the value of every assignment of key in content,
// leaving every other byte — comments, blank lines, key order, the prefix and
// separator style the user wrote, inline comments and line endings — exactly as
// it was. It returns the new content, how many assignments were rewritten, and
// an error when the value cannot be written at all: a value the parser would
// read back as something other than what was written is refused, not corrupted
// (see envValueReplacement and verifyEnvRewrite).
func rewriteEnvValues(content []byte, key, value string) ([]byte, int, error) {
	out := make([]byte, 0, len(content)+len(value))
	last := 0
	rewritten := 0
	for _, a := range scanEnvAssignments(content) {
		if a.key != key {
			continue
		}
		replacement, err := envValueReplacement(content, a, value)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, content[last:a.valueStart]...)
		out = append(out, replacement...)
		last = a.valueEnd
		rewritten++
	}
	out = append(out, content[last:]...)
	return out, rewritten, nil
}

// envValueReplacement renders value in the span of a, preserving the parser's
// end-of-value boundary.
//
// For a quoted value the parser ends the value at the closing quote, and
// whatever follows it is a separate statement: "#prod-secret-tag" is a comment,
// "OTHER=x" is another assignment. Replacing the whole span (quotes included)
// with an unquoted value glued those bytes onto the new value — the parser then
// read "NEWVALUE#prod-secret-tag", or lost the OTHER assignment entirely
// ("NEWVALUEOTHER=x"). So the quoting style is kept and only the bytes between
// the delimiters are replaced: `"old"#tag` becomes `"NEWVALUE"#tag`.
//
// Keeping the original quotes is only safe when the value survives that form
// (envValueFitsQuote). Otherwise the value is written in a form the parser
// reads back verbatim, in this order: bare when that is safe
// (envValueFitsBare), then single-quoted, because that form is taken literally
// — it is the only form that can hold a "$", which the parser expands in both
// the bare and the double-quoted branch. Double quotes come last, for a value
// single quotes cannot hold (one starting with a "'"); they are safe only when
// the value has no "$", '"' or backslash, which is exactly what
// envValueFitsQuote('"') checks, because the parser expands and unescapes
// inside them (#2164). A line break is still unrepresentable in every form. A bare value keeps the user's
// style and gets one separating space before a trailing "#" comment so it
// cannot be glued ("xy\"z #tag"). When another assignment follows on the same
// line, a bare value would swallow it (an unquoted value runs to the end of
// the line), so the quoted form is used instead. When no form can hold the
// value, the rewrite fails closed instead of writing a file the service cannot
// read.
func envValueReplacement(content []byte, a envAssignment, value string) ([]byte, error) {
	if a.quote != 0 && envValueFitsQuote(a.quote, value) {
		replacement := make([]byte, 0, len(value)+2)
		replacement = append(replacement, a.quote)
		replacement = append(replacement, value...)
		replacement = append(replacement, a.quote)
		return replacement, nil
	}

	// What the parser will see after the value span: a comment, a line ending,
	// the end of the file, or another statement. Only a "#" can be separated
	// from a bare value by one space and still read as a comment.
	var trailing byte
	if a.valueEnd < len(content) {
		if r, _ := utf8.DecodeRune(content[a.valueEnd:]); !envSpace(r) && r != '\n' {
			trailing = content[a.valueEnd]
		}
	}

	if envValueFitsBare(value) && (trailing == 0 || trailing == '#') {
		replacement := make([]byte, 0, len(value)+1)
		replacement = append(replacement, value...)
		if trailing == '#' {
			replacement = append(replacement, ' ')
		}
		return replacement, nil
	}
	if envValueFitsQuote('\'', value) {
		return []byte("'" + value + "'"), nil
	}
	// The double-quoted branch is expanded and unescaped by the parser, so
	// envValueFitsQuote('"') admits it only for a value with no "$", '"' or
	// backslash. That still rescues values single quotes cannot hold — one
	// starting with "'" — which used to be refused outright (#2164).
	if envValueFitsQuote('"', value) {
		return []byte(`"` + value + `"`), nil
	}
	return nil, envValueRefusal(content, &a, a.key)
}

// envValueFitsQuote reports whether value can be written between the given
// quotes and read back unchanged by godotenv.
//
// A quote character would close the value early; a line break would split the
// statement (and change the file's line structure). The double-quoted form is
// additionally unescaped and variable-expanded by the parser (extractVarValue
// calls expandEscapes and expandVariables), so a backslash or a "$" would not
// survive verbatim. The single-quoted form is taken literally, but the parser's
// closing-quote search skips a quote preceded by a backslash for *both* styles
// (extractVarValue: "skip escaped quote symbol"), so a value ending in a
// backslash would escape the closing delimiter and leave the rest of the file
// unparseable.
func envValueFitsQuote(quote byte, value string) bool {
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r':
			return false
		case r == rune(quote):
			return false
		case quote == '"' && (r == '\\' || r == '$'):
			return false
		}
	}
	return !strings.HasSuffix(value, `\`)
}

// envValueFitsBare reports whether value can be written unquoted and read back
// unchanged by godotenv.
//
// The parser trims a bare value and then expands variables in it
// (extractVarValue's unquoted branch ends with expandVariables), so a "$" does
// not survive verbatim. A "#" is refused too, conservatively: preceded by
// whitespace it starts an inline comment, and at the start of the value it
// becomes literal only by accident of the parser's `i > 0` rule. A line break
// ends the statement. Surrounding whitespace is trimmed and a leading quote
// switches the parser to its quoted branch. Everything else is literal — a
// backslash included, because only "\$" is special to the expander and "$" is
// already refused.
func envValueFitsBare(value string) bool {
	if value == "" {
		// An empty bare value reads back as "", but "''" states it explicitly
		// and cannot be confused with a missing value.
		return false
	}
	for _, r := range value {
		switch r {
		case '\n', '\r', '$', '#':
			return false
		}
	}
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	if envSpace(first) || envSpace(last) {
		return false
	}
	return first != '\'' && first != '"'
}

// envValueRefusal is the error for a value no quoting style in the file can
// hold. It names the key and the line to edit, because the only fix is a hand
// edit: the file is left byte-identical rather than corrupted.
func envValueRefusal(content []byte, a *envAssignment, key string) error {
	if a == nil {
		return fmt.Errorf("cannot write %s: the value cannot be represented in this file's quoting style; edit the file by hand — the file is unchanged", key)
	}
	line, text := envLineAt(content, a.valueStart)
	return fmt.Errorf("cannot write %s: the value cannot be represented in this file's quoting style (line %d: %s); edit that line by hand — the file is unchanged", key, line, text)
}

// envLineAt reports the 1-based line number containing offset and that line's
// text with its ending removed, so an error can point at something the user can
// find and edit.
func envLineAt(content []byte, offset int) (int, string) {
	if offset > len(content) {
		offset = len(content)
	}
	line := 1 + bytes.Count(content[:offset], []byte("\n"))
	start := bytes.LastIndexByte(content[:offset], '\n') + 1
	end := len(content)
	if n := bytes.IndexByte(content[offset:], '\n'); n >= 0 {
		end = offset + n
	}
	return line, strings.TrimSuffix(string(content[start:end]), "\r")
}

// appendEnvAssignment adds key=value as the last line, matching the file's
// existing line ending and preserving whether it ended with a newline at all.
// The value uses the same safe rendering as a rewrite, so a value containing a
// "$" is quoted instead of being expanded by the service's loader.
func appendEnvAssignment(content []byte, key, value string) ([]byte, error) {
	var text string
	switch {
	case envValueFitsBare(value):
		text = value
	case envValueFitsQuote('\'', value):
		text = "'" + value + "'"
	case envValueFitsQuote('"', value):
		text = `"` + value + `"`
	default:
		return nil, envValueRefusal(content, nil, key)
	}

	assignment := key + "=" + text
	out := make([]byte, 0, len(content)+len(assignment)+2)
	out = append(out, content...)
	switch {
	case bytes.HasSuffix(out, []byte("\r\n")):
		out = append(out, assignment...)
		out = append(out, '\r', '\n')
	case len(out) == 0 || bytes.HasSuffix(out, []byte("\n")):
		out = append(out, assignment...)
		out = append(out, '\n')
	default:
		out = append(out, '\n')
		out = append(out, assignment...)
	}
	return out, nil
}

// Update rewrites every assignment of key in an env file, or appends one if the
// key is not assigned, and replaces the file atomically at mode.
//
// "Every assignment" is the point: godotenv and `docker compose --env-file`
// are both last-wins, so rewriting only the first occurrence left the service
// reading the stale duplicate while the command reported success (#2119). All
// occurrences are rewritten to the new value — when a file defines the same key
// twice with different values the user's intent is already ambiguous, and
// last-wins means the value the service read before this call was the last
// one. A single stderr line reports how many assignments were rewritten, so a
// duplicated file is normalised visibly rather than silently.
//
// The rewrite is deliberately line-preserving: comments, blank lines, key
// order and the way the user wrote each assignment (indentation, "export ", "="
// or ":" and the spacing around it, quoting, an inline comment, CRLF endings)
// survive, and every line that is not an assignment of key is carried through
// untouched. A span rewrite therefore cannot destroy lines it does not
// understand, which is why this does not route through writeEnvLocal's godotenv
// round-trip: that would reorder a hand-maintained server env file and drop its
// comments.
//
// The mode is not inherited. The file holds billable provider credentials, so
// an existing over-permissive file (written by an older CLI or by the user's
// umask) is replaced at the mode the caller passes: os.WriteFile applies its
// perm argument only when it creates the file, while the atomic replace below
// always creates the new file itself. The new value is therefore never readable
// at the old mode.
//
// Before anything is written the candidate content goes through a round-trip
// guard (verifyEnvRewrite): the target key must parse back as the new value and
// every other key must keep its old value. That turns any remaining edge case in
// the writer into a refusal with the file untouched, instead of a silently
// wrong or unparseable .env.local.
//
// The write is atomic (temp file + rename), so a write error cannot leave the
// env file truncated with its secrets half-written.
func Update(envPath, key, value string, mode os.FileMode) error {
	content, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	updated, rewritten, err := applyOne(content, key, value)
	if err != nil {
		return err
	}

	if err := replaceFileAtomically(envPath, updated, mode); err != nil {
		return err
	}

	reportDuplicates(envPath, key, rewritten)
	return nil
}

// UpdateAll applies every pair to the same file in one rewrite. A value that
// cannot be represented refuses the whole update with the file byte-identical,
// so a multi-key write (the install wizard's provider settings) never lands half
// applied — the old writer joined the lines and wrote them in one go, and that
// all-or-nothing property is kept. Keys are applied in sorted order, so the
// append order and the duplicate notices are deterministic.
func UpdateAll(envPath string, values map[string]string, mode os.FileMode) error {
	if len(values) == 0 {
		return nil
	}
	content, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	updated := content
	rewritten := make(map[string]int, len(values))
	for _, key := range keys {
		next, n, err := applyOne(updated, key, values[key])
		if err != nil {
			return err
		}
		updated = next
		rewritten[key] = n
	}

	if err := replaceFileAtomically(envPath, updated, mode); err != nil {
		return err
	}
	for _, key := range keys {
		reportDuplicates(envPath, key, rewritten[key])
	}
	return nil
}

// applyOne rewrites (or appends) one key in content and returns the candidate
// bytes. Nothing is written here: the caller decides when the whole update is
// ready, which is what makes UpdateAll atomic across keys.
func applyOne(content []byte, key, value string) ([]byte, int, error) {
	updated, rewritten, err := rewriteEnvValues(content, key, value)
	if err != nil {
		return nil, 0, err
	}
	if rewritten == 0 {
		updated, err = appendEnvAssignment(content, key, value)
		if err != nil {
			return nil, 0, err
		}
	}

	if err := verifyRewrite(content, updated, key, value); err != nil {
		return nil, 0, err
	}
	return updated, rewritten, nil
}

// reportDuplicates names an ambiguous file only once the new value is on disk,
// so the notice describes what actually happened. One assignment is the quiet,
// common case; more than one is worth naming, because the file the service reads
// was ambiguous before this rewrite.
func reportDuplicates(envPath, key string, rewritten int) {
	if rewritten > 1 {
		fmt.Fprintf(os.Stderr, "warning: %s is defined %d times in %s; rewrote all %d occurrences to the new value\n", key, rewritten, envPath, rewritten)
	}
}

// verifyRewrite is the seam applyOne calls. Production never reassigns it; a
// test swaps it to prove the guard is on the write path at all — the guard's
// own cases cannot fail that way, because a candidate it would refuse is a
// candidate the renderer already refuses, so no input distinguishes "guard ran
// and approved" from "guard was skipped" (issue #2148).
var verifyRewrite = verifyEnvRewrite

// verifyEnvRewrite is the round-trip guard that runs before anything is
// written: the candidate content is parsed with the loader the CLI itself uses
// (godotenv), and the target key must read back as the new value while every
// other key keeps the value it had. A candidate that fails either half is
// refused: the file is the service's environment, so a silently wrong value or
// an unparseable file is worse than an error the user can act on.
//
// The guard exists so the writer does not have to model every quoting edge case
// correctly to be safe. The renderer turns the common cases into successes; the
// guard turns every remaining one into a refusal with the file byte-identical.
//
// A file that did not parse before the rewrite is not gated: it is already
// unreadable by the loader, and the rewrite is byte-preserving apart from the
// target's value span, so there is no map to compare and nothing the rewrite
// could make parse worse except the span it owns.
func verifyEnvRewrite(before, after []byte, key, value string) error {
	previous, err := godotenv.Unmarshal(string(before))
	if err != nil {
		return nil
	}

	parsed, err := godotenv.Unmarshal(string(after))
	if err != nil || parsed[key] != value {
		return envRewriteRefusal(before, key)
	}

	delete(previous, key)
	delete(parsed, key)
	if !maps.Equal(previous, parsed) {
		return envRewriteRefusal(before, key)
	}
	return nil
}

// envRewriteRefusal is the guard's error. It points at the assignment the user
// has to fix by hand and says the file was not touched, so the refusal is
// actionable rather than a mystery.
func envRewriteRefusal(content []byte, key string) error {
	for _, a := range scanEnvAssignments(content) {
		if a.key != key {
			continue
		}
		line, text := envLineAt(content, a.valueStart)
		return fmt.Errorf("cannot write %s: the rewrite would not read back as the new value (line %d: %s); the value cannot be represented in this file's quoting style, so edit that line by hand — the file is unchanged", key, line, text)
	}
	return fmt.Errorf("cannot write %s: the rewrite would not read back as the new value; the value cannot be represented in this file's quoting style, so edit the file by hand — the file is unchanged", key)
}

// os.CreateTemp and os.Rename as seams: tests force the two failure points of
// the atomic write (the directory cannot take a temp file, the rename fails)
// without a read-only mount or a bind mount.
var (
	createTempFile = os.CreateTemp
	renamePath     = os.Rename
)

// replaceFileAtomically writes data to path through a temp file in the same
// directory and a rename, so a failed write never truncates the file the
// service reads: an in-place write that hits ENOSPC on a small tmpfs leaves an
// empty credential file, while a failed temp write leaves the old bytes and the
// old name intact (#2119).
//
// A symlinked path is resolved first: rename would replace the link itself,
// while writing through a link is the behaviour to keep.
//
// Consequences, all deliberate and all fail-closed:
//
//   - The containing directory must be writable even when the file is. An
//     in-place write could update a writable file in a read-only directory;
//     this returns an error naming the file and the directory instead, with the
//     original file intact.
//   - A bind-mounted file cannot be renamed over: the rename fails EBUSY
//     ("device or resource busy") and the original file is intact.
//   - The inode is replaced, so hard links to the old file keep the old value.
//     Ownership is restored from the file being replaced when the platform and
//     the caller's privileges allow it (adoptOwnership): root writing a
//     user-owned .env.local leaves it user-owned. A non-root writer with write
//     access to someone else's file cannot chown, so the result is owned by
//     that writer, and an ACL or xattr on the old inode is not carried over
//     either.
//   - Durability: the temp file is fsynced before the rename and the directory
//     after it, which is what makes a completed call survive a crash on a
//     filesystem that honours fsync. Atomicity is the stronger promise: a crash
//     before the rename leaves the old file (and possibly the temp file, which
//     nothing can clean up after a crash), never a mixture of old and new.
func replaceFileAtomically(path string, data []byte, mode os.FileMode) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", path, err)
	}
	dir := filepath.Dir(resolved)

	tmp, err := createTempFile(dir, tempNamePrefix(resolved))
	if err != nil {
		return fmt.Errorf("cannot replace %s atomically: the directory %s must be writable to write a temporary file there: %w", resolved, dir, err)
	}
	tmpPath := tmp.Name()
	// No-op after a successful rename; this removes the temp file on every error
	// path below, so a failed write never leaves a credential copy behind.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed writing the new contents of %s (the original file is unchanged): %w", resolved, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to flush the new contents of %s to disk (the original file is unchanged): %w", resolved, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close the new contents of %s (the original file is unchanged): %w", resolved, err)
	}
	// CreateTemp already creates 0600, but the mode is stated explicitly so a
	// future temp-creation change cannot widen it silently.
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("failed to set mode %04o on the replacement for %s (the original file is unchanged): %w", mode.Perm(), resolved, err)
	}
	// rename replaces the inode, so without this the owner would become whoever
	// ran the command.
	adoptOwnership(resolved, tmpPath)
	if err := renamePath(tmpPath, resolved); err != nil {
		return fmt.Errorf("failed to replace %s (the original file is unchanged): %w", resolved, err)
	}
	// The rename is durable only once the directory entry is on disk. Best
	// effort: some platforms and filesystems reject fsync on a directory
	// (EINVAL), and the file contents are already flushed above.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// tempNamePrefix is the hidden prefix for the temp file that replaces path:
// ".env.local.tmp-", not "..env.local.tmp-", so the name stays recognisable in
// an error message and in a directory listing.
func tempNamePrefix(path string) string {
	return "." + strings.TrimLeft(filepath.Base(path), ".") + ".tmp-"
}

// Read returns the value key has in an env file's content, the way the parser
// the service uses reads it: the last assignment wins (godotenv and
// `docker compose --env-file` are both last-wins), a quoted value comes back
// without its delimiters, and an inline "# comment" is not part of the value.
//
// It is the read side of this package, so a caller that displays or forwards a
// value does not re-implement the quoting: a value the writer single-quoted
// because it contains a "#" reads back as the service sees it instead of with
// the quotes still on (#2170).
//
// A quote the file never closes is reported by the scanner as "no quote
// character", so the span keeps its opening delimiter: the value's end is not
// knowable, and guessing it would be worse than showing the file's bytes.
//
// A "$" reference is not expanded and a backslash escape is not resolved — the
// scanner exists to rewrite values, not to evaluate them. Callers that need the
// loader's expansion, or that need to know the file is well-formed, should read
// the file the way the service does (godotenv) and use this only where the
// loader has rejected the file or where the value is handed to a tool that reads
// it literally.
func Read(content []byte, key string) (string, bool) {
	var (
		value string
		found bool
	)
	for _, a := range scanEnvAssignments(content) {
		if a.key != key {
			continue
		}
		text := content[a.valueStart:a.valueEnd]
		if a.quote != 0 && len(text) > 0 {
			// A terminated quote: the span starts at the opening delimiter and
			// ends after the closing one. (An unterminated quote is reported
			// with no quote character at all, so this branch does not run and
			// the raw span — opening delimiter included — is returned.)
			text = bytes.TrimSuffix(text[1:], []byte{a.quote})
		}
		value, found = string(text), true
	}
	return value, found
}
