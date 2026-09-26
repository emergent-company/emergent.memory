// Command mcp-authz-guard enforces the domain/mcp authorization boundary at
// the method level, not just the import level.
//
// The #1092 depguard rule stops domain/mcp from importing a new sibling
// domain's store/repository, but it cannot stop a tool from calling a method on
// an already allow-listed raw handle in a way that skips the domain's shared
// Authorize* helper — the #1040 shape, where the MCP skills delete path called
// skills.Repository.Delete directly and destroyed another project's skill.
//
// This guard scans apps/server/domain/mcp/*.go with an AST extractor and fails
// when:
//
//  1. a call on the raw skills.Repository handle (Service.skillsRepo) uses a
//     method that is not classified, or uses a method that crosses a project
//     boundary (FindByID / Update / Delete) without the shared
//     AuthorizeSkillAccess / AuthorizeSkillWrite helper in the same function; or
//  2. a function issues raw s.db.* SQL (against tables no import rule can see)
//     and is not declared in the checked-in census apps/server/mcp-authz.yaml.
//
// An --update flag regenerates the census from the current tree; new sites
// appear with an empty justification that must be filled before the guard will
// pass, so every new raw-SQL site is a reviewed, justified diff.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/mcpauthzguard"
)

func main() {
	os.Exit(run())
}

func run() int {
	update := flag.Bool("update", false, "regenerate the raw-SQL census from the current tree instead of checking it")
	table := flag.String("table", "apps/server/mcp-authz.yaml", "table path, repo-root-relative")
	flag.Parse()

	root, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: locating repo root: %v\n", err)
		return 2
	}
	serverDir := filepath.Join(root, "apps/server")
	tablePath := filepath.Join(root, *table)

	res, err := mcpauthzguard.Extract(serverDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: extracting access sites: %v\n", err)
		return 2
	}

	if *update {
		t := &mcpauthzguard.Table{Version: 1}
		// Preserve existing justifications so regeneration of an unchanged tree
		// is a no-op diff; new entries start empty and must be justified.
		existing := map[string]string{}
		if old, err := mcpauthzguard.LoadTable(tablePath); err == nil {
			for _, e := range old.RawSQL {
				existing[e.File+"\x00"+e.Func] = e.Why
			}
		}
		for _, f := range res.RawSQLFuncs {
			t.RawSQL = append(t.RawSQL, mcpauthzguard.RawSQLEntry{
				File: f.File,
				Func: f.Func,
				Why:  existing[f.File+"\x00"+f.Func],
			})
		}
		if err := mcpauthzguard.SaveTable(tablePath, t); err != nil {
			fmt.Fprintf(os.Stderr, "error: writing table: %v\n", err)
			return 2
		}
		fmt.Printf("wrote %s: %d raw-SQL functions\n", *table, len(t.RawSQL))
		missing := 0
		for _, e := range t.RawSQL {
			if strings.TrimSpace(e.Why) == "" {
				fmt.Fprintf(os.Stderr, "  needs justification: %s %s\n", e.File, e.Func)
				missing++
			}
		}
		if missing > 0 {
			fmt.Fprintf(os.Stderr, "fill in the %d empty justification(s), then re-run the guard (check mode).\n", missing)
			return 1
		}
		return 0
	}

	declared, err := mcpauthzguard.LoadTable(tablePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: loading declared table: %v\n", err)
		return 2
	}

	vs := mcpauthzguard.Check(res, declared)
	if len(vs) > 0 {
		fmt.Fprint(os.Stderr, mcpauthzguard.FormatViolations(vs))
		return 1
	}

	fmt.Printf("mcp-authz OK: %d skills.Repository call(s) classified and paired, %d raw-SQL function(s) declared\n", len(res.RepoCalls), len(res.RawSQLFuncs))
	return 0
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
