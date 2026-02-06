// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"fmt"
	"iter"
	"log/slog"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// QualityCheckerConfig holds configuration for the quality checker agent.
type QualityCheckerConfig struct {
	// OrphanThreshold is the max acceptable orphan rate (0.0-1.0).
	// Default is 0.3 (30% orphans allowed).
	OrphanThreshold float64

	// Logger for debug output.
	Logger *slog.Logger
}

// NewQualityCheckerAgent creates an agent that checks extraction quality.
// It calculates the orphan rate (entities not in any relationship).
// If quality passes, it escalates to break the loop.
// If quality fails, it stores orphan IDs in state for retry.
func NewQualityCheckerAgent(cfg QualityCheckerConfig) (agent.Agent, error) {
	threshold := cfg.OrphanThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default 30%
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	return agent.New(agent.Config{
		Name:        "QualityChecker",
		Description: "Checks extraction quality and decides whether to retry relationship building",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				state := ctx.Session().State()

				// Get entities and relationships from state
				entities, err := getEntitiesFromState(state)
				if err != nil {
					yield(nil, fmt.Errorf("failed to get entities: %w", err))
					return
				}

				relationships, err := getRelationshipsFromState(state)
				if err != nil {
					// No relationships yet is OK on first iteration
					relationships = nil
				}

				// Calculate orphan rate
				orphanRate := CalculateOrphanRate(entities, relationships)
				orphanIDs := GetOrphanTempIDs(entities, relationships)

				log.Info("quality check",
					slog.Float64("orphan_rate", orphanRate),
					slog.Float64("threshold", threshold),
					slog.Int("orphan_count", len(orphanIDs)),
					slog.Int("entity_count", len(entities)),
					slog.Int("relationship_count", len(relationships)),
				)

				// Get current iteration from state
				iteration := 1
				if iterRaw, err := state.Get("retry_iteration"); err == nil {
					if i, ok := iterRaw.(int); ok {
						iteration = i
					}
				}

				// Create the event
				event := session.NewEvent(ctx.InvocationID())
				event.Author = "QualityChecker"

				if orphanRate <= threshold {
					// Quality passes - escalate to break the loop
					log.Info("quality check passed, escalating",
						slog.Float64("orphan_rate", orphanRate),
					)
					event.Content = genai.NewContentFromText(
						fmt.Sprintf("Quality check passed. Orphan rate: %.2f%% (threshold: %.2f%%)",
							orphanRate*100, threshold*100), "model")
					event.Actions.Escalate = true

					// Clear retry state
					event.Actions.StateDelta = map[string]any{
						"quality_passed": true,
						"final_orphan_rate": orphanRate,
					}
				} else {
					// Quality fails - store orphan IDs for retry
					log.Info("quality check failed, will retry",
						slog.Float64("orphan_rate", orphanRate),
						slog.Int("iteration", iteration),
					)
					event.Content = genai.NewContentFromText(
						fmt.Sprintf("Quality check failed. Orphan rate: %.2f%% (threshold: %.2f%%). Retry iteration %d.",
							orphanRate*100, threshold*100, iteration+1), "model")

					// Store state for retry
					event.Actions.StateDelta = map[string]any{
						"orphan_temp_ids":   orphanIDs,
						"retry_iteration":  iteration + 1,
						"quality_passed":   false,
						"last_orphan_rate": orphanRate,
					}
				}

				yield(event, nil)
			}
		},
	})
}

// getEntitiesFromState retrieves entities from session state.
func getEntitiesFromState(state session.State) ([]InternalEntity, error) {
	raw, err := state.Get("extracted_entities")
	if err != nil {
		return nil, err
	}

	switch v := raw.(type) {
	case []InternalEntity:
		return v, nil
	case []any:
		var entities []InternalEntity
		for _, item := range v {
			if e, ok := item.(InternalEntity); ok {
				entities = append(entities, e)
			} else if m, ok := item.(map[string]any); ok {
				entity := InternalEntity{
					TempID:      getString(m, "temp_id"),
					Name:        getString(m, "name"),
					Type:        getString(m, "type"),
					Description: getString(m, "description"),
				}
				if entity.TempID != "" {
					entities = append(entities, entity)
				}
			}
		}
		return entities, nil
	default:
		return nil, fmt.Errorf("unexpected type for entities: %T", raw)
	}
}

// getRelationshipsFromState retrieves relationships from session state.
func getRelationshipsFromState(state session.State) ([]ExtractedRelationship, error) {
	raw, err := state.Get("extracted_relationships")
	if err != nil {
		return nil, err
	}

	switch v := raw.(type) {
	case []ExtractedRelationship:
		return v, nil
	case *RelationshipExtractionOutput:
		return v.Relationships, nil
	case []any:
		var rels []ExtractedRelationship
		for _, item := range v {
			if r, ok := item.(ExtractedRelationship); ok {
				rels = append(rels, r)
			} else if m, ok := item.(map[string]any); ok {
				rel := ExtractedRelationship{
					SourceRef:   getString(m, "source_ref"),
					TargetRef:   getString(m, "target_ref"),
					Type:        getString(m, "type"),
					Description: getString(m, "description"),
				}
				if rel.SourceRef != "" && rel.TargetRef != "" {
					rels = append(rels, rel)
				}
			}
		}
		return rels, nil
	default:
		return nil, fmt.Errorf("unexpected type for relationships: %T", raw)
	}
}

// getString safely gets a string value from a map.
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
