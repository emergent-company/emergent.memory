// Command schemadrift-guard verifies that every bun model in the source tree is
// registered in the schemadrift model registry (models.go), so a newly added
// model cannot silently escape the model↔schema drift guard.
//
// This is the deterministic, DB-less half of the schemadrift guard. It runs the
// static source census (census.go, go/parser) and cross-checks it against the
// explicit registry, failing on any table the census finds that the registry
// does not cover (unless it is named with a reason in censusExclusions), or any
// table the registry covers that the census cannot find.
//
// The DB-backed half — comparing every registered model's columns against the
// real migrated PostgreSQL schema (the #1093 ADKState drift class) — lives in
// internal/schemadrift/drift_test.go and runs in CI's test-db job, since it
// needs Postgres and cannot run in the DB-less preflight pipeline.
//
// Wired into CI via scripts/preflight/schemadrift-guard.sh (run by
// scripts/preflight/all.sh in the branch-protection-required `ci` job). Unlike
// the stdlib-only sibling guards, this one imports the server module — the
// model registry references the full domain tree — so `go run` compiles the
// module graph.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/internal/schemadrift"
)

func main() {
	os.Exit(run())
}

func run() int {
	violations := schemadrift.CheckRegistry()
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "schemadrift-guard failed: %d model registry violation(s)\n\n", len(violations))
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  %s\n", strings.TrimSpace(v))
		}
		fmt.Fprintf(os.Stderr, "\nEvery bun model must be registered in apps/server/internal/schemadrift/models.go\n")
		fmt.Fprintf(os.Stderr, "or named with a reason in censusExclusions (apps/server/internal/schemadrift/census.go).\n")
		return 1
	}
	fmt.Println("schemadrift-guard: OK (all bun models registered)")
	return 0
}
