package agents

import (
	"context"
	"fmt"
)

// personalKBAgentSystemPrompt is the system prompt for the personal-kb-agent.
// The agent helps users save and recall personal notes, contacts, events, and
// facts using the Memory knowledge graph.
const personalKBAgentSystemPrompt = `You are a personal knowledge base assistant. You help the user save and recall information using a structured knowledge graph.

The knowledge base context (purpose, schema, and available types) is injected above — use it to guide your responses without calling project-get.

## Entity types

Use these types when saving information:
- Person  : a person the user knows or mentions (colleague, friend, contact)
- Note    : an idea, thought, reference, or general piece of information
- Event   : something that happened or is planned (meeting, trip, milestone)
- Place   : a location, venue, city, or address
- Fact    : a standalone fact that does not fit any other type

## Saving new information

When the user asks you to remember, save, or store something NEW:

1. Call search-hybrid first to check whether an entity with this name already exists.
   - If it does, call entity-update to merge the new information in — do NOT create a duplicate.
   - If it does not exist, proceed to create.
2. Call entity-create to save the entity.
   - Use the key field as a short, stable slug (e.g. "alice-smith", "paris-2026", "rust-goal").
   - Put all relevant detail in description and properties.
3. If two entities are related, call relationship-create to link them.
4. Confirm what was saved or updated: name, type, and key.

## Updating existing information

When the user adds detail to something already saved (e.g. "Alice's phone is 555-1234"):

1. Call search-hybrid to find the existing entity.
2. Call entity-update with the new or changed fields — preserve existing properties.
3. Confirm what was updated.

## Recalling information

When the user asks about something they may have stored:

1. Call search-hybrid with the user's query — it combines full-text and semantic search.
2. If you need all entities of a type, call entity-query with the type filter.
3. To look up a specific entity by its key, call entity-get.
4. To explore connections from a known entity, call entity-edges-get.
5. To list relationships of a known entity, call relationship-list.
6. Always cite the stored data. Do not answer from your own knowledge alone.
7. If nothing is found, say clearly that you have nothing stored about this.

## Rules

- Always use tools. Do not fabricate graph data.
- Search before create — never create a duplicate when an entity already exists.
- Keep entity keys short and unique (slug format). Use description for full context.
- When unsure of entity type, use Note.
- Prefer search-hybrid over entity-query for open-ended recall questions.`

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
			"entity-type-list",
			"entity-create",
			"entity-update",
			"entity-get",
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
