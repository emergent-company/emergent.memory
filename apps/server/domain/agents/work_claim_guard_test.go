package agents

import (
	"errors"
	"net/http"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// TestForeignIssueClaim locks the peer-claim rule the board path applies. Each
// case is a label set that has been seen on a real issue: our own claim, a
// Paseo lane claim, the two-label claim claim-issue.sh writes (manager + lane),
// a parked claim, and the legacy in-progress claim with no manager.
func TestForeignIssueClaim(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{name: "no labels", labels: nil},
		{name: "unrelated labels", labels: []string{"bug", "area:agents"}},
		{name: "ours only", labels: []string{"status: in-progress", "manager: memory-coding"}},
		{name: "peer lane only", labels: []string{"manager: lane-5d320db5"}, want: true},
		{name: "peer lane with lifecycle label", labels: []string{"status: in-progress", "manager: memory-coding", "manager: lane-5d320db5"}, want: true},
		{
			// claim-issue.sh writes the human manager first, then the lane.
			name:   "two managers",
			labels: []string{"status: in-progress", "manager: memory-coding", "manager: other-session"},
			want:   true,
		},
		{name: "blocked", labels: []string{"status: blocked", "manager: memory-coding"}, want: true},
		{name: "legacy in-progress with no manager", labels: []string{"status: in-progress"}, want: true},
		{name: "case insensitive", labels: []string{"MANAGER: Lane-ABC"}, want: true},
		{name: "manager label with no value", labels: []string{"manager: "}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := foreignIssueClaim(tc.labels); got != tc.want {
				t.Fatalf("foreignIssueClaim(%v) = %v, want %v", tc.labels, got, tc.want)
			}
		})
	}
}

func TestIssueLabels(t *testing.T) {
	if got := issueLabels(nil); got != nil {
		t.Fatalf("nil properties must yield no labels, got %v", got)
	}
	if got := issueLabels(map[string]any{"labels": []string{"a", "b"}}); len(got) != 2 {
		t.Fatalf("[]string labels must pass through, got %v", got)
	}
	got := issueLabels(map[string]any{"labels": []any{"a", "", 7, "b"}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("[]any labels must keep non-empty strings only, got %v", got)
	}
	if got := issueLabels(map[string]any{"labels": "not-a-list"}); got != nil {
		t.Fatalf("a non-list labels property must be ignored, got %v", got)
	}
}

// TestSkipForeignIssueClaim covers the two conditions the dispatch guard adds on
// top of the labels: only GitHub issues are claims, and a head is required.
func TestSkipForeignIssueClaim(t *testing.T) {
	claimed := &graph.WorkObjectHead{
		Type: "GitHubIssue",
		Properties: map[string]any{
			"labels": []any{"status: in-progress", "manager: lane-5d320db5"},
		},
	}
	free := &graph.WorkObjectHead{Type: "GitHubIssue", Properties: map[string]any{"labels": []any{"bug"}}}

	if !skipForeignIssueClaim("GitHubIssue", claimed) {
		t.Fatal("a peer-claimed issue must not dispatch")
	}
	if skipForeignIssueClaim("GitHubIssue", free) {
		t.Fatal("an unclaimed issue must dispatch")
	}
	if skipForeignIssueClaim("GitHubIssue", nil) {
		t.Fatal("a missing head must not suppress dispatch")
	}
	if skipForeignIssueClaim("PullRequest", claimed) {
		t.Fatal("the claim guard applies to GitHub issues only")
	}
}

// TestForeignIssueClaimConflict locks the human-action refusal: a peer-claimed
// issue yields the 409 the handlers surface, while every other head passes.
func TestForeignIssueClaimConflict(t *testing.T) {
	claimed := &graph.WorkObjectHead{
		Type: "GitHubIssue",
		Properties: map[string]any{
			"labels": []any{"status: in-progress", "manager: lane-5d320db5"},
		},
	}
	err := foreignIssueClaimConflict(claimed)
	appErr, ok := errors.AsType[*apperror.Error](err)
	if !ok {
		t.Fatalf("a peer-claimed issue must yield an app error, got %v", err)
	}
	if appErr.HTTPStatus != http.StatusConflict {
		t.Fatalf("peer-claim refusal status = %d, want %d", appErr.HTTPStatus, http.StatusConflict)
	}
	if appErr.Code != "conflict" {
		t.Fatalf("peer-claim refusal code = %q, want conflict", appErr.Code)
	}

	free := &graph.WorkObjectHead{Type: "GitHubIssue", Properties: map[string]any{"labels": []any{"bug"}}}
	ours := &graph.WorkObjectHead{
		Type:       "GitHubIssue",
		Properties: map[string]any{"labels": []any{"status: in-progress", "manager: memory-coding"}},
	}
	for _, head := range []*graph.WorkObjectHead{nil, free, ours, {Type: "ResearchRequest"}} {
		if err := foreignIssueClaimConflict(head); err != nil {
			t.Fatalf("foreignIssueClaimConflict must allow %+v, got %v", head, err)
		}
	}
}
