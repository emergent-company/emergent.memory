package search

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// Regression coverage for #1902: a namespace-restricted search must not leak
// chunks from other namespaces. kb.chunks has no namespace column, so the text
// leg scopes chunks through the extraction link (kb.object_chunks →
// kb.graph_objects.namespace). These tests pin both the predicate builder and
// the fact that every chunk query actually applies it.
//
// bun interpolates the bound args into the SQL it hands the driver, so the
// assertions match the rendered value (o.namespace = 'system'), which is exactly
// the property under test.
func newChunkSearchRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewRepository(bun.NewDB(sqldb, pgdialect.New()), log), mock
}

func TestChunkNamespacePredicate(t *testing.T) {
	t.Run("nil and empty namespace add no predicate", func(t *testing.T) {
		for _, ns := range []*string{nil, strPtr("")} {
			clause, args := chunkNamespacePredicate(ns, false)
			assert.Empty(t, clause, "no namespace restriction must not alter the query")
			assert.Empty(t, args)
		}
	})

	t.Run("a set namespace scopes chunks through the object link", func(t *testing.T) {
		clause, args := chunkNamespacePredicate(strPtr("system"), false)
		// The restriction must be expressed as an EXISTS over the extraction
		// link, not as a column on kb.chunks (which has none).
		assert.Contains(t, clause, "EXISTS")
		assert.Contains(t, clause, "kb.object_chunks")
		assert.Contains(t, clause, "kb.graph_objects")
		assert.Contains(t, clause, "oc.chunk_id = c.id")
		assert.Contains(t, clause, "o.namespace = ?")
		// Fail-closed and project-bounded.
		assert.Contains(t, clause, "o.project_id = d.project_id")
		// Deletes are versioned: the link (kb.object_chunks.object_id) points at
		// the version row at extraction time, so the predicate must resolve it to
		// the object's live HEAD (supersedes_id IS NULL AND deleted_at IS NULL)
		// before testing namespace; otherwise a superseded prior HEAD — whose own
		// deleted_at is still NULL after a SoftDelete tombstone supersedes it —
		// would still admit the chunk.
		assert.Contains(t, clause, "ov.id = oc.object_id")
		assert.Contains(t, clause, "o.canonical_id = ov.canonical_id")
		assert.Contains(t, clause, "o.supersedes_id IS NULL")
		assert.Contains(t, clause, "o.deleted_at IS NULL")
		assert.Equal(t, []any{"system"}, args)
	})

	t.Run("null namespace scopes chunks to namespace IS NULL", func(t *testing.T) {
		clause, args := chunkNamespacePredicate(nil, true)
		assert.Contains(t, clause, "kb.object_chunks")
		assert.Contains(t, clause, "o.supersedes_id IS NULL")
		assert.Contains(t, clause, "o.deleted_at IS NULL")
		assert.Contains(t, clause, "o.namespace IS NULL")
		assert.NotContains(t, clause, "o.namespace = ?")
		assert.Empty(t, args, "the IS NULL form carries no bind arg")
	})

	t.Run("an explicit namespace wins over the null flag", func(t *testing.T) {
		clause, args := chunkNamespacePredicate(strPtr("system"), true)
		assert.Contains(t, clause, "o.namespace = ?")
		assert.NotContains(t, clause, "o.namespace IS NULL")
		assert.Equal(t, []any{"system"}, args)
	})
}

// chunkScopedRE matches the namespace-scoped chunk query: the EXISTS restriction
// resolves the linked version to the object's live HEAD (supersedes_id /
// deleted_at), is project-bounded, and carries the requested namespace. bun
// interpolates the bound args into the SQL it hands the driver, so the assertion
// matches the rendered value (o.namespace = 'system').
const chunkScopedRE = `EXISTS[\s\S]*kb\.object_chunks[\s\S]*ov\.id = oc\.object_id[\s\S]*o\.canonical_id = ov\.canonical_id[\s\S]*oc\.chunk_id = c\.id[\s\S]*o\.project_id = d\.project_id[\s\S]*o\.supersedes_id IS NULL[\s\S]*o\.deleted_at IS NULL[\s\S]*o\.namespace = 'system'`

