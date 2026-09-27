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
//   - a registered model the census cannot find (its `bun` table tag is not
//     being parsed, or its type identity resolves to no source struct), or
//   - a bun model in the source tree that is neither registered in models.go
//     nor named with a reason in censusExclusions (a newly added model left out
//     of the registry would drift silently).
//
// The comparison is keyed by MODEL identity ("import.path.TypeName"), not table
// name: two models that share a table are each checked independently, so a
// model added for a table another model already covers still fails if it is not
// itself registered (or excluded). This closes the gap where a duplicate-table
// model (e.g. provider.budgetNotification on kb.notifications) was invisible to
// both this check and the DB-backed column check.
//
// This is the deterministic, DB-less half of the schemadrift guard, wired into
// preflight via scripts/preflight/schemadrift-guard.sh (run by
// scripts/preflight/all.sh in the branch-protection-required `ci` job). The
// DB-backed half — comparing every registered model's columns against the real
// migrated PostgreSQL schema (the #1093 ADKState drift class) — lives in
// drift_test.go and runs in CI's test-db job, because it needs Postgres and
// cannot run in the DB-less preflight pipeline.
func CheckRegistry() []string {
	c, err := census()
	if err != nil {
		return []string{fmt.Sprintf("census: %v", err)}
	}
	db := bun.NewDB(nil, pgdialect.New())
	return registryViolations(c, reflectModels(db, models), censusExclusions)
}

// registryViolations is the pure, deterministic core of CheckRegistry. It
// cross-checks the static source census against the reflected model registry,
// keyed by model identity rather than table name, so it is directly unit-testable
// with synthetic inputs (see TestCheckRegistryMissesModelSharingExistingTable).
func registryViolations(census []censusModel, registered []modelTable, exclusions map[string]string) []string {
	censusByIdentity := make(map[string]censusModel, len(census))
	for _, m := range census {
		censusByIdentity[m.Identity()] = m
	}
	registeredIdentity := make(map[string]bool, len(registered))

	var violations []string

	// Reverse: every registered model must be found by the census and must map
	// to the same table, otherwise its bun table tag is not being parsed (or its
	// identity no longer resolves to a source struct).
	for _, mt := range registered {
		registeredIdentity[mt.Identity] = true
		m, ok := censusByIdentity[mt.Identity]
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"registry model %s (table %s) is not found by the source census",
				mt.Identity, mt.qualified()))
			continue
		}
		if m.Table != mt.qualified() {
			violations = append(violations, fmt.Sprintf(
				"registry model %s maps to table %s but the source census sees %s",
				mt.Identity, mt.qualified(), m.Table))
		}
	}

	// Forward: every census model must be registered or explicitly excluded.
	for _, m := range census {
		if registeredIdentity[m.Identity()] {
			continue
		}
		if _, ok := exclusions[m.Identity()]; ok {
			continue
		}
		violations = append(violations, fmt.Sprintf(
			"model %s (%s.%s, table %s) is not registered in models.go and not excluded — add it or name its reason",
			m.Identity(), m.Pkg, m.Name, m.Table))
	}

	sort.Strings(violations)
	return violations
}
