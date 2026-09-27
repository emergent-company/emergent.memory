package schemadrift

import (
	"fmt"
	"sort"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// CheckRegistry verifies — without a database — that the explicit model
// registry in models.go is complete and consistent with the static source
// census. It returns one human-readable violation per problem, empty when the
// registry is complete:
//
//   - a table the registry covers but the census found no model for (the
//     registry's `bun:"table:..."` tag is not being parsed), or
//   - a bun model in the source tree that is neither registered in models.go
//     nor named with a reason in censusExclusions (a newly added model left out
//     of the registry would drift silently).
//
// This is the deterministic, DB-less half of the schemadrift guard, wired into
// preflight via scripts/preflight/schemadrift-guard.sh (run by
// scripts/preflight/all.sh in the branch-protection-required `ci` job). The
// DB-backed half — comparing every registered model's columns against the real
// migrated PostgreSQL schema (the #1093 ADKState drift class) — lives in
// drift_test.go and runs in CI's test-db job, because it needs Postgres and
// cannot run in the DB-less preflight pipeline.
func CheckRegistry() []string {
	var violations []string

	c, err := census()
	if err != nil {
		return []string{fmt.Sprintf("census: %v", err)}
	}

	censusSet := make(map[string]bool, len(c))
	for table := range c {
		censusSet[table] = true
	}

	covered := make(map[string]bool)
	db := bun.NewDB(nil, pgdialect.New())
	for _, mt := range reflectModels(db, models) {
		covered[mt.qualified()] = true
	}

	for table := range covered {
		if !censusSet[table] {
			violations = append(violations, fmt.Sprintf(
				"registry covers %s but the census found no model with that table tag", table))
		}
	}

	var missing []string
	for table := range censusSet {
		if covered[table] {
			continue
		}
		if _, ok := censusExclusions[table]; ok {
			continue
		}
		missing = append(missing, fmt.Sprintf(
			"model for table %s (%s.%s) is not registered in models.go and not excluded — add it or name its reason",
			table, c[table].Pkg, c[table].Name))
	}
	sort.Strings(missing)
	violations = append(violations, missing...)

	sort.Strings(violations)
	return violations
}
