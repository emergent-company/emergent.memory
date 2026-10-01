package graph

import (
	"net/http"
	"slices"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// isBoardEnabledConfig reports whether a type's per-type work config marks the
// type board-enabled. Replaces the P2 `assignee IS NOT NULL` proxy with the real
// per-type `boardEnabled` flag (P4). A nil config (unknown/unconfigured type) is
// never board-enabled.
func isBoardEnabledConfig(cfg *agents.ObjectTypeWorkConfig) bool {
	return cfg != nil && cfg.BoardEnabled
}

// validateTypeStatus enforces the per-type allowed work-status values on a
// write (P4.1). For a board-enabled type with a declared allowed set, a write
// that sets a status (via the built-in status field or properties["status"])
// outside that set is rejected. Unconfigured or unconstrained types accept any
// status.
func validateTypeStatus(cfg *agents.ObjectTypeWorkConfig, status *string, props map[string]any) error {
	if !isBoardEnabledConfig(cfg) || len(cfg.AllowedStatuses) == 0 {
		return nil
	}
	effective := ""
	if status != nil {
		effective = *status
	}
	if st, ok := props["status"].(string); ok && st != "" {
		effective = st
	}
	if effective == "" {
		return nil // no status being set
	}
	if slices.Contains(cfg.AllowedStatuses, effective) {
		return nil
	}
	return workStatusNotAllowed(effective)
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

// workStatusNotAllowed is the rejection error for a status value outside a
// board-enabled type's declared allowed set (P4.1).
func workStatusNotAllowed(status string) error {
	return apperror.New(http.StatusBadRequest, "work_status_not_allowed", "status is not in the type's allowed work-status set: "+status)
}

// workStatusKeyRequired is the rejection error for a board-enabled object
// created without a key. The claim lock keys on (project, type, key), so a
// keyless board-enabled object could be dispatched but never claimed.
func workStatusKeyRequired() error {
	return apperror.New(http.StatusBadRequest, "work_status_key_required", "board-enabled work objects require a non-empty key")
}
