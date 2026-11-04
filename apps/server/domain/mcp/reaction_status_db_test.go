package mcp

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// issueWorkSchemaProvider reports Issue as a board-enabled type with the
// webpage-feedback status vocabulary, so the real graph guard is active in the
// executeUpdateEntity test.
type issueWorkSchemaProvider struct{}

func (issueWorkSchemaProvider) GetProjectSchemas(context.Context, string) (*graph.ExtractionSchemas, error) {
	return &graph.ExtractionSchemas{
		ObjectSchemas: map[string]agents.ObjectSchema{
			"Issue": {
				Name: "Issue",
				ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
					BoardEnabled:    true,
					AllowedStatuses: []string{"new", "triaged", "needs-info", "wont-fix"},
				},
			},
		},
	}, nil
}

func (issueWorkSchemaProvider) InvalidateProjectCache(string) {}

func newReactionStatusService(t *testing.T, db bun.IDB) *Service {
	t.Helper()
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	graphRepo := graph.NewRepository(db, log, cfg)
	graphSvc := graph.NewService(graphRepo, log, issueWorkSchemaProvider{}, nil, &fakeEmbedder{vec: testVector()}, nil, nil, nil, nil, nil)
	return &Service{graphService: graphSvc, db: db, log: log}
}

func seedIssueWorkObject(t *testing.T, db bun.IDB, projectID string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status, key,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, 'Issue', 'new', ?,
			 '{}'::jsonb, '{}'::text[], ?, ?, ?)
	`, id, projectID, id, "issue-"+id, "content-hash-"+id, now, now)
	require.NoError(t, err)
	return id
}

// TestExecuteUpdateEntityReactionSubjectStatus covers the MCP half of the
// plain-reaction-agent exemption: an agent write to a board-enabled Issue is
// denied unless the run's stamped subject matches the target canonical object,
// and the allowed write still passes validateTypeStatus.
func TestExecuteUpdateEntityReactionSubjectStatus(t *testing.T) {
	db := connectTestDB(t)
	orgID, projectID := seedProject(t, db)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.projects WHERE id = ?", projectID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.orgs WHERE id = ?", orgID)
	})

	issueID := seedIssueWorkObject(t, db, projectID)
	otherID := seedIssueWorkObject(t, db, projectID)
	svc := newReactionStatusService(t, db)
	projectUUID := uuid.MustParse(projectID)
	agentID := uuid.New()
	agentCtx := auth.WithActor(context.Background(), graph.ActorAgent, &agentID)
	args := map[string]any{"entity_id": issueID, "status": "triaged"}

	// No reaction subject: the platform guard still rejects the status write.
	_, err := svc.executeUpdateEntity(agentCtx, projectID, args)
	require.Error(t, err)
	require.Contains(t, err.Error(), "work_status_owned_by_platform")

	// A subject that names a different object must not authorize this one.
	_, err = svc.executeUpdateEntity(auth.WithAgentWorkStatusSubject(agentCtx, otherID), projectID, args)
	require.Error(t, err)
	require.Contains(t, err.Error(), "work_status_owned_by_platform")

	// The matching subject allows the status write.
	res, err := svc.executeUpdateEntity(auth.WithAgentWorkStatusSubject(agentCtx, issueID), projectID, args)
	require.NoError(t, err)
	require.NotNil(t, res)

	obj, err := svc.graphService.GetByID(context.Background(), projectUUID, uuid.MustParse(issueID), true)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.NotNil(t, obj.Status)
	require.Equal(t, "triaged", *obj.Status)
}
