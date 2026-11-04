package email

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// recordingSender captures the messages the worker attempts to send so a test
// can assert on the rendered content.
type recordingSender struct {
	sends []SendOptions
}

func (s *recordingSender) Send(_ context.Context, opts SendOptions) (*SendResult, error) {
	s.sends = append(s.sends, opts)
	return &SendResult{Success: true, MessageID: "test-message-id"}, nil
}

// writeTemplate writes a Handlebars template into dir.
func writeTemplate(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".hbs"), []byte(body), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
}

// newInviteWorkerFixture wires a Worker against an isolated test database, a
// recording sender, and a template directory the test controls.
func newInviteWorkerFixture(t *testing.T, templateDir string) (*Worker, *JobsService, *recordingSender, func()) {
	t.Helper()
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "email_worker_invite")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := &Config{MaxRetries: 1, RetryDelaySec: 1, WorkerBatchSize: 10, WorkerIntervalMs: 5000}
	jobs := NewJobsService(testDB.GetDB(), log, cfg)
	sender := &recordingSender{}
	templates := NewTemplateService(templateDir, log)
	worker := NewWorker(jobs, sender, templates, cfg, log)
	return worker, jobs, sender, func() { testDB.Close() }
}

func enqueueInviteJob(t *testing.T, ctx context.Context, jobs *JobsService, tmpl string, data map[string]interface{}) *EmailJob {
	t.Helper()
	to := "invitee@example.com"
	job, err := jobs.Enqueue(ctx, EnqueueOptions{
		TemplateName: tmpl,
		ToEmail:      to,
		ToName:       &to,
		Subject:      "You've been invited",
		TemplateData: data,
		MaxAttempts:  intPtr(1),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return job
}

func intPtr(v int) *int { return &v }

func dequeueOne(t *testing.T, ctx context.Context, jobs *JobsService) *EmailJob {
	t.Helper()
	jobsBatch, err := jobs.Dequeue(ctx, 10)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(jobsBatch) != 1 {
		t.Fatalf("dequeue: got %d jobs, want 1", len(jobsBatch))
	}
	return jobsBatch[0]
}

func reloadStatus(t *testing.T, ctx context.Context, jobs *JobsService, id string) *EmailJob {
	t.Helper()
	job, err := jobs.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil {
		t.Fatalf("job %s not found", id)
	}
	return job
}

// TestProcessJob_MissingInviteTemplate_FailsWithoutSending is the regression
// test for issue #1214: an invitation job whose template cannot be found used
// to fall back to a generic body that only understood ctaUrl/message — so the
// recipient got an invitation with no accept link — and the job was still
// marked sent. The worker must instead fail the job (retry/dead-letter) and
// never deliver a CTA-less invite.
func TestProcessJob_MissingInviteTemplate_FailsWithoutSending(t *testing.T) {
	ctx := context.Background()
	worker, jobs, sender, cleanup := newInviteWorkerFixture(t, t.TempDir())
	defer cleanup()

	const acceptURL = "https://memory.example.com/invites/accept?token=abc123"
	enqueued := enqueueInviteJob(t, ctx, jobs, "project-invitation", map[string]interface{}{
		"inviterName": "Alice",
		"projectName": "Acme",
		"roleLabel":   "Member",
		"acceptUrl":   acceptURL,
	})
	job := dequeueOne(t, ctx, jobs)

	if err := worker.processJob(ctx, job); err == nil {
		t.Fatal("processJob returned nil for a missing template; want an error so the job is not marked sent")
	}

	if len(sender.sends) != 0 {
		t.Errorf("worker sent %d email(s) for a missing template; want 0", len(sender.sends))
	}

	got := reloadStatus(t, ctx, jobs, enqueued.ID)
	if got.Status == JobStatusSent {
		t.Errorf("job status = %q; want not sent (failed/dead_letter)", got.Status)
	}
	if got.LastError == nil || *got.LastError == "" {
		t.Error("job last_error is empty; want the render failure recorded")
	}
}

// TestProcessJob_InviteTemplateWithoutAcceptURL_FailsWithoutSending proves the
// stronger guarantee: even when a template renders successfully, an invitation
// job that carries no usable accept link must not be sent or marked sent.
func TestProcessJob_InviteTemplateWithoutAcceptURL_FailsWithoutSending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeTemplate(t, dir, "project-invitation", "<p>Hi {{inviterName}}, join {{projectName}}</p>")
	worker, jobs, sender, cleanup := newInviteWorkerFixture(t, dir)
	defer cleanup()

	enqueued := enqueueInviteJob(t, ctx, jobs, "project-invitation", map[string]interface{}{
		"inviterName": "Alice",
		"projectName": "Acme",
		"roleLabel":   "Member",
	})
	job := dequeueOne(t, ctx, jobs)

	if err := worker.processJob(ctx, job); err == nil {
		t.Fatal("processJob returned nil for an invite with no acceptUrl; want an error")
	}
	if len(sender.sends) != 0 {
		t.Errorf("worker sent %d email(s) for an invite with no acceptUrl; want 0", len(sender.sends))
	}

	got := reloadStatus(t, ctx, jobs, enqueued.ID)
	if got.Status == JobStatusSent {
		t.Errorf("job status = %q; want not sent", got.Status)
	}
}

// TestProcessJob_InviteTemplateRelativeAcceptURL_FailsWithoutSending proves the
// accept link must be an absolute URL — a relative link is dead in an email
// client, so the job must fail rather than deliver it.
func TestProcessJob_InviteTemplateRelativeAcceptURL_FailsWithoutSending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeTemplate(t, dir, "project-invitation", `<a href="{{acceptUrl}}">Accept</a>`)
	worker, jobs, sender, cleanup := newInviteWorkerFixture(t, dir)
	defer cleanup()

	enqueued := enqueueInviteJob(t, ctx, jobs, "project-invitation", map[string]interface{}{
		"projectName": "Acme",
		"acceptUrl":   "/invites/accept?token=abc",
	})
	job := dequeueOne(t, ctx, jobs)

	if err := worker.processJob(ctx, job); err == nil {
		t.Fatal("processJob returned nil for a relative acceptUrl; want an error")
	}
	if len(sender.sends) != 0 {
		t.Errorf("worker sent %d email(s) for a relative acceptUrl; want 0", len(sender.sends))
	}
	got := reloadStatus(t, ctx, jobs, enqueued.ID)
	if got.Status == JobStatusSent {
		t.Errorf("job status = %q; want not sent", got.Status)
	}
}

