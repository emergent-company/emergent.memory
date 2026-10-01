package invites

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"golang.org/x/sync/errgroup"

	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// newResendService builds a Service wired to an isolated test database AND a
// real email JobsService, so resend re-enqueues a real kb.email_jobs row.
func newResendService(t *testing.T) (*Service, *testdb.TestDB) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "invites_resend")
	db := testDB.GetDB()
	log := slog.Default()
	emailSvc := email.NewJobsService(db, log, &email.Config{})
	svc := NewService(db, emailSvc, &config.Config{AppURL: "http://localhost:3000"}, nil, log)
	return svc, testDB
}

// seedOrgAndProject inserts an org + project and returns their ids.
func seedOrgAndProject(t *testing.T, db bun.IDB) (orgID, projectID string) {
	t.Helper()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	mustExecResend(t, db, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'Invite Org', NOW(), NOW())`, orgID)
	mustExecResend(t, db, `INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at) VALUES (?, ?, 'Test Project', NOW(), NOW())`, projectID, orgID)
	return orgID, projectID
}

func mustExecResend(t *testing.T, db bun.IDB, query string, args ...any) {
	t.Helper()
	if _, err := db.NewRaw(query, args...).Exec(context.Background()); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

// TestResendPendingReenqueuesAndExtendsExpiry proves resending a pending invite
// keeps the SAME token, extends expires_at by 7 days, and re-enqueues a
// project-invitation job scoped to the invite.
func TestResendPendingReenqueuesAndExtendsExpiry(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)

	inviteID := uuid.NewString()
	const token = "resend-token"
	mustExecResend(t, db, `INSERT INTO kb.invites (id, organization_id, project_id, email, role, token, status, expires_at, created_at)
		VALUES (?, ?, ?, 'invitee@example.com', 'project_user', ?, 'pending', NOW() + interval '1 day', NOW())`,
		inviteID, orgID, projectID, token)

	invite, err := svc.Resend(ctx, inviteID)
	if err != nil {
		t.Fatalf("Resend: %v", err)
	}

	if invite.Token != token {
		t.Fatalf("Resend must keep the same token: got %q, want %q", invite.Token, token)
	}

	// expires_at extended to ~now + 7 days.
	lower := time.Now().Add(6*24*time.Hour + 23*time.Hour)
	upper := time.Now().Add(7*24*time.Hour + 1*time.Hour)
	if invite.ExpiresAt == nil || invite.ExpiresAt.Before(lower) || invite.ExpiresAt.After(upper) {
		t.Fatalf("Resend expires_at = %v, want ~now+7d", invite.ExpiresAt)
	}

	// A fresh project-invitation job was re-enqueued for the invite.
	var count int
	if err := db.NewRaw(
		`SELECT COUNT(*) FROM kb.email_jobs WHERE source_type = 'invite' AND source_id = ? AND template_name = 'project-invitation'`,
		inviteID,
	).Scan(ctx, &count); err != nil {
		t.Fatalf("count email jobs: %v", err)
	}
	if count == 0 {
		t.Fatal("Resend must re-enqueue a project-invitation email job")
	}
}

// countInviteEmailJobs returns the number of email jobs scoped to an invite.
func countInviteEmailJobs(t *testing.T, db bun.IDB, inviteID string) int {
	t.Helper()
	var count int
	if err := db.NewRaw(
		`SELECT COUNT(*) FROM kb.email_jobs WHERE source_type = 'invite' AND source_id = ?`,
		inviteID,
	).Scan(context.Background(), &count); err != nil {
		t.Fatalf("count invite email jobs: %v", err)
	}
	return count
}

// seedPendingInvite inserts a pending invite and returns its id.
func seedPendingInvite(t *testing.T, db bun.IDB, orgID, projectID, email string) string {
	t.Helper()
	id := uuid.NewString()
	mustExecResend(t, db, `INSERT INTO kb.invites (id, organization_id, project_id, email, role, token, status, expires_at, created_at)
		VALUES (?, ?, ?, ?, 'project_user', ?, 'pending', NOW() + interval '1 day', NOW())`,
		id, orgID, projectID, email, "tok-"+id)
	return id
}

// TestResendWithinGuardWindowDoesNotEnqueueDuplicate proves the resend guard is
// server-enforced: a second resend immediately after the first (a double-submit
// or client retry) does not enqueue a second invite email job.
func TestResendWithinGuardWindowDoesNotEnqueueDuplicate(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

	if _, err := svc.Resend(ctx, inviteID); err != nil {
		t.Fatalf("first Resend: %v", err)
	}
	if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
		t.Fatalf("after first Resend job count = %d, want 1", got)
	}

	// Second resend within the guard window must be a no-op.
	if _, err := svc.Resend(ctx, inviteID); err != nil {
		t.Fatalf("second Resend: %v", err)
	}
	if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
		t.Fatalf("after second Resend within guard window job count = %d, want 1 (duplicate email enqueued)", got)
	}
}

// TestResendAfterGuardWindowEnqueuesAgain proves the guard is a window, not a
// permanent block: once the previous invite-scoped job is older than the guard
// window, a resend enqueues a fresh job.
func TestResendAfterGuardWindowEnqueuesAgain(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

	// A prior invite job older than the guard window.
	mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, created_at)
		VALUES ('project-invitation', 'invitee@example.com', 'Hi', 'sent', 'invite', ?, now() - interval '2 minutes')`,
		inviteID)

	if _, err := svc.Resend(ctx, inviteID); err != nil {
		t.Fatalf("Resend past guard window: %v", err)
	}
	if got := countInviteEmailJobs(t, db, inviteID); got != 2 {
		t.Fatalf("after Resend past guard window job count = %d, want 2", got)
	}
}

