package graph

import (
	"net/http"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// isBoardEnabledType reports whether an object (or an incoming write) is
// board-enabled under the P2 minimal signal: a non-null assignee. This is the
// pragmatic, self-contained mechanism available before the per-type
// `boardEnabled` schema flag lands (P4); see ListWorkObjectsByStatus and
// design.md "Object-type configuration". The full per-type flag will replace
// this heuristic.
func isBoardEnabledType(head *GraphObject, incomingAssignee *string) bool {
	if incomingAssignee != nil && *incomingAssignee != "" {
		return true
	}
	return head != nil && head.Assignee != nil && *head.Assignee != ""
}

// writesStatus reports whether a write entry point attempts to set work status,
// either through the built-in status field or by smuggling it through
// properties["status"]. Both must be rejected so the two copies can never
// diverge (design.md "Single status writer").
func writesStatus(reqStatus *string, props map[string]any) bool {
	if reqStatus != nil {
		return true
	}
	if props == nil {
		return false
	}
	_, ok := props["status"]
	return ok
}

// rejectAgentWorkStatusWrite rejects a direct agent write that sets work status
// on a board-enabled object. The platform's own transitions (work_complete /
// work_block / claim / reaper) bypass the graph write entry points and stamp
// ActorSystem directly, so they are never rejected here. Returns nil when the
// write is allowed.
func rejectAgentWorkStatusWrite(actorType string, boardEnabled, setsStatus bool) error {
	if actorType != ActorAgent {
		return nil
	}
	if !boardEnabled {
		return nil
	}
	if !setsStatus {
		return nil
	}
	return workStatusWriteForbidden()
}

// workStatusWriteForbidden is the single rejection error for agent work-status
// writes, used by every write entry point so the message cannot drift.
func workStatusWriteForbidden() error {
	return apperror.New(http.StatusForbidden, "work_status_owned_by_platform", "agents cannot set work status directly; use work_complete or work_block")
}
