package provider

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMigration00218_UpDown applies the embedded migrations to head, rolls back
// the latest migration (00218 provider extra config) and asserts the added
// columns disappear, then re-applies it and asserts they return. This exercises
// both the Up and Down halves of the migration.
func TestMigration00218_UpDown(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "mig218")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	extraConfigColumnCount := func() int {
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM information_schema.columns
			WHERE table_schema = 'kb'
			  AND table_name IN ('project_provider_configs', 'org_provider_configs')
			  AND column_name IN ('extra_config', 'extra_secret_encrypted', 'extra_secret_nonce')`,
		).Scan(&count)
		if err != nil {
			t.Fatalf("count columns: %v", err)
		}
		return count
	}

	// SetupTestDB applies all migrations to head, so 00218 is Up: six columns
	// (three per table).
	if got := extraConfigColumnCount(); got != 6 {
		t.Fatalf("after Up: extra-config column count = %d, want 6", got)
	}

	mig := migrate.NewMigrator(db, zap.NewNop())
	if err := mig.Down(ctx); err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	if got := extraConfigColumnCount(); got != 0 {
		t.Fatalf("after Down: extra-config column count = %d, want 0", got)
	}

	if err := mig.UpTo(ctx, 218); err != nil {
		t.Fatalf("UpTo(218) error = %v", err)
	}
	if got := extraConfigColumnCount(); got != 6 {
		t.Fatalf("after re-Up: extra-config column count = %d, want 6", got)
	}
}
