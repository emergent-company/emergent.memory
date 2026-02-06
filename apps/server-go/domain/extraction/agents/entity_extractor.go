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

// EntityExtractorConfig holds configuration for the entity extractor agent.
type EntityExtractorConfig struct {
	Model          model.LLM
	GenerateConfig *genai.GenerateContentConfig
	OutputSchema   *genai.Schema // Optional: dynamic schema from template pack. Falls back to EntityExtractionSchema().
	OutputKey      string
	Logger         *slog.Logger
	TraceLogger    TraceLogger
}

// NewEntityExtractorAgent creates an ADK agent for entity extraction.
//
// The agent uses structured output (OutputSchema) to ensure valid JSON.
// Extracted entities are stored in session state under the OutputKey.
func NewEntityExtractorAgent(cfg EntityExtractorConfig) (agent.Agent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	outputKey := cfg.OutputKey
	if outputKey == "" {
		outputKey = "extracted_entities"
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	traceLogger := cfg.TraceLogger

	outputSchema := cfg.OutputSchema
	if outputSchema == nil {
		outputSchema = EntityExtractionSchema()
	}

	agentCfg := llmagent.Config{
		Name:        "EntityExtractor",
		Description: "Extracts entities from document text with type-specific properties",

		Model:                 cfg.Model,
		GenerateContentConfig: cfg.GenerateConfig,
		OutputSchema:          outputSchema,
		OutputKey:             outputKey,

		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			state := ctx.ReadonlyState()

			documentTextRaw, err := state.Get("document_text")
			if err != nil {
				return "", fmt.Errorf("document_text is required in session state: %w", err)
			}
			documentText, ok := documentTextRaw.(string)
			if !ok || documentText == "" {
				return "", fmt.Errorf("document_text must be a non-empty string")
			}

			// Get optional parameters
			objectSchemas := make(map[string]ObjectSchema)
			if schemasRaw, err := state.Get("object_schemas"); err == nil {
				if schemas, ok := schemasRaw.(map[string]ObjectSchema); ok {
					objectSchemas = schemas
				} else if schemasMap, ok := schemasRaw.(map[string]any); ok {
					// Convert from generic map
					for k, v := range schemasMap {
						if schemaMap, ok := v.(map[string]any); ok {
							objectSchemas[k] = convertToObjectSchema(schemaMap)
						}
					}
				}
			}

			var allowedTypes []string
			if typesRaw, err := state.Get("allowed_types"); err == nil {
				if types, ok := typesRaw.([]string); ok {
					allowedTypes = types
				}
			}

			var existingEntities []ExistingEntityContext
			if entitiesRaw, err := state.Get("existing_entities"); err == nil {
				if entities, ok := entitiesRaw.([]ExistingEntityContext); ok {
					existingEntities = entities
				}
			}

			// Build the extraction prompt
			prompt := BuildEntityExtractionPrompt(
				documentText,
				objectSchemas,
				allowedTypes,
				existingEntities,
			)

			log.Debug("built entity extraction prompt",
				slog.Int("prompt_length", len(prompt)),
				slog.Int("document_length", len(documentText)),
				slog.Int("schema_count", len(objectSchemas)),
				slog.Int("existing_entity_count", len(existingEntities)),
			)

			if traceLogger != nil {
				traceLogger.LogStageStart("ENTITY EXTRACTION")
				traceLogger.LogPrompt("EntityExtractor", prompt)
			}

			return prompt, nil
		},
	}

	return llmagent.New(agentCfg)
}

// EntityExtractorFactory creates entity extractor agents with a shared model factory.
type EntityExtractorFactory struct {
	modelFactory *adk.ModelFactory
	log          *slog.Logger
}

// NewEntityExtractorFactory creates a new factory for entity extractor agents.
func NewEntityExtractorFactory(modelFactory *adk.ModelFactory, log *slog.Logger) *EntityExtractorFactory {
	return &EntityExtractorFactory{
		modelFactory: modelFactory,
		log:          log,
	}
}

// CreateAgent creates a new entity extractor agent with default (static) schema.
func (f *EntityExtractorFactory) CreateAgent(ctx context.Context) (agent.Agent, error) {
	return f.CreateAgentWithSchema(ctx, nil)
}

// CreateAgentWithSchema creates an entity extractor agent with a dynamic schema from template pack.
// If objectSchemas is nil or empty, falls back to the static EntityExtractionSchema().
func (f *EntityExtractorFactory) CreateAgentWithSchema(ctx context.Context, objectSchemas map[string]ObjectSchema) (agent.Agent, error) {
	llm, err := f.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	var outputSchema *genai.Schema
	if len(objectSchemas) > 0 {
		outputSchema = BuildEntitySchemaFromTemplatePack(objectSchemas)
	}

	return NewEntityExtractorAgent(EntityExtractorConfig{
		Model:          llm,
		GenerateConfig: f.modelFactory.ExtractionGenerateConfig(),
		OutputSchema:   outputSchema,
		Logger:         f.log,
	})
}

// ParseEntityExtractionOutput parses the entity extraction output from session state.
func ParseEntityExtractionOutput(output any) (*EntityExtractionOutput, error) {
	if output == nil {
		return nil, fmt.Errorf("output is nil")
	}

	// If it's already the right type, return it
	if result, ok := output.(*EntityExtractionOutput); ok {
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

		var result EntityExtractionOutput
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

	var result EntityExtractionOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal output: %w", err)
	}

	return &result, nil
}

// convertToObjectSchema converts a generic map to ObjectSchema.
func convertToObjectSchema(m map[string]any) ObjectSchema {
	schema := ObjectSchema{}

	if name, ok := m["name"].(string); ok {
		schema.Name = name
	}
	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}
	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]PropertyDef)
		for k, v := range props {
			if propMap, ok := v.(map[string]any); ok {
				pd := PropertyDef{}
				if t, ok := propMap["type"].(string); ok {
					pd.Type = t
				}
				if d, ok := propMap["description"].(string); ok {
					pd.Description = d
				}
				schema.Properties[k] = pd
			}
		}
	}
	if req, ok := m["required"].([]any); ok {
		schema.Required = make([]string, 0, len(req))
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}
