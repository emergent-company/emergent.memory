package sources

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func TestSourceWorkflowStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_workflow_store")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, sourceID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "workflow-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "workflow-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		VALUES (?, ?, 'github_app', 'bot', '{}'::jsonb, 'active')`, sourceID, projectID)
	require.NoError(t, err)

	require.Nil(t, func() *SourceWorkflow { wf, _ := repo.GetSourceWorkflow(ctx, projectID, sourceID); return wf }())

	wf := &SourceWorkflow{ProjectID: projectID, SourceID: sourceID, Enabled: true, Config: map[string]any{
		"reviewers": []any{map[string]any{"lane": "security", "agent": "security-review"}},
	}}
	require.NoError(t, repo.UpsertSourceWorkflow(ctx, wf))
	got, err := repo.GetSourceWorkflow(ctx, projectID, sourceID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, got.Enabled)
	require.NotEmpty(t, got.Config["reviewers"])
}
