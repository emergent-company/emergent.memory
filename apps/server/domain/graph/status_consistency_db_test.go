package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestCreateOrUpdate_MirrorsPropertiesStatus pins #2241: the upsert path must
// mirror properties["status"] into the status column exactly like Create/Patch.
func TestCreateOrUpdate_MirrorsPropertiesStatus(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"Scenario": {},
	})

	props := map[string]any{"status": "proposed"}
	created, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "Scenario",
		Key:        strPtr("S-005"),
		Properties: props,
	}, nil)
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotNil(t, created.Status)
	require.Equal(t, "proposed", *created.Status)

	updated, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "Scenario",
		Key:        strPtr("S-005"),
		Properties: map[string]any{"status": "accepted"},
	}, nil)
	require.NoError(t, err)
	require.False(t, isNew)
	require.NotNil(t, updated.Status)
	require.Equal(t, "accepted", *updated.Status)

	// The column is what every other consumer reads; re-read it from the store.
	got, err := svc.GetByID(ctx, projectID, created.ID, false)
	require.NoError(t, err)
	require.NotNil(t, got.Status)
	require.Equal(t, "accepted", *got.Status)
}

// TestCreateOrUpdate_PropertiesStatusWinsOverExplicit pins the documented
// precedence: properties["status"] is authoritative when both are supplied.
func TestCreateOrUpdate_PropertiesStatusWinsOverExplicit(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{})

	explicit := "ready"
	props := map[string]any{"status": "done"}
	created, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "Scenario",
		Key:        strPtr("S-006"),
		Status:     &explicit,
		Properties: props,
	}, nil)
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotNil(t, created.Status)
	require.Equal(t, "done", *created.Status)
}

// TestCreateSubgraph_MirrorsPropertiesStatus pins #2241 for the batched path.
func TestCreateSubgraph_MirrorsPropertiesStatus(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"Scenario": {},
	})

	resp, err := svc.CreateSubgraph(ctx, projectID, &graph.CreateSubgraphRequest{
		Objects: []graph.SubgraphObjectRequest{
			{Ref: "s1", Type: "Scenario", Properties: map[string]any{"status": "proposed"}},
		},
	}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Objects, 1)
	require.NotNil(t, resp.Objects[0].Status)
	require.Equal(t, "proposed", *resp.Objects[0].Status)
}

// TestBulkUpdateStatus_RejectsTypeWithoutWorkflow pins #2242: the bulk path must
// not stamp a work status on a type that declares none, while a legitimate
// workflow type in the same request still succeeds.
func TestBulkUpdateStatus_RejectsTypeWithoutWorkflow(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"Scenario": {},
		"BoardTask": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:    true,
			AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
		}},
	})

	scenario, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "Scenario",
		Key:        strPtr("S-007"),
		Properties: map[string]any{"status": "proposed"},
	}, nil)
	require.NoError(t, err)

	board, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "BoardTask",
		Key:        strPtr("T-1"),
		Properties: map[string]any{"status": "ready"},
	}, nil)
	require.NoError(t, err)
	require.True(t, isNew)

	bulk, err := svc.BulkUpdateStatus(ctx, projectID, &graph.BulkUpdateStatusRequest{
		IDs:    []string{scenario.ID.String(), board.ID.String()},
		Status: "done",
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, bulk.Success)
	require.Equal(t, 1, bulk.Failed)

	for _, r := range bulk.Results {
		switch r.ID {
		case scenario.ID.String():
			require.False(t, r.Success)
			require.NotNil(t, r.Error)
			require.Contains(t, *r.Error, "declares no workflow")
		case board.ID.String():
			require.True(t, r.Success)
		}
	}
}
