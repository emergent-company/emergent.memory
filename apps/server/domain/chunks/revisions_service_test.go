package chunks

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// These tests exercise the validation paths of the curation service, which run
// before any repository call and therefore need no database. The SQL-backed
// append/list/rollback behavior is covered by the database-backed tests that
// skip when Postgres is unreachable.

func TestEditChunkRejectsEmptyText(t *testing.T) {
	svc := &Service{}
	_, err := svc.EditChunk(context.Background(), uuid.New(), uuid.New(), "   \n  ")
	if err == nil {
		t.Fatal("expected error for empty chunk text")
	}
}

func TestEditChunkRejectsOversizedText(t *testing.T) {
	svc := &Service{}
	_, err := svc.EditChunk(context.Background(), uuid.New(), uuid.New(), strings.Repeat("a", MaxChunkTextBytes+1))
	if err == nil {
		t.Fatal("expected error for oversized chunk text")
	}
}

func TestDiffRevisionsRejectsNonPositive(t *testing.T) {
	svc := &Service{}
	_, err := svc.DiffRevisions(context.Background(), uuid.New(), uuid.New(), 0, 1)
	if err == nil {
		t.Fatal("expected error for from < 1")
	}
	_, err = svc.DiffRevisions(context.Background(), uuid.New(), uuid.New(), 1, 0)
	if err == nil {
		t.Fatal("expected error for to < 1")
	}
}

func TestRollbackChunkRejectsNonPositiveSeq(t *testing.T) {
	svc := &Service{}
	_, err := svc.RollbackChunk(context.Background(), uuid.New(), uuid.New(), 0)
	if err == nil {
		t.Fatal("expected error for seq < 1")
	}
}

func TestAuthorFromContextNoUser(t *testing.T) {
	if got := authorFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil author without a user in context, got %v", *got)
	}
}
