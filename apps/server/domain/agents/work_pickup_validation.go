package agents

import (
	"context"
	"errors"
	"fmt"
)

// hasWorkConfigForDefinition reports whether the definition referenced by
// definitionID carries a non-zero work config (an object-driven work agent). A
// nil or unresolvable id reports false (an inline reaction agent).
func (h *Handler) hasWorkConfigForDefinition(ctx context.Context, definitionID *string) bool {
	if definitionID == nil || *definitionID == "" {
		return false
	}
	def, err := h.repo.FindDefinitionByID(ctx, *definitionID, nil)
	if err != nil || def == nil {
		return false
	}
	return !def.WorkConfig.IsZero()
}

// ValidateWorkPickup validates a reaction config against the object-driven work
// pickup constraints. When hasWorkConfig is false the config is an inline
// reaction agent and keeps its historical multi-type/multi-event shape. A work
// agent (hasWorkConfig true) must declare exactly one object type and exactly
// one pickup signal from created|updated|status_changed|assigned, must not use
// the deleted signal, and must set concurrencyStrategy: skip — a single-object
// work lane has no defined semantics for empty or parallel concurrency, and a
// reassign during a live run creates a new object version that the
// (agent, object, version, event) dedup cannot catch.
func ValidateWorkPickup(hasWorkConfig bool, rc *ReactionConfig) error {
	if !hasWorkConfig {
		return nil
	}
	if rc == nil {
		return errors.New("work agent requires a reaction config declaring exactly one object type and one pickup signal")
	}
	if len(rc.ObjectTypes) != 1 {
		return fmt.Errorf("work agent must declare exactly one object type (got %d)", len(rc.ObjectTypes))
	}
	if len(rc.Events) != 1 {
		return fmt.Errorf("work agent must declare exactly one pickup signal (got %d)", len(rc.Events))
	}
	switch rc.Events[0] {
	case EventTypeCreated, EventTypeUpdated, EventTypeStatusChanged, EventTypeAssigned:
		// valid signal
	default:
		return fmt.Errorf("work agent pickup signal must be one of created, updated, status_changed, assigned (got %q)", rc.Events[0])
	}
	if rc.ConcurrencyStrategy != ConcurrencySkip {
		return fmt.Errorf("work agent must set concurrencyStrategy: skip (got %q)", rc.ConcurrencyStrategy)
	}
	return nil
}
