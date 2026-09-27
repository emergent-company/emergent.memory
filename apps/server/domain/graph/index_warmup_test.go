package graph

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// TestEmbeddingWarmTargetsEmitExactRegclass is the fail-first for the prewarm
// identifier-resolution bug: pg_prewarm(?) receives a bare index name, and
// `regclassin` downcases unquoted input while leaving resolution to search_path.
// The object index (migrations 00164/00175) is a quoted mixed-case identifier
// that must keep its quotes and `kb` schema; the relationships index (00171) is
// lowercase and only needs the schema qualifier. Asserting the formatted SQL
// (which is exactly what hits the driver) guarantees both the quoting and the
// schema qualifier are emitted.
func TestEmbeddingWarmTargetsEmitExactRegclass(t *testing.T) {
	sqldb, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, pgdialect.New())

	want := map[string]string{
		"IDX_graph_objects_embedding_v2_hnsw":    `SELECT pg_prewarm('kb."IDX_graph_objects_embedding_v2_hnsw"'::regclass)`,
		"idx_graph_relationships_embedding_hnsw": `SELECT pg_prewarm('kb.idx_graph_relationships_embedding_hnsw'::regclass)`,
	}

	require.Len(t, embeddingWarmTargets, len(want))
	for _, target := range embeddingWarmTargets {
		expected, ok := want[target.index]
		require.Truef(t, ok, "unexpected warm target %q", target.index)
		got := db.NewRaw(`SELECT pg_prewarm(?::regclass)`, target.prewarm).String()
		require.Equal(t, expected, got)
	}
}

// TestWarmEmbeddingIndexesPrewarmFailureFallsThroughToANN is the regression test
// for the other half of the defect: when pg_prewarm fails (e.g. an unresolved
// identifier), warming must fall through to the ANN query instead of `continue`-
// ing and silently leaving the index cold.
func TestWarmEmbeddingIndexesPrewarmFailureFallsThroughToANN(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })

	repo := &Repository{db: bun.NewDB(sqldb, pgdialect.New()), log: slog.Default()}

	// pg_prewarm is present, so the prewarm path is taken.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS") + `[\s\S]*pg_extension WHERE extname = 'pg_prewarm'`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	// For each target: prewarm fails with a name-resolution error, then the
	// loop must fall through to that target's ANN query before moving on.
	mock.ExpectQuery(`SELECT pg_prewarm\('kb\."IDX_graph_objects_embedding_v2_hnsw"'::regclass\)`).
		WillReturnError(errors.New(`relation "idx_graph_objects_embedding_v2_hnsw" does not exist`))
	mock.ExpectExec(`SELECT id FROM kb\.graph_objects`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(`SELECT pg_prewarm\('kb\.idx_graph_relationships_embedding_hnsw'::regclass\)`).
		WillReturnError(errors.New(`relation "idx_graph_relationships_embedding_hnsw" does not exist`))
	mock.ExpectExec(`SELECT id FROM kb\.graph_relationships`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	warmEmbeddingIndexes(context.Background(), repo)

	require.NoError(t, mock.ExpectationsWereMet())
}
