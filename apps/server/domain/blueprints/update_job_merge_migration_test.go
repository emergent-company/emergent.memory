package blueprints

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/migrations"
)

// TestMigration_00263_AddsUpdateJobMergeFlag guards the schema addition the
// async update merge path depends on.
func TestMigration_00263_AddsUpdateJobMergeFlag(t *testing.T) {
	b, err := migrations.FS.ReadFile("00263_blueprint_update_merge_type_conflicts.sql")
	require.NoError(t, err)
	sql := string(b)

	assert.Contains(t, sql, "ALTER TABLE kb.blueprint_update_jobs")
	assert.Contains(t, sql, "ADD COLUMN IF NOT EXISTS merge_type_conflicts boolean NOT NULL DEFAULT false")
	assert.True(t, strings.Contains(sql, "merge_type_conflicts"), "migration must add the merge flag column")
	// Up-only by design; guard against an accidental destructive Down sneaking in.
	assert.NotContains(t, sql, "-- +goose Down")
}
