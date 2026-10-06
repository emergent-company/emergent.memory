package mcpregistry_test

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMigration00220_UpDown applies the embedded migrations to head, rolls back
// the latest migration (00220 device target) and asserts the added columns
// disappear, then re-applies it and asserts they return. Exercises both halves.
func TestMigration00220_UpDown(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mig220")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	columnCount := func() int {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM information_schema.columns
			WHERE table_schema = 'kb'
			  AND table_name = 'mcp_servers'
			  AND column_name IN ('target', 'target_instance_id')`,
		).Scan(&count)
		if err != nil {
			t.Fatalf("count columns: %v", err)
		}
		return count
	}

	if got := columnCount(); got != 2 {
		t.Fatalf("after Up: column count = %d, want 2", got)
	}

	mig := migrate.NewMigrator(db, zap.NewNop())
	if err := mig.Down(ctx); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if got := columnCount(); got != 0 {
		t.Fatalf("after Down: column count = %d, want 0", got)
	}

	if err := mig.UpTo(ctx, 220); err != nil {
		t.Fatalf("UpTo(220) error = %v", err)
	}
	if got := columnCount(); got != 2 {
		t.Fatalf("after re-Up: column count = %d, want 2", got)
	}
}
