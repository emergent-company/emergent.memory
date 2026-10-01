package graph_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestApproveWorkObject_Finalizes verifies approve transitions review→done and
// writes the previously dormant reviewed_by/reviewed_at columns while clearing
// needs_review.
func TestApproveWorkObject_Finalizes(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-approve", "in_progress")

	ok, err := svc.CompleteWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "done", "review", true)
	require.NoError(t, err)
	require.True(t, ok)

	reviewer := uuid.NewString()
	ok, err = svc.ApproveWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "review", "done", reviewer)
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "done", head.Status)
	require.Equal(t, "done", statusProperty(t, ctx, db, obj.CanonicalID.String()))

	var needsReview bool
	var reviewedBy string
	var reviewedAt *time.Time
	require.NoError(t, db.NewRaw(
		`SELECT needs_review, reviewed_by::text, reviewed_at FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String(),
	).Scan(ctx, &needsReview, &reviewedBy, &reviewedAt))
	require.False(t, needsReview)
	require.Equal(t, reviewer, reviewedBy)
	require.NotNil(t, reviewedAt)
}

// TestApproveWorkObject_NotReviewRejected verifies approve does not transition a
// non-review item.
func TestApproveWorkObject_NotReviewRejected(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-approve-nr", "in_progress")

	ok, err := svc.ApproveWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "review", "done", uuid.NewString())
	require.NoError(t, err)
	require.False(t, ok)
}

// TestRequestChangesWorkObject verifies request-changes transitions review→revision
// and clears needs_review.
func TestRequestChangesWorkObject(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-req-changes", "in_progress")

	ok, err := svc.CompleteWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "in_progress", "done", "review", true)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = svc.RequestChangesWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "review", "revision")
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "revision", head.Status)

	var needsReview bool
	require.NoError(t, db.NewRaw(
		`SELECT needs_review FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String(),
	).Scan(ctx, &needsReview))
	require.False(t, needsReview)
}

// TestReassignWorkObject verifies reassign sets and clears the assignee without
// a status move.
func TestReassignWorkObject(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-reassign", "review")

	newAssignee := "reviewer"
	ok, err := svc.ReassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), &newAssignee)
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "review", head.Status) // status preserved
	require.Equal(t, "reviewer", head.Assignee)

	empty := ""
	ok, err = svc.ReassignWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), &empty)
	require.NoError(t, err)
	require.True(t, ok)

	head, err = svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "", head.Assignee)
}

// TestCancelWorkObject verifies cancel transitions any status to blocked.
func TestCancelWorkObject(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)
	obj := createAssignedWorkObject(t, ctx, svc, projectID, "k-cancel", "in_progress")

	ok, err := svc.CancelWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), "blocked")
	require.NoError(t, err)
	require.True(t, ok)

	head, err := svc.GetHeadObject(ctx, projectID.String(), obj.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, "blocked", head.Status)
}
