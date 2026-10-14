// Command runrole-guard is the ratchet that stops consumers from re-introducing
// the persisted run-message role bug (#1832): an agent turn is stored in
// kb.agent_run_messages.role under the ADK event Author — the sanitized agent
// name ("coder", "Norwegian_Law_Assistant"), not in practice the literal
// "assistant" (see persistEventContent and agents.IsAgentReplyRole). A consumer
// that equality-checks `role == "assistant"` therefore silently drops every real
// agent reply.
//
// The contract is enforced by the shared predicate agents.IsAgentReplyRole. This
// guard fails when a role- or author-named value is compared to the literal
// "assistant" with `==` or `!=`, unless the site carries a documented
// `runrole:allow` marker explaining why the comparison is safe.
//
// Detection is AST-based (go/parser, not regex) for .go files so nested and
// aliased forms resolve exactly, and a same-line check for authored .templ files
// (the in-expression role comparisons live there). Scope:
//
//   - Only equality/inequality comparisons against the literal "assistant" are
//     flagged. Switch `case "assistant":` clauses, `strings.EqualFold`, and
//     helper-based classification are out of scope: the OpenAI/Anthropic provider
//     protocol switches on request `messages[].role` in a different contract and
//     must not be flagged.
//   - Provider protocol is exempt wholesale: apps/server/pkg/adk
//     (OpenAI/Anthropic request+response roles) and apps/server/domain/agentcompat
//     (the OpenAI-compatible endpoint's client-supplied ChatMessage.Role) are a
//     DIFFERENT contract from persisted run-message data.
//   - Generated *_templ.go, docs/swagger, vendor and node_modules are skipped.
//
// Imports only the stdlib, so `go run` does not touch the network. Wired into CI
// via scripts/preflight/runrole-guard.sh (which scripts/preflight/all.sh runs in
// the branch-protection-required `ci` job). Run from anywhere; resolves the repo
// root itself.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const marker = "runrole:allow"

// markerWindow is how many lines above a comparison site the guard scans for the
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
	appsDir := filepath.Join(root, "apps")

	sites, err := findSites(appsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: scanning %s: %v\n", appsDir, err)
		return 2
	}

	var violations []site
	allowed := 0
	for _, s := range sites {
		if s.allowed {
			allowed++
			continue
		}
		violations = append(violations, s)
	}

	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "runrole-guard failed: %d run-message role comparison(s) against literal \"assistant\"\n\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  %s:%d: %s\n", v.file, v.line, strings.TrimSpace(v.snippet))
		}
		fmt.Fprintf(os.Stderr, "\nPersisted run-message roles hold the ADK event Author (a sanitized agent\n")
		fmt.Fprintf(os.Stderr, "name such as \"coder\"), not in practice the literal \"assistant\"; classify the\n")
		fmt.Fprintf(os.Stderr, "role with agents.IsAgentReplyRole instead. If this comparison is genuinely not\n")
		fmt.Fprintf(os.Stderr, "about run-message data, document why with a %q comment on or just above it.\n", marker)
		return 1
	}

	fmt.Printf("runrole-guard OK: %d role-vs-\"assistant\" comparison(s), all allowlisted\n", allowed)
	return 0
}

// site is one role-vs-"assistant" comparison.
type site struct {
	file    string // repo-relative path
	line    int
	snippet string
	allowed bool
}

// findSites walks apps/ for .go and authored .templ files and returns every
// run-message role comparison against the literal "assistant".
func findSites(appsDir string) ([]site, error) {
	var sites []site
	err := filepath.Walk(appsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "vendor", ".git", "node_modules", "swagger":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(appsDir, path)
		// Provider protocol — a DIFFERENT contract from persisted run messages:
		//   - apps/server/pkg/adk: OpenAI/Anthropic request+response roles.
		//   - apps/server/domain/agentcompat: the OpenAI-compatible chat
		//     endpoint, whose ChatMessage.Role is client-supplied request protocol.
		// The run-message role lives in kb.agent_run_messages (domain/agents),
		// domain/chat, and the gateway, all of which stay covered.
		relSlash := filepath.ToSlash(rel)
		if strings.HasPrefix(relSlash, "server/pkg/adk/") ||
			strings.HasPrefix(relSlash, "server/domain/agentcompat/") {
			return nil
		}
		switch {
		case strings.HasSuffix(path, "_templ.go"):
			return nil // generated from the authored .templ
		case strings.HasSuffix(path, ".go"):
			found, err := scanGoFile(path)
			if err != nil {
				return err
			}
			sites = append(sites, found...)
		case strings.HasSuffix(path, ".templ"):
			found, err := scanTemplFile(path)
			if err != nil {
				return err
			}
			sites = append(sites, found...)
		}
		return nil
	})
	return sites, err
}

