package agents

import (
	"context"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// WorkInvocationResolver resolves a source trigger's invocation from the
// server-owned trigger row. The board item only carries the trigger id; the
// invocation (model, context, env allowlist, credential request) is never
// trusted from graph properties, which project users and agents can write.
type WorkInvocationResolver interface {
	ResolveSourceTriggerInvocation(ctx context.Context, projectID, sourceID, triggerID string) (*AgentInvocation, error)
}

func workRunInvocation(ctx context.Context, resolver WorkInvocationResolver, projectID string, h *graph.WorkObjectHead, trigger *AgentWorkTrigger) *AgentInvocation {
	if h != nil && h.Properties != nil {
		if triggerID, _ := h.Properties["source_trigger_id"].(string); triggerID != "" && resolver != nil {
			sourceID, _ := h.Properties["source_id"].(string)
			if inv, err := resolver.ResolveSourceTriggerInvocation(ctx, projectID, sourceID, triggerID); err == nil && inv != nil {
				return NormalizeInvocation(inv)
			}
		}
	}
	if trigger != nil && trigger.Invocation != nil {
		return NormalizeInvocation(trigger.Invocation)
	}
	return nil
}

// workRunTrusted mirrors the source-event trust boundary for work items whose
// content comes from an external issue. A human assigning the item chooses the
// agent, but must not upgrade externally authored text to internal trust.
func workRunTrusted(h *graph.WorkObjectHead) bool {
	if h == nil {
		return true
	}
	return workRunTrustedType(h.Type)
}

func workRunTrustedType(objectType string) bool {
	return objectType != "GitHubIssue"
}

// skipUnassignedIssue keeps a bulk GitHub issue backfill idle: ready items wait
// for a human assignment, and only an assigned item may dispatch a coding run.
func skipUnassignedIssue(objectType, assignee string) bool {
	return objectType == "GitHubIssue" && assignee == ""
}

// IssueClaimLane is the `manager:` lane the platform writes when its own coding
// agent claims a GitHub issue. The sources domain writes the same label
// (sources.issueClaimManagerLabel) on the issue-trigger path and treats every
// other manager as a peer claim. The agents domain cannot import sources
// (sources imports agents), so the value is exported here and a sources test
// asserts the two cannot drift.
const IssueClaimLane = "memory-coding"

const (
	issueClaimStatusLabel  = "status: in-progress"
	issueClaimBlockedLabel = "status: blocked"
	issueClaimManagerLabel = "manager: "
)

// issueLabels returns the mirrored GitHub label names carried on a work
// object's properties. The GitHub source stores them as []any; []string is
// accepted for objects written by tests and by the graph API.
func issueLabels(props map[string]any) []string {
	if props == nil {
		return nil
	}
	switch labels := props["labels"].(type) {
	case []string:
		return labels
	case []any:
		out := make([]string, 0, len(labels))
		for _, item := range labels {
			if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}

// foreignIssueClaim reports whether a GitHub issue's mirrored labels show a live
// claim held by another lane: a blocked status, any `manager:` label naming a
// lane other than ours, or an in-progress lifecycle label with no manager at all
// (the legacy claim shape). It is the board-path counterpart of the peer-claim
// rule in sources.issueClaimConflict, and it is deliberately the stricter of the
// two: the trigger path reads the first manager label, while a lane claim that
// also carries our label must still block a second implementation here.
func foreignIssueClaim(labels []string) bool {
	managed := false
	inProgress := false
	foreignManager := false
	for _, label := range labels {
		lower := strings.ToLower(strings.TrimSpace(label))
		switch {
		case lower == issueClaimBlockedLabel:
			return true
		case lower == issueClaimStatusLabel:
			inProgress = true
		case strings.HasPrefix(lower, issueClaimManagerLabel):
			lane := strings.TrimSpace(lower[len(issueClaimManagerLabel):])
			if lane == "" {
				continue
			}
			managed = true
			if !strings.EqualFold(lane, IssueClaimLane) {
				foreignManager = true
			}
		}
	}
	if foreignManager {
		return true
	}
	if managed {
		return false
	}
	return inProgress
}

// skipForeignIssueClaim keeps an object-driven run from starting on a GitHub
// issue another lane already owns. Without it the board dispatches the coding
// agent on `status: ready` even though `status: in-progress` and `manager: <lane>`
// say a peer session has the issue: two implementations of one issue, the
// second of which can only conflict (emergent.memory #990/#987).
func skipForeignIssueClaim(objectType string, head *graph.WorkObjectHead) bool {
	if objectType != "GitHubIssue" || head == nil {
		return false
	}
	return foreignIssueClaim(issueLabels(head.Properties))
}

func workSourceTriggerID(h *graph.WorkObjectHead) *string {
	if h == nil || h.Properties == nil {
		return nil
	}
	id, _ := h.Properties["source_trigger_id"].(string)
	if id == "" {
		return nil
	}
	return &id
}
