package agents

import (
	"context"

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
