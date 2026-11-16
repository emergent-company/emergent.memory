// Package toolargscan finds the call shape a tool argument validator must not
// use: a plain fmt.Errorf / errors.New whose message reads like input
// validation ("is required", "invalid …", "must be …").
//
// Such an error is indistinguishable from a driver failure at the tools/call
// boundary, so it collapses to "An internal error occurred" and the caller
// never learns which argument to fix (issue #668). The scan complements the
// handler tests: a handler test can only cover a validator that exists, while
// this catches the next one before a model does.
//
// The scan is deliberately call-shaped rather than line-shaped. A validator
// split across lines (or written with errors.New) is exactly the site a naive
// per-line grep misses, so it locates each fmt.Errorf(/errors.New( call and
// extracts the balanced argument list before matching the message keywords.
package toolargscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Exemptions lists the deliberate non-argument errors a scanned file may
// contain. Both maps exist so an exemption is a reviewed decision with a
// reason, not a silent hole in the scan's coverage.
type Exemptions struct {
	// Files maps a base file name to why every match in it is not a tool
	// argument refusal.
	Files map[string]string

	// Messages maps a substring of a call's message to why that particular
	// message is not a tool argument refusal.
	Messages map[string]string
}

// Finding is one offending call site.
type Finding struct {
	File    string
	Line    int
	Message string
}

// String renders a finding as path:line plus the matched call's first line, so
// a test failure points straight at the site.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Message)
}

// callRe matches the two constructors whose result reaches a caller as an
// error. Both are scanned: a validator is as likely to be written with
// errors.New as with fmt.Errorf.
var callRe = regexp.MustCompile(`\b(?:fmt\.Errorf|errors\.New)\(`)

// keywordRe is the validation vocabulary from issue #668: the original scan's
// keywords plus the sibling families the conversion found.
var keywordRe = regexp.MustCompile(
	`(?i)(is required|are required|required parameter|required argument|must be|must not|invalid |unsupported|not supported|unknown |missing|cannot be combined|no valid |exceeds )`,
)

// Scan reports the offending call sites in paths.
func Scan(paths []string, ex Exemptions) ([]Finding, error) {
	var findings []Finding
	for _, path := range paths {
		base := filepath.Base(path)
		if _, ok := ex.Files[base]; ok {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		text := string(src)
		for _, loc := range callRe.FindAllStringIndex(text, -1) {
			// loc[1] is just past the '('.
			args := balancedArguments(text, loc[1]-1)
			if args == "" || !keywordRe.MatchString(args) {
				continue
			}
			if exemptMessage(args, ex.Messages) {
				continue
			}
			findings = append(findings, Finding{
				File:    base,
				Line:    1 + strings.Count(text[:loc[0]], "\n"),
				Message: firstNonEmptyLine(args),
			})
		}
	}
	return findings, nil
}

// ScanDir scans the non-test .go files directly inside dir.
func ScanDir(dir string, ex Exemptions) ([]Finding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return Scan(paths, ex)
}

// balancedArguments returns the text between the '(' at open and its matching
// ')' — which may span lines — skipping string and rune literals so a ')' (or
// a '(') inside a message cannot end the call early. It returns "" when the
// text ends before the call closes.
func balancedArguments(text string, open int) string {
	depth := 0
	for i := open; i < len(text); i++ {
		switch c := text[i]; c {
		case '"':
			i = skipQuoted(text, i, '"')
		case '`':
			end := strings.IndexByte(text[i+1:], '`')
			if end < 0 {
				return ""
			}
			i += end + 1
		case '\'':
			i = skipQuoted(text, i, '\'')
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[open+1 : i]
			}
		}
	}
	return ""
}

// skipQuoted returns the index of the closing quote for the literal starting at
// start, honouring backslash escapes, or len(text)-1 when unterminated.
func skipQuoted(text string, start int, quote byte) int {
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return len(text) - 1
}

func exemptMessage(args string, messages map[string]string) bool {
	for substring := range messages {
		if strings.Contains(args, substring) {
			return true
		}
	}
	return false
}

// firstNonEmptyLine keeps the failure output to the line that carries the
// message for a multi-line call.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}
