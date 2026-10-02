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

	invite, outcome, err := svc.Resend(ctx, inviteID)
	if err != nil {
		t.Fatalf("Resend: %v", err)
	}
	if outcome != ResendOutcomeSent {
		t.Fatalf("Resend outcome = %q, want %q", outcome, ResendOutcomeSent)
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
// or client retry) does not enqueue a second invite email job — and reports the
// no-op outcome so the caller/UI can tell it apart from a real send (issue #1327).
func TestResendWithinGuardWindowDoesNotEnqueueDuplicate(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

	_, outcome, err := svc.Resend(ctx, inviteID)
	if err != nil {
		t.Fatalf("first Resend: %v", err)
	}
	if outcome != ResendOutcomeSent {
		t.Fatalf("first Resend outcome = %q, want %q", outcome, ResendOutcomeSent)
	}
	if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
		t.Fatalf("after first Resend job count = %d, want 1", got)
	}

	// Second resend within the guard window must be a no-op that says so.
	_, outcome, err = svc.Resend(ctx, inviteID)
	if err != nil {
		t.Fatalf("second Resend: %v", err)
	}
	if outcome != ResendOutcomeNoOp {
		t.Fatalf("second Resend outcome = %q, want %q (caller cannot tell no-op from a real send)", outcome, ResendOutcomeNoOp)
	}
	if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
		t.Fatalf("after second Resend within guard window job count = %d, want 1 (duplicate email enqueued)", got)
	}
}

// TestResendAllowsResendWhenRecentJobFailedWithinWindow is the bounce-recovery
// proof for issue #1327: when the most recent invite-scoped job is a terminal
// failure (job failed/dead_letter, or a bounce/complaint/failed delivery
// status), a resend INSIDE the 60s guard window must still enqueue a fresh job
// and report ResendOutcomeSent. The window must not lock an admin out of
// recovering a bounced invitation.
func TestResendAllowsResendWhenRecentJobFailedWithinWindow(t *testing.T) {
	failures := []struct {
		name           string
		status         string
		deliveryStatus any
	}{
		{name: "delivery bounced", status: "sent", deliveryStatus: "bounced"},
		{name: "delivery soft_bounced", status: "sent", deliveryStatus: "soft_bounced"},
		{name: "delivery complained", status: "sent", deliveryStatus: "complained"},
		{name: "delivery failed", status: "sent", deliveryStatus: "failed"},
		{name: "job dead_letter", status: "dead_letter", deliveryStatus: nil},
		{name: "job failed", status: "failed", deliveryStatus: nil},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			svc, testDB := newResendService(t)
			defer testDB.Close()
			ctx := context.Background()
			db := testDB.GetDB()

			orgID, projectID := seedOrgAndProject(t, db)
			inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

			// A recent (inside the 60s window) job that already failed.
			if tc.deliveryStatus != nil {
				mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, delivery_status, delivery_status_at, created_at)
					VALUES ('project-invitation', 'invitee@example.com', 'Hi', ?, 'invite', ?, ?, now(), now())`,
					tc.status, inviteID, tc.deliveryStatus)
			} else {
				mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, created_at)
					VALUES ('project-invitation', 'invitee@example.com', 'Hi', ?, 'invite', ?, now())`,
					tc.status, inviteID)
			}

			_, outcome, err := svc.Resend(ctx, inviteID)
			if err != nil {
				t.Fatalf("Resend after %s: %v", tc.name, err)
			}
			if outcome != ResendOutcomeSent {
				t.Fatalf("Resend after %s outcome = %q, want %q", tc.name, outcome, ResendOutcomeSent)
			}
			if got := countInviteEmailJobs(t, db, inviteID); got != 2 {
				t.Fatalf("Resend after %s job count = %d, want 2 (bounce recovery was blocked)", tc.name, got)
			}
		})
	}
}

