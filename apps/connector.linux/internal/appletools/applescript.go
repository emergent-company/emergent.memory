package appletools

import (
	"errors"
	"fmt"
	"strings"
)

// escapeHelpers is AppleScript prelude shared by all generated scripts. It
// JSON-escapes text values that came from the user's own data (note names,
// snippets, reminder names) so returned output is always valid JSON.
//
// AppleScript escapes `"` and `\` inside string literals with a backslash, so
// a literal backslash is `"\\"` and a literal double quote is `"\""`.
// Order matters: backslash first, then double quotes, then line endings.
const escapeHelpers = `on escapeForJSON(s)
	set s to replaceAll(s, "\\", "\\\\")
	set s to replaceAll(s, "\"", "\\\"")
	set s to replaceAll(s, linefeed, "\\n")
	set s to replaceAll(s, return, "\\n")
	set s to replaceAll(s, tab, "\\t")
	return "\"" & s & "\""
end escapeForJSON

on replaceAll(s, findText, replText)
	set savedTID to AppleScript's text item delimiters
	set AppleScript's text item delimiters to findText
	set parts to every text item of s
	set AppleScript's text item delimiters to replText
	set joined to parts as text
	set AppleScript's text item delimiters to savedTID
	return joined
end replaceAll
`

// appleExpr renders s as a single-line AppleScript string expression. It
// escapes backslashes and double quotes (AppleScript string-literal escapes)
// and splices real newlines into `"…" & linefeed & "…"` concatenations,
// because an AppleScript string literal cannot span source lines. The result
// contains no newline bytes and is safe to interpolate into a script.
func appleExpr(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	parts := strings.Split(s, "\n")
	if len(parts) == 1 {
		return "\"" + s + "\""
	}
	var b strings.Builder
	b.WriteString("(")
	for i, p := range parts {
		if i > 0 {
			b.WriteString(" & linefeed & ")
		}
		b.WriteString("\"")
		b.WriteString(p)
		b.WriteString("\"")
	}
	b.WriteString(")")
	return b.String()
}

// preview bounds s for embedding in error messages.
func preview(s string) string {
	const max = 200
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// mapRunnerError turns runner failures into guidance a tool caller can act on.
func mapRunnerError(err error) error {
	switch {
	case errors.Is(err, ErrNotAuthorized):
		return fmt.Errorf("%w: grant Automation permission for this host in System Settings → Privacy & Security", ErrNotAuthorized)
	case errors.Is(err, ErrUnavailable):
		return fmt.Errorf("%w: Notes/Reminders tools need macOS", ErrUnavailable)
	default:
		return err
	}
}
