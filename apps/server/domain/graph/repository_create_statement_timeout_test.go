package graph_test

import (
	"context"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// newGraphSQLRepoWithBulkTimeout is newGraphSQLRepo with the bulk statement
// timeout configured, so BulkContext attaches a bound.
func newGraphSQLRepoWithBulkTimeout(db bun.IDB, d time.Duration) *graph.Repository {
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	cfg.Database.BulkStatementTimeout = d
	return graph.NewRepository(db, slog.Default(), cfg)
}

// TestRepositoryCreate_AppliesBulkStatementTimeout is the regression guard for
// the objects/bulk endpoint (issue #1481): Create is a bare single-statement
// insert, so when a bulk statement timeout is attached it MUST run the insert
// inside a transaction and emit the transaction-local set_config first —
// otherwise the bulk endpoint's timeout is silently a no-op.
func TestRepositoryCreate_AppliesBulkStatementTimeout(t *testing.T) {
	db, mock := newGraphSQLMockDB(t)
	repo := newGraphSQLRepoWithBulkTimeout(db, 60*time.Second)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('statement_timeout', '60000', true)")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err := repo.Create(repo.BulkContext(context.Background()), &graph.GraphObject{
		ProjectID: uuid.New(),
		Type:      "Widget",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRepositoryCreate_NoTransactionWithoutTimeout proves ordinary creates do
// not pay a BEGIN/set_config/COMMIT round trip: with no bulk bound attached the
// insert stays a single statement.
func TestRepositoryCreate_NoTransactionWithoutTimeout(t *testing.T) {
	db, mock := newGraphSQLMockDB(t)
	repo := newGraphSQLRepoWithBulkTimeout(db, 60*time.Second)

	mock.ExpectQuery("INSERT INTO").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	err := repo.Create(context.Background(), &graph.GraphObject{
		ProjectID: uuid.New(),
		Type:      "Widget",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
