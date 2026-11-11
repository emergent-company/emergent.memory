package extraction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCountAgentExtractionOutput_IsJobAndBranchScoped pins what an agent-run
// job reports: only the rows this job stamped, only on its staging branch.
// Another job's rows on the same branch, this job's rows on another branch and
// this job's rows on main must not leak into the counts a reviewer sees.
func TestCountAgentExtractionOutput_IsJobAndBranchScoped(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	jobID := uuid.NewString()
	otherJobID := uuid.NewString()
	branchID := uuid.New()
	otherBranchID := uuid.New()

	_, err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, status, created_at, updated_at)
		VALUES (?, ?, 'processing', now(), now())`, jobID, projectID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.branches (id, project_id, name, origin)
		VALUES (?, ?, 'extraction/doc/job', 'extraction')`, branchID, projectID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.branches (id, project_id, name, origin)
		VALUES (?, ?, 'extraction/doc/other', 'extraction')`, otherBranchID, projectID).Exec(ctx)
	require.NoError(t, err)

	insertObject := func(job string, branch any) string {
		id := uuid.NewString()
		_, iErr := db.NewRaw(`INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at, extraction_job_id)
			VALUES (?, ?, ?, ?, NULL, 1, 'Person', ?, 'suggested', '{}'::jsonb, '{}'::text[], now(), now(), ?)`,
			id, projectID, branch, id, "k-"+id[:8], job).Exec(ctx)
		require.NoError(t, iErr)
		return id
	}
	first := insertObject(jobID, branchID)
	insertObject(jobID, branchID)
	insertObject(otherJobID, branchID) // same branch, other job
	insertObject(jobID, otherBranchID) // this job, other branch
	insertObject(jobID, nil)           // this job, main graph (no branch)

	_, err = db.NewRaw(`INSERT INTO kb.graph_relationships
		(id, project_id, type, src_id, dst_id, properties, canonical_id, version, branch_id, created_at)
		VALUES (gen_random_uuid(), ?, 'KNOWS', ?, ?, ?::jsonb, gen_random_uuid(), 1, ?, now())`,
		projectID, first, first, `{"_extraction_job_id":"`+jobID+`"}`, branchID).Exec(ctx)
	require.NoError(t, err)

	svc := NewObjectExtractionJobsService(db, quietLogger(), nil)
	w := &ObjectExtractionWorker{jobsService: svc, log: quietLogger()}
	res, err := w.countAgentExtractionOutput(context.Background(), &ObjectExtractionJob{ID: jobID, ProjectID: projectID}, &branchID)
	require.NoError(t, err)
	assert.Equal(t, 2, res.ObjectsCreated)
	assert.Equal(t, 1, res.RelationshipsCreated)
	assert.Len(t, res.CreatedObjectIDs, 2)
	assert.Equal(t, "agent", res.DebugInfo["mode"])
}
