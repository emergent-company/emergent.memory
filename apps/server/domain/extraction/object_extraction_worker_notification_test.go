package extraction

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// reviewNotificationHarness wires a real branch/doc/notification service stack
// over the throwaway test database and returns a worker plus helpers to seed a
// document-sourced extraction job with a staging branch in the given state.
type reviewNotificationHarness struct {
	db  bun.IDB
	w   *ObjectExtractionWorker
	ctx context.Context
}

func newReviewNotificationHarness(t *testing.T) *reviewNotificationHarness {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "extraction_review_notification")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()
	log := quietLogger()

	w := &ObjectExtractionWorker{
		branchService:    branches.NewService(branches.NewStore(db)),
		docService:       documents.NewService(documents.NewRepository(db, log), log),
		notificationsSvc: notifications.NewService(notifications.NewRepository(db, log), nil, log),
		log:              log,
	}
	return &reviewNotificationHarness{db: db, w: w, ctx: ctx}
}

// seedProject creates an org, a project, one project member, and a source
// document, returning the project ID and document ID.
func (h *reviewNotificationHarness) seedProject(t *testing.T) (projectID, documentID string) {
	t.Helper()
	orgID := uuid.NewString()
	projectID = uuid.NewString()
	documentID = uuid.NewString()
	memberID := uuid.NewString()

	_, err := h.db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Review Org', now(), now())`, orgID).Exec(h.ctx)
	require.NoError(t, err)
	_, err = h.db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Review Project', ?, now(), now())`, projectID, orgID).Exec(h.ctx)
	require.NoError(t, err)
	_, err = h.db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, first_name, created_at, updated_at)
		VALUES (?, ?, 'Reviewer', now(), now())`, memberID, "review-member-"+memberID).Exec(h.ctx)
	require.NoError(t, err)
	_, err = h.db.NewRaw(`INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, 'member', now())`, projectID, memberID).Exec(h.ctx)
	require.NoError(t, err)
	_, err = h.db.NewRaw(`INSERT INTO kb.documents (id, project_id, filename, created_at, updated_at)
		VALUES (?, ?, 'quarterly-report.pdf', now(), now())`, documentID, projectID).Exec(h.ctx)
	require.NoError(t, err)

	return projectID, documentID
}

// seedBranch inserts a staging branch with the given merged state (false = open).
func (h *reviewNotificationHarness) seedBranch(t *testing.T, projectID, branchID string, merged bool) {
	t.Helper()
	mergedExpr := "NULL"
	if merged {
		mergedExpr = "now()"
	}
	_, err := h.db.NewRaw(`INSERT INTO kb.branches (id, project_id, name, created_at, merged_at)
		VALUES (?, ?, 'extraction/staging', now(), `+mergedExpr+`)`, branchID, projectID).Exec(h.ctx)
	require.NoError(t, err)
}

// notificationCount returns the number of notifications with the given group key.
func (h *reviewNotificationHarness) notificationCount(t *testing.T, groupKey string) int {
	t.Helper()
	var n int
	require.NoError(t, h.db.NewRaw(`SELECT count(*) FROM kb.notifications WHERE group_key = ?`, groupKey).Scan(h.ctx, &n))
	return n
}

func (h *reviewNotificationHarness) docJob(t *testing.T, projectID, documentID string) *ObjectExtractionJob {
	t.Helper()
	docType := string(SourceTypeDocument)
	return &ObjectExtractionJob{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		DocumentID: &documentID,
		SourceType: &docType,
		SourceID:   &documentID,
		CreatedAt:  time.Now().UTC(),
	}
}

// TestEmitReviewReadyNotificationMergedBranchSkips proves a staging branch that
// is actually merged does not produce a review-ready notification, regardless of
// whether auto-merge is enabled.
func TestEmitReviewReadyNotificationMergedBranchSkips(t *testing.T) {
	h := newReviewNotificationHarness(t)
	projectID, documentID := h.seedProject(t)

	job := h.docJob(t, projectID, documentID)
	branchID := uuid.NewString()
	h.seedBranch(t, projectID, branchID, true)

	result := &ObjectExtractionResults{ObjectsCreated: 3, RelationshipsCreated: 2}
	bid := uuid.MustParse(branchID)
	h.w.emitReviewReadyNotification(h.ctx, job, &bid, result)

	require.Equal(t, 0, h.notificationCount(t, "extraction.review:"+job.ID))
}

// TestEmitReviewReadyNotificationOpenBranchNotifies proves an open staging
// branch produces one review-ready notification with the correct action URL,
// label, group key, and payload.
func TestEmitReviewReadyNotificationOpenBranchNotifies(t *testing.T) {
	h := newReviewNotificationHarness(t)
	projectID, documentID := h.seedProject(t)

	job := h.docJob(t, projectID, documentID)
	branchID := uuid.NewString()
	h.seedBranch(t, projectID, branchID, false)

	result := &ObjectExtractionResults{ObjectsCreated: 3, RelationshipsCreated: 2}
	bid := uuid.MustParse(branchID)
	h.w.emitReviewReadyNotification(h.ctx, job, &bid, result)

	groupKey := "extraction.review:" + job.ID
	require.Equal(t, 1, h.notificationCount(t, groupKey))

	var n notifications.Notification
	require.NoError(t, h.db.NewSelect().Model(&n).Where("group_key = ?", groupKey).Scan(h.ctx))

	require.NotNil(t, n.EventKey)
	require.Equal(t, "extraction.review_ready", *n.EventKey)
	require.Equal(t, branchID, *n.RelatedResourceID)
	require.Equal(t, "graph_branch", *n.RelatedResourceType)

	require.NotNil(t, n.ActionURL)
	require.Equal(t, "/documents/"+documentID+"/extractions", *n.ActionURL)
	require.NotNil(t, n.ActionLabel)
	require.Equal(t, "Review merge", *n.ActionLabel)

	// Message prefers the source document filename.
	require.Contains(t, n.Message, "quarterly-report.pdf")
	require.Contains(t, n.Message, "3 objects, 2 relationships")

	var details map[string]any
	require.NoError(t, json.Unmarshal(n.Details, &details))
	require.Equal(t, job.ID, details["job_id"])
	require.Equal(t, branchID, details["branch_id"])
	require.Equal(t, float64(3), details["objects"])
	require.Equal(t, float64(2), details["relationships"])
	require.Equal(t, float64(3), details["objects_created"])
	require.Equal(t, float64(2), details["relationships_created"])
	require.Equal(t, documentID, details["document_id"])
	require.Equal(t, float64(1), details["document_version"])
}

// TestEmitReviewReadyNotificationNotFoundBranchNotifies proves a missing branch
// (e.g. already cleaned up) still notifies: the reviewer needs an alert rather
// than a silent skip.
func TestEmitReviewReadyNotificationNotFoundBranchNotifies(t *testing.T) {
	h := newReviewNotificationHarness(t)
	projectID, documentID := h.seedProject(t)

	job := h.docJob(t, projectID, documentID)
	branchID := uuid.NewString() // never inserted

	result := &ObjectExtractionResults{ObjectsCreated: 1, RelationshipsCreated: 0}
	bid := uuid.MustParse(branchID)
	h.w.emitReviewReadyNotification(h.ctx, job, &bid, result)

	require.Equal(t, 1, h.notificationCount(t, "extraction.review:"+job.ID))
}
