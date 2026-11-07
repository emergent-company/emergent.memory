package search

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func searchTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type branchFixture struct {
	branchID    uuid.UUID
	name        string
	origin      string
	originID    *uuid.UUID
	originLabel *string
	merged      bool
	createdAt   time.Time
}

// TestListUnmergedBranchesTargets covers the one query a scoped search uses: it
// must return only open branches, exclude an extraction branch whose source
// document was deleted (however the branch is linked to its job), order newest
// first, and honour an origin filter (#265, #246). Runs against a real database
// because the deleted-source exclusion is SQL, not Go.
func TestListUnmergedBranchesTargets(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "searchbranches")
	t.Cleanup(tdb.Close)

	ctx := context.Background()
	db := tdb.DB
	repo := NewRepository(db, searchTestLogger())
	projectID := uuid.New()

	// kb.projects references kb.orgs, and documents/jobs reference the project.
	orgID := uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "search-branch-org")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "search-branch-test")
	require.NoError(t, err)

	// A live extraction branch: document exists, job exists, branch points at it.
	liveDocID := uuid.New()
	liveJobID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.documents (id, project_id, filename) VALUES (?, ?, ?)`,
		liveDocID, projectID, "Invoice.pdf")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.object_extraction_jobs (id, project_id, document_id) VALUES (?, ?, ?)`,
		liveJobID, projectID, liveDocID)
	require.NoError(t, err)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	label := "Invoice.pdf"

	insertBranch := func(f branchFixture) {
		t.Helper()
		var mergedAt *time.Time
		if f.merged {
			mergedAt = &f.createdAt
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO kb.branches (id, project_id, name, origin, origin_id, origin_label, created_at, merged_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			f.branchID, projectID, f.name, f.origin, f.originID, f.originLabel, f.createdAt, mergedAt)
		require.NoError(t, err)
	}

	liveBranchID := uuid.New()
	insertBranch(branchFixture{branchID: liveBranchID, name: "extraction/live/1", origin: "extraction",
		originID: &liveJobID, originLabel: &label, createdAt: base})

	// Merged: excluded regardless of origin.
	insertBranch(branchFixture{branchID: uuid.New(), name: "extraction/merged/1", origin: "extraction",
		originID: &liveJobID, merged: true, createdAt: base.Add(time.Hour)})

	// Extraction branch whose source document was deleted: DeleteWithCascade
	// removes the extraction job, leaving origin_id dangling (there is no foreign
	// key from a branch to a job). Such a branch must not be readable.
	deletedDocID := uuid.New()
	deletedJobID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.documents (id, project_id, filename) VALUES (?, ?, ?)`,
		deletedDocID, projectID, "Deleted.pdf")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.object_extraction_jobs (id, project_id, document_id) VALUES (?, ?, ?)`,
		deletedJobID, projectID, deletedDocID)
	require.NoError(t, err)
	insertBranch(branchFixture{branchID: uuid.New(), name: "extraction/deleted-doc/1", origin: "extraction",
		originID: &deletedJobID, createdAt: base.Add(2 * time.Hour)})
	_, err = db.ExecContext(ctx, `DELETE FROM kb.object_extraction_jobs WHERE id = ?`, deletedJobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM kb.documents WHERE id = ?`, deletedDocID)
	require.NoError(t, err)

	// Extraction branch with no origin_id (legacy shape) whose only link is a job
	// that survived an older delete path with a null document_id: excluded.
	stagingBranchID := uuid.New()
	insertBranch(branchFixture{branchID: stagingBranchID, name: "extraction/staging-only/1", origin: "extraction",
		createdAt: base.Add(3 * time.Hour)})
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.object_extraction_jobs (id, project_id, document_id, staging_branch_id) VALUES (?, ?, NULL, ?)`,
		uuid.New(), projectID, stagingBranchID)
	require.NoError(t, err)

	// Extraction branch whose origin_id names no job at all: a dangling source,
	// excluded (the source was deleted and the job row went with it).
	insertBranch(branchFixture{branchID: uuid.New(), name: "extraction/dangling/1", origin: "extraction",
		originID: func() *uuid.UUID { id := uuid.New(); return &id }(), createdAt: base.Add(4 * time.Hour)})

	// Extraction branch with no job link at all: nothing says its source was
	// deleted, so it is kept.
	noLinkBranchID := uuid.New()
	insertBranch(branchFixture{branchID: noLinkBranchID, name: "extraction/no-link/1", origin: "extraction",
		createdAt: base.Add(5 * time.Hour)})

	manualBranchID := uuid.New()
	insertBranch(branchFixture{branchID: manualBranchID, name: "plan/q3", origin: "manual",
		createdAt: base.Add(6 * time.Hour)})

	agentBranchID := uuid.New()
	insertBranch(branchFixture{branchID: agentBranchID, name: "agent/scratch", origin: "agent",
		createdAt: base.Add(7 * time.Hour)})

	got, err := repo.ListUnmergedBranches(ctx, projectID, nil)
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(got))
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	assert.ElementsMatch(t,
		[]uuid.UUID{liveBranchID, noLinkBranchID, manualBranchID, agentBranchID},
		ids,
		"only open branches with a live or unknown source are readable")
	assert.NotContains(t, ids, stagingBranchID, "job-linked branch with a deleted document must be excluded")

	require.Len(t, got, 4)
	assert.Equal(t, agentBranchID, got[0].ID, "newest first")
	assert.Equal(t, manualBranchID, got[1].ID)
	assert.Equal(t, noLinkBranchID, got[2].ID)
	assert.Equal(t, liveBranchID, got[3].ID)

	// The live extraction branch keeps its provenance so a scoped result can
	// identify the source document.
	var live *BranchTarget
	for _, b := range got {
		if b.ID == liveBranchID {
			live = b
		}
	}
	require.NotNil(t, live)
	assert.Equal(t, "extraction", live.Origin)
	require.NotNil(t, live.OriginLabel)
	assert.Equal(t, "Invoice.pdf", *live.OriginLabel)

	// An origin filter is applied in SQL and excludes every other origin.
	agentOnly, err := repo.ListUnmergedBranches(ctx, projectID, []string{"agent"})
	require.NoError(t, err)
	require.Len(t, agentOnly, 1)
	assert.Equal(t, agentBranchID, agentOnly[0].ID)

	// Another project's branches never leak in.
	other, err := repo.ListUnmergedBranches(ctx, uuid.New(), nil)
	require.NoError(t, err)
	assert.Empty(t, other)
}
