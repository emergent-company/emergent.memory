package agents

import (
	"context"
	"fmt"
	"log/slog"
)

// forgetAgentSystemPromptTemplate is the base system prompt for the forget-agent.
// It is parameterised at runtime with cascade depth, strategy, and dry-run flag
// via buildForgetSystemPrompt.
const forgetAgentSystemPromptTemplate = `You are the forget-agent. Your job is to find and soft-delete graph objects
and relationships that match the user's query.

Soft-deletes are REVERSIBLE — deleted objects can be restored with entity-restore. Tell the
user this at the end of every run so they know the operation is safe.

NEVER call any tool not listed below.
ONLY use: search-hybrid, entity-query, entity-edges-get, entity-delete, relationship-delete, ask_user, entity-type-list.

---
STRATEGY: %s
%s

---
CASCADE DEPTH: %d
%s

---
DRY RUN: %v
%s

---
WORKFLOW

1. Use search-hybrid or entity-query to find entities matching the user's query.
   Use entity-type-list to understand available types if needed.

2. For each matched entity, apply cascade expansion:
   - depth=1: delete only the matched entities (direct).
   - depth=2: also expand 1-hop neighbors via entity-edges-get and include them.
   - depth=3: expand 2-hop neighbors (run entity-edges-get again on the 1-hop results).

3. Before deleting, pass reason="<brief reason from user query>" to entity-delete and
   relationship-delete so the tombstone records why the object was removed.

4. Delete relationships before entities to avoid dangling references.

5. After all deletes (or at the end of a dry run), report:
   - How many entities and relationships were (or would be) deleted.
   - Remind the user: soft-delete is REVERSIBLE — use entity-restore to undo.`

// buildForgetSystemPrompt returns the rendered system prompt for a forget-agent run.
func buildForgetSystemPrompt(cascadeDepth int, strategy string, dryRun bool) string {
	var strategyNote string
	switch strategy {
	case "confirm":
		strategyNote = "You MUST call ask_user before executing any batch of deletes. Present the list of entities/relationships to delete and wait for approval."
	case "ask":
		strategyNote = "Before each individual entity-delete or relationship-delete call, the system will pause for human confirmation (tool policy enforces this). Proceed one-by-one."
	default: // auto
		strategyNote = "Execute deletes automatically without asking for confirmation."
	}

	var cascadeNote string
	switch cascadeDepth {
	case 1:
		cascadeNote = "Direct matches only — do NOT expand to neighbors."
	case 2:
		cascadeNote = "Expand to 1-hop neighbors via entity-edges-get for each matched entity."
	case 3:
		cascadeNote = "Expand to 2-hop neighbors: run entity-edges-get on the direct matches, then again on the 1-hop results."
	default:
		cascadeNote = "Direct matches only."
	}

	var dryRunNote string
	if dryRun {
		dryRunNote = "THIS IS A DRY RUN. Do NOT call entity-delete or relationship-delete. Instead, report only what would be deleted."
	} else {
		dryRunNote = "This is a live run. Deletes will be executed."
	}

	return fmt.Sprintf(forgetAgentSystemPromptTemplate,
		strategy, strategyNote,
		cascadeDepth, cascadeNote,
		dryRun, dryRunNote,
	)
}

// buildForgetToolPolicies returns tool_policies for the given strategy.
//
//   - "confirm" — ask_user requires human confirmation (agent must ask before batch delete)
//   - "ask"     — entity-delete and relationship-delete each require per-call confirmation
//   - "auto" or anything else — no gates
func buildForgetToolPolicies(strategy string) map[string]ToolPolicy {
	switch strategy {
	case "confirm":
		return map[string]ToolPolicy{
			"ask_user": {
				Confirm: true,
				Message: "The forget-agent wants to call ask_user to present the list of items to delete. Do you approve?",
			},
		}
	case "ask":
		return map[string]ToolPolicy{
			"entity-delete": {
				Confirm: true,
				Message: "The forget-agent wants to delete this entity. Do you approve?",
			},
			"relationship-delete": {
				Confirm: true,
				Message: "The forget-agent wants to delete this relationship. Do you approve?",
			},
		}
	default:
		return map[string]ToolPolicy{}
	}
}

// buildForgetAgentDefinition constructs an AgentDefinition value (not yet persisted)
// for the given strategy and cascade depth. Used by EnsureForgetAgent and tests.
func buildForgetAgentDefinition(strategy string, cascadeDepth int) *AgentDefinition {
	temperature := float32(0.1)
	maxSteps := 60
	systemPrompt := buildForgetSystemPrompt(cascadeDepth, strategy, false)

	tools := []string{
		"search-hybrid",
		"entity-query",
		"entity-edges-get",
		"entity-delete",
		"relationship-delete",
		"ask_user",
		"entity-type-list",
	}

	return &AgentDefinition{
		Name:         "forget-agent",
		Description:  strPtr("Forget agent — finds and soft-deletes graph objects matching a query. Deletes are reversible."),
		SystemPrompt: &systemPrompt,
		Model: &ModelConfig{
			Temperature: &temperature,
		},
		Tools:        tools,
		Skills:       []string{},
		FlowType:     FlowTypeSingle,
		IsDefault:    false,
		MaxSteps:     &maxSteps,
		Visibility:   VisibilityProject,
		Config:       map[string]any{},
		ToolPolicies: buildForgetToolPolicies(strategy),
	}
}

// EnsureForgetAgent returns the forget-agent for the project, creating it if it does not
// exist yet. strategy controls delete confirmation behaviour:
//   - "auto"    — delete automatically without asking
//   - "confirm" — ask user before executing a batch of deletes
//   - "ask"     — require per-delete confirmation via tool policy
//
// Safe to call concurrently.
func (r *Repository) EnsureForgetAgent(ctx context.Context, projectID string, strategy string, cascadeDepth int) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "forget-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up forget-agent: %w", err)
	}

	if existing != nil {
		changed := false

		// Sync tool_policies based on strategy.
		desired := buildForgetToolPolicies(strategy)
		if existing.ToolPolicies == nil {
			existing.ToolPolicies = map[string]ToolPolicy{}
		}
		// Simple heuristic: if the policy sets differ in size or key presence, resync.
		if len(existing.ToolPolicies) != len(desired) {
			existing.ToolPolicies = desired
			changed = true
		}

		// Sync system prompt so improvements propagate.
		canonicalPrompt := buildForgetSystemPrompt(cascadeDepth, strategy, false)
		if existing.SystemPrompt == nil || *existing.SystemPrompt != canonicalPrompt {
			existing.SystemPrompt = &canonicalPrompt
			changed = true
		}

		// Sync canonical tools list.
		canonicalTools := []string{
			"search-hybrid",
			"entity-query",
			"entity-edges-get",
			"entity-delete",
			"relationship-delete",
			"ask_user",
			"entity-type-list",
		}
		if !sliceEq(existing.Tools, canonicalTools) {
			existing.Tools = canonicalTools
			changed = true
		}

		if changed {
			if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
				slog.Warn("forget-agent: failed to sync definition",
					"projectID", projectID,
					"error", updateErr,
				)
			}
		}
		return existing, nil
	}

	def := buildForgetAgentDefinition(strategy, cascadeDepth)
	def.ProjectID = projectID

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race: another caller inserted first — return that row.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, "forget-agent"); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create forget-agent: %w", err)
	}

	return def, nil
}
