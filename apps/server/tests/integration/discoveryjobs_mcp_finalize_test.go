package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestFinalizeDiscoveryFromMCP covers the adapter the find-schema-types MCP
// tool calls (domain_tools.go → Service.FinalizeDiscoveryFromMCP). It is the
// only path that finalizes a run the agent did not start, so it is also the only
// path that can arrive with no job row at all — and the adapter's answer to that
// is to fabricate a pre-completed stub job rather than start a real discovery run
// (which would burn an LLM call and orphan the schema it produced).
//
// Nothing here needs a model: finalize installs a pack from the agent-supplied
// types, and extraction-prompt generation is best-effort (its failure is logged,
// not returned). A nil model factory therefore pins that a run cannot be blocked
// by an unreachable LLM after the user already confirmed the types.
func TestFinalizeDiscoveryFromMCP(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "discmcpfinalize")
	t.Cleanup(testDB.Close)

	require.NoError(t, testutil.SetupTestFixtures(ctx, testDB.DB))

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	docsSvc := documents.NewService(documents.NewRepository(testDB.DB, log), log)
	djRepo := discoveryjobs.NewRepository(testDB.DB, log)
	djSvc := discoveryjobs.NewService(djRepo, docsSvc, &config.Config{}, nil, log)

	// Finalize re-checks the caller's membership of the job's project
	// (requireJobAccess), so the call has to carry the identity the MCP tool
	// runs as. SetupFullTestProject makes this user an org member of every
	// project built below.
	authCtx := auth.ContextWithUser(ctx, &auth.AuthUser{ID: testutil.AdminUser.ID})

	// Each subtest gets its own project and document so one finalize's pack or
	// job cannot be read as another's.
	newProject := func(t *testing.T) (string, uuid.UUID, documents.Document) {
		t.Helper()
		orgID := uuid.New().String()
		projectID := uuid.New().String()
		require.NoError(t, testutil.SetupFullTestProject(ctx, testDB.DB, orgID, projectID))

		filename := "org.txt"
		sourceType := "manual"
		content := "Alice Johnson is the CEO. Bob Smith reports to Alice."
		doc, _, err := docsSvc.Create(ctx, documents.CreateParams{
			ProjectID:  projectID,
			Filename:   &filename,
			Content:    &content,
			SourceType: &sourceType,
		})
		require.NoError(t, err)
		return projectID, uuid.MustParse(projectID), *doc
	}

	// finalizeResponse decodes the adapter's unexported response type through its
	// JSON form, which is also what the MCP tool serialises back to the agent.
	finalizeResponse := func(t *testing.T, resp any) (string, string) {
		t.Helper()
		raw, err := json.Marshal(resp)
		require.NoError(t, err)
		var out struct {
			SchemaID string `json:"schema_id"`
			Message  string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(raw, &out))
		return out.SchemaID, out.Message
	}

	orgChartTypes := []map[string]any{
		{"type_name": "Person", "description": "An employee", "frequency": float64(6)},
		{"type_name": "Department", "description": "A business unit", "frequency": float64(2)},
	}

	t.Run("a document with no job creates a pre-completed stub job", func(t *testing.T) {
		projectID, projectUUID, doc := newProject(t)

		resp, err := djSvc.FinalizeDiscoveryFromMCP(authCtx, mcp.DiscoveryFinalizeRequest{
			DocumentID:    doc.ID,
			ProjectID:     projectID,
			Mode:          "create",
			PackName:      "OrgChart",
			IncludedTypes: orgChartTypes,
			IncludedRels: []map[string]any{
				{"source_type": "Person", "target_type": "Department", "relation_type": "works_in"},
			},
		})
		require.NoError(t, err)

		schemaIDStr, message := finalizeResponse(t, resp)
		require.NotEmpty(t, schemaIDStr, "finalize must return the schema it installed")
		require.Contains(t, message, "OrgChart")
		schemaID, err := uuid.Parse(schemaIDStr)
		require.NoError(t, err)

		// The pack built from the agent-supplied types is what got installed.
		pack, err := djRepo.GetMemorySchema(ctx, schemaID)
		require.NoError(t, err)
		require.Contains(t, pack.ObjectTypeSchemas, "Person")
		require.Contains(t, pack.ObjectTypeSchemas, "Department")

		// Exactly one job exists — the stub — so no background discovery run was
		// started behind the agent's back.
		jobs, err := djRepo.ListByProject(ctx, projectUUID, 20)
		require.NoError(t, err)
		require.Len(t, jobs, 1, "finalize must not start a discovery run")

		stub := jobs[0]
		require.Equal(t, discoveryjobs.StatusCompleted, stub.Status)
		require.Contains(t, stub.Progress, "message")
		require.Equal(t, "stub job created by MCP finalize", stub.Progress["message"])
		// The job is linked to the pack, which is the whole reason it exists.
		require.NotNil(t, stub.SchemaID)
		require.Equal(t, schemaID, *stub.SchemaID)
		// ...and it carries the document the agent finalized, so the run's scope
		// is readable afterwards.
		configJSON, err := json.Marshal(stub.Config)
		require.NoError(t, err)
		require.JSONEq(t, `{"document_ids":["`+doc.ID+`"]}`, string(configJSON))
	})

	t.Run("an explicit job id reuses the job instead of creating one", func(t *testing.T) {
		projectID, projectUUID, doc := newProject(t)

		job := &discoveryjobs.DiscoveryJob{
			ID:        uuid.New(),
			ProjectID: projectUUID,
			Status:    discoveryjobs.StatusCompleted,
			KBPurpose: "HR org chart",
			Progress:  discoveryjobs.JSONMap{"message": "completed"},
			Config:    discoveryjobs.JSONMap{"document_ids": []string{doc.ID}},
			DiscoveredTypes: discoveryjobs.JSONArray{
				map[string]any{"type_name": "Person"},
				map[string]any{"type_name": "Department"},
			},
		}
		require.NoError(t, djRepo.Create(ctx, job))

		resp, err := djSvc.FinalizeDiscoveryFromMCP(authCtx, mcp.DiscoveryFinalizeRequest{
			JobID:         job.ID.String(),
			DocumentID:    doc.ID,
			ProjectID:     projectID,
			Mode:          "create",
			PackName:      "OrgChart2",
			IncludedTypes: orgChartTypes,
		})
		require.NoError(t, err)

		schemaIDStr, _ := finalizeResponse(t, resp)
		schemaID, err := uuid.Parse(schemaIDStr)
		require.NoError(t, err)

		jobs, err := djRepo.ListByProject(ctx, projectUUID, 20)
		require.NoError(t, err)
		require.Len(t, jobs, 1, "a supplied job id must not spawn a second job")
		require.NotNil(t, jobs[0].SchemaID)
		require.Equal(t, schemaID, *jobs[0].SchemaID)
	})

	t.Run("neither a job nor a document is a bad request", func(t *testing.T) {
		projectID, projectUUID, _ := newProject(t)

		_, err := djSvc.FinalizeDiscoveryFromMCP(authCtx, mcp.DiscoveryFinalizeRequest{
			ProjectID: projectID,
			Mode:      "create",
			PackName:  "NoScope",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "either job_id or document_id is required")

		jobs, listErr := djRepo.ListByProject(ctx, projectUUID, 20)
		require.NoError(t, listErr)
		require.Empty(t, jobs, "a rejected request must not leave a stub job behind")
	})

	t.Run("an unparseable project id is a bad request", func(t *testing.T) {
		_, _, doc := newProject(t)

		_, err := djSvc.FinalizeDiscoveryFromMCP(authCtx, mcp.DiscoveryFinalizeRequest{
			DocumentID: doc.ID,
			ProjectID:  "not-a-uuid",
			Mode:       "create",
			PackName:   "BadProject",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid project_id")
	})
}
