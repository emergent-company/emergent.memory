package schemaregistry

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func newVersionHistoryRepo(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return NewRepository(bun.NewDB(sqldb, pgdialect.New())), mock
}

// TestGetTypeVersionRowsSelectsByRegistryID pins the read-path fix from #1236:
// version rows are selected by registry_id, not type_name, so a version
// written under an earlier spelling of the type name (for example one carried
// onto the winning registry row by the operator repair) is still returned. The
// stored type_name travels with each row so a client can render the rename.
func TestGetTypeVersionRowsSelectsByRegistryID(t *testing.T) {
	repo, mock := newVersionHistoryRepo(t)

	const (
		projectID  = "11111111-1111-1111-1111-111111111111"
		registryID = "22222222-2222-2222-2222-222222222222"
	)

	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// bun.NewRaw inlines its arguments, so the expectation matches the resolved
	// filter: project_id AND registry_id — never type_name.
	filter := fmt.Sprintf(
		`SELECT[\s\S]*FROM kb\.project_object_schema_registry_versions[\s\S]*WHERE project_id = '%s' AND registry_id = '%s'[\s\S]*ORDER BY schema_version DESC`,
		regexp.QuoteMeta(projectID), regexp.QuoteMeta(registryID),
	)
	mock.ExpectQuery(filter).
		WillReturnRows(sqlmock.NewRows([]string{
			"registry_id", "type_name", "schema_version", "json_schema", "ui_config",
			"extraction_config", "enabled", "description", "namespace", "created_by",
			"created_at",
		}).
			AddRow(registryID, "Person", 4, `{"type":"object"}`, nil, nil, true, nil, nil, nil, createdAt).
			AddRow(registryID, "person", 3, `{"type":"object"}`, nil, nil, true, nil, nil, nil, createdAt))

	rows, err := repo.getTypeVersionRows(context.Background(), projectID, registryID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 4, rows[0].SchemaVersion)
	require.Equal(t, "Person", rows[0].TypeName)
	require.Equal(t, 3, rows[1].SchemaVersion)
	require.Equal(t, "person", rows[1].TypeName, "a version written under the old spelling must stay in the history")
	require.NoError(t, mock.ExpectationsWereMet())
}
