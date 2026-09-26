package graph_test

import (
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// newGraphSQLMockDB wraps a *bun.DB over go-sqlmock (PostgreSQL dialect) so a
// repository's raw query string can be asserted without a live database.
func newGraphSQLMockDB(t *testing.T) (*bun.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return bun.NewDB(sqldb, pgdialect.New()), mock
}

func newGraphSQLRepo(db bun.IDB) *graph.Repository {
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	return graph.NewRepository(db, slog.Default(), cfg)
}

// annSubqueryRe matches the HNSW-safe shape: distance is computed inside an ANN
// subquery (aliased `_dist`) ordered by the raw `embedding_v2 <=> ...::vector`
// expression only — no secondary sort key, so the HNSW index stays usable — and
// the deterministic (_dist, id) tie-break is applied in the outer query. The old
// buggy form used `AS distance` + `ORDER BY distance ASC, id ASC` with no
// subquery, so these markers cleanly distinguish it.
var annSubqueryRe = regexp.MustCompile(
	`(?s)AS _dist[\s\S]*\) AS ann[\s\S]*ORDER BY _dist ASC, id ASC`,
)

// TestVectorSearchUsesHNSWSubquery is a regression test for the HNSW index
// bypass: adding a secondary sort key (`id ASC`) to the vector ORDER BY forced
// a full Seq Scan + Sort. The query must keep the raw distance ordering inside
// an ANN subquery and apply the tie-break only on the limited window.
func TestVectorSearchUsesHNSWSubquery(t *testing.T) {
	db, mock := newGraphSQLMockDB(t)
	repo := newGraphSQLRepo(db)

	mock.ExpectQuery(annSubqueryRe.String()).WillReturnRows(sqlmock.NewRows([]string{}))

	_, err := repo.VectorSearch(context.Background(), graph.VectorSearchParams{
		ProjectID: uuid.New(),
		Vector:    []float32{0.1, 0.2, 0.3},
		Limit:     20,
		Offset:    0,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestFindSimilarObjectsUsesHNSWSubquery asserts the same ANN-subquery shape for
// the similar-objects vector query.
func TestFindSimilarObjectsUsesHNSWSubquery(t *testing.T) {
	db, mock := newGraphSQLMockDB(t)
	repo := newGraphSQLRepo(db)

	// First query: source object embedding lookup.
	mock.ExpectQuery(`(?s)SELECT embedding_v2::text[\s\S]*kb\.graph_objects`).
		WillReturnRows(sqlmock.NewRows([]string{"embedding_v2"}).AddRow("[0.1,0.2,0.3]"))
	// Second query: ANN subquery for similar objects.
	mock.ExpectQuery(annSubqueryRe.String()).WillReturnRows(sqlmock.NewRows([]string{}))

	_, err := repo.FindSimilarObjects(context.Background(), graph.SimilarSearchParams{
		ProjectID: uuid.New(),
		ObjectID:  uuid.New(),
		Limit:     10,
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
