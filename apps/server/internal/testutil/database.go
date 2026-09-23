package testutil

import (
	"context"
	"testing"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestDB is an alias for the domain-free test database handle owned by
// internal/testdb. The alias keeps existing testutil.* call sites unchanged
// while the actual mechanism lives in a package that does not import the
// domain packages testutil imports (which would otherwise form an import cycle).
type TestDB = testdb.TestDB

// SetupTestDB creates an isolated throwaway test database. See testdb.SetupTestDB.
func SetupTestDB(ctx context.Context, suffix string) (*TestDB, error) {
	return testdb.SetupTestDB(ctx, suffix)
}

// SetupTestDBOrFail is SetupTestDB with the repo's standard database-required
// behaviour. See testdb.SetupTestDBOrFail.
func SetupTestDBOrFail(t testing.TB, ctx context.Context, suffix string) *TestDB {
	return testdb.SetupTestDBOrFail(t, ctx, suffix)
}

// TruncateTables truncates all tables in the test database. See testdb.TruncateTables.
func TruncateTables(ctx context.Context, db bun.IDB) error {
	return testdb.TruncateTables(ctx, db)
}

// DropTemplateDB drops the template database. See testdb.DropTemplateDB.
func DropTemplateDB(ctx context.Context) error {
	return testdb.DropTemplateDB(ctx)
}
