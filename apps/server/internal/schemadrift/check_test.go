package schemadrift

import "testing"

// TestCheckRegistryClean proves the deterministic, DB-less registry check that
// the preflight guard (cmd/schemadrift-guard) runs returns no violations on the
// current tree, so the guard stays under `go test` even though its primary
// wiring is the DB-less preflight pipeline. A violation here is the same real
// drift the guard exists to catch: a bun model missing from models.go (or a
// registry entry the census cannot resolve).
func TestCheckRegistryClean(t *testing.T) {
	for _, v := range CheckRegistry() {
		t.Errorf("CheckRegistry: %s", v)
	}
}
