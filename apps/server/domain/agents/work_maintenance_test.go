package agents

import (
	"context"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// TestListWorkAgentStatuses_UnionsTriggerStatuses verifies the reconciler/reaper
// scan unions each agent's triggerStatuses (the status_changed signal) into the
// ready set, alongside the ready/in_progress work-config statuses and the
// built-in defaults.
func TestListWorkAgentStatuses_UnionsTriggerStatuses(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	// Query 1: ready/in_progress statuses from work_config->status.
	mock.ExpectQuery(`(?s)SELECT DISTINCT.*work_config->'status'.*ready.*in_progress`).
		WillReturnRows(sqlmock.NewRows([]string{"ready", "in_progress"}).
			AddRow("ready", "in_progress").
			AddRow("queued", "working"))

	// Query 2: trigger statuses from work_config->triggerStatuses.
	mock.ExpectQuery(`(?s)SELECT DISTINCT jsonb_array_elements_text.*triggerStatuses`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).
			AddRow("todo").
			AddRow("review"))

	ready, inProgress, err := repo.listWorkAgentStatuses(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ready", "queued", "todo", "review"}, ready)
	assert.ElementsMatch(t, []string{"in_progress", "working"}, inProgress)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestListWorkAgentStatuses_DefaultsWhenNoAgents verifies the scan returns the
// built-in defaults even when no work agents are configured (both queries empty).
func TestListWorkAgentStatuses_DefaultsWhenNoAgents(t *testing.T) {
	sqldb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	repo := NewRepository(bun.NewDB(sqldb, pgdialect.New()))

	mock.ExpectQuery(`(?s)SELECT DISTINCT.*work_config->'status'.*ready.*in_progress`).
		WillReturnRows(sqlmock.NewRows([]string{"ready", "in_progress"}))
	mock.ExpectQuery(`(?s)SELECT DISTINCT jsonb_array_elements_text.*triggerStatuses`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}))

	ready, inProgress, err := repo.listWorkAgentStatuses(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ready"}, ready)
	assert.ElementsMatch(t, []string{"in_progress"}, inProgress)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkTriggerMetadataIncludesSourceSubjectOnlyForLaneObjects(t *testing.T) {
	plain := workTriggerMetadata(&graph.WorkObjectHead{Status: "ready", Properties: map[string]any{"title": "x"}})
	if _, ok := plain["repo"]; ok {
		t.Fatalf("plain work object got source keys: %#v", plain)
	}
	head := &graph.WorkObjectHead{Status: "ready", Properties: map[string]any{
		"repo": "acme/repo", "pr_number": float64(7), "head_sha": "abc", "source_id": "src-1",
	}}
	lane := workTriggerMetadata(head)
	if lane["repo"] != "acme/repo" || lane["prNumber"] != 7 || lane["headSha"] != "abc" || lane["subjectRef"] != "acme/repo#7" || lane["sourceId"] != "src-1" {
		t.Fatalf("lane metadata = %#v", lane)
	}
	// A work-dispatched review lane run must carry the verified source subject
	// as a run column too: the review tools key evidence on repo#number@headSha,
	// and trigger_metadata is model-settable while this column is not (#1022).
	subject := workSourceSubject(head)
	if subject == nil || subject.SourceID != "src-1" || subject.Repo != "acme/repo" || subject.PRNumber != 7 || subject.HeadSHA != "abc" {
		t.Fatalf("lane source subject = %#v", subject)
	}
	if got := workSourceSubject(&graph.WorkObjectHead{Status: "ready", Properties: map[string]any{"title": "x"}}); got != nil {
		t.Fatalf("plain work object got a source subject: %#v", got)
	}
	// An issue object carries no head SHA but is still a trusted coding subject:
	// the issue lane needs it for its implicit source credential.
	issue := workSourceSubject(&graph.WorkObjectHead{Status: "ready", Properties: map[string]any{
		"repo": "acme/repo", "issue_number": float64(9), "source_id": "src-1",
	}})
	if issue == nil || issue.Kind != "issue" || issue.IssueNumber != 9 || issue.SourceID != "src-1" || issue.Repo != "acme/repo" {
		t.Fatalf("issue source subject = %#v", issue)
	}
	if issue.RepositoryValid() {
		t.Fatalf("an issue subject must not validate as a pull request subject: %#v", issue)
	}
}

func TestIsTerminalLaneStatus(t *testing.T) {
	for _, status := range []string{"done", "revision", "cancelled"} {
		if !isTerminalLaneStatus(&graph.WorkObjectHead{Status: status}) {
			t.Errorf("%s should be terminal", status)
		}
	}
	for _, status := range []string{"ready", "in_progress", "blocked", ""} {
		if isTerminalLaneStatus(&graph.WorkObjectHead{Status: status}) {
			t.Errorf("%s must not be terminal", status)
		}
	}
}

func TestWorkTriggerMetadataAcceptsIntegerIssueNumber(t *testing.T) {
	meta := workTriggerMetadata(&graph.WorkObjectHead{Status: "ready", Type: "GitHubIssue", Properties: map[string]any{
		"repo":         "emergent-company/emergent.memory",
		"issue_number": 819,
		"source_id":    "source-1",
		"url":          "https://github.com/emergent-company/emergent.memory/issues/819",
	}})
	if meta["issueNumber"] != 819 || meta["sourceId"] != "source-1" || meta["subjectType"] != "issue" {
		t.Fatalf("meta = %#v", meta)
	}
}

// TestWorkTriggerMetadataBindsLinkedIssueToPullRequestHead pins #1091: a coding
// issue linked to a pull request carries pr_number/pr_head_sha/pr_branch (not
// head_sha), so a rework run dispatched from a blocking review verdict must
// still receive the pull request number, the reviewed head SHA and the branch
// it has to update.
func TestWorkTriggerMetadataBindsLinkedIssueToPullRequestHead(t *testing.T) {
	meta := workTriggerMetadata(&graph.WorkObjectHead{Status: "revision", Type: "GitHubIssue", Properties: map[string]any{
		"repo":         "acme/repo",
		"issue_number": float64(42),
		"pr_number":    float64(7),
		"pr_head_sha":  "abc123",
		"pr_branch":    "fix/42-review-finding",
		"source_id":    "src-1",
	}})
	require.Equal(t, 7, meta["prNumber"])
	require.Equal(t, "abc123", meta["headSha"])
	require.Equal(t, "fix/42-review-finding", meta["branch"])
	require.Equal(t, "acme/repo#7", meta["subjectRef"])
}

func TestMergeWorkObjectMetadataPreservesEventAndAddsSource(t *testing.T) {
	meta := map[string]any{"work_trigger_event": "created", "work_claim_status": "ready"}
	mergeWorkObjectMetadata(meta, &graph.WorkObjectHead{Type: "GitHubIssue", Status: "ready", Properties: map[string]any{
		"repo":         "emergent-company/emergent.memory",
		"issue_number": 819,
		"source_id":    "source-1",
	}})
	if meta["work_trigger_event"] != "created" || meta["repo"] != "emergent-company/emergent.memory" || meta["sourceId"] != "source-1" || meta["issueNumber"] != 819 {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestIsIssueReviewFinal(t *testing.T) {
	if !isIssueReviewFinal(&graph.WorkObjectHead{Type: "GitHubIssue", Status: "review"}) {
		t.Fatal("issue review should finalize a coding run")
	}
	if isIssueReviewFinal(&graph.WorkObjectHead{Type: "GitHubIssue", Status: "ready"}) {
		t.Fatal("ready issue must not finalize")
	}
	if isIssueReviewFinal(&graph.WorkObjectHead{Type: "GitHubReviewLane", Status: "review"}) {
		t.Fatal("review lane must use its own finalizer")
	}
}

func strptr(s string) *string { return &s }

// A step-budget park is the only durable block the reconciler may clear on its
// own: a budget park has its own recovery, and every other block (a human stop,
// a deterministic failure, a poison item) must stay until a human unblocks it.
func TestIsStepLimitBlock(t *testing.T) {
	cases := []struct {
		name  string
		block *WorkBlock
		want  bool
	}{
		{"nil block", nil, false},
		{"unclassified block", &WorkBlock{Reason: "cancelled by a human"}, false},
		{"step limit", &WorkBlock{FailureClass: strptr(string(FailureClassStepLimit))}, true},
		{"budget is not a step limit", &WorkBlock{FailureClass: strptr(string(FailureClassBudget))}, false},
		{"retryable is not a step limit", &WorkBlock{FailureClass: strptr(string(FailureClassRetryable))}, false},
	}
	for _, tc := range cases {
		if got := isStepLimitBlock(tc.block); got != tc.want {
			t.Errorf("%s: isStepLimitBlock = %v, want %v", tc.name, got, tc.want)
		}
		if tc.block != nil && isStepLimitBlock(tc.block) && isBudgetBlock(tc.block) {
			t.Errorf("%s: a block cannot be both a budget park and a step-limit park", tc.name)
		}
	}
}

// The automatic-resume budget is what stops a run that can never converge from
// looping for ever: the first exhaustion is resumed, the last one is a human's.
func TestShouldResumeStepLimit(t *testing.T) {
	if shouldResumeStepLimit(0) {
		t.Error("a zero exhaustion count is not a park to resume")
	}
	for n := 1; n <= stepLimitAutoResumeLimit; n++ {
		if !shouldResumeStepLimit(n) {
			t.Errorf("exhaustion %d should still resume", n)
		}
	}
	if shouldResumeStepLimit(stepLimitAutoResumeLimit + 1) {
		t.Errorf("exhaustion %d must stop and wait for a human", stepLimitAutoResumeLimit+1)
	}
}

func TestStepLimitResumeMessageCarriesTheAttemptAndTheInstruction(t *testing.T) {
	msg := stepLimitResumeMessage(1)
	for _, want := range []string{"continuation attempt 1 of 2", "Check the current state first", "do not start the work over"} {
		if !strings.Contains(msg, want) {
			t.Errorf("resume message %q missing %q", msg, want)
		}
	}
}
