package agents

import "testing"

func TestSkipUnassignedCreatedIssue(t *testing.T) {
	if !skipUnassignedCreatedIssue(EventTypeCreated, "GitHubIssue", "") {
		t.Fatal("unassigned backfilled issue must stay idle")
	}
	if skipUnassignedCreatedIssue(EventTypeCreated, "GitHubIssue", "coding") {
		t.Fatal("assigned auto-start issue must dispatch")
	}
	if skipUnassignedCreatedIssue(EventTypeAssigned, "GitHubIssue", "") {
		t.Fatal("assigned event path must not be suppressed")
	}
}
