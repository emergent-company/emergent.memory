package migrate

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
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

// TestFindInvalidIndexes requires a live PostgreSQL database. It is skipped
// unless TEST_DATABASE_URL is set, since the pure assertions above are the
// unconditional coverage.
func TestFindInvalidIndexes(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping live FindInvalidIndexes test")
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	found, err := FindInvalidIndexes(context.Background(), db)
	if err != nil {
		t.Fatalf("FindInvalidIndexes: %v", err)
	}
	// No assertion on found contents — depends on the target database. But the
	// call must succeed against a valid schema.
	t.Logf("found %d invalid index(es)", len(found))
}
