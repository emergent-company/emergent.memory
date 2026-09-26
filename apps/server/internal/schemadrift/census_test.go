package schemadrift

import (
	"sort"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// censusExclusions names the tables that the census sees a model for but the
// registry cannot reflect because the model is unexported. Each entry must name
// its reason; a table here is still checked at name level by the DB-backed test
// (see drift_test.go), so it is not silently exempt.
var censusExclusions = map[string]string{
	"kb.auth_introspection_cache": "unexported model pkg/auth.introspectionCacheEntry; cannot be referenced cross-package",
	"kb.embedding_cache":          "unexported model domain/extraction.embeddingCacheRow; cannot be referenced cross-package",
}

// registryTables returns the set of tables covered by the explicit model
// registry, resolved through bun's own dialect so it matches runtime.
func registryTables(t *testing.T) []string {
	t.Helper()
	db := bun.NewDB(nil, pgdialect.New())
	seen := map[string]bool{}
	for _, mt := range reflectModels(db, models) {
		seen[mt.qualified()] = true
	}
	return sortedKeys(seen)
}

// TestRegistryCoversEveryModel proves the explicit registry in models.go is
// complete: every bun model struct in the source tree (found by the census)
// maps to a table that the registry covers, except the documented exclusions.
// This closes the coverage gap that would otherwise let a newly added model
// drift silently.
func TestRegistryCoversEveryModel(t *testing.T) {
	c, err := census()
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	censusSet := map[string]bool{}
	for table := range c {
		censusSet[table] = true
	}

	covered := map[string]bool{}
	for _, table := range registryTables(t) {
		covered[table] = true
	}

	// Registry must not reference a table the census did not find (that would
	// mean a registered model's table tag is not being parsed).
	for table := range covered {
		if !censusSet[table] {
			t.Errorf("registry covers %s but the census found no model with that table tag", table)
		}
	}

	// Every census table must be covered or explicitly excluded.
	var missing []string
	for table := range censusSet {
		if covered[table] {
			continue
		}
		if _, ok := censusExclusions[table]; ok {
			continue
		}
		missing = append(missing, table)
	}
	sort.Strings(missing)

	coveredTables := sortedKeys(covered)
	t.Logf("models covered vs present: %d registered tables covered; %d tables found by census (exclusions: %d)",
		len(coveredTables), len(censusSet), len(censusExclusions))

	if len(missing) > 0 {
		for _, table := range missing {
			t.Errorf("model for table %s (%s.%s) is not registered in models.go and not excluded — add it or name its reason",
				table, c[table].Pkg, c[table].Name)
		}
	}
}
