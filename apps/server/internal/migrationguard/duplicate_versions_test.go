package migrationguard

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/emergent-company/emergent.memory/migrations"
)

// TestEmbeddedMigrationsHaveUniqueVersions is the definitive regression guard
// against the exact goose startup panic that took main red: two migration files
// sharing a version (`panic: goose: duplicate version 227 detected`). Goose
// records applied migrations by numeric version, so a duplicate both panics at
// load and leaves the schema diverged. This collects the embedded migration set
// through goose's own collector — whose Migrations.Less panics on a duplicate —
// and asserts every version is unique.
func TestEmbeddedMigrationsHaveUniqueVersions(t *testing.T) {
	goose.SetBaseFS(migrations.FS)
	t.Cleanup(func() { goose.SetBaseFS(nil) })

	collected := collectAllMigrations(t)

	seen := make(map[int64]string, len(collected))
	for _, m := range collected {
		if prev, dup := seen[m.Version]; dup {
			t.Fatalf("duplicate migration version %d: %s and %s (goose panics at startup on this)", m.Version, prev, m.Source)
		}
		seen[m.Version] = m.Source
	}
	if len(seen) == 0 {
		t.Fatal("no embedded migrations collected")
	}
}

// collectAllMigrations runs goose's collector over the embedded FS. A duplicate
// version makes goose panic inside sort.Migrations.Less; recover so the test
// reports a clear failure rather than an opaque panic.
func collectAllMigrations(t *testing.T) goose.Migrations {
	t.Helper()
	var (
		ms  goose.Migrations
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("goose migration collection panicked (duplicate version): %v", r)
			}
		}()
		ms, err = goose.CollectMigrations(".", 0, goose.MaxVersion)
	}()
	if err != nil {
		t.Fatalf("goose.CollectMigrations over embedded migrations: %v", err)
	}
	return ms
}
