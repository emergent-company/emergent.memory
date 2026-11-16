package agents

import (
	"context"
	"github.com/emergent-company/emergent.memory/domain/graph"
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
	lane := workTriggerMetadata(&graph.WorkObjectHead{Status: "ready", Properties: map[string]any{
		"repo": "acme/repo", "pr_number": float64(7), "head_sha": "abc", "source_id": "src-1",
	}})
	if lane["repo"] != "acme/repo" || lane["prNumber"] != 7 || lane["headSha"] != "abc" || lane["subjectRef"] != "acme/repo#7" || lane["sourceId"] != "src-1" {
		t.Fatalf("lane metadata = %#v", lane)
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
