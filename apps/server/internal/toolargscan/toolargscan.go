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
	"strconv"
	"strings"
)

// Exemptions lists the deliberate non-argument errors a scanned file may
// contain. Both maps exist so an exemption is a reviewed decision with a
// reason, not a silent hole in the scan's coverage.
type Exemptions struct {
	// Files maps a base file name to why every match in it is not a tool
	// argument refusal.
	Files map[string]string

	// Sites maps one concrete call site to why it is not a tool argument
	// refusal. Use this rather than a phrase-wide exemption: see Site.
	Sites map[Site]string
}

// Site pins an exemption to one concrete constructor call: the file that holds
// it and the exact text of the call's first message literal. Both must match.
//
// The keys used to be bare substrings of the message, so an exemption written
// for one site also silenced every future site that happened to contain the
// same phrase — the scan stopped being a gate for the class it exists to catch
// (#930). An exact literal under a named file cannot be inherited by a new
// caller-facing error unless that error repeats the message verbatim in the
// same file, which is the same message under review again.
type Site struct {
	// File is the base name of the file holding the call, as reported in
	// Finding.File.
	File string

	// Message is the exact content of the call's first string literal (the
	// format string for fmt.Errorf). For a message built from several
	// literals, it is the first one, so pin a site whose message starts with
	// the reviewed phrase.
	Message string
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
//
// "not found" was added in review round 2 (#899): the first pass converted the
// argument-naming messages and left the lookup misses behind, so a caller who
// named a branch, schema or skill that does not exist got "An internal error
// occurred" (-32603) instead of the guidance telling it what to pass. A miss is
// as caller-addressable as a malformed argument, so it is scanned the same way.
var keywordRe = regexp.MustCompile(
	`(?i)(is required|are required|required parameter|required argument|must be|must not|invalid |unsupported|not supported|unknown |missing|cannot be combined|no valid |exceeds |not found)`,
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
			if exemptSite(base, args, ex.Sites) {
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

// exemptSite reports whether this call is one of the exempted sites. The file
// and the call's first message literal must both match exactly, so an
// exemption written for one call cannot silence a different one that merely
// contains the same phrase (#930).
func exemptSite(file, args string, sites map[Site]string) bool {
	if len(sites) == 0 {
		return false
	}
	literal, ok := firstStringLiteral(args)
	if !ok {
		return false
	}
	_, exempt := sites[Site{File: file, Message: literal}]
	return exempt
}

// firstStringLiteral returns the content of the first Go string literal in a
// call's argument list, so an exemption can name the exact message a call
// passes. It skips rune literals and reports false when a call passes no
// literal at all (a message held in a variable cannot be pinned, so it is not
// exemptable by literal).
func firstStringLiteral(args string) (string, bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '"':
			end := skipQuoted(args, i, '"')
			if end <= i {
				return "", false
			}
			if lit, err := strconv.Unquote(args[i : end+1]); err == nil {
				return lit, true
			}
			// An unterminated or otherwise unquotable literal: fall back to
			// the raw content so the scan still offers a stable key.
			return args[i+1 : end], true
		case '`':
			end := strings.IndexByte(args[i+1:], '`')
			if end < 0 {
				return "", false
			}
			return args[i+1 : i+1+end], true
		case '\'':
			i = skipQuoted(args, i, '\'')
		}
	}
	return "", false
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
