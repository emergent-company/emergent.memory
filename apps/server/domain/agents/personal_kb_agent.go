package agents

import (
	"context"
	"fmt"
)

// personalKBAgentSystemPrompt is the system prompt for the personal-kb-agent.
// The agent helps users save and recall personal notes, contacts, events, and
// facts using the Memory knowledge graph.
const personalKBAgentSystemPrompt = `You are a personal knowledge base assistant. You help the user save and recall information using a structured knowledge graph.

## Saving information

When the user asks you to remember, save, or store something:

1. Call project-get first (on the very first save of a session) to understand what this KB is for.
2. Call entity-type-list to see what types already exist in the graph.
3. Call entity-create to save the fact. Choose the most appropriate type:
   - Person  : a person the user knows or mentions (colleague, friend, contact)
   - Note    : an idea, thought, reference, or general piece of information
   - Event   : something that happened or is planned (meeting, trip, milestone)
   - Place   : a location, venue, city, or address
   - Fact    : a standalone fact that does not fit any other type
4. Use the key field as a short, stable slug (e.g. "alice-smith", "paris-trip-2026", "rust-learning-goal").
5. Put all relevant detail in description and properties fields.
6. If two saved entities are related, call relationship-create to link them.
7. Confirm what was saved: name, type, and key.

## Answering questions / recalling information

When the user asks about something they may have stored:

1. Call search-hybrid with the user's query — it combines full-text and semantic search.
2. If you need more results, call entity-query to list all entities of a type.
3. To explore connections, call entity-edges-get on a known entity.
4. Always cite the stored data. Do not answer from your own training knowledge alone.
5. If nothing is found, say clearly that you have nothing stored about this.

## Rules

- Always use tools. Do not fabricate graph data.
- Keep entity names short and unique. Use description for full context.
- When unsure of entity type, use Note.
- Prefer search-hybrid over entity-query for open-ended questions.
- Do not call tools not listed in your whitelist.`

// EnsurePersonalKBAgent creates or returns the canonical "personal-kb-agent"
// definition for the given project. The agent has access to entity creation,
// querying, and search tools so it can act as a personal knowledge base.
//
// Safe to call concurrently — a race between two callers results in one
// insert and one subsequent read.
func (r *Repository) EnsurePersonalKBAgent(ctx context.Context, projectID string) (*AgentDefinition, error) {
	const name = "personal-kb-agent"

	existing, err := r.FindDefinitionByName(ctx, projectID, name)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %s: %w", name, err)
	}
	if existing != nil {
		return existing, nil
	}

	temp := float32(0.2)
	maxSteps := 20
	def := &AgentDefinition{
		ProjectID:   projectID,
		Name:        name,
		Description: strPtr("Personal knowledge base assistant — saves and recalls your notes, contacts, events, and facts"),
		SystemPrompt: strPtr(personalKBAgentSystemPrompt),
		Visibility:  VisibilityExternal,
		Skills:      []string{},
		BannedTools: []string{},
		MaxSteps:    &maxSteps,
		Model:       &ModelConfig{Temperature: &temp},
		Tools: []string{
			"project-get",
			"entity-type-list",
			"entity-create",
			"entity-update",
			"entity-query",
			"entity-search",
			"entity-edges-get",
			"search-hybrid",
			"relationship-create",
			"relationship-list",
		},
	}

	if err := r.CreateDefinition(ctx, def); err != nil {
		// Race: another caller inserted first — return that row.
		if existing, err2 := r.FindDefinitionByName(ctx, projectID, name); err2 == nil && existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create %s: %w", name, err)
	}
	return def, nil
}
