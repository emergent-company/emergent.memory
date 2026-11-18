// Agent work-claim e2e: the board must not dispatch an issue another lane
// already claimed.
//
// Regression: #990 duplicated #988. A session claimed issue #987 on GitHub
// (`status: in-progress` plus `manager: <lane>`), and the board still dispatched
// the coding agent, because the object-driven path only looked at the object's
// status and assignee. The issue-trigger path had deferred to peer claims for a
// while (issueClaimConflict); the board path never consulted the mirrored
// labels, so one issue got two implementations and the second could only
// conflict.
//
// This drives the real HTTP surface end to end: a reaction agent whose
// definition picks up GitHubIssue in `ready`, then two board items that differ
// only in their mirrored labels. The unclaimed one must enqueue a run; the
// peer-claimed one must enqueue nothing. No GitHub App, no model provider and no
// sandbox are involved — the claim is a label on the work object, which is
// exactly what the guard reads.
package api_test

import (
	"net/http"
	"testing"
	"time"
)

const workClaimPollTimeout = 30 * time.Second

// workClaimFixture creates a work definition that picks up GitHubIssue in
// `ready`, and returns the assignee name dispatch routes on. Creating a
// definition with objectRules makes the server bind a reaction agent for it
// (syncDefinitionWorkTriggers) named after the definition — that is the same
// path a UI-authored definition takes, so the test exercises it rather than
// hand-assembling an equivalent agent.
func workClaimFixture(t *testing.T, rl *runLog, projectID, token string) string {
	t.Helper()
	defName := "claim-e2e-coder"

	defBody := mustStatus(t, doAPILogged(t, rl, "POST", defPath(projectID, ""), token, projectID, jsonBody(map[string]any{
		"name":         defName,
		"enabled":      true,
		"dispatchMode": "queued",
		// objectRules is what makes the definition object-driven (WorkConfig
		// non-zero), which routes a pickup through the queued work path instead
		// of an inline execution.
		"workConfig": map[string]any{
			"status": map[string]any{"ready": "ready", "inProgress": "in_progress"},
			"objectRules": []any{map[string]any{
				"objectType": "GitHubIssue",
				"triggers":   []any{map[string]any{"status": "ready"}},
			}},
		},
	})), http.StatusCreated)
	var defResult map[string]any
	parseBodyJSON(t, defBody, &defResult)
	defData, _ := defResult["data"].(map[string]any)
	if id, _ := defData["id"].(string); id == "" {
		t.Fatalf("definition create returned no data.id: %s", defBody)
	}
	return defName
}

// createIssueObject writes a GitHubIssue board item in `ready`, assigned to the
// dispatch lane, carrying the mirrored GitHub labels the claim guard reads. The
// GitHub source stores those names in properties.labels (not the graph object's
// first-class labels, which are a separate tagging feature) — see
// sources.upsertBackfilledIssue.
func createIssueObject(t *testing.T, rl *runLog, projectID, token, key, assignee string, labels []string) string {
	t.Helper()
	resp := doAPILogged(t, rl, "POST", "/api/graph/objects", token, projectID, jsonBody(map[string]any{
		"type":     "GitHubIssue",
		"key":      key,
		"name":     key,
		"status":   "ready",
		"assignee": assignee,
		"properties": map[string]any{
			"repo":         "emergent-company/claim-e2e-fixture",
			"issue_number": 1,
			"state":        "open",
			"labels":       labels,
		},
	}))
	body := mustStatus(t, resp, http.StatusCreated)
	var obj map[string]any
	parseBodyJSON(t, body, &obj)
	id, _ := obj["id"].(string)
	// Board runs key on the object's canonical id, which the dispatch path
	// carries as subject_object_id.
	canonicalID, _ := obj["canonical_id"].(string)
	if id == "" || canonicalID == "" {
		t.Fatalf("graph object create returned no id/canonical_id: %s", body)
	}
	t.Cleanup(func() { deleteGraphObject(t, projectID, id) })
	return canonicalID
}

// boardRunsFor counts the project's board runs (runs bound to a subject object)
// for one work object.
func boardRunsFor(t *testing.T, rl *runLog, projectID, token, canonicalID string) int {
	t.Helper()
	body := mustStatus(t, doAPILogged(t, rl, "GET",
		"/api/projects/"+projectID+"/work-items/runs?limit=200", token, projectID, nil), http.StatusOK)
	var res struct {
		Data []struct {
			SubjectObjectID string `json:"subjectObjectId"`
		} `json:"data"`
	}
	parseBodyJSON(t, body, &res)
	n := 0
	for _, run := range res.Data {
		if run.SubjectObjectID == canonicalID {
			n++
		}
	}
	return n
}

// awaitBoardRuns waits for the work object to reach the wanted run count, which
// is 0 when the guard is expected to suppress dispatch entirely.
func awaitBoardRuns(t *testing.T, rl *runLog, projectID, token, canonicalID string, want int) int {
	t.Helper()
	deadline := time.Now().Add(workClaimPollTimeout)
	for boardRunsFor(t, rl, projectID, token, canonicalID) < want && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	return boardRunsFor(t, rl, projectID, token, canonicalID)
}

// TestWorkClaim_PeerClaimedIssueIsNotDispatched is the end-to-end lock on the
// guard: two ready, assigned GitHubIssue items, one of them claimed by another
// lane. Only the unclaimed one may produce a run.
func TestWorkClaim_PeerClaimedIssueIsNotDispatched(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentName := workClaimFixture(t, rl, projectID, token)

	// Exactly the label shape claim-issue.sh writes: a lifecycle label plus the
	// claiming session's manager label. #987 carried both.
	claimedID := createIssueObject(t, rl, projectID, token, "claim-e2e-peer-claimed", agentName,
		[]string{"status: in-progress", "manager: lane-e2e-peer"})
	freeID := createIssueObject(t, rl, projectID, token, "claim-e2e-unclaimed", agentName,
		[]string{"bug"})

	// The unclaimed item's run is also the barrier: both objects were created
	// before this wait, so the claimed item had the same opportunity to dispatch.
	if got := awaitBoardRuns(t, rl, projectID, token, freeID, 1); got != 1 {
		t.Fatalf("unclaimed ready issue: got %d board runs, want 1", got)
	}
	if got := boardRunsFor(t, rl, projectID, token, claimedID); got != 0 {
		t.Fatalf("peer-claimed issue: got %d board runs, want 0 — the board dispatched work another lane owns (#990)", got)
	}
}

// TestWorkClaim_PlatformOwnClaimStillDispatches is the other half of the rule:
// the platform's own claim (`manager: memory-coding`) is rework, not a peer
// claim, so the board must still dispatch it. Without this, a predicate that was
// too eager would silently stop all coding work.
func TestWorkClaim_PlatformOwnClaimStillDispatches(t *testing.T) {
	rl := newRunLog(t)
	defer rl.Close()
	skipIfServerDown(t, rl)

	projectID, _ := setupProjectLogged(t, rl)
	token := e2eTestToken()
	agentName := workClaimFixture(t, rl, projectID, token)

	oursID := createIssueObject(t, rl, projectID, token, "claim-e2e-own-claim", agentName,
		[]string{"status: in-progress", "manager: memory-coding"})

	if got := awaitBoardRuns(t, rl, projectID, token, oursID, 1); got != 1 {
		t.Fatalf("platform-owned claim: got %d board runs, want 1 (rework must still dispatch)", got)
	}
}
