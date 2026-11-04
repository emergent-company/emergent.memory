package appletools

import (
	"strings"
	"testing"
)

// scriptSeeds returns pairs (name, adversarial input) exercising quotes,
// backslashes, line endings, and unicode through every generator.
func scriptSeeds() []struct{ name, seed string } {
	return []struct{ name, seed string }{
		{name: "quotes", seed: `say "hi" then \ done`},
		{name: "backslash", seed: `C:\Users\me\file`},
		{name: "newlines", seed: "line one\nline two\r\nline three\r"},
		{name: "trailing backslash", seed: `trailing\`},
		{name: "unicode", seed: "naïve café — 日本語 🙂"},
		{name: "parens and commas", seed: `a(b), c[d]; e"f`},
	}
}

// allScripts returns every generated script with the given seed threaded
// through each generator's input positions.
func allScripts(seed string) []string {
	return []string{
		notesSearchScript(seed, seed, 20),
		notesSearchScript("plain", "Work", 3),
		notesCreateScript("Title "+seed, "Body "+seed),
		remindersListScript("List "+seed, false, false),
		remindersListScript("", true, true),
		remindersListsScript(),
		remindersAddScript("Task "+seed, "Notes "+seed, true, "(current date) + 3600", "List "+seed),
		remindersAddScript("Task "+seed, "", false, "missing value", ""),
	}
}

// appleScriptStringsValid checks that every line of script is lexically sound:
// string literals open and close on the same line (AppleScript strings cannot
// span lines) honouring the `\"` and `\\` escapes. This deterministically
// catches runaway-string bugs of the class found in the field
// (an unterminated literal making osascript fail with "Expected ',' or ')'").
func appleScriptStringsValid(t *testing.T, script string) {
	t.Helper()
	for i, line := range strings.Split(script, "\n") {
		inString := false
		escaped := false
		for _, r := range line {
			switch {
			case inString && escaped:
				escaped = false
			case inString && r == '\\':
				escaped = true
			case inString && r == '"':
				inString = false
			case !inString && r == '"':
				inString = true
			}
		}
		if inString {
			t.Errorf("line %d has an unterminated string literal: %q", i+1, line)
		}
	}
}

func TestGeneratedScriptsLexicallyValid(t *testing.T) {
	for _, tc := range scriptSeeds() {
		t.Run(tc.name, func(t *testing.T) {
			for _, script := range allScripts(tc.seed) {
				appleScriptStringsValid(t, script)
			}
		})
	}
}

func TestEscapeHelpersCanonicalBackslashEscaping(t *testing.T) {
	// Regression: the backslash replacement used to be emitted as
	// replaceAll(s, "\", "\\") — an escaped double quote with no closing
	// delimiter — which makes osascript choke mid-script. The find string
	// must be a literal backslash ("\\") and the replacement two backslashes
	// ("\\\\").
	if !strings.Contains(escapeHelpers, `replaceAll(s, "\\", "\\\\")`) {
		t.Errorf("escapeHelpers must replace a backslash with two backslashes; got:\n%s", escapeHelpers)
	}
	if strings.Contains(escapeHelpers, `replaceAll(s, "\", "\\")`) {
		t.Errorf("escapeHelpers contains the buggy unterminated-string form:\n%s", escapeHelpers)
	}
	if !strings.Contains(escapeHelpers, `replaceAll(s, "\"", "\\\"")`) {
		t.Errorf("escapeHelpers must escape double quotes; got:\n%s", escapeHelpers)
	}
	if !strings.Contains(escapeHelpers, `return "\"" & s & "\""`) {
		t.Errorf("escapeHelpers must wrap values in double quotes; got:\n%s", escapeHelpers)
	}
}

func TestEscapeHelpersRunsLineByLine(t *testing.T) {
	appleScriptStringsValid(t, escapeHelpers)
}

func TestAppleExprUnescapeRoundTrip(t *testing.T) {
	cases := []string{
		`plain`,
		`with "quotes" and \backslashes`,
		"multi\nline\ntext",
		"crlf\r\nand\r",
		`ends with backslash\`,
		"unicode 🙂 and \"quoted\"\nsecond line",
		"",
	}
	for _, in := range cases {
		expr := appleExpr(in)
		got := evalAppleExpr(t, expr)
		want := strings.ReplaceAll(strings.ReplaceAll(in, "\r\n", "\n"), "\r", "\n")
		if got != want {
			t.Errorf("appleExpr round trip:\n  in   = %q\n  expr = %s\n  got  = %q\n  want = %q", in, expr, got, want)
		}
	}
}

// evalAppleExpr evaluates the tiny expression language appleExpr emits:
// a single quoted literal or a parenthesised `"a" & linefeed & "b"` chain.
func evalAppleExpr(t *testing.T, expr string) string {
	t.Helper()
	inner := expr
	if strings.HasPrefix(inner, "(") && strings.HasSuffix(inner, ")") {
		inner = inner[1 : len(inner)-1]
	}
	parts := strings.Split(inner, " & linefeed & ")
	var out strings.Builder
	for i, part := range parts {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(unescapeAppleLiteral(t, part))
	}
	return out.String()
}

// unescapeAppleLiteral decodes one AppleScript string literal (including the
// surrounding quotes) under AppleScript's rules: `\"` -> `"`, `\\` -> `\`.
func unescapeAppleLiteral(t *testing.T, lit string) string {
	t.Helper()
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		t.Fatalf("not an AppleScript string literal: %q", lit)
	}
	var b strings.Builder
	for i := 1; i < len(lit)-1; i++ {
		c := lit[i]
		if c == '\\' && i+1 < len(lit)-1 {
			n := lit[i+1]
			if n == '"' || n == '\\' {
				b.WriteByte(n)
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func TestRemindersListDueEmissionBalanced(t *testing.T) {
	// The reminders row embeds a JSON object with an optional due_date value;
	// verify the quoted skeleton survives escaping intact.
	script := remindersListScript(`Errands "A"`, false, false)
	for _, want := range []string{`"{\"id\":"`, `",\"name\":"`, `",\"list\":"`, `",\"due_date\":"`, `",\"completed\":"`, `set dq to "\""`} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %s:\n%s", want, script)
		}
	}
	appleScriptStringsValid(t, script)
}

// handlerPlacementValid asserts every `on <handler>` declaration begins its
// own line at statement position — never glued onto the tail of a preceding
// statement/string (the reminders `use framework` defect class, which made
// osascript report: A "on" can't go after this """.).
func handlerPlacementValid(t *testing.T, script string) {
	t.Helper()
	lines := strings.Split(script, "\n")
	prev := "" // previous non-empty line, trimmed
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "on ") {
			if prev != "" && !strings.HasPrefix(prev, "end ") && prev != `use framework "Foundation"` && !strings.HasPrefix(prev, `--`) {
				t.Errorf("line %d: handler %q follows %q on a separate statement it cannot follow", i+1, trimmed, prev)
			}
		}
		if trimmed != "" {
			prev = trimmed
		}
	}
	// Direct glue detector: a handler name immediately after a closing quote.
	for _, glued := range []string{`"on escapeForJSON`, `"on replaceAll`} {
		if strings.Contains(script, glued) {
			t.Errorf("script glues a handler onto a string literal (contains %q)", glued)
		}
	}
}

func TestHandlerDeclarationsNeverGlued(t *testing.T) {
	for _, tc := range scriptSeeds() {
		t.Run(tc.name, func(t *testing.T) {
			for _, script := range allScripts(tc.seed) {
				handlerPlacementValid(t, script)
				appleScriptStringsValid(t, script)
			}
		})
	}
}

func TestRemindersPreludeLayout(t *testing.T) {
	// `use framework` owns line 1; the escape helpers follow on their own
	// lines (regression for the glued "Foundation"on escapeForJSON defect).
	scripts := []string{
		remindersListScript("", false, false),
		remindersListScript(`List "quoted" \`, true, true),
		remindersAddScript("t", "", false, "missing value", ""),
	}
	for i, script := range scripts {
		lines := strings.Split(script, "\n")
		if strings.HasPrefix(lines[0], `use framework "Foundation"`) {
			if lines[0] != `use framework "Foundation"` {
				t.Errorf("script %d line 1 has trailing content after use statement: %q", i, lines[0])
			}
			if !strings.HasPrefix(lines[1], "on escapeForJSON") {
				t.Errorf("script %d line 2 = %q, want handler declaration", i, lines[1])
			}
		} else if !strings.HasPrefix(lines[0], "on escapeForJSON") {
			t.Errorf("script %d line 1 = %q, want use framework or handler", i, lines[0])
		}
	}
}

// TestRemindersListBatchFetchShape guards the reminders_list AppleScript shape:
// ONE batched `get {name, due date, completed} of every reminder` (a single
// Apple event) whose result is the PARALLEL COLUMN LISTS
// {{names}, {dates}, {completed}}, destructured and zipped by index in a loop
// that runs strictly AFTER the `tell application "Reminders"` block closes, so
// the loop never issues an Apple event per reminder.
//
// The `whose completed is false` query is deliberately NOT used: on a large
// library it hangs / returns -1728. Completed filtering happens locally from
// the materialized `completed` column instead.
func TestRemindersListBatchFetchShape(t *testing.T) {
	open := remindersListScript("", false, false)
	openFolder := remindersListScript("Errands", false, false)
	all := remindersListScript("", true, false)

	for _, script := range []string{open, openFolder, all} {
		appleScriptStringsValid(t, script)
		handlerPlacementValid(t, script)
	}

	// Every reminder fetch must be the single batched property form.
	const batch = "set cols to (get {name, due date, completed, id, body} of every reminder"
	for i, line := range strings.Split(open, "\n") {
		if strings.Contains(line, "every reminder") && !strings.HasPrefix(strings.TrimSpace(line), batch) {
			t.Errorf("line %d fetches reminders outside the single batch get:\n%s", i+1, line)
		}
	}
	if strings.Contains(open, "whose") {
		t.Errorf("open script must not use a `whose` query (hangs on large libraries):\n%s", open)
	}
	if !strings.Contains(openFolder, "of list listName") {
		t.Errorf("folder-scoped open script must scope to the named list:\n%s", openFolder)
	}
	if !strings.Contains(all, "set includeCompleted to true") {
		t.Errorf("include_completed=true script must carry the include-all flag:\n%s", all)
	}

	// The processing loop must not live inside the tell block.
	for _, script := range []string{open, openFolder, all} {
		tell := strings.Index(script, "tell application \"Reminders\"")
		endTell := strings.Index(script, "end tell")
		repeat := strings.Index(script, "repeat with i from 1 to (count of theNames)")
		if tell < 0 || endTell < 0 || repeat < 0 {
			t.Fatalf("script missing tell/repeat markers:\n%s", script)
		}
		if repeat <= endTell {
			t.Errorf("repeat loop must run after end tell (no per-item app reads inside tell):\n%s", script)
		}
	}

	// The loop destructures the parallel column lists and zips them by index.
	for _, want := range []string{
		"set theNames to item 1 of cols",
		"set theDates to item 2 of cols",
		"set theDone to item 3 of cols",
		"set theIDs to item 4 of cols",
		"set theBodies to item 5 of cols",
		"set nm to item i of theNames",
		"set dueVal to item i of theDates",
		"set doneVal to item i of theDone",
		"set idVal to item i of theIDs",
		"if includeCompleted is true or doneVal is false then",
	} {
		if !strings.Contains(open, want) {
			t.Errorf("open script missing %q (columns must be destructured and zipped):\n%s", want, open)
		}
	}

	// No row-shaped iteration or per-item property reads remain.
	for _, bad := range []string{"repeat with rec in recs", "item 1 of rec", "item 2 of rec", "name of rec", "due date of rec", "of r\n", "of r "} {
		if strings.Contains(open, bad) {
			t.Errorf("script still uses row-shaped/per-item access (%q):\n%s", bad, open)
		}
	}

	// Row emission contract: enriched JSON skeleton.
	for _, want := range []string{`"{\"id\":"`, `",\"name\":"`, `",\"list\":"`, `",\"due_date\":"`, `",\"completed\":"`} {
		if !strings.Contains(open, want) {
			t.Errorf("open script missing %s:\n%s", want, open)
		}
	}
}
