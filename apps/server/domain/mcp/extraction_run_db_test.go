package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestAgentExtractionWriteDefaults pins the write-path contract: with an
// extraction run installed, an omitted branch resolves to the job's staging
// branch, a named branch outside the job is refused instead of auto-created,
// and other jobs keep the existing "omitted means main" behaviour.
func TestAgentExtractionWriteDefaults(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	branchID := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, 'extraction/doc/job', 'extraction')`,
		branchID, projectID)
	require.NoError(t, err)

	svc := &Service{db: db}

	// No extraction run: omitted branch stays main (nil).
	branch, err := svc.resolveOrCreateBranchID(context.Background(), projectID, "")
	require.NoError(t, err)
	assert.Nil(t, branch)

	scope, err := branchscope.New(branchscope.Config{
		Unmerged:    true,
		Origins:     []string{"extraction"},
		Branches:    []string{"extraction/doc/job"},
		MaxBranches: 1,
	})
	require.NoError(t, err)

	ctx := ContextWithExtractionRun(context.Background(), ExtractionRunConfig{
		JobID:     uuid.NewString(),
		BranchRef: "extraction/doc/job",
	})
	ctx = ContextWithFixedBranchScope(ctx, scope)

	got, err := svc.resolveOrCreateBranchID(ctx, projectID, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, branchID, *got)

	got, err = svc.resolveOrCreateBranchID(ctx, projectID, "extraction/doc/job")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, branchID, *got)

	_, err = svc.resolveOrCreateBranchID(ctx, projectID, "extraction/doc/elsewhere")
	require.Error(t, err, "a branch outside the extraction job must be refused")
	assert.Contains(t, err.Error(), "outside this extraction job's scope")
}

// TestExtractionRunEntityCreateWritesJobBranch proves the tool path, not just
// the branch resolver: an omitted branch lands the entity on the job's staging
// branch with the job-owned status and provenance overwriting model values, and
// a branch outside the job is refused.
func TestExtractionRunEntityCreateWritesJobBranch(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)

	branchID := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, 'extraction/doc/job', 'extraction')`,
		branchID, projectID)
	require.NoError(t, err)

	scope, err := branchscope.New(branchscope.Config{
		Unmerged:    true,
		Origins:     []string{"extraction"},
		Branches:    []string{"extraction/doc/job"},
		MaxBranches: 1,
	})
	require.NoError(t, err)
	jobID := uuid.NewString()

	ctx := ContextWithExtractionRun(context.Background(), ExtractionRunConfig{
		JobID:      jobID,
		BranchRef:  "extraction/doc/job",
		Status:     "suggested",
		Provenance: map[string]any{"_extraction_job_id": jobID, "_source_id": "44444444-4444-4444-4444-444444444444"},
	})
	ctx = ContextWithFixedBranchScope(ctx, scope)

	args := map[string]any{"entities": []any{map[string]any{
		"type": "Person",
		"name": "Monica",
		"properties": map[string]any{
			"status":             "verified",   // model-supplied: must be overwritten
			"_extraction_job_id": "forged-job", // model-supplied: must be overwritten
		},
	}}}
	if _, err := svc.executeBatchCreateEntities(ctx, projectID, args); err != nil {
		t.Fatalf("entity-create: %v", err)
	}

	var status, gotJob, gotSource string
	require.NoError(t, db.NewRaw(`SELECT status, properties->>'_extraction_job_id', properties->>'_source_id'
		FROM kb.graph_objects WHERE project_id = ? AND branch_id = ? AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, projectID, branchID).Scan(context.Background(), &status, &gotJob, &gotSource))
	assert.Equal(t, "suggested", status, "the job's review status must overwrite the model's")
	assert.Equal(t, jobID, gotJob, "the job's provenance must overwrite the model's")
	assert.Equal(t, "44444444-4444-4444-4444-444444444444", gotSource)

	var mainRows int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL AND deleted_at IS NULL`, projectID).Scan(context.Background(), &mainRows))
	assert.Zero(t, mainRows, "an agent-run extraction must not write to main")

	outside := map[string]any{
		"branch":   "extraction/doc/elsewhere",
		"entities": []any{map[string]any{"type": "Person", "name": "Ross"}},
	}
	_, err = svc.executeBatchCreateEntities(ctx, projectID, outside)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside this extraction job's scope")
}
