package graph_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestListWorkItems_JoinsLatestRun verifies the board projection: board-enabled
// objects are joined to their latest run (execution status + failure class +
// run count) keyed on subject_object_id = canonical_id, ordered by updated_at,
// and filterable by status and type.
func TestListWorkItems_JoinsLatestRun(t *testing.T) {
	ctx, svc, projectID, _, db := setupTwoProjectService(t)

	// A single schema pack declaring BoardTask + Research board-enabled.
	schemaID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, object_type_schemas, relationship_type_schemas, created_at, updated_at)
		VALUES (?::uuid, 'board-pack', '1.0.0', ?::jsonb, '{}'::jsonb, NOW(), NOW())
	`, schemaID, `[{"name":"BoardTask","boardEnabled":true},{"name":"Research","boardEnabled":true}]`).Exec(ctx)
	require.NoError(t, err)

	assignmentID := uuid.NewString()
	_, err = db.NewRaw(`
		INSERT INTO kb.project_schemas (id, project_id, schema_id, active, installed_at, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, ?::uuid, true, NOW(), NOW(), NOW())
	`, assignmentID, projectID, schemaID).Exec(ctx)
	require.NoError(t, err)

	ready := "ready"
	blocked := "blocked"
	objA, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "BoardTask", Key: strPtr("a-1"), Status: &ready,
	}, nil)
	require.NoError(t, err)
	objB, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "Research", Key: strPtr("b-1"), Status: &blocked,
	}, nil)
	require.NoError(t, err)

	// A runtime agent (kb.agents) plus two runs for objA: an older success run and
	// a newer error run — the projection must pick the newest (error, with its
	// failure class).
	agentID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?,?,?,?,?)`,
		agentID, "board-agent", "graph", "", projectID.String()).Exec(ctx)
	require.NoError(t, err)

	insertRun := func(subjectCanonical string, status, failureClass string, createdAt time.Time) {
		var fc any
		if failureClass != "" {
			fc = failureClass
		}
		_, err = db.NewRaw(`
			INSERT INTO kb.agent_runs (id, agent_id, status, subject_object_id, failure_class, started_at, created_at)
			VALUES (?, ?, ?, ?::uuid, ?, NOW(), ?)`,
			uuid.NewString(), agentID, status, subjectCanonical, fc, createdAt).Exec(ctx)
		require.NoError(t, err)
	}
	insertRun(objA.CanonicalID.String(), "success", "", time.Now().Add(-time.Hour))
	insertRun(objA.CanonicalID.String(), "error", "deterministic", time.Now())

	// Filter by status and type.
	items, err := svc.ListWorkItems(ctx, projectID.String(), "", "", 100)
	require.NoError(t, err)
	require.Len(t, items, 2)

	byCanonical := map[string]*graph.WorkItem{}
	for _, it := range items {
		byCanonical[it.CanonicalID] = it
	}

	a := byCanonical[objA.CanonicalID.String()]
	require.NotNil(t, a)
	require.Equal(t, "ready", a.Status)
	require.Equal(t, "BoardTask", a.Type)
	require.Equal(t, "a-1", a.Key)
	require.Equal(t, "error", a.LatestRunStatus)
	require.Equal(t, "deterministic", a.LatestRunFailureClass)
	require.Equal(t, 2, a.RunCount)

	b := byCanonical[objB.CanonicalID.String()]
	require.NotNil(t, b)
	require.Equal(t, "blocked", b.Status)
	require.Equal(t, "", b.LatestRunStatus)
	require.Equal(t, 0, b.RunCount)

	// Status filter.
	readyOnly, err := svc.ListWorkItems(ctx, projectID.String(), "ready", "", 100)
	require.NoError(t, err)
	require.Len(t, readyOnly, 1)
	require.Equal(t, objA.CanonicalID.String(), readyOnly[0].CanonicalID)

	// Type filter.
	researchOnly, err := svc.ListWorkItems(ctx, projectID.String(), "", "Research", 100)
	require.NoError(t, err)
	require.Len(t, researchOnly, 1)
	require.Equal(t, objB.CanonicalID.String(), researchOnly[0].CanonicalID)
}
