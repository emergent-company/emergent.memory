// Command migration-order-guard fails a PR that adds a migration file whose
// version number is lower than the maximum migration version already present on
// the base branch. A lower-numbered gap hard-blocks Goose startup once the
// higher-numbered migration has been applied anywhere.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/migrationguard"
)

func main() {
	os.Exit(run())
}

func run() int {
	base := flag.String("base", "origin/main", "base ref to compare against")
	dir := flag.String("dir", "apps/server/migrations", "migrations dir, repo-root-relative")
	flag.Parse()

	root, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: locating repo root: %v\n", err)
		return 2
	}

	baseNames, err := gitOutput("-C", root, "ls-tree", "-r", "--name-only", *base, "--", *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: listing base migrations: %v\n", err)
		return 2
	}

	baseMax, ok := migrationguard.MaxVersion(strings.Fields(baseNames))
	if !ok {
		// No base migrations: nothing can be out of order.
		baseMax = 0
	}

	addedNames, err := gitOutput("-C", root, "diff", "--name-only", "--diff-filter=A", *base+"...HEAD", "--", *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: listing added migrations: %v\n", err)
		return 2
	}

	var added []migrationguard.AddedMigration
	for _, name := range strings.Fields(addedNames) {
		version, valid := migrationguard.VersionFromFilename(name)
		if !valid {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "error: reading %s: %v\n", name, readErr)
			return 2
		}
		reason, exempt := migrationguard.ExemptReason(string(content))
		added = append(added, migrationguard.AddedMigration{
			Path:    name,
			Version: version,
			Exempt:  exempt,
			Reason:  reason,
		})
	}

	violations := migrationguard.FindViolations(added, baseMax)
	if len(violations) == 0 {
		fmt.Printf("OK: no out-of-order migrations. base max version: %d, added migrations: %d\n", baseMax, len(added))
		return 0
	}

	fmt.Fprint(os.Stderr, migrationguard.FormatViolationError(violations, baseMax))
	return 1
}

// gitOutput runs a git command and returns trimmed stdout. The repo root is
// discovered via `git rev-parse --show-toplevel`; all subsequent commands run
// with `git -C <root> ...`.
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
