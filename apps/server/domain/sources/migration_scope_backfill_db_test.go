package sources

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/emergent-company/emergent.memory/internal/migrate"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestMigration00287BackfillKeepsLabelOnlyScope covers must-fix 3: the legacy
// backfill stamps `in:inbox` on a Gmail source that has no persisted scope, but a
// label-only source (config.label_ids, no query, no filter) is a complete scope
// under the validator. Narrowing it to the inbox would drop archived mail the
// source already synced.
func TestMigration00287BackfillKeepsLabelOnlyScope(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "src_scope_backfill")
	t.Cleanup(tdb.Close)
	db := tdb.DB

	mig := migrate.NewMigrator(db, zap.NewNop())
	// The test database is migrated to head, so the backfill has already run.
	// Roll it back before seeding the rows it is about to inspect.
	if err := mig.DownTo(ctx, 286); err != nil {
		t.Fatalf("DownTo(286): %v", err)
	}

	projectID := uuid.New()
	seed := func(config string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := db.NewRaw(
			`INSERT INTO kb.sources (id, project_id, provider, config) VALUES (?, ?, 'gmail', ?::jsonb)`,
			id, projectID, config,
		).Exec(ctx); err != nil {
			t.Fatalf("seed source %s: %v", config, err)
		}
		return id
	}

	labelOnly := seed(`{"label_ids":["Label_9","Label_10"]}`)
	legacyScopeLess := seed(`{}`)
	existingFilter := seed(`{"filter":{"query":"is:starred"}}`)
	blankQuery := seed(`{"query":"   "}`)
	// Whitespace beyond spaces is still no scope: the predicate trims tabs and
	// newlines too, so such a row keeps syncing instead of silently never
	// matching a configured query.
	whitespaceQuery := seed(`{"query":" \t\n"}`)
	// A legacy row with a malformed label_ids must not abort the migration.
	malformedLabels := seed(`{"label_ids":"Label_9"}`)

	if err := mig.UpTo(ctx, 287); err != nil {
		t.Fatalf("UpTo(287): %v", err)
	}

	configOf := func(id uuid.UUID) map[string]any {
		t.Helper()
		var row struct {
			Config map[string]any `bun:"config"`
		}
		if err := db.NewRaw(`SELECT config FROM kb.sources WHERE id = ?`, id).Scan(ctx, &row); err != nil {
			t.Fatalf("read config: %v", err)
		}
		return row.Config
	}
	filterOf := func(cfg map[string]any) map[string]any {
		f, _ := cfg["filter"].(map[string]any)
		return f
	}
	wantInbox := func(name string, id uuid.UUID) {
		t.Helper()
		filter := filterOf(configOf(id))
		if filter == nil || filter["query"] != "in:inbox" {
			t.Fatalf("%s: filter = %v, want {\"query\":\"in:inbox\"}", name, filter)
		}
	}

	// The whole point: a label-only scope keeps its labels and gains no filter.
	labelCfg := configOf(labelOnly)
	if filter := filterOf(labelCfg); filter != nil {
		t.Fatalf("label-only source was rewritten to filter %v", filter)
	}
	labels, _ := labelCfg["label_ids"].([]any)
	if len(labels) != 2 || labels[0] != "Label_9" || labels[1] != "Label_10" {
		t.Fatalf("label-only source lost its label scope: %v", labelCfg["label_ids"])
	}

	wantInbox("legacy scopeless source", legacyScopeLess)
	wantInbox("blank-query source", blankQuery)
	wantInbox("whitespace-query source", whitespaceQuery)
	wantInbox("malformed label_ids source", malformedLabels)

	// A source that already has a filter is never rewritten.
	filter := filterOf(configOf(existingFilter))
	if filter == nil || filter["query"] != "is:starred" {
		t.Fatalf("existing filter = %v, want unchanged", filter)
	}
}
