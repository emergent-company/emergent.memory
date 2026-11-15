package agents

import (
	"encoding/json"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// workRunInvocation resolves the invocation for a Board dispatch. A source
// auto-start item carries the sanitized invocation it was created with; a
// manual assignment falls back to the assigned agent's pickup trigger.
func workRunInvocation(h *graph.WorkObjectHead, trigger *AgentWorkTrigger) *AgentInvocation {
	if h != nil && h.Properties != nil {
		if raw, ok := h.Properties["run_invocation"]; ok && raw != nil {
			if inv := decodeInvocation(raw); inv != nil {
				return inv
			}
		}
	}
	if trigger != nil && trigger.Invocation != nil {
		return NormalizeInvocation(trigger.Invocation)
	}
	return nil
}

// workRunInvocationFromMetadata is the reaction/assignment path's equivalent
// of workRunInvocation: it can read run_invocation from the event payload.
func workRunInvocationFromMetadata(data map[string]any, trigger *AgentWorkTrigger) *AgentInvocation {
	if data != nil {
		if raw, ok := data["run_invocation"]; ok && raw != nil {
			if inv := decodeInvocation(raw); inv != nil {
				return inv
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

func decodeInvocation(raw any) *AgentInvocation {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var inv AgentInvocation
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil
	}
	return NormalizeInvocation(&inv)
}