// TestResendGuardIsPerInvite proves the guard is keyed per invitation: a recent
// resend of one invite does not block resending a different invite.
func TestResendGuardIsPerInvite(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteA := seedPendingInvite(t, db, orgID, projectID, "a@example.com")
	inviteB := seedPendingInvite(t, db, orgID, projectID, "b@example.com")

	if _, err := svc.Resend(ctx, inviteA); err != nil {
		t.Fatalf("Resend A: %v", err)
	}
	// B is a different invite and must not be blocked by A's recent job.
	if _, err := svc.Resend(ctx, inviteB); err != nil {
		t.Fatalf("Resend B: %v", err)
	}

	if got := countInviteEmailJobs(t, db, inviteA); got != 1 {
		t.Fatalf("invite A job count = %d, want 1", got)
	}
	if got := countInviteEmailJobs(t, db, inviteB); got != 1 {
		t.Fatalf("invite B job count = %d, want 1 (per-invite guard blocked another invite)", got)
	}
}

// TestResendConcurrentEnqueuesExactlyOne is the atomicity proof for the resend
// guard: N truly-concurrent Resend calls for the SAME invite must enqueue
// EXACTLY ONE email job. All goroutines are held on a barrier and released
// together, so they genuinely race the guard rather than running serially (the
// pre-fix check-then-act implementation fails this — see the PR body).
//
// Several rounds are run over fresh invites so a single lucky interleaving
// cannot mask a broken guard.
func TestResendConcurrentEnqueuesExactlyOne(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)

	const (
		rounds     = 4
		concurrent = 16
	)

	for round := 0; round < rounds; round++ {
		inviteID := seedPendingInvite(t, db, orgID, projectID, fmt.Sprintf("race-%d@example.com", round))

		start := make(chan struct{})
		var ready sync.WaitGroup
		ready.Add(concurrent)

		eg, egCtx := errgroup.WithContext(ctx)
		for i := 0; i < concurrent; i++ {
			eg.Go(func() error {
				ready.Done()
				<-start // barrier: every goroutine races from the same instant
				_, err := svc.Resend(egCtx, inviteID)
				return err
			})
		}

		ready.Wait()
		close(start)
		if err := eg.Wait(); err != nil {
			t.Fatalf("round %d: concurrent Resend: %v", round, err)
		}

		if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
			t.Fatalf("round %d: %d concurrent resends produced %d invite email job(s), want exactly 1",
				round, concurrent, got)
		}
	}
}

// TestResendNonPendingOrUnknownNotFound proves resending a non-pending invite or
// an unknown id is 404 (matching the revoke guard).
func TestResendNonPendingOrUnknownNotFound(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, _ := seedOrgAndProject(t, db)

	seed := func(id, status string) {
		mustExecResend(t, db, `INSERT INTO kb.invites (id, organization_id, email, role, token, status, created_at)
			VALUES (?, ?, 'invitee@example.com', 'project_user', ?, ?, NOW())`,
			id, orgID, "tok-"+id, status)
	}

	accepted := uuid.NewString()
	revoked := uuid.NewString()
	declined := uuid.NewString()
	seed(accepted, "accepted")
	seed(revoked, "revoked")
	seed(declined, "declined")

	for _, id := range []string{accepted, revoked, declined, uuid.NewString()} {
		_, err := svc.Resend(ctx, id)
		status, _ := apperror.ToHTTPError(err)
		if status != 404 {
			t.Fatalf("Resend(%s) status = %d, want 404 (err=%v)", id, status, err)
		}
	}
}

// TestListByProjectDeliveryStatus proves ListByProject resolves each invite's
// delivery status from its most recent invite-scoped email job in one query: an
// invite with an 'opened' job reports 'opened'; an invite with no job reports nil.
func TestListByProjectDeliveryStatus(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)

	withJob := uuid.NewString()
	withoutJob := uuid.NewString()

	mustExecResend(t, db, `INSERT INTO kb.invites (id, organization_id, project_id, email, role, token, status, created_at)
		VALUES (?, ?, ?, 'invitee@example.com', 'project_user', ?, 'pending', NOW())`,
		withJob, orgID, projectID, "tok-with")
	mustExecResend(t, db, `INSERT INTO kb.invites (id, organization_id, project_id, email, role, token, status, created_at)
		VALUES (?, ?, ?, 'invitee2@example.com', 'project_user', ?, 'pending', NOW())`,
		withoutJob, orgID, projectID, "tok-without")

	// Attach a delivery status to the first invite's job.
	mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, delivery_status, delivery_status_at, created_at)
		VALUES ('project-invitation', 'invitee@example.com', 'Hi', 'sent', 'invite', ?, 'opened', NOW(), NOW())`,
		withJob)

	invites, err := svc.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}

	byID := map[string]SentInvite{}
	for _, inv := range invites {
		byID[inv.ID] = inv
	}

	gotWith := byID[withJob]
	if gotWith.DeliveryStatus == nil || *gotWith.DeliveryStatus != "opened" {
		t.Fatalf("invite with job DeliveryStatus = %v, want 'opened'", gotWith.DeliveryStatus)
	}
	if gotWith.DeliveryStatusAt == nil {
		t.Fatal("invite with job DeliveryStatusAt must be set")
	}

	gotWithout := byID[withoutJob]
	if gotWithout.DeliveryStatus != nil {
		t.Fatalf("invite without job DeliveryStatus = %v, want nil", *gotWithout.DeliveryStatus)
	}
	if gotWithout.DeliveryStatusAt != nil {
		t.Fatalf("invite without job DeliveryStatusAt = %v, want nil", gotWithout.DeliveryStatusAt)
	}
}
