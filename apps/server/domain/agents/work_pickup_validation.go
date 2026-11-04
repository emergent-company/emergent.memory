package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// WorkRuleType is the schema-side shape validation needs for one object type.
// Fields is the declared property-name set; AllowedStatuses the declared work
// vocabulary. Both empty means "unconstrained".
type WorkRuleType struct {
	AllowedStatuses []string
	Fields          []string
}

// ValidateWorkRules structurally validates a definition's per-type rules. When
// types is non-nil, allowed statuses and writable fields are checked against the
// type's declared vocabulary/properties; an unknown type is left to the caller
// (a nil types map skips schema-scoped checks). It returns the first error.
func ValidateWorkRules(def *AgentDefinition, types map[string]WorkRuleType) error {
	if def == nil || len(def.WorkConfig.ObjectRules) == 0 {
		return nil
	}
	seenTypes := make(map[string]bool, len(def.WorkConfig.ObjectRules))
	for i := range def.WorkConfig.ObjectRules {
		r := &def.WorkConfig.ObjectRules[i]
		if strings.TrimSpace(r.ObjectType) == "" {
			return fmt.Errorf("object rule %d: object type is required", i+1)
		}
		if seenTypes[r.ObjectType] {
			return fmt.Errorf("object rule for %q is declared more than once", r.ObjectType)
		}
		seenTypes[r.ObjectType] = true

		var schema *WorkRuleType
		if types != nil {
			if t, ok := types[r.ObjectType]; ok {
				schema = &t
			}
		}

		phases := def.WorkConfig.EffectiveStatus(r.ObjectType)
		nonPickup := map[string]string{
			phases.InProgress: "in-progress",
			phases.Review:     "review",
			phases.Revision:   "revision",
			phases.Blocked:    "blocked",
			phases.Done:       "done",
		}
		seenTriggers := make(map[string]bool, len(r.Triggers))
		for j := range r.Triggers {
			tr := &r.Triggers[j]
			if tr.Status == "" {
				return fmt.Errorf("%s: trigger status is required", r.ObjectType)
			}
			if phase, bad := nonPickup[tr.Status]; bad {
				return fmt.Errorf("%s: %q is the agent's %s status and cannot be a pickup trigger", r.ObjectType, tr.Status, phase)
			}
			if schema != nil && len(schema.AllowedStatuses) > 0 && !slices.Contains(schema.AllowedStatuses, tr.Status) {
				return fmt.Errorf("%s: trigger status %q is not in the type's allowed statuses", r.ObjectType, tr.Status)
			}
			if seenTriggers[tr.Status] {
				return fmt.Errorf("%s: trigger status %q is declared more than once", r.ObjectType, tr.Status)
			}
			seenTriggers[tr.Status] = true
		}

		for j := range r.Transitions {
			t := &r.Transitions[j]
			if t.From == "" || t.To == "" {
				return fmt.Errorf("%s: transition edges require both from and to statuses", r.ObjectType)
			}
			if t.From == t.To {
				return fmt.Errorf("%s: transition cannot move %q to itself", r.ObjectType, t.From)
			}
			if schema != nil && len(schema.AllowedStatuses) > 0 {
				if !slices.Contains(schema.AllowedStatuses, t.From) || !slices.Contains(schema.AllowedStatuses, t.To) {
					return fmt.Errorf("%s: transition %q → %q uses a status outside the type's allowed statuses", r.ObjectType, t.From, t.To)
				}
			}
		}

		if schema != nil && len(schema.Fields) > 0 {
			for _, f := range r.Fields {
				if !slices.Contains(schema.Fields, f) {
					return fmt.Errorf("%s: writable field %q is not a declared property of the type", r.ObjectType, f)
				}
			}
		}
	}
	return nil
}

// ValidateWorkPickupForDefinition validates a reaction config against the
// definition's work rules. A definition with no rules keeps the legacy
// one-type/one-signal contract; a rules-based definition may declare multiple
// object types (the reaction config is derived from the rules).
func ValidateWorkPickupForDefinition(def *AgentDefinition, rc *ReactionConfig) error {
	if def == nil || def.WorkConfig.IsZero() {
		return ValidateWorkPickup(false, rc)
	}
	if len(def.WorkConfig.ObjectRules) == 0 {
		return ValidateWorkPickup(true, rc)
	}
	if err := ValidateWorkRules(def, nil); err != nil {
		return err
	}
	if rc == nil {
		return nil
	}
	if rc.ConcurrencyStrategy != ConcurrencySkip {
		return fmt.Errorf("work agent must set concurrencyStrategy: skip (got %q)", rc.ConcurrencyStrategy)
	}
	for _, e := range rc.Events {
		if e == EventTypeDeleted {
			return fmt.Errorf("work agent pickup signal cannot be deleted")
		}
	}
	return nil
}

// definitionForID resolves a definition id, or nil when absent/unresolvable.
func (h *Handler) definitionForID(ctx context.Context, definitionID *string) *AgentDefinition {
	if definitionID == nil || *definitionID == "" {
		return nil
	}
	def, err := h.repo.FindDefinitionByID(ctx, *definitionID, nil)
	if err != nil {
		return nil
	}
	return def
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
