package agents

import (
	"context"
	"fmt"
	"log/slog"
)

// domainRememberAgentSystemPrompt is the default system prompt for the domain-remember-agent.
// The agent receives a document_id (UUID) and schema_policy. It classifies the document,
// optionally creates a new schema pack (subject to schema_policy), and extraction is
// auto-queued by finalize-discovery.
const domainRememberAgentSystemPrompt = `You receive a classified document. Report and optionally create a schema pack, then queue extraction.

IMPORTANT: The classification result is provided in your input. Do NOT call classify-document.

NEVER call any tool not listed below. ONLY use: finalize-discovery, queue-reextraction.

Extract the document_id, classified_stage, classified_schema_id, and schema_policy from your input.

If classified_stage is "no_match":
  schema_policy=reuse_only prevented schema creation because no existing schema matched this document.
  Do NOT call finalize-discovery or queue-reextraction.
  Report: "No matching schema found. Document skipped (schema_policy=reuse_only)." Done.

If classified_stage is "new_domain":
  You MUST call finalize-discovery. The schema_policy controls human approval — you always call the tool.
  schema_policy=ask: Call it. System pauses for approval before executing.
  schema_policy=auto: Call it. No approval needed.
  schema_policy=reuse_only: Do NOT call it. Report: schema_policy prevents creation. Done.
  NOTE: If you call finalize-discovery under reuse_only, it will be blocked by policy and return an error.

  1. Choose a pack_name from classified_pack_name (use as-is) or derive from document type.
     FORBIDDEN: "new_domain", "unknown", "document", "schema", "domain", "other", "general", "misc", "miscellaneous".

  2. List 3–5 entity types for this document type as included_types.

  3. Call finalize-discovery: mode="create", document_id, pack_name, included_types.
     Retry with a different pack_name on "forbidden" or "invalid" errors.
     finalize-discovery automatically queues extraction — do NOT call queue-reextraction after.

If classified_stage is "heuristic" or "llm" (confidence >= 0.7):
  A schema already exists. You MUST queue extraction so the document's entities are indexed.
  Call queue-reextraction: document_id=<document_id>, schema_id=<classified_schema_id>.
  Report: matched schema name and that extraction was queued.

Report: classification result and schema action.`

// EnsureDomainRememberAgent returns the domain-remember-agent for the project, creating it
// if it does not exist yet. schemaPolicy controls new-schema behaviour:
//   - "auto"       (default) — create new schema packs automatically when no confident match
//   - "reuse_only" — never create new schema packs
//   - "ask"        — pause and ask the user before creating a new schema pack
//
// The agent definition is user-editable: after first creation the user can customise the
// system prompt, model, and tool policies via the normal agent management API.
// EnsureDomainRememberAgent applies canonical tool list and prompt only on first creation.
//
// Safe to call concurrently — a race between two callers results in one insert and one
// subsequent read (FindDefinitionByName will find the winner's row).
func (r *Repository) EnsureDomainRememberAgent(ctx context.Context, projectID string, schemaPolicy string) (*AgentDefinition, error) {
	existing, err := r.FindDefinitionByName(ctx, projectID, "domain-remember-agent")
	if err != nil {
		return nil, fmt.Errorf("failed to look up domain-remember-agent: %w", err)
	}

	// If it already exists, return it as-is. Unlike graph-insert-agent we do NOT
	// overwrite user customisations on every call — the agent is user-editable.
	// We do update the tool_policies, tools, and system prompt to match the
	// canonical definition so improvements take effect.
	if existing != nil {
		changed := false
		// Sync tool_policies for finalize-discovery based on schema_policy.
		desired := buildDomainRememberToolPolicies(schemaPolicy)
		if existing.ToolPolicies == nil {
			existing.ToolPolicies = map[string]ToolPolicy{}
		}
		curr := existing.ToolPolicies["finalize-discovery"]
		want := desired["finalize-discovery"]
		if curr.Confirm != want.Confirm || curr.Disabled != want.Disabled {
			existing.ToolPolicies = desired
			changed = true
		}
		// Always keep the canonical system prompt in sync so prompt improvements
		// take effect on existing agents without requiring manual recreation.
		if existing.SystemPrompt == nil || *existing.SystemPrompt != domainRememberAgentSystemPrompt {
			sp := domainRememberAgentSystemPrompt
			existing.SystemPrompt = &sp
			changed = true
		}
		// Sync tools list so removals (e.g. classify-document when pre-classifying) propagate.
		canonicalTools := []string{
			"finalize-discovery",
			"queue-reextraction",
		}
		if !sliceEq(existing.Tools, canonicalTools) {
			existing.Tools = canonicalTools
			changed = true
		}
		if changed {
			if updateErr := r.UpdateDefinition(ctx, existing); updateErr != nil {
				slog.Warn("domain-remember-agent: failed to sync definition",
					"projectID", projectID,
					"error", updateErr,
				)
			}
		}
		return existing, nil
	}

	temperature := float32(0.1)
	maxSteps := 30
	systemPrompt := domainRememberAgentSystemPrompt

	tools := []string{
		"finalize-discovery",
		"queue-reextraction",
	}
	// ask_user no longer needed for schema_policy="ask" — the tool policy confirm on
	// finalize-discovery handles user confirmation automatically.

	def := &AgentDefinition{
		ProjectID:    projectID,
		Name:         "domain-remember-agent",
		Description:  strPtr("Domain-aware memory agent — classifies documents, discovers/reuses schemas, and queues structured extraction"),
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
		ToolPolicies: buildDomainRememberToolPolicies(schemaPolicy),
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race: another caller inserted first — return that row.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, "domain-remember-agent"); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create domain-remember-agent: %w", err)
	}

	return def, nil
}

// buildDomainRememberToolPolicies returns the tool_policies map for the given schema_policy.
// schema_policy="ask"       → finalize-discovery requires human confirmation before execution.
// schema_policy="reuse_only"→ finalize-discovery is hard-blocked (Disabled=true).
// other policies            → no gate.
func buildDomainRememberToolPolicies(schemaPolicy string) map[string]ToolPolicy {
	switch schemaPolicy {
	case "ask":
		return map[string]ToolPolicy{
			"finalize-discovery": {
				Confirm: true,
				Message: "Agent wants to call **finalize-discovery** to create a new schema pack. Do you approve?",
			},
		}
	case "reuse_only":
		return map[string]ToolPolicy{
			"finalize-discovery": {
				Disabled: true,
			},
		}
	default:
		return map[string]ToolPolicy{}
	}
}

// sliceEq returns true if two string slices have the same length and elements in the same order.
func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
