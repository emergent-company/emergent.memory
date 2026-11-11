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
