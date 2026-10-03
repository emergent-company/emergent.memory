package invites

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestListByProjectDeliveryLog proves ListByProject attaches each invitation's
// full email send history — every invite-scoped kb.email_jobs row, newest first
// — with the Mailgun delivery events recorded against each send (including the
// bounce reason). It also proves the history is scoped to the project and that
// an invitation with no sends reports an empty (non-nil) log.
func TestListByProjectDeliveryLog(t *testing.T) {
	svc, testDB := newResendService(t)
	defer testDB.Close()
	ctx := context.Background()
	db := testDB.GetDB()

	orgID, projectID := seedOrgAndProject(t, db)
	inviteA := seedPendingInvite(t, db, orgID, projectID, "a@example.com")
	inviteB := seedPendingInvite(t, db, orgID, projectID, "b@example.com")

	// A second project whose invite email must not leak into projectID's log.
	otherProjectID := uuid.NewString()
	mustExecResend(t, db, `INSERT INTO kb.projects (id, organization_id, name, created_at, updated_at) VALUES (?, ?, 'Other Project', NOW(), NOW())`, otherProjectID, orgID)
	inviteOther := seedPendingInvite(t, db, orgID, otherProjectID, "other@example.com")

	jobOld := uuid.NewString()
	jobNew := uuid.NewString()
	old := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	newer := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	oldProcessed := old.Add(5 * time.Second)
	newProcessed := newer.Add(5 * time.Second)

	// Two sends. processed_at is when the send actually completed, distinct
	// from the enqueue time (created_at), so the log can report the real
	// send time rather than the queue time.
	mustExecResend(t, db, `INSERT INTO kb.email_jobs (id, template_name, to_email, subject, status, source_type, source_id, delivery_status, delivery_status_at, processed_at, created_at)
		VALUES (?, 'project-invitation', 'a@example.com', 'Invitation', 'sent', 'invite', ?, 'delivered', ?, ?, ?)`,
		jobOld, inviteA, old, oldProcessed, old)
	mustExecResend(t, db, `INSERT INTO kb.email_jobs (id, template_name, to_email, subject, status, source_type, source_id, delivery_status, delivery_status_at, processed_at, created_at)
		VALUES (?, 'project-invitation', 'a@example.com', 'Invitation', 'sent', 'invite', ?, 'bounced', ?, ?, ?)`,
		jobNew, inviteA, newer, newProcessed, newer)
	// The oldest send's two events are inserted in reverse chronological order
	// (later event first) to prove the returned order comes from created_at,
	// not insertion order or id.
	mustExecResend(t, db, `INSERT INTO kb.email_logs (email_job_id, event_type, details, created_at)
		VALUES (?, 'opened', '{}'::jsonb, ?)`, jobOld, old.Add(2*time.Minute))
	mustExecResend(t, db, `INSERT INTO kb.email_logs (email_job_id, event_type, details, created_at)
		VALUES (?, 'delivered', '{}'::jsonb, ?)`, jobOld, old)
	mustExecResend(t, db, `INSERT INTO kb.email_logs (email_job_id, event_type, details, created_at)
		VALUES (?, 'bounced', '{"reason":"mailbox full","severity":"permanent"}'::jsonb, ?)`, jobNew, newer)

	// A send for the other project's invite: present in the table, absent from
	// projectID's log.
	otherJob := uuid.NewString()
	mustExecResend(t, db, `INSERT INTO kb.email_jobs (id, template_name, to_email, subject, status, source_type, source_id, created_at)
		VALUES (?, 'project-invitation', 'other@example.com', 'Invitation', 'sent', 'invite', ?, ?)`,
		otherJob, inviteOther, newer)

	invites, err := svc.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	byID := make(map[string]SentInvite, len(invites))
	for _, inv := range invites {
		byID[inv.ID] = inv
	}

	a, ok := byID[inviteA]
	if !ok {
		t.Fatalf("invite A missing from list: %+v", invites)
	}
	if len(a.DeliveryLog) != 2 {
		t.Fatalf("invite A DeliveryLog len = %d, want 2 (one per send)", len(a.DeliveryLog))
	}
	// Newest send first (jobNew was created later).
	if a.DeliveryLog[0].JobID != jobNew {
		t.Errorf("DeliveryLog[0].JobID = %q, want newest %q", a.DeliveryLog[0].JobID, jobNew)
	}
	if a.DeliveryLog[1].JobID != jobOld {
		t.Errorf("DeliveryLog[1].JobID = %q, want oldest %q", a.DeliveryLog[1].JobID, jobOld)
	}
	// The most recent delivery state is still surfaced on the invite itself.
	if a.DeliveryStatus == nil || *a.DeliveryStatus != "bounced" {
		t.Errorf("invite A DeliveryStatus = %v, want bounced (latest)", a.DeliveryStatus)
	}
	// The bounce detail is attached to the newest send's event.
	newest := a.DeliveryLog[0]
	if newest.DeliveryStatus == nil || *newest.DeliveryStatus != "bounced" {
		t.Errorf("newest send DeliveryStatus = %v, want bounced", newest.DeliveryStatus)
	}
	if len(newest.Events) != 1 || newest.Events[0].Type != "bounced" {
		t.Fatalf("newest send events = %+v, want one bounced event", newest.Events)
	}
	if got := newest.Events[0].Detail; got != "mailbox full" {
		t.Errorf("bounce detail = %q, want %q", got, "mailbox full")
	}
	// The send's real completion time is surfaced, not just its enqueue time.
	if newest.ProcessedAt == nil || !newest.ProcessedAt.Equal(newProcessed) {
		t.Errorf("newest send ProcessedAt = %v, want %v", newest.ProcessedAt, newProcessed)
	}
	// The oldest send carries its two events oldest-first, even though the
	// later event was inserted first.
	oldest := a.DeliveryLog[1]
	if len(oldest.Events) != 2 {
		t.Fatalf("oldest send events = %+v, want two events", oldest.Events)
	}
	if oldest.Events[0].Type != "delivered" || oldest.Events[1].Type != "opened" {
		t.Errorf("oldest send events = %+v, want delivered then opened (oldest first)", oldest.Events)
	}

	// B: no sends → an empty, non-nil log (stable JSON [] rather than null).
	b, ok := byID[inviteB]
	if !ok {
		t.Fatal("invite B missing from list")
	}
	if b.DeliveryLog == nil {
		t.Error("invite B DeliveryLog must be non-nil (empty slice), got nil")
	}
	if len(b.DeliveryLog) != 0 {
		t.Errorf("invite B DeliveryLog = %+v, want empty", b.DeliveryLog)
	}

	// The other project's send must not leak into this project's log.
	for _, inv := range invites {
		for _, send := range inv.DeliveryLog {
			if send.JobID == otherJob {
				t.Errorf("other project's job %q leaked into project %q's log", otherJob, projectID)
			}
		}
	}
}