// TestResendStillBlocksWhenRecentJobLiveWithinWindow proves the guard window
// still prevents accidental duplicate sends for the success case: a recent job
// that is pending/processing, or sent with no failure delivery status (or a
// delivered/opened/clicked one), must suppress the resend and report
// ResendOutcomeNoOp.
func TestResendStillBlocksWhenRecentJobLiveWithinWindow(t *testing.T) {
	live := []struct {
		name           string
		status         string
		deliveryStatus any
	}{
		{name: "pending", status: "pending", deliveryStatus: nil},
		{name: "processing", status: "processing", deliveryStatus: nil},
		{name: "sent no event", status: "sent", deliveryStatus: nil},
		{name: "delivered", status: "sent", deliveryStatus: "delivered"},
		{name: "opened", status: "sent", deliveryStatus: "opened"},
		{name: "clicked", status: "sent", deliveryStatus: "clicked"},
	}

	for _, tc := range live {
		t.Run(tc.name, func(t *testing.T) {
			svc, testDB := newResendService(t)
			defer testDB.Close()
			ctx := context.Background()
			db := testDB.GetDB()

			orgID, projectID := seedOrgAndProject(t, db)
			inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

			if tc.deliveryStatus != nil {
				mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, delivery_status, delivery_status_at, created_at)
					VALUES ('project-invitation', 'invitee@example.com', 'Hi', ?, 'invite', ?, ?, now(), now())`,
					tc.status, inviteID, tc.deliveryStatus)
			} else {
				mustExecResend(t, db, `INSERT INTO kb.email_jobs (template_name, to_email, subject, status, source_type, source_id, created_at)
					VALUES ('project-invitation', 'invitee@example.com', 'Hi', ?, 'invite', ?, now())`,
					tc.status, inviteID)
			}

			_, outcome, err := svc.Resend(ctx, inviteID)
			if err != nil {
				t.Fatalf("Resend with live %s job: %v", tc.name, err)
			}
			if outcome != ResendOutcomeNoOp {
				t.Fatalf("Resend with live %s job outcome = %q, want %q", tc.name, outcome, ResendOutcomeNoOp)
			}
			if got := countInviteEmailJobs(t, db, inviteID); got != 1 {
				t.Fatalf("Resend with live %s job count = %d, want 1 (duplicate enqueued)", tc.name, got)
			}
		})
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

	if _, outcome, err := svc.Resend(ctx, inviteID); err != nil {
		t.Fatalf("Resend past guard window: %v", err)
	} else if outcome != ResendOutcomeSent {
		t.Fatalf("Resend past guard window outcome = %q, want %q", outcome, ResendOutcomeSent)
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

	if _, outcome, err := svc.Resend(ctx, inviteA); err != nil {
		t.Fatalf("Resend A: %v", err)
	} else if outcome != ResendOutcomeSent {
		t.Fatalf("Resend A outcome = %q, want %q", outcome, ResendOutcomeSent)
	}
	// B is a different invite and must not be blocked by A's recent job.
	if _, outcome, err := svc.Resend(ctx, inviteB); err != nil {
		t.Fatalf("Resend B: %v", err)
	} else if outcome != ResendOutcomeSent {
		t.Fatalf("Resend B outcome = %q, want %q", outcome, ResendOutcomeSent)
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

		// outcomes records what each racing caller was told, so the test also
		// proves the truthfulness contract under concurrency: exactly one caller
		// is told "sent" and every other is told "noop" (#1327).
		outcomes := make([]ResendOutcome, concurrent)

		eg, egCtx := errgroup.WithContext(ctx)
		for i := 0; i < concurrent; i++ {
			eg.Go(func() error {
				ready.Done()
				<-start // barrier: every goroutine races from the same instant
				_, outcome, err := svc.Resend(egCtx, inviteID)
				outcomes[i] = outcome
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

		sent, noop := 0, 0
		for _, outcome := range outcomes {
			switch outcome {
			case ResendOutcomeSent:
				sent++
			case ResendOutcomeNoOp:
				noop++
			}
		}
		if sent != 1 || noop != concurrent-1 {
			t.Fatalf("round %d: outcomes sent=%d noop=%d, want sent=1 noop=%d", round, sent, noop, concurrent-1)
		}
	}
}

// TestResendEnqueueFailureIsNonFatalAndPersistsExpiry proves the documented
// enqueue-failure contract survives the atomic-guard rewrite: when the
// invite-email INSERT fails inside Resend's transaction, the request still
// SUCCEEDS and the expires_at extension is still persisted.
//
// The failure is forced at the database with a BEFORE INSERT trigger that always
// raises on kb.email_jobs, which is exactly the reviewer's method: without a
// savepoint around the enqueue, the raised error aborts the surrounding
// transaction, the subsequent COMMIT fails ("commit unexpectedly resulted in
// rollback"), and the expires_at extension rolls back with it.
func TestResendEnqueueFailureIsNonFatalAndPersistsExpiry(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteID := seedPendingInvite(t, db, orgID, projectID, "invitee@example.com")

	// Force every INSERT into kb.email_jobs to fail at the database, simulating
	// a DB-side enqueue failure (constraint, trigger, write outage, ...).
	mustExecResend(t, db, `CREATE OR REPLACE FUNCTION resend_fail_email_job_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced email job insert failure (test)'; END; $$`)
	mustExecResend(t, db, `CREATE TRIGGER resend_fail_email_job_insert
		BEFORE INSERT ON kb.email_jobs FOR EACH ROW EXECUTE FUNCTION resend_fail_email_job_insert()`)

	resendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	invite, outcome, err := svc.Resend(resendCtx, inviteID)
	if err != nil {
		t.Fatalf("Resend must remain non-fatal when the enqueue fails, got error: %v", err)
	}
	if outcome != ResendOutcomeEnqueueFailed {
		t.Fatalf("Resend outcome with failed enqueue = %q, want %q (caller would wrongly believe an email went out)", outcome, ResendOutcomeEnqueueFailed)
	}

	// The returned invite reflects the expiry extension.
	if invite.ExpiresAt == nil || invite.ExpiresAt.Before(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("returned expires_at = %v, want ~now+7d", invite.ExpiresAt)
	}

	// ...and the extension is PERSISTED, not rolled back together with the
	// failed enqueue.
	var persisted time.Time
	if err := db.NewRaw(`SELECT expires_at FROM kb.invites WHERE id = ?`, inviteID).Scan(ctx, &persisted); err != nil {
		t.Fatalf("read persisted expires_at: %v", err)
	}
	lower := time.Now().Add(6*24*time.Hour + 23*time.Hour)
	upper := time.Now().Add(7*24*time.Hour + time.Hour)
	if persisted.Before(lower) || persisted.After(upper) {
		t.Fatalf("persisted expires_at = %v, want ~now+7d (extension was rolled back)", persisted)
	}

	// The failed enqueue left no job behind.
	if got := countInviteEmailJobs(t, db, inviteID); got != 0 {
		t.Fatalf("job count after failed enqueue = %d, want 0", got)
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
		_, _, err := svc.Resend(ctx, id)
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
