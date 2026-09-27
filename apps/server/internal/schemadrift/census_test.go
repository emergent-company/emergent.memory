package schemadrift

import (
	"sort"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// registryIdentities returns the sorted set of model identities covered by the
// explicit model registry, resolved through bun's own dialect.
func registryIdentities(t *testing.T) []string {
	t.Helper()
	db := bun.NewDB(nil, pgdialect.New())
	seen := map[string]bool{}
	for _, mt := range reflectModels(db, models) {
		seen[mt.Identity] = true
	}
	return sortedKeys(seen)
}

// TestRegistryCoversEveryModel proves the explicit registry in models.go is
// complete: every bun model struct in the source tree (found by the census),
// keyed by model identity (import path + type name), must be registered or
// explicitly excluded. Keying by identity — not table name — means two models
// sharing a table are each checked independently, closing the gap where a
// duplicate-table model slipped past the old table-keyed comparison.
func TestRegistryCoversEveryModel(t *testing.T) {
	c, err := census()
	if err != nil {
		t.Fatalf("census: %v", err)
	}

	censusSet := map[string]bool{}
	censusCount := 0
	for _, m := range c {
		censusSet[m.Identity()] = true
		censusCount++
	}

	covered := map[string]bool{}
	for _, id := range registryIdentities(t) {
		covered[id] = true
	}

	// Registry must not reference a model the census did not find (that would
	// mean a registered model's table tag is not being parsed, or its identity
	// no longer resolves to a source struct).
	for id := range covered {
		if !censusSet[id] {
			t.Errorf("registry covers model %s but the census found no such source struct", id)
		}
	}

	// Every census model must be registered or explicitly excluded.
	var missing []string
	for id := range censusSet {
		if covered[id] {
			continue
		}
		if _, ok := censusExclusions[id]; ok {
			continue
		}
		missing = append(missing, id)
	}
	sort.Strings(missing)

	// Exclusions must be honest: each names a real census model (a stale entry
	// that no longer matches any source struct would silently exempt nothing).
	for id := range censusExclusions {
		if !censusSet[id] {
			t.Errorf("stale censusExclusions entry names no real source model: %s", id)
		}
	}

	t.Logf("models covered vs present: %d registered models; %d models found by census (exclusions: %d)",
		len(covered), censusCount, len(censusExclusions))

	if len(missing) > 0 {
		for _, id := range missing {
			t.Errorf("model %s is not registered in models.go and not excluded — add it or name its reason", id)
		}
	}
}
