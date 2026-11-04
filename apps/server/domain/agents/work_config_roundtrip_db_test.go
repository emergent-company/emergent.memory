package agents

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestUpdateDefinition_WorkConfigRoundTrip drives h.UpdateDefinition against a
// real repository and asserts the full workConfig survives the write/read
// cycle, field-for-field. It guards the gateway's Work settings form: every
// field group (status, requiresReview, failureLimit, revisionLimit,
// retryPolicy, workContract, triggerStatuses) must round-trip through the
// JSONB column without the handler dropping or reshaping any part of it.
func TestUpdateDefinition_WorkConfigRoundTrip(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_work_config_roundtrip")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "work-agent", string(VisibilityProject))

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	body := `{
		"workConfig": {
			"status": {
				"ready": "todo",
				"inProgress": "doing",
				"review": "in_review",
				"revision": "rework",
				"blocked": "waiting",
				"done": "shipped"
			},
			"requiresReview": true,
			"failureLimit": 5,
			"revisionLimit": 7,
			"retryPolicy": {
				"maxAttempts": 4,
				"initialIntervalMs": 1500,
				"backoffCoefficient": 2.5,
				"maxIntervalMs": 60000
			},
			"workContract": {
				"requireArtifacts": true,
				"requiredDeliverableTypes": ["kb.documents", "kb.chunks"]
			},
			"triggerStatuses": ["todo", "rework"]
		}
	}`

	c := newAgentDefWriteContext(http.MethodPatch, "/api/projects/"+projectID+"/agent-definitions/"+defID, projectID,
		[]string{"id"}, []string{defID}, body)
	require.NoError(t, h.UpdateDefinition(c))

	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)

	wc := def.WorkConfig
	require.Equal(t, "todo", wc.Status.Ready)
	require.Equal(t, "doing", wc.Status.InProgress)
	require.Equal(t, "in_review", wc.Status.Review)
	require.Equal(t, "rework", wc.Status.Revision)
	require.Equal(t, "waiting", wc.Status.Blocked)
	require.Equal(t, "shipped", wc.Status.Done)
	require.True(t, wc.RequiresReview)
	require.Equal(t, 5, wc.FailureLimit)
	require.Equal(t, 7, wc.RevisionLimit)
	require.Equal(t, 4, wc.RetryPolicy.MaxAttempts)
	require.Equal(t, 1500, wc.RetryPolicy.InitialIntervalMS)
	require.Equal(t, 2.5, wc.RetryPolicy.BackoffCoefficient)
	require.Equal(t, 60000, wc.RetryPolicy.MaxIntervalMS)
	require.True(t, wc.WorkContract.RequireArtifacts)
	require.Equal(t, []string{"kb.documents", "kb.chunks"}, wc.WorkContract.RequiredDeliverableTypes)
	require.Equal(t, []string{"todo", "rework"}, wc.TriggerStatuses)

	// The response DTO must also carry the round-tripped config so the gateway
	// renders the saved values back into the form on the same request.
	dto := h.toDefinitionDTO(ctx, def)
	require.Equal(t, wc, dto.WorkConfig)
}
