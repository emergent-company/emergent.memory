// Command route-authority-guard keeps apps/server/route-authority.yaml honest:
// it enumerates every Echo route registered under apps/server/domain, derives
// each route's authority tier from its applied middleware chain, and fails the
// build when the checked-in table diverges — a route that exists but is
// undeclared, a declared route that vanished, or a derived tier that no longer
// matches the declared tier.
//
// An --update flag regenerates the table from the current tree so an
// intentional route/middleware change appears as a reviewable diff. The
// extractor fails closed (non-zero exit) on any registration pattern it cannot
// classify, so unparsed registration styles cannot silently pass.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/routeguard"
)

func main() {
	os.Exit(run())
}

func run() int {
	update := flag.Bool("update", false, "regenerate the table from the current tree instead of checking it")
	table := flag.String("table", "apps/server/route-authority.yaml", "table path, repo-root-relative")
	flag.Parse()

	root, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: locating repo root: %v\n", err)
		return 2
	}
	serverDir := filepath.Join(root, "apps/server")
	tablePath := filepath.Join(root, *table)

	res, err := routeguard.Extract(serverDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: extracting routes: %v\n", err)
		return 2
	}

	if *update {
		t := &routeguard.Table{Routes: res.Routes}
		if err := routeguard.SaveTable(tablePath, t); err != nil {
			fmt.Fprintf(os.Stderr, "error: writing table: %v\n", err)
			return 2
		}
		fmt.Printf("wrote %s: %d routes (%d unclassified)\n", *table, len(res.Routes), len(res.Unclassified))
		if len(res.Unclassified) > 0 {
			for _, u := range res.Unclassified {
				fmt.Fprintf(os.Stderr, "  unclassified: %s\n", u)
			}
			return 1
		}
		return 0
	}

	if len(res.Unclassified) > 0 {
		fmt.Fprintf(os.Stderr, "route-authority guard failed: %d unclassifiable registration pattern(s)\n", len(res.Unclassified))
		for _, u := range res.Unclassified {
			fmt.Fprintf(os.Stderr, "  - %s\n", u)
		}
		fmt.Fprintf(os.Stderr, "\nThe extractor fails closed on registration patterns it cannot classify.\n")
		fmt.Fprintf(os.Stderr, "Update the extractor (internal/routeguard) to handle the new pattern.\n")
		return 1
	}

	declared, err := routeguard.LoadTable(tablePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: loading declared table: %v\n", err)
		return 2
	}

	violations, noteDrifts := routeguard.Compare(res.Routes, declared)
	for _, d := range noteDrifts {
		fmt.Printf("note drift (informational): %s %s -> %q\n", d.Method, d.Path, d.Note)
	}

	if len(violations) > 0 {
		fmt.Fprint(os.Stderr, routeguard.FormatViolations(violations))
		return 1
	}

	fmt.Printf("route-authority OK: %d routes, all declared, all tiers match\n", len(res.Routes))
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