// scanGoFile parses one Go file and returns its role-vs-"assistant" ==/!=
// comparisons.
func scanGoFile(path string) ([]site, error) {
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

	var sites []site
	ast.Inspect(f, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if bin.Op != token.EQL && bin.Op != token.NEQ {
			return true
		}
		if !isAssistantLiteral(bin.X) && !isAssistantLiteral(bin.Y) {
			return true
		}
		// The other side must look like a role value, not an unrelated
		// "assistant"-vs-something comparison.
		if !isRoleLike(bin.X) && !isRoleLike(bin.Y) {
			return true
		}
		line := fset.Position(bin.Pos()).Line
		sites = append(sites, site{
			file:    path,
			line:    line,
			snippet: lineText(lines, line),
			allowed: hasMarker(lines, line),
		})
		return true
	})
	return sites, nil
}

// templRoleIdent matches a role-ish value written in templ markup: an
// identifier or selector whose final name contains "role"/"Role" or
// "author"/"Author" (e.g. `role`, `it.Role`, `it.Author`).
const templRoleIdent = `[A-Za-z0-9_.]*[Rr]ole[A-Za-z0-9_]*|[A-Za-z0-9_.]*[Aa]uthor[A-Za-z0-9_]*`

// templComparison matches a `<role-ish> == "assistant"` / `!=` comparison, in
// either operand order. The literal is case-sensitive (a persisted role is never
// "Assistant").
var templComparison = regexp.MustCompile(`(?:` + templRoleIdent + `)\s*(?:==|!=)\s*"assistant"|"assistant"\s*(?:==|!=)\s*(?:` + templRoleIdent + `)`)

// scanTemplFile returns role-vs-"assistant" comparisons in an authored .templ
// file. templ bodies are Go expressions, so a same-line match is exact enough;
// the AST scan owns .go precision. `//` comment lines are skipped (a comment is
// not code, and the .go AST scan ignores comments by construction).
func scanTemplFile(path string) ([]site, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(src), "\n")
	var sites []site
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if !templComparison.MatchString(line) {
			continue
		}
		sites = append(sites, site{
			file:    path,
			line:    i + 1,
			snippet: line,
			allowed: hasMarker(lines, i+1),
		})
	}
	return sites, nil
}

// isAssistantLiteral reports whether expr is the string literal "assistant".
func isAssistantLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == "assistant"
}

// isRoleLike reports whether expr names a role value: an identifier or selector
// whose final name contains "role" or "author" (case-insensitive), e.g. `role`,
// `m.Role`, `it.Author`, `msgs[i].Role`, `m["role"].(string)`.
func isRoleLike(expr ast.Expr) bool {
	for {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.TypeAssertExpr:
			expr = e.X
		case *ast.IndexExpr:
			// m["role"] and m["role"].(string) name the role by key.
			if isRoleNameLiteral(e.Index) {
				return true
			}
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return isRoleName(e.Name)
		case *ast.SelectorExpr:
			return isRoleName(e.Sel.Name)
		default:
			return false
		}
	}
}

// isRoleNameLiteral reports whether expr is a string literal that names a role
// key, e.g. "role".
func isRoleNameLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && isRoleName(s)
}

func isRoleName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "role") || strings.Contains(l, "author")
}

// lineText returns the trimmed text of a 1-based line, or "" when out of range.
func lineText(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// hasMarker reports whether a runrole:allow comment appears on the given 1-based
// line or any of the markerWindow lines immediately above it.
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
