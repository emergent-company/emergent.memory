// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/pkg/adk"
)

// RelationshipBuilderConfig holds configuration for the relationship builder agent.
type RelationshipBuilderConfig struct {
	Model          model.LLM
	GenerateConfig *genai.GenerateContentConfig
	OutputSchema   *genai.Schema // Optional: dynamic schema from template pack. Falls back to RelationshipExtractionSchema().
	OutputKey      string
	Logger         *slog.Logger
	TraceLogger    TraceLogger
}

// NewRelationshipBuilderAgent creates an ADK agent for relationship extraction.
//
// The agent uses structured output (OutputSchema) to ensure valid JSON.
// Extracted relationships are stored in session state under the OutputKey.
//
// Required session state:
// - extracted_entities: []InternalEntity - entities extracted in previous step
// - document_text: string - the document text for context
//
// Optional session state:
// - relationship_schemas: map[string]RelationshipSchema - relationship type definitions
// - orphan_temp_ids: []string - entity temp_ids that need relationships (for retry)
func NewRelationshipBuilderAgent(cfg RelationshipBuilderConfig) (agent.Agent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	outputKey := cfg.OutputKey
	if outputKey == "" {
		outputKey = "extracted_relationships"
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	traceLogger := cfg.TraceLogger

	outputSchema := cfg.OutputSchema
	if outputSchema == nil {
		outputSchema = RelationshipExtractionSchema()
	}

	agentCfg := llmagent.Config{
		Name:        "RelationshipBuilder",
		Description: "Builds relationships between extracted entities based on document context",

		Model:                 cfg.Model,
		GenerateContentConfig: cfg.GenerateConfig,
		OutputSchema:          outputSchema,
		OutputKey:             outputKey,

		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			state := ctx.ReadonlyState()

			// Get extracted entities (required)
			entitiesRaw, err := state.Get("extracted_entities")
			if err != nil {
				return "", fmt.Errorf("extracted_entities is required in session state: %w", err)
			}

			var entities []InternalEntity
			switch e := entitiesRaw.(type) {
			case []InternalEntity:
				entities = e
			case []any:
				// Convert from generic slice
				for _, item := range e {
					if data, err := json.Marshal(item); err == nil {
						var entity InternalEntity
						if err := json.Unmarshal(data, &entity); err == nil {
							entities = append(entities, entity)
						}
					}
				}
			default:
				return "", fmt.Errorf("extracted_entities must be []InternalEntity, got %T", entitiesRaw)
			}

			if len(entities) == 0 {
				return "", fmt.Errorf("no entities to build relationships from")
			}

			// Get document text (required)
			documentTextRaw, err := state.Get("document_text")
			if err != nil {
				return "", fmt.Errorf("document_text is required in session state: %w", err)
			}
			documentText, ok := documentTextRaw.(string)
			if !ok || documentText == "" {
				return "", fmt.Errorf("document_text must be a non-empty string")
			}

			// Get optional parameters
			relationshipSchemas := make(map[string]RelationshipSchema)
			if schemasRaw, err := state.Get("relationship_schemas"); err == nil {
				if schemas, ok := schemasRaw.(map[string]RelationshipSchema); ok {
					relationshipSchemas = schemas
				} else if schemasMap, ok := schemasRaw.(map[string]any); ok {
					// Convert from generic map
					for k, v := range schemasMap {
						if schemaMap, ok := v.(map[string]any); ok {
							relationshipSchemas[k] = convertToRelationshipSchema(schemaMap)
						}
					}
				}
			}

			var existingEntities []ExistingEntityContext
			if entitiesRaw, err := state.Get("existing_entities"); err == nil {
				if ents, ok := entitiesRaw.([]ExistingEntityContext); ok {
					existingEntities = ents
				}
			}

			var orphanTempIDs []string
			if orphansRaw, err := state.Get("orphan_temp_ids"); err == nil {
				if orphans, ok := orphansRaw.([]string); ok {
					orphanTempIDs = orphans
				}
			}

			// Build the relationship prompt
			prompt := BuildRelationshipPrompt(
				entities,
				relationshipSchemas,
				documentText,
				existingEntities,
				orphanTempIDs,
			)

			log.Debug("built relationship prompt",
				slog.Int("prompt_length", len(prompt)),
				slog.Int("entity_count", len(entities)),
				slog.Int("schema_count", len(relationshipSchemas)),
				slog.Int("orphan_count", len(orphanTempIDs)),
			)

			if traceLogger != nil {
				traceLogger.LogStageStart("RELATIONSHIP EXTRACTION")
				traceLogger.LogPrompt("RelationshipBuilder", prompt)
			}

			return prompt, nil
		},
	}

	return llmagent.New(agentCfg)
}

