package graph_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// fixedSchemaProvider returns a hand-built schema set so the test does not need
// to seed kb.graph_schemas; only the graph runtime work-config surface matters.
type fixedSchemaProvider struct {
	schemas *graph.ExtractionSchemas
}

func (p fixedSchemaProvider) GetProjectSchemas(context.Context, string) (*graph.ExtractionSchemas, error) {
	return p.schemas, nil
}

func (p fixedSchemaProvider) InvalidateProjectCache(string) {}

func initialStatusSchemas() *graph.ExtractionSchemas {
	emptyProps := map[string]agents.PropertyDef{}
	return &graph.ExtractionSchemas{ObjectSchemas: map[string]agents.ObjectSchema{
		// Fallback case: no explicit initialStatus, first allowed status wins.
		"TaskFallback": {
			Name:       "TaskFallback",
			Properties: emptyProps,
			ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
				BoardEnabled:    true,
				AllowedStatuses: []string{"pending", "in_progress", "done"},
			},
		},
		// Explicit initialStatus that differs from the first allowed status.
		"TaskExplicit": {
			Name:       "TaskExplicit",
			Properties: emptyProps,
			ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
				BoardEnabled:    true,
				InitialStatus:   "open",
				AllowedStatuses: []string{"ready", "open", "closed"},
			},
		},
		// Board-enabled but with no declared vocabulary: no default applies.
		"TaskEmpty": {
			Name:       "TaskEmpty",
			Properties: emptyProps,
			ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
				BoardEnabled: true,
			},
		},
		// Non-board type is unaffected.
		"Note": {
			Name:       "Note",
			Properties: emptyProps,
		},
	}}
}

func initialStatePtr(s string) *string { return &s }

func setupInitialStatusTest(t *testing.T) (context.Context, *graph.Service, bun.IDB, *config.Config, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "boardinitial")
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Board Initial Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Board Initial Project",
	}, testutil.AdminUser.ID))

	cfg := *testDB.Config
	repo := graph.NewRepository(db, slog.Default(), &cfg)
	svc := graph.NewService(repo, slog.Default(), fixedSchemaProvider{schemas: initialStatusSchemas()},
		nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, db, &cfg, uuid.MustParse(projectID)
}

func objectStatus(t *testing.T, db bun.IDB, projectID uuid.UUID, key string) string {
	t.Helper()
	var status *string
	err := db.NewRaw(`SELECT status FROM kb.graph_objects WHERE project_id = ? AND key = ? AND supersedes_id IS NULL`,
		projectID, key).Scan(context.Background(), &status)
	require.NoError(t, err)
	require.NotNil(t, status)
	return *status
}

// TestCreateBoardObjectAppliesInitialStatus is the reported-case regression
// (#422): an agent-authenticated create of a board-enabled type with no status
// must land in the type's resolved initial status, not the "Unknown" lane.
func TestCreateBoardObjectAppliesInitialStatus(t *testing.T) {
	ctx, svc, db, _, projectID := setupInitialStatusTest(t)
	agentID := uuid.New()
	agentCtx := auth.WithActor(ctx, graph.ActorAgent, &agentID)

	t.Run("agent create with no status gets the resolved initial status", func(t *testing.T) {
		resp, err := svc.Create(agentCtx, projectID, &graph.CreateGraphObjectRequest{
			Type: "TaskFallback",
			Key:  initialStatePtr("agent-no-status"),
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, resp.Status)
		require.Equal(t, "pending", *resp.Status)
		require.Equal(t, "pending", objectStatus(t, db, projectID, "agent-no-status"))
	})

	t.Run("explicit initialStatus is honoured", func(t *testing.T) {
		resp, err := svc.Create(agentCtx, projectID, &graph.CreateGraphObjectRequest{
			Type: "TaskExplicit",
			Key:  initialStatePtr("agent-explicit"),
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, resp.Status)
		require.Equal(t, "open", *resp.Status)
	})

	t.Run("agent create with status equal to the initial status is allowed", func(t *testing.T) {
		status := "open"
		resp, err := svc.Create(agentCtx, projectID, &graph.CreateGraphObjectRequest{
			Type:   "TaskExplicit",
			Key:    initialStatePtr("agent-explicit-allowed"),
			Status: &status,
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, resp.Status)
		require.Equal(t, "open", *resp.Status)
	})

	t.Run("agent create with a non-initial status is still forbidden", func(t *testing.T) {
		status := "ready"
		_, err := svc.Create(agentCtx, projectID, &graph.CreateGraphObjectRequest{
			Type:   "TaskExplicit",
			Key:    initialStatePtr("agent-explicit-forbidden"),
			Status: &status,
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "work_status_owned_by_platform")
	})

	t.Run("human create with no status is defaulted too", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "TaskFallback",
			Key:  initialStatePtr("human-no-status"),
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, resp.Status)
		require.Equal(t, "pending", *resp.Status)
	})

	t.Run("non-board type keeps the empty status", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "Note",
			Key:  initialStatePtr("human-note"),
		}, nil)
		require.NoError(t, err)
		require.Nil(t, resp.Status)
	})

	t.Run("board type with empty allowedStatuses is not broken", func(t *testing.T) {
		resp, err := svc.Create(agentCtx, projectID, &graph.CreateGraphObjectRequest{
			Type: "TaskEmpty",
			Key:  initialStatePtr("agent-empty"),
		}, nil)
		require.NoError(t, err)
		require.Nil(t, resp.Status)
	})
}
