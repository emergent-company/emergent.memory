package main

import (
	"go/parser"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Regression guards for #1903.
//
// Inside a templ component children block, a bare expression written on a single
// line — `@ui.DialogTitle(...) { item.Key }` — is parsed as the children
// delimiter plus raw text, not as an expression: templ emits
// templruntime.WriteString(buf, n, "item.Key ") and the page renders the source
// expression. `#1875` converted `<h3>{ item.Key }</h3>` into that form and the
// board drawer title silently started rendering the literal string `item.Key`.
//
// Two guards, the second wider than the component that exposed the bug:
//
//  1. TestWorkCardRendersItemLabel renders the affected component and asserts
//     the value appears and the source expression does not.
//  2. TestNoLiteralExpressionChildrenInGeneratedTemplates sweeps the generated
//     *_templ.go output for a raw-text write whose literal is a code-shaped Go
//     expression — exactly the shape the parser bug produces, anywhere in the
//     gateway, not just on the board. Sweeping the generated output (rather than
//     the templates) is exact: an expression compiled through JoinStringErrs
//     never appears as a WriteString literal, and prose never parses as a Go
//     expression, so no allowlist is needed. TestIsLiteralExpression pins the
//     classifier on both sides.

// TestWorkCardRendersItemLabel is the rendered-output assertion: the card shows
// the item's label value (the object header's name/key), never the source
// expression.
func TestWorkCardRendersItemLabel(t *testing.T) {
	const sentinel = "BOARD-LABEL-SENTINEL"
	item := WorkItem{CanonicalID: "w1", Type: "Task", Key: "k", Name: sentinel, Status: "todo"}

	html := renderHTML(t, workCard(item, workData{}))

	if !strings.Contains(html, sentinel) {
		t.Fatalf("card must render the work-item label %q; not found in rendered output", sentinel)
	}
	for _, expr := range []string{"it.Name", "it.Key"} {
		if strings.Contains(html, expr) {
			t.Fatalf("card rendered the literal expression %q instead of its value (#1903)", expr)
		}
	}
}

// writeStringLiteralRE extracts the string literal from a generated
// templruntime.WriteString(buf, n, "…") raw-text write.
var writeStringLiteralRE = regexp.MustCompile(`WriteString\([^,]*,\s*\d+,\s*("(?:[^"\\]|\\.)*")\)`)

// codeShapedLiteralRE reports whether text reads as Go code rather than prose: a
// selector (`item.Key`), a call (`fn(x)`), or an index (`item[0]`). Each
// alternative requires an identifier immediately adjacent to the operator, so
// prose containing a period or a parenthesised aside does not qualify.
var codeShapedLiteralRE = regexp.MustCompile(
	`[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_]|[A-Za-z_][A-Za-z0-9_]*\(|[A-Za-z_][A-Za-z0-9_]*\[`,
)

// TestNoLiteralExpressionChildrenInGeneratedTemplates fails when any gateway
// template writes a code-shaped Go expression as raw text, i.e. the #1903 bug
// shape. It scans the generated output because that is where the distinction is
// observable and exact.
func TestNoLiteralExpressionChildrenInGeneratedTemplates(t *testing.T) {
	globs := []string{"*_templ.go", filepath.Join("components", "*_templ.go")}

	var offenders []string
	scanned := 0

	for _, glob := range globs {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		if len(files) == 0 {
			// The package cannot compile without its generated files, so an empty
			// glob means the sweep is silently unscanned.
			t.Fatalf("glob %s matched no generated templates — the sweep would pass vacuously", glob)
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			scanned++
			for _, m := range writeStringLiteralRE.FindAllStringSubmatch(string(data), -1) {
				lit, err := strconv.Unquote(m[1])
				if err != nil {
					continue
				}
				expr := strings.TrimSpace(lit)
				if !isLiteralExpression(expr) {
					continue
				}
				offenders = append(offenders, filepath.ToSlash(file)+": "+strconv.Quote(expr))
			}
		}
	}

	if scanned == 0 {
		t.Fatal("no generated templates scanned — the sweep would pass vacuously")
	}
	if len(offenders) > 0 {
		t.Fatalf("generated template writes a Go expression as literal text (#1903); "+
			"put the expression on its own line inside a multi-line children block, "+
			"or wrap it in a second pair of braces ({ { expr } }):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// isLiteralExpression reports whether a raw-text literal is a code-shaped Go
// expression — the signature of the #1903 parser bug.
func isLiteralExpression(s string) bool {
	if s == "" || !codeShapedLiteralRE.MatchString(s) {
		return false
	}
	_, err := parser.ParseExpr(s)
	return err == nil
}

// TestIsLiteralExpression pins the classifier on both sides so the sweep above
// cannot silently widen into prose or narrow away from the bug shape.
func TestIsLiteralExpression(t *testing.T) {
	cases := []struct {
		text string
		want bool
		note string
	}{
		// The bug shape: a bare expression that templ wrote out as raw text.
		{"item.Key", true, "the #1903 instance"},
		{"item", false, "a bare identifier is ambiguous with a one-word label"},
		{"item[0]", true, "index expression"},
		{"fn(item)", true, "call expression"},
		{"*ptr", false, "no selector/call/index, so it is not distinguishable from text"},
		{"p.Description.After", true, "chained selector"},
		// Prose, mixed text and legit markup that must never be flagged.
		{"Roll back chunk", false, "prose label"},
		{"Backup in progress — this page refreshes automatically until it finishes.", false, "prose ending in a period"},
		{"The Memory API exposes the backup entity (the settings and counts above).", false, "prose with a parenthesised aside"},
		{"v", false, "single-letter fragment from `{ v{ b.Version } draft }`"},
		{" · draft", false, "text fragment after an inner expression"},
		{"Paste a GitHub repository URL to install the blueprint it contains.", false, "instructional prose"},
		{"", false, "empty"},
	}

	for _, tc := range cases {
		if got := isLiteralExpression(tc.text); got != tc.want {
			t.Errorf("isLiteralExpression(%q) = %v, want %v (%s)", tc.text, got, tc.want, tc.note)
		}
	}
}
