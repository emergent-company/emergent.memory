package agents

import "testing"

func TestSkipUnassignedIssue(t *testing.T) {
	if !skipUnassignedIssue("GitHubIssue", "") {
		t.Fatal("unassigned backfilled issue must stay idle")
	}
	if skipUnassignedIssue("GitHubIssue", "coding") {
		t.Fatal("assigned auto-start issue must dispatch")
	}
	if skipUnassignedIssue("ResearchRequest", "") {
		t.Fatal("other object types are not suppressed")
	}
}
