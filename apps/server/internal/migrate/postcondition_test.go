package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func TestMarkAppliedBypassNotice(t *testing.T) {
	notice := MarkAppliedBypassNotice(171)

	if !strings.Contains(notice, "171") {
		t.Errorf("notice should name the version 171:\n%s", notice)
	}
	if !strings.Contains(notice, "Post-conditions are NOT verified") {
		t.Errorf("notice should state post-conditions are not verified:\n%s", notice)
	}
	if !strings.Contains(notice, "mark-applied") {
		t.Errorf("notice should mention mark-applied:\n%s", notice)
	}
	if !strings.Contains(notice, "goose_db_version") {
		t.Errorf("notice should mention goose_db_version bypass:\n%s", notice)
	}
	if !strings.Contains(notice, InvalidIndexQuery) {
		t.Errorf("notice should include the exact inspection query:\n%s", notice)
	}
	if !strings.Contains(notice, "REINDEX INDEX CONCURRENTLY") {
		t.Errorf("notice should include the remediation:\n%s", notice)
	}
}

func TestInvalidIndexesErrorFormatting(t *testing.T) {
	found := []InvalidIndex{
		{Schema: "kb", Table: "chunks", Index: "chunks_embedding_idx"},
		{Schema: "core", Table: "user_profiles", Index: "user_profiles_pkey"},
	}
	err := invalidIndexesError(found)

	text := err.Error()
	if !strings.Contains(text, "2 invalid index") {
		t.Errorf("error should state the count:\n%s", text)
	}
	if !strings.Contains(text, "kb.chunks -> chunks_embedding_idx") {
		t.Errorf("error should name the first index:\n%s", text)
	}
	if !strings.Contains(text, "core.user_profiles -> user_profiles_pkey") {
		t.Errorf("error should name the second index:\n%s", text)
	}
	if !strings.Contains(text, "REINDEX INDEX CONCURRENTLY") {
		t.Errorf("error should include the remediation:\n%s", text)
	}
}

func TestIndexNames(t *testing.T) {
	found := []InvalidIndex{
		{Schema: "kb", Table: "documents", Index: "documents_title_idx"},
	}
	names := indexNames(found)
	if len(names) != 1 || names[0] != "kb.documents -> documents_title_idx" {
		t.Errorf("indexNames() = %v, want [kb.documents -> documents_title_idx]", names)
	}
}

// TestFindInvalidIndexes requires a live PostgreSQL database (skipped when
// unavailable or in short mode, because the unit job has no Postgres service).
// It asserts the detection behaviour against a *controlled* invalid index: a
// CONCURRENTLY unique build over duplicate values is aborted by PostgreSQL and
// leaves the index behind with indisvalid = false, exactly the residue the query
// targets. It also asserts the healthy (no-probe) case reports nothing. The SQL
// predicate itself lives in the query, so it can only be exercised against a
// real catalog — a mock driver would just return pre-filtered rows.
func TestFindInvalidIndexes(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}

	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "migrate_invalid_indexes")
	t.Cleanup(tdb.Close)

	// FindInvalidIndexes needs a database/sql handle; open one from the pool.
	db := stdlib.OpenDBFromPool(tdb.Pool)
	defer db.Close()
	ctx := context.Background()

	const probeIndex = "idx_find_invalid_indexes_probe"
	const probeTable = "kb._find_invalid_indexes_probe"

	cleanup := func() {
		// Plain DROP INDEX: the probe index is intentionally invalid and this is a
		// throwaway database, so the exclusive lock a non-concurrent drop takes is
		// acceptable and avoids CONCURRENTLY's restrictions.
		_, _ = db.ExecContext(ctx, "DROP INDEX IF EXISTS kb."+probeIndex)
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+probeTable)
	}
	cleanup()
	defer cleanup()

	// Healthy case: no probe index yet, so the probe must not be reported.
	healthy, err := FindInvalidIndexes(ctx, db)
	if err != nil {
		t.Fatalf("FindInvalidIndexes (pre): %v", err)
	}
	if containsIndex(healthy, probeIndex) {
		t.Fatalf("pre-condition: stale probe index %s already present", probeIndex)
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE "+probeTable+" (id serial primary key, v int)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+probeTable+" (v) VALUES (1), (1)"); err != nil {
		t.Fatalf("insert duplicate probe rows: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+probeIndex+" ON "+probeTable+" (v)"); err == nil {
		t.Fatal("concurrent unique build over duplicates unexpectedly succeeded; cannot exercise the invalid-index path")
	}

	// Invalid case: the aborted build must be reported.
	found, err := FindInvalidIndexes(ctx, db)
	if err != nil {
		t.Fatalf("FindInvalidIndexes (post): %v", err)
	}
	if !containsIndex(found, probeIndex) {
		t.Fatalf("invalid index %s not detected; got %#v", probeIndex, found)
	}
	if !strings.Contains(invalidIndexesError(found).Error(), probeIndex) {
		t.Fatalf("diagnostic error does not name %s: %v", probeIndex, invalidIndexesError(found))
	}
}

// containsIndex reports whether list contains an index with the given name.
func containsIndex(list []InvalidIndex, name string) bool {
	for _, ii := range list {
		if ii.Index == name {
			return true
		}
	}
	return false
}
