package agents

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// TestListWorkAgentStatuses_UnionsTriggerStatuses verifies the reconciler/reaper
// scan unions each agent's triggerStatuses (the status_changed signal) into the
// ready set, alongside the ready/in_progress work-config statuses and the
// built-in defaults.
func TestListWorkAgentStatuses_UnionsTriggerStatuses(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	// Query 1: ready/in_progress statuses from work_config->status.
	mock.ExpectQuery(`(?s)SELECT DISTINCT.*work_config->'status'.*ready.*in_progress`).
		WillReturnRows(sqlmock.NewRows([]string{"ready", "in_progress"}).
			AddRow("ready", "in_progress").
			AddRow("queued", "working"))

	// Query 2: trigger statuses from work_config->triggerStatuses.
	mock.ExpectQuery(`(?s)SELECT DISTINCT jsonb_array_elements_text.*triggerStatuses`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).
			AddRow("todo").
			AddRow("review"))

	ready, inProgress, err := repo.listWorkAgentStatuses(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ready", "queued", "todo", "review"}, ready)
	assert.ElementsMatch(t, []string{"in_progress", "working"}, inProgress)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListWorkAgentStatuses_DefaultsWhenNoAgents verifies the scan returns the
// built-in defaults even when no work agents are configured (both queries empty).
func TestListWorkAgentStatuses_DefaultsWhenNoAgents(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	mock.ExpectQuery(`(?s)SELECT DISTINCT.*work_config->'status'.*ready.*in_progress`).
		WillReturnRows(sqlmock.NewRows([]string{"ready", "in_progress"}))
	mock.ExpectQuery(`(?s)SELECT DISTINCT jsonb_array_elements_text.*triggerStatuses`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}))

	ready, inProgress, err := repo.listWorkAgentStatuses(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ready"}, ready)
	assert.ElementsMatch(t, []string{"in_progress"}, inProgress)
	require.NoError(t, mock.ExpectationsWereMet())
}
