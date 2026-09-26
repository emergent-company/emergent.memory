// Command modelref-guard keeps model-name prefix parsing in exactly one place:
// pkg/modelref.
//
// Issue #1064 removed the last ad-hoc "exactly one slash" prefix parsers
// (domain/provider's stripModelPrefix and pkg/adk's stripRoutingPrefix) in
// favour of the canonical modelref.Parse / modelref.StripRoutingPrefix. This
// guard is the ratchet that keeps them from coming back: it fails when a model
// name is split on a literal "/" with strings.Cut / strings.Split / strings.SplitN
// anywhere outside pkg/modelref, unless the site carries a documented
// "modelref:allow" marker explaining why it is not reconstructing provider
// identity from a routed model string.
//
// The check is AST-based (go/parser, not regex) so nested calls and unrelated
// "strings.Cut(x, \"/\")" uses are resolved exactly: only a call whose second
// argument is the literal "/" separator is flagged. pkg/modelref is exempt as
// the canonical home. Each surviving site must justify itself with a
// modelref:allow comment on the offending line or the immediately preceding
// lines; an unmarked site fails the guard.
//
// Run from anywhere; resolves the repo root itself. Wired into CI via
// scripts/preflight/modelref-split.sh (which scripts/preflight/all.sh runs in
// the branch-protection-required `ci` job). Imports only the stdlib, so
// `go run` does not touch the network.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const marker = "modelref:allow"

// markerWindow is how many lines above a split site the guard scans for the
// allow marker (the offending line plus this many preceding lines).
const markerWindow = 5

func main() {
	os.Exit(run())
}

func run() int {
	root, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: locating repo root: %v\n", err)
		return 2
	}
	serverDir := filepath.Join(root, "apps", "server")

	sites, err := findSplitSites(serverDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: scanning %s: %v\n", serverDir, err)
		return 2
	}

	var violations []site
	for _, s := range sites {
		if s.allowed {
			continue
		}
		violations = append(violations, s)
	}

	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "modelref-split guard failed: %d unmarked model-name split(s) outside pkg/modelref\n\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  %s:%d: %s\n", v.file, v.line, strings.TrimSpace(v.snippet))
		}
		fmt.Fprintf(os.Stderr, "\nModel-name prefix parsing must live in pkg/modelref (Parse / StripRoutingPrefix).\n")
		fmt.Fprintf(os.Stderr, "If this split is genuinely not reconstructing provider identity from a routed\n")
		fmt.Fprintf(os.Stderr, "model string, document why with a %q comment on or just above the line.\n", marker)
		return 1
	}

	allowed := 0
	for _, s := range sites {
		if s.allowed {
			allowed++
		}
	}
	fmt.Printf("modelref-split OK: %d split(s) outside pkg/modelref, all allowlisted\n", allowed)
	return 0
}

// site is a single strings.Cut/Split/SplitN call on a literal "/" separator.
type site struct {
	file    string // repo-relative path
	line    int
	snippet string
	allowed bool
}

// findSplitSites walks apps/server for Go files and returns every
// strings.{Cut,Split,SplitN}(x, "/") call found outside pkg/modelref, marking
// each as allowed only when a modelref:allow marker sits on or just above it.
func findSplitSites(serverDir string) ([]site, error) {
	var sites []site
	err := filepath.Walk(serverDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(serverDir, path)
		if err != nil {
			return err
		}
		inModelref := rel == "pkg/modelref/modelref.go" || strings.HasPrefix(rel, "pkg/modelref/")
		if inModelref {
			return nil // canonical home — exempt
		}
		found, err := scanFile(path)
		if err != nil {
			return err
		}
		sites = append(sites, found...)
		return nil
	})
	return sites, err
}

// scanFile parses one Go file and returns its split-on-slash call sites.
func scanFile(path string) ([]site, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(src), "\n")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Resolve the local name that imports the stdlib "strings" package, so a
	// shadowed or aliased "strings" identifier is not misclassified.
	stringsIdent := ""
	for _, imp := range f.Imports {
		pkgPath, _ := strconv.Unquote(imp.Path.Value)
		if pkgPath != "strings" {
			continue
		}
		if imp.Name != nil {
			stringsIdent = imp.Name.Name
		} else {
			stringsIdent = "strings"
		}
	}

	var sites []site
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Cut", "Split", "SplitN":
		default:
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || stringsIdent == "" || x.Name != stringsIdent {
			return true
		}
		// Separator is the second argument for Cut/Split/SplitN.
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		sep, err := strconv.Unquote(lit.Value)
		if err != nil || sep != "/" {
			return true
		}

		pos := fset.Position(call.Pos())
		line := pos.Line
		s := site{file: path, line: line, snippet: lineText(lines, line)}

		// Mark allowed when a modelref:allow comment sits on the line or just
		// above it. This is the documented-exception ratchet: an unmarked site
		// is a violation.
		s.allowed = hasMarker(lines, line)
		sites = append(sites, s)
		return true
	})
	return sites, nil
}

// lineText returns the trimmed text of a 1-based line, or "" when out of range.
func lineText(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// hasMarker reports whether a modelref:allow comment appears on the given
// 1-based line or any of the markerWindow lines immediately above it.
func hasMarker(lines []string, line int) bool {
	lo := line - markerWindow
	if lo < 1 {
		lo = 1
	}
	hi := line
	if hi > len(lines) {
		hi = len(lines)
	}
	for i := lo; i <= hi; i++ {
		if strings.Contains(lines[i-1], marker) {
			return true
		}
	}
	return false
}

// gitOutput runs a git command and returns trimmed stdout.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), ee, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
