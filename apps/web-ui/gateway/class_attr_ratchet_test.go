package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// class_attr_ratchet_test.go enforces the shape behind #855: a component that
// writes its own class attribute and then spreads Attrs renders a *second*
// class attribute when the caller passes "class", and browsers keep only the
// first — so the caller's utility disappears with no error and no visual clue
// at the code. The go-daisy pin (v0.20.0) has that shape on ui.Badge, ui.Alert
// and ui.Button; upstream issue #150 is the fix.
//
// The supported slots are components.BadgeWithClass / components.AlertWithClass (Class, merged into
// the element's class list) and ui.Button (ExtraClass, which is a member of the
// button's own class list). This scan fails on the next call site that reaches
// for Attrs instead, because that call site would look right and render wrong.
//
// Best-effort and line-oriented, like the other source scans in this package: it
// reads authored .templ, tolerates nested calls and strings, and cannot tell a
// comment that quotes the call from a real one.

// classHookCallee names a callee whose own class attribute is written before
// Attrs is spread, plus the supported slot to use instead.
type classHookCallee struct {
	callee    string
	supported string
}

var classHookCallees = []classHookCallee{
	{callee: "ui.Badge(", supported: "components.BadgeWithClass's Class field"},
	{callee: "ui.Alert(", supported: "components.AlertWithClass's Class field"},
	{callee: "ui.Button(", supported: "ui.ButtonProps.ExtraClass"},
}

// classKeyRE matches a templ.Attributes map key named class.
var classKeyRE = regexp.MustCompile(`"class"\s*:`)

// classHookFindings scans one source file and reports every class key inside a
// call to one of the guarded callees, as "file:line".
func classHookFindings(file string, src []byte) []string {
	text := string(src)
	var findings []string
	for _, hook := range classHookCallees {
		for offset := 0; ; {
			idx := strings.Index(text[offset:], hook.callee)
			if idx < 0 {
				break
			}
			start := offset + idx
			end := balancedCallEnd(text, start+len(hook.callee)-1)
			if end < 0 {
				// Unterminated: point at the call rather than passing it.
				end = len(text)
			}
			if loc := classKeyRE.FindStringIndex(text[start:end]); loc != nil {
				line := strings.Count(text[:start+loc[0]], "\n") + 1
				findings = append(findings, fmt.Sprintf(
					"%s:%d: %s passes \"class\" through Attrs — use %s instead (a class in Attrs renders a duplicate class attribute, which the browser drops; see #855)",
					file, line, strings.TrimSuffix(hook.callee, "("), hook.supported,
				))
			}
			offset = end
		}
	}
	return findings
}

// balancedCallEnd returns the index just past the parenthesis that closes the
// call whose opening parenthesis is at open, skipping string and raw-string
// literals so a ")" inside one does not end the call early. It returns -1 when
// the call never closes.
func balancedCallEnd(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '"', '\'':
			quote := text[i]
			for i++; i < len(text); i++ {
				if text[i] == '\\' {
					i++
					continue
				}
				if text[i] == quote {
					break
				}
			}
		case '`':
			for i++; i < len(text) && text[i] != '`'; i++ {
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// TestClassInAttrsScannerCatchesTheShape proves the guard is not vacuous: the
// pre-#855 call shapes must be reported, and the supported ones must not.
func TestClassInAttrsScannerCatchesTheShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{
			name: "badge with class in Attrs",
			src:  "@ui.Badge(ui.BadgeProps{Label: \"3\", Attrs: templ.Attributes{\"class\": \"ml-1\"}})\n",
			want: 1,
		},
		{
			name: "alert with class in Attrs, multi-line and nested call",
			src:  "@ui.Alert(ui.AlertProps{Type: ui.AlertError, Message: n(\"x\"),\n\tAttrs: templ.Attributes{\"class\": \"mb-4\"}})\n",
			want: 1,
		},
		{
			name: "button with class in Attrs",
			src:  "@ui.Button(ui.ButtonProps{Attrs: templ.Attributes{\"class\": \"shrink-0\"}})\n",
			want: 1,
		},
		{
			name: "wrapper Class field is the supported shape",
			src:  "@components.AlertWithClass(components.AlertWithClassProps{AlertProps: ui.AlertProps{Message: \"m\"}, Class: \"mb-4\"})\n",
			want: 0,
		},
		{
			name: "button ExtraClass is the supported shape",
			src:  "@ui.Button(ui.ButtonProps{ExtraClass: \"shrink-0\", Attrs: templ.Attributes{\"data-testid\": \"x\"}})\n",
			want: 0,
		},
		{
			name: "a class key outside a guarded callee is not a finding",
			src:  "<div class=\"mb-4\"></div>\n",
			want: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(classHookFindings("x.templ", []byte(tc.src))); got != tc.want {
				t.Fatalf("findings = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestClassInAttrsRatchet is the guard half of #855: no authored template passes
// a class through Attrs to a component that owns a class attribute.
func TestClassInAttrsRatchet(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatalf("glob *.templ: %v", err)
	}
	componentFiles, err := filepath.Glob("components/*.templ")
	if err != nil {
		t.Fatalf("glob components/*.templ: %v", err)
	}
	files = append(files, componentFiles...)
	if len(files) == 0 {
		t.Fatal("no authored .templ sources found")
	}

	var findings []string
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		findings = append(findings, classHookFindings(file, src)...)
	}
	if len(findings) > 0 {
		t.Fatalf("class passed through Attrs to a class-owning component:\n%s", strings.Join(findings, "\n"))
	}
}
