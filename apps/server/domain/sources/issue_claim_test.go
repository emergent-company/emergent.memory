package sources

import (
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
)

func issueStateFromJSON(t *testing.T, raw string) *githubapp.IssueState {
	t.Helper()
	var issue githubapp.IssueState
	if err := json.Unmarshal([]byte(raw), &issue); err != nil {
		t.Fatalf("unmarshal issue: %v", err)
	}
	return &issue
}

func TestIssueClaimConflict(t *testing.T) {
	tests := []struct {
		name      string
		labels    string
		assignees string
		ours      bool
		foreign   bool
	}{
		{name: "unclaimed", labels: `[]`, assignees: `[]`},
		{name: "our in-progress claim", labels: `[{"name":"status: in-progress"},{"name":"manager: memory-coding"}]`, assignees: `[{"login":"mkucharz"}]`, ours: true},
		{name: "peer manager", labels: `[{"name":"manager: peer"}]`, assignees: `[]`, foreign: true},
		{name: "peer in-progress", labels: `[{"name":"status: in-progress"},{"name":"manager: peer"}]`, assignees: `[]`, foreign: true},
		{name: "legacy in-progress", labels: `[{"name":"status: in-progress"}]`, assignees: `[]`, foreign: true},
		{name: "blocked", labels: `[{"name":"status: blocked"},{"name":"manager: memory-coding"}]`, assignees: `[{"login":"mkucharz"}]`, foreign: true},
		{name: "foreign assignee", labels: `[]`, assignees: `[{"login":"someone-else"}]`, foreign: true},
		{name: "responsible assignee only", labels: `[]`, assignees: `[{"login":"mkucharz"}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issue := issueStateFromJSON(t, `{"labels":`+tc.labels+`,"assignees":`+tc.assignees+`}`)
			ours, foreign := issueClaimConflict(issue, "mkucharz")
			if ours != tc.ours || foreign != tc.foreign {
				t.Fatalf("ours=%v foreign=%v, want ours=%v foreign=%v", ours, foreign, tc.ours, tc.foreign)
			}
		})
	}
}

func TestSourceAssigneeLogin(t *testing.T) {
	if got := sourceAssigneeLogin(&Source{Config: map[string]any{"assignee_login": " mkucharz "}}); got != "mkucharz" {
		t.Fatalf("assignee = %q", got)
	}
	if got := sourceAssigneeLogin(&Source{Config: map[string]any{}}); got != "" {
		t.Fatalf("empty assignee = %q", got)
	}
}

// TestIssueClaimLaneMatchesAgentsDomain guards the one value both dispatch paths
// must agree on: the manager label the platform writes when it claims an issue.
// The issue-trigger path (this package) and the board path (agents) each carry
// it, and a divergence would make one path dispatch work the other refuses.
func TestIssueClaimLaneMatchesAgentsDomain(t *testing.T) {
	if want := "manager: " + agents.IssueClaimLane; issueClaimManagerLabel != want {
		t.Fatalf("issueClaimManagerLabel = %q, want %q (agents.IssueClaimLane)", issueClaimManagerLabel, want)
	}
}
