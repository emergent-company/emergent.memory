package extraction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddingStatsByProject_ScopesToProject proves the project-scoped queue
// statistics count only the addressed project's jobs, for both the graph object
// and graph relationship queues. A user with an empty project must see zeroes
// even while other projects have pending work.
func TestEmbeddingStatsByProject_ScopesToProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)

	projectA := seedEmbeddingProject(t, ctx, db)
	projectB := seedEmbeddingProject(t, ctx, db)

	// Object jobs: one pending in A, two pending in B.
	seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectA, "a-object"))
	seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectB, "b-object-1"))
	seedPendingEmbeddingJob(t, ctx, db, seedEmbeddingObject(t, ctx, db, projectB, "b-object-2"))

	// Relationship job: one pending in A only.
	seedPendingRelJob(t, ctx, db, seedRelPair(t, ctx, db, projectA, "a-src", "a-dst"))

	objectJobs := NewGraphEmbeddingJobsService(db, quietLogger(), DefaultGraphEmbeddingConfig())
	relJobs := NewGraphRelationshipEmbeddingJobsService(db, quietLogger(), DefaultGraphEmbeddingConfig())

	objA, err := objectJobs.StatsByProject(ctx, projectA)
	require.NoError(t, err)
	objB, err := objectJobs.StatsByProject(ctx, projectB)
	require.NoError(t, err)
	relA, err := relJobs.StatsByProject(ctx, projectA)
	require.NoError(t, err)
	relB, err := relJobs.StatsByProject(ctx, projectB)
	require.NoError(t, err)

	assert.EqualValues(t, 1, objA.Pending, "project A object pendings")
	assert.EqualValues(t, 2, objB.Pending, "project B object pendings")
	assert.EqualValues(t, 1, relA.Pending, "project A relationship pendings")
	assert.EqualValues(t, 0, relB.Pending, "project B relationship pendings (must not see A's job)")
}
