// Package e2eframework — fixtures.go
//
// Blueprint fixture corpora live outside this repository: they are
// customer-shaped projects kept in a separate private place, so a fresh
// checkout cannot run the suites that apply them. They are supplied by
// MEMORY_TEST_BLUEPRINTS_DIR instead of being committed, the same way
// DOC_PROCESSING_SUITE_DIR supplies the extraction corpus (#2209).
//
// This file is e2e-local, not part of the runlog re-export wrapper above, so it
// survives the Phase B move of consumers onto runlog directly.
package e2eframework

import (
	"os"
	"path/filepath"
	"testing"
)

// BlueprintsFixtureEnv is the environment variable that points at the directory
// holding the blueprint fixture corpora (one subdirectory per blueprint).
const BlueprintsFixtureEnv = "MEMORY_TEST_BLUEPRINTS_DIR"

// LookupBlueprintFixture resolves <MEMORY_TEST_BLUEPRINTS_DIR>/<name> without
// skipping anything. Use it where the fixture is optional — a missing corpus
// should fall back to the process environment, not abort the test.
func LookupBlueprintFixture(name string) (string, bool) {
	base := os.Getenv(BlueprintsFixtureEnv)
	if base == "" {
		return "", false
	}
	dir := filepath.Join(base, name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

// BlueprintFixtureDir returns <MEMORY_TEST_BLUEPRINTS_DIR>/<name> and skips the
// test when the corpus is unavailable: the fixtures are not in this repo, so a
// checkout without them should say so instead of failing on a missing path.
func BlueprintFixtureDir(t testing.TB, name string) string {
	t.Helper()
	dir, ok := LookupBlueprintFixture(name)
	if ok {
		return dir
	}
	if os.Getenv(BlueprintsFixtureEnv) == "" {
		t.Skipf("%s is not set: the %q blueprint fixture is not part of this repo — "+
			"point %s at a directory holding it to run this suite",
			BlueprintsFixtureEnv, name, BlueprintsFixtureEnv)
	}
	t.Skipf("%s=%s has no %q blueprint fixture directory", BlueprintsFixtureEnv, os.Getenv(BlueprintsFixtureEnv), name)
	return ""
}
