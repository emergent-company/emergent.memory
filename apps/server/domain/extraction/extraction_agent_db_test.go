package extraction

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	domainagents "github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/testdb"
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

// setupExtractionAgentTest opens a throwaway DB, seeds an org/project/definition,
// and returns the pieces the extraction-shell tests need. The definition is
// named "Extractor" so the shell's derived name ("extraction-agent:Extractor")
// can never accidentally collide with a definition name in the resolver's
// exact-name step.
func setupExtractionAgentTest(t *testing.T) (context.Context, bun.IDB, *domainagents.Repository, string, *domainagents.AgentDefinition) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "extraction_runtime_agent")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name) VALUES (?, 'extraction-test-org')`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, 'extraction-test-project')`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	defID := uuid.NewString()
	_, err = db.NewRaw(`INSERT INTO kb.agent_definitions (id, project_id, name) VALUES (?, ?, 'Extractor')`, defID, projectID).Exec(ctx)
	require.NoError(t, err)

	def := &domainagents.AgentDefinition{ID: defID, Name: "Extractor", ProjectID: projectID}
	return ctx, db, domainagents.NewRepository(db), projectID, def
}

// newExtractionRuntimeWorker builds a worker wired only to the jobs service —
// everything ensureExtractionRuntimeAgent touches.
func newExtractionRuntimeWorker(db bun.IDB) *ObjectExtractionWorker {
	return &ObjectExtractionWorker{
		jobsService: NewObjectExtractionJobsService(db, quietLogger(), nil),
	}
}

// shellRow is the subset of the kb.agents shell row the assertions pin.
func shellRow(t *testing.T, ctx context.Context, db bun.IDB, projectID, defID string) (id string, link *string, enabled bool, triggerType string, cron string) {
	t.Helper()
	err := db.NewRaw(
		`SELECT id::text, agent_definition_id, enabled, trigger_type, cron_schedule
		 FROM kb.agents WHERE project_id = ? AND strategy_type = ?`,
		projectID, "extraction-agent:"+defID).
		Scan(ctx, &id, &link, &enabled, &triggerType, &cron)
	require.NoError(t, err)
	return id, link, enabled, triggerType, cron
}

// TestEnsureExtractionRuntimeAgent_CreatesUnlinkedShell pins the round-4
// invariant: the shell is materialised with NO definition link and its identity
// carried in the strategy_type marker, so a future refactor cannot silently
// re-add the FK and regress the extraction shell back into a definition owner.
func TestEnsureExtractionRuntimeAgent_CreatesUnlinkedShell(t *testing.T) {
	ctx, db, _, projectID, def := setupExtractionAgentTest(t)
	w := newExtractionRuntimeWorker(db)

	id, err := w.ensureExtractionRuntimeAgent(ctx, projectID, def)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	gotID, link, enabled, triggerType, cron := shellRow(t, ctx, db, projectID, def.ID)
	require.Equal(t, id, gotID)
	require.Nil(t, link, "the shell must not carry a definition link")
	require.False(t, enabled, "the shell must be disabled (never scheduled)")
	require.Equal(t, "manual", triggerType)
	require.Equal(t, "0 0 * * *", cron)
}

// TestEnsureExtractionRuntimeAgent_ReusesExistingShell pins that a second call
// returns the same row and does not mint a duplicate.
func TestEnsureExtractionRuntimeAgent_ReusesExistingShell(t *testing.T) {
	ctx, db, _, projectID, def := setupExtractionAgentTest(t)
	w := newExtractionRuntimeWorker(db)

	first, err := w.ensureExtractionRuntimeAgent(ctx, projectID, def)
	require.NoError(t, err)
	second, err := w.ensureExtractionRuntimeAgent(ctx, projectID, def)
	require.NoError(t, err)
	require.Equal(t, first, second)

	var count int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.agents WHERE project_id = ? AND strategy_type = ?`,
		projectID, "extraction-agent:"+def.ID).Scan(ctx, &count))
	require.Equal(t, 1, count)
}

// TestEnsureExtractionRuntimeAgent_ConcurrentSingleRow pins the advisory-lock
// create path: N concurrent callers agree on one shell id and leave exactly one
// row.
func TestEnsureExtractionRuntimeAgent_ConcurrentSingleRow(t *testing.T) {
	ctx, db, _, projectID, def := setupExtractionAgentTest(t)
	w := newExtractionRuntimeWorker(db)

	const n = 8
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			ids[i], errs[i] = w.ensureExtractionRuntimeAgent(context.Background(), projectID, def)
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.NotEmpty(t, ids[i])
		require.Equal(t, ids[0], ids[i], "all concurrent callers must agree on the single shell id")
	}

	var count int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.agents WHERE project_id = ? AND strategy_type = ?`,
		projectID, "extraction-agent:"+def.ID).Scan(ctx, &count))
	require.Equal(t, 1, count)
}

// TestEnsureExtractionRuntimeAgent_NotResolvableAsDefinition pins that the
// shell does not resolve to its definition through any resolver step (no FK,
// no agent-def/chat-session marker, no exact-name match).
func TestEnsureExtractionRuntimeAgent_NotResolvableAsDefinition(t *testing.T) {
	ctx, db, repo, projectID, def := setupExtractionAgentTest(t)
	w := newExtractionRuntimeWorker(db)

	id, err := w.ensureExtractionRuntimeAgent(ctx, projectID, def)
	require.NoError(t, err)

	shell, err := repo.FindByID(ctx, id, &projectID)
	require.NoError(t, err)
	require.NotNil(t, shell)

	resolved, err := repo.ResolveDefinitionForAgent(ctx, shell)
	require.NoError(t, err)
	require.Nil(t, resolved, "the extraction shell must not resolve to its definition")
}

// TestEnsureExtractionRuntimeAgent_NotListedByDefinitionID pins that the shell
// is not returned by the definition-id query used by FindOrCreateRuntimeAgentForDefinition,
// so it can never be mistaken for the definition's canonical runtime row.
func TestEnsureExtractionRuntimeAgent_NotListedByDefinitionID(t *testing.T) {
	ctx, db, repo, projectID, def := setupExtractionAgentTest(t)
	w := newExtractionRuntimeWorker(db)

	_, err := w.ensureExtractionRuntimeAgent(ctx, projectID, def)
	require.NoError(t, err)

	rows, err := repo.FindAgentsByDefinitionID(ctx, projectID, def.ID)
	require.NoError(t, err)
	require.Empty(t, rows, "the extraction shell must not be returned for its definition id")
}