// TestProcessJob_MCPInviteMissingRequiredContent_FailsWithoutSending covers the
// general case: any transactional template whose required content is missing
// must fail loudly rather than send a degraded message and mark it sent.
func TestProcessJob_MCPInviteMissingRequiredContent_FailsWithoutSending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeTemplate(t, dir, "mcp-invite", "<p>{{senderName}} shared {{projectName}}</p>")
	worker, jobs, sender, cleanup := newInviteWorkerFixture(t, dir)
	defer cleanup()

	enqueued := enqueueInviteJob(t, ctx, jobs, "mcp-invite", map[string]interface{}{
		"senderName":  "Alice",
		"projectName": "Acme",
		"mcpUrl":      "https://api.example.com/api/mcp",
	})
	job := dequeueOne(t, ctx, jobs)

	if err := worker.processJob(ctx, job); err == nil {
		t.Fatal("processJob returned nil for an mcp-invite with no apiKey; want an error")
	}
	if len(sender.sends) != 0 {
		t.Errorf("worker sent %d email(s) for an mcp-invite with no apiKey; want 0", len(sender.sends))
	}
	got := reloadStatus(t, ctx, jobs, enqueued.ID)
	if got.Status == JobStatusSent {
		t.Errorf("job status = %q; want not sent", got.Status)
	}
}

// TestProcessJob_InviteHappyPath_SendsAcceptLink is the positive regression
// test: a renderable invitation template with an absolute acceptUrl is sent and
// the delivered body actually contains the link, and the job is marked sent.
func TestProcessJob_InviteHappyPath_SendsAcceptLink(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeTemplate(t, dir, "project-invitation", `<a href="{{acceptUrl}}">{{roleLabel}}</a>`)
	worker, jobs, sender, cleanup := newInviteWorkerFixture(t, dir)
	defer cleanup()

	const acceptURL = "https://memory.example.com/invites/accept?token=happy"
	enqueued := enqueueInviteJob(t, ctx, jobs, "project-invitation", map[string]interface{}{
		"inviterName": "Alice",
		"projectName": "Acme",
		"roleLabel":   "Member",
		"acceptUrl":   acceptURL,
	})
	job := dequeueOne(t, ctx, jobs)

	if err := worker.processJob(ctx, job); err != nil {
		t.Fatalf("processJob: %v", err)
	}
	if len(sender.sends) != 1 {
		t.Fatalf("worker sent %d email(s); want 1", len(sender.sends))
	}
	if body := sender.sends[0].HTML; !strings.Contains(body, acceptURL) {
		t.Errorf("sent HTML does not contain the accept link %q", acceptURL)
	}
	got := reloadStatus(t, ctx, jobs, enqueued.ID)
	if got.Status != JobStatusSent {
		t.Errorf("job status = %q; want %q", got.Status, JobStatusSent)
	}
}
