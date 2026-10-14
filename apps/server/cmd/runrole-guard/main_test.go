package main

import (
	"go/parser"
	"os"
	"path/filepath"
	"testing"
)

func TestIsAssistantLiteral(t *testing.T) {
	cases := map[string]bool{
		`"assistant"`: true,
		`"Assistant"`: false, // a persisted role is never capitalized
		`"user"`:      false,
		`42`:          false,
	}
	for src, want := range cases {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if got := isAssistantLiteral(expr); got != want {
			t.Errorf("isAssistantLiteral(%s) = %v, want %v", src, got, want)
		}
	}
}

func TestIsRoleLike(t *testing.T) {
	cases := map[string]bool{
		`role`:               true,
		`m.Role`:             true,
		`it.Author`:          true, // the ADK event Author is the contract's subject
		`msgs[i].Role`:       true,
		`m["role"]`:          true,
		`m["role"].(string)`: true,
		`it.Content.Text`:    false,
		`x.Name`:             false,
		`42`:                 false,
	}
	for src, want := range cases {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if got := isRoleLike(expr); got != want {
			t.Errorf("isRoleLike(%s) = %v, want %v", src, got, want)
		}
	}
}

func TestTemplComparison(t *testing.T) {
	cases := map[string]bool{
		`} else if role != "assistant" && role != "model" {`: true,
		`if it.Role == "assistant" {`:                        true,
		`if "assistant" == it.Author {`:                      true,
		`if it.Role == "Assistant" {`:                        false, // case-sensitive literal
		`if name == "assistant" {`:                           false, // not role-ish
		`if m.Role == "user" {`:                              false,
	}
	for line, want := range cases {
		if got := templComparison.MatchString(line); got != want {
			t.Errorf("templComparison(%q) = %v, want %v", line, got, want)
		}
	}
}

// TestScanGoFileDetectsRoleComparison proves the AST path catches the bug, and
// the guard is not tautological.
func TestScanGoFileDetectsRoleComparison(t *testing.T) {
	src := `package p

func drops(m struct{ Role string }) bool {
	if m.Role == "assistant" {
		return true
	}
	role := m.Role
	return role != "assistant"
}
`
	sites, err := scanGoFile(writeTemp(t, "probe.go", src))
	if err != nil {
		t.Fatalf("scanGoFile: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("sites = %d, want 2: %+v", len(sites), sites)
	}
	for _, s := range sites {
		if s.allowed {
			t.Errorf("unmarked site %s:%d should not be allowlisted", s.file, s.line)
		}
	}
}

// TestScanGoFileIgnoresCommentsAndProviderNames proves the AST path is
// comment-blind and does not flag an unrelated comparison.
func TestScanGoFileIgnoresCommentsAndProviderNames(t *testing.T) {
	src := `package p

// role == "assistant" is the bug this guard exists for
func ok(name string) bool {
	return name == "assistant"
}
`
	sites, err := scanGoFile(writeTemp(t, "probe.go", src))
	if err != nil {
		t.Fatalf("scanGoFile: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("sites = %d, want 0: %+v", len(sites), sites)
	}
}

// TestScanTemplFileSkipsCommentsAndHonorsMarker covers the .templ path: a `//`
// comment is not code, and a documented runrole:allow marker exempts the site.
func TestScanTemplFileSkipsCommentsAndHonorsMarker(t *testing.T) {
	src := `// role == "assistant"
{{ role := it.Role }}
{{ if role == "assistant" { }}
// runrole:allow — labels the author only
{{ } else if it.Role != "assistant" { }}
`
	sites, err := scanTemplFile(writeTemp(t, "probe.templ", src))
	if err != nil {
		t.Fatalf("scanTemplFile: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("sites = %d, want 2 (comment skipped): %+v", len(sites), sites)
	}
	if sites[0].allowed {
		t.Errorf("unmarked site line %d should not be allowlisted", sites[0].line)
	}
	if !sites[1].allowed {
		t.Errorf("marked site line %d should be allowlisted", sites[1].line)
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}
