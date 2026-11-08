package graph

import (
	"context"
	"net/http"
	"slices"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// isBoardEnabledConfig reports whether a type's per-type work config marks the
// type board-enabled. Replaces the P2 `assignee IS NOT NULL` proxy with the real
// per-type `boardEnabled` flag (P4). A nil config (unknown/unconfigured type) is
// never board-enabled.
func isBoardEnabledConfig(cfg *agents.ObjectTypeWorkConfig) bool {
	return cfg != nil && cfg.BoardEnabled
}

// resolvedInitialStatus returns the entry status of a board-enabled type
// (#422): its declared initialStatus when set and valid, otherwise the first
// declared allowed status. A type that is not board-enabled, or declares no
// allowed statuses, has no initial status and is left untouched.
//
// A type with no allowed statuses is unaffected: without a declared
// vocabulary there is no lane to choose, even if a stray initialStatus is
// present.
//
// A stored definition whose initialStatus is outside its own allowed set (a pack
// authored before Validate existed) degrades to the first allowed status rather
// than creating an object in a lane the board does not offer.
func resolvedInitialStatus(cfg *agents.ObjectTypeWorkConfig) string {
	if !isBoardEnabledConfig(cfg) || len(cfg.AllowedStatuses) == 0 {
		return ""
	}
	if cfg.InitialStatus != "" && slices.Contains(cfg.AllowedStatuses, cfg.InitialStatus) {
		return cfg.InitialStatus
	}
	return cfg.AllowedStatuses[0]
}

// requestedWorkStatus returns the status a create caller explicitly asked for,
// with properties["status"] winning over the top-level field (matching
// statusForWrite). An explicit empty string is not a status: it is the column's
// zero value, so it is treated as "not supplied" and receives the initial
// status like a missing field.
func requestedWorkStatus(status *string, props map[string]any) *string {
	if st := propertyStatus(props); st != nil {
		return st
	}
	if status != nil && *status != "" {
		return status
	}
	return nil
}

// applyInitialStatus returns the status a create should persist. An explicitly
// requested status (or a properties["status"] mirror) is left untouched; only a
// create with no status at all receives the type's resolved initial status. It
// is a default, not a status write: it applies to every actor and does not need
// to bypass the agent guard.
func applyInitialStatus(cfg *agents.ObjectTypeWorkConfig, status *string, props map[string]any) *string {
	if requestedWorkStatus(status, props) != nil {
		return status
	}
	if initial := resolvedInitialStatus(cfg); initial != "" {
		return &initial
	}
	return status
}

// rejectAgentCreateWorkStatus applies the agent work-status rule to a create
// (#422 option 2): an agent may name the resolved initial status explicitly, so
// existing watcher code that sends status: pending/open keeps working, while
// every other status is still platform-owned. It falls through to the ordinary
// guard for non-board types and for any other status.
func rejectAgentCreateWorkStatus(ctx context.Context, actorType string, cfg *agents.ObjectTypeWorkConfig, status *string, props map[string]any) error {
	return rejectAgentCreateWorkStatusWithAllow(actorType, cfg, requestedWorkStatus(status, props), auth.AgentWorkStatusAllowedFromContext(ctx))
}

// rejectAgentCreateWorkStatusWithAllow is the context-free form used by the
// subgraph/bulk create path, which deliberately never inherits the scoped
// reaction-agent exemption.
func rejectAgentCreateWorkStatusWithAllow(actorType string, cfg *agents.ObjectTypeWorkConfig, requested *string, allow bool) error {
	if requested == nil {
		return nil
	}
	if initial := resolvedInitialStatus(cfg); initial != "" && *requested == initial {
		return nil
	}
	return rejectAgentWorkStatusWriteWithAllow(actorType, isBoardEnabledConfig(cfg), true, allow)
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

// ValidateWorkObjectStatus enforces the per-type allowed work-status values on
// an exact-status write (the human set-status action). For a board-enabled type
// with a declared allowed set, a status outside that set is rejected.
// Unconfigured or unconstrained types accept any status. It is the exported
// sibling of validateTypeStatus, used by the agents domain to reject a
// disallowed status before dispatching to UpdateWorkObjectStatus.
func ValidateWorkObjectStatus(cfg *agents.ObjectTypeWorkConfig, status string) error {
	if !isBoardEnabledConfig(cfg) || len(cfg.AllowedStatuses) == 0 {
		return nil
	}
	if slices.Contains(cfg.AllowedStatuses, status) {
		return nil
	}
	return workStatusNotAllowed(status)
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
	return rejectAgentWorkStatusWriteWithAllow(actorType, boardEnabled, setsStatus, false)
}

// rejectAgentWorkStatusWriteCtx is rejectAgentWorkStatusWrite for the
// single-object write paths. It honors the scoped reaction-agent exemption
// stamped by the MCP entity-update path when the target is the run's subject
// object. The batch/subgraph path uses the context-free form so a bulk write
// can never inherit the exemption.
func rejectAgentWorkStatusWriteCtx(ctx context.Context, actorType string, boardEnabled, setsStatus bool) error {
	return rejectAgentWorkStatusWriteWithAllow(actorType, boardEnabled, setsStatus, auth.AgentWorkStatusAllowedFromContext(ctx))
}

func rejectAgentWorkStatusWriteWithAllow(actorType string, boardEnabled, setsStatus, allow bool) error {
	if allow {
		return nil
	}
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

// requireBoardEnabledKey enforces the board-enabled stable-key rule for a single
// object. The claim lock keys on (project, type, key), so a board-enabled object
// created without a key could be dispatched but never claimed. Shared by every
// create entry point (single Create and the atomic subgraph/bulk path) so the
// rule cannot drift. Returns nil when the type is not board-enabled (or its
// config is unknown) or the object carries a non-empty key.
func requireBoardEnabledKey(cfg *agents.ObjectTypeWorkConfig, key *string) error {
	if isBoardEnabledConfig(cfg) && (key == nil || *key == "") {
		return workStatusKeyRequired()
	}
	return nil
}

// validateSubgraphObjectKeys applies the board-enabled stable-key guard to every
// object in a subgraph/bulk create request. It is pure — it reads only the
// already-resolved schemas — so the whole batch can be validated before any
// write, making a keyless board-enabled object fail the request atomically with
// no partial state. Unknown/unconfigured types are never board-enabled. A nil
// schema set (schema provider unavailable) leaves the guard inert, matching the
// rest of the subgraph path's fail-open property validation.
func validateSubgraphObjectKeys(schemas *ExtractionSchemas, objects []SubgraphObjectRequest) error {
	if schemas == nil {
		return nil
	}
	for i := range objects {
		schema, ok := schemas.ObjectSchemas[objects[i].Type]
		if !ok {
			continue
		}
		if err := requireBoardEnabledKey(&schema.ObjectTypeWorkConfig, objects[i].Key); err != nil {
			return err
		}
	}
	return nil
}

// validateSubgraphWorkStatus applies the per-type work-status rules to a
// subgraph/bulk create batch, matching the single-object create path: for a
// board-enabled type a status outside its declared allowed set is rejected
// (validateTypeStatus), and an agent-originated create that sets a work status
// is rejected (rejectAgentWorkStatusWrite). It is pure — it reads only the
// already-resolved schemas — so the whole batch can be validated before any
// write, making an off-vocabulary or agent status write fail the request
// atomically with no partial state. Unknown/unconfigured types are never
// constrained. A nil schema set (schema provider unavailable) leaves the guard
// inert, matching the rest of the subgraph path's fail-open validation.
func validateSubgraphWorkStatus(actorType string, schemas *ExtractionSchemas, objects []SubgraphObjectRequest) error {
	if schemas == nil {
		return nil
	}
	for i := range objects {
		schema, ok := schemas.ObjectSchemas[objects[i].Type]
		if !ok {
			continue
		}
		cfg := &schema.ObjectTypeWorkConfig
		requested := requestedWorkStatus(objects[i].Status, objects[i].Properties)
		if err := rejectAgentCreateWorkStatusWithAllow(actorType, cfg, requested, false); err != nil {
			return err
		}
		if err := validateTypeStatus(cfg, objects[i].Status, objects[i].Properties); err != nil {
			return err
		}
	}
	return nil
}