// RelationshipBuilderFactory creates relationship builder agents with a shared model factory.
type RelationshipBuilderFactory struct {
	modelFactory *adk.ModelFactory
	log          *slog.Logger
}

// NewRelationshipBuilderFactory creates a new factory for relationship builder agents.
func NewRelationshipBuilderFactory(modelFactory *adk.ModelFactory, log *slog.Logger) *RelationshipBuilderFactory {
	return &RelationshipBuilderFactory{
		modelFactory: modelFactory,
		log:          log,
	}
}

// CreateAgent creates a new relationship builder agent with default (static) schema.
func (f *RelationshipBuilderFactory) CreateAgent(ctx context.Context) (agent.Agent, error) {
	return f.CreateAgentWithSchema(ctx, nil)
}

// CreateAgentWithSchema creates a relationship builder agent with a dynamic schema from template pack.
// If relationshipSchemas is nil or empty, falls back to the static RelationshipExtractionSchema().
func (f *RelationshipBuilderFactory) CreateAgentWithSchema(ctx context.Context, relationshipSchemas map[string]RelationshipSchema) (agent.Agent, error) {
	llm, err := f.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	var outputSchema *genai.Schema
	if len(relationshipSchemas) > 0 {
		outputSchema = BuildRelationshipSchemaFromTemplatePack(relationshipSchemas)
	}

	return NewRelationshipBuilderAgent(RelationshipBuilderConfig{
		Model:          llm,
		GenerateConfig: f.modelFactory.ExtractionGenerateConfig(),
		OutputSchema:   outputSchema,
		Logger:         f.log,
	})
}

// ParseRelationshipExtractionOutput parses the relationship extraction output from session state.
func ParseRelationshipExtractionOutput(output any) (*RelationshipExtractionOutput, error) {
	if output == nil {
		return nil, fmt.Errorf("output is nil")
	}

	// If it's already the right type, return it
	if result, ok := output.(*RelationshipExtractionOutput); ok {
		return result, nil
	}

	// Try to parse as JSON string
	if str, ok := output.(string); ok {
		str = strings.TrimSpace(str)
		// Remove markdown code blocks if present
		str = strings.TrimPrefix(str, "```json")
		str = strings.TrimPrefix(str, "```")
		str = strings.TrimSuffix(str, "```")
		str = strings.TrimSpace(str)

		var result RelationshipExtractionOutput
		if err := json.Unmarshal([]byte(str), &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		return &result, nil
	}

	// Try to marshal and unmarshal for type conversion
	data, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal output: %w", err)
	}

	var result RelationshipExtractionOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal output: %w", err)
	}

	return &result, nil
}

// convertToRelationshipSchema converts a generic map to RelationshipSchema.
func convertToRelationshipSchema(m map[string]any) RelationshipSchema {
	schema := RelationshipSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if st, ok := m["source_types"].([]any); ok {
		for _, t := range st {
			if s, ok := t.(string); ok {
				schema.SourceTypes = append(schema.SourceTypes, s)
			}
		}
	}
	if tt, ok := m["target_types"].([]any); ok {
		for _, t := range tt {
			if s, ok := t.(string); ok {
				schema.TargetTypes = append(schema.TargetTypes, s)
			}
		}
	}
	if g, ok := m["extraction_guidelines"].(string); ok {
		schema.ExtractionGuidelines = g
	}

	return schema
}