// chunkNullScopedRE matches the default (null) namespace chunk query.
const chunkNullScopedRE = `EXISTS[\s\S]*kb\.object_chunks[\s\S]*ov\.id = oc\.object_id[\s\S]*o\.canonical_id = ov\.canonical_id[\s\S]*oc\.chunk_id = c\.id[\s\S]*o\.project_id = d\.project_id[\s\S]*o\.supersedes_id IS NULL[\s\S]*o\.deleted_at IS NULL[\s\S]*o\.namespace IS NULL`

func TestLexicalSearch_NamespaceScopesChunkQuery(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()

	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 20`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "hit", 0.9))

	resp, err := repo.LexicalSearch(context.Background(), TextSearchParams{
		ProjectID: projectID,
		Query:     "needle",
		Limit:     20,
		Namespace: strPtr("system"),
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVectorSearch_NamespaceScopesChunkQuery(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()

	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 20`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "hit", 0.9))

	resp, err := repo.VectorSearch(context.Background(), TextSearchParams{
		ProjectID: projectID,
		Vector:    []float32{0.1, 0.2},
		Limit:     20,
		Namespace: strPtr("system"),
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHybridSearch_NamespaceScopesBothLegs pins that the hybrid path bounds the
// vector leg too: the lexical leg alone is not enough, because a chunk can be
// reached by embedding similarity without matching the lexical query. Both legs
// run at 2x limit (fetchLimit).
func TestHybridSearch_NamespaceScopesBothLegs(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()

	// Lexical leg: one hit, so lexicalSearch short-circuits (no relax retry).
	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "lexical hit", 0.9))
	// Vector leg: must also carry the namespace predicate.
	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "vector hit", 0.9))

	resp, err := repo.HybridSearch(context.Background(), TextSearchParams{
		ProjectID: projectID,
		Query:     "needle",
		Vector:    []float32{0.1, 0.2},
		Limit:     20,
		Namespace: strPtr("system"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestLexicalSearch_NullNamespaceScopesChunkQuery pins the default-search (#1910)
// scoping: with no namespace but NamespaceIsNull set, the chunk query carries
// the `o.namespace IS NULL` predicate so chunks extracted into non-null-namespace
// objects cannot leak into an MCP default search.
func TestLexicalSearch_NullNamespaceScopesChunkQuery(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()

	mock.ExpectQuery(chunkNullScopedRE + `[\s\S]*LIMIT 20`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "hit", 0.9))

	resp, err := repo.LexicalSearch(context.Background(), TextSearchParams{
		ProjectID:       projectID,
		Query:           "needle",
		Limit:           20,
		NamespaceIsNull: true,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestExecuteTextSearch_ForwardsNamespace is the #1910(c) regression pin: the
// exact line #1902 broke — `executeTextSearch` → `TextSearchParams.Namespace`.
// Deleting it leaves the emitted chunk query unscoped, so the mock expectation
// (which requires the namespace predicate) fails.
func TestExecuteTextSearch_ForwardsNamespace(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewService(repo, nil, nil, log, nil)

	// A queryVector is supplied so executeTextSearch runs HybridSearch (both
	// legs) without needing an embedder; both legs must carry the predicate.
	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "lexical hit", 0.9))
	mock.ExpectQuery(chunkScopedRE + `[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "vector hit", 0.9))

	ns := "system"
	results, _, _, err := svc.executeTextSearch(context.Background(), projectID, &UnifiedSearchRequest{
		Query:     "needle",
		Limit:     20,
		Namespace: &ns,
	}, []float32{0.1, 0.2}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, mock.ExpectationsWereMet(),
		"executeTextSearch must forward req.Namespace into TextSearchParams (#1902/#1910)")
}
