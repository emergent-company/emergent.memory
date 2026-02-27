// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent/pkg/adk"
	"github.com/emergent-company/emergent/pkg/tracing"
)

// ExtractionPipelineConfig holds configuration for the extraction pipeline.
type ExtractionPipelineConfig struct {
	// ModelFactory creates LLM models for the agents.
	ModelFactory *adk.ModelFactory

	// ObjectSchemas defines the entity types for dynamic ResponseSchema.
	// If provided, the pipeline will use these to build type-constrained schemas.
	ObjectSchemas map[string]ObjectSchema

	// RelationshipSchemas defines the relationship types for dynamic ResponseSchema.
	// If provided, the pipeline will use these to build type-constrained schemas.
	RelationshipSchemas map[string]RelationshipSchema

	// OrphanThreshold is the max acceptable orphan rate (0.0-1.0).
	// Default is 0.3 (30% orphans allowed).
	OrphanThreshold float64

	// MaxRetries is the max number of relationship extraction retries.
	// Default is 3.
	MaxRetries uint

	// Logger for debug output.
	Logger *slog.Logger

	// TraceLogger for detailed extraction logging.
	// If nil, no tracing is performed.
	TraceLogger TraceLogger
}

// ExtractionPipelineInput is the input for running the extraction pipeline.
type ExtractionPipelineInput struct {
	// DocumentText is the document text to extract from.
	DocumentText string

	// ObjectSchemas defines the entity types and their properties.
	ObjectSchemas map[string]ObjectSchema

	// RelationshipSchemas defines the relationship types.
	RelationshipSchemas map[string]RelationshipSchema

	// AllowedTypes limits extraction to specific entity types (optional).
	AllowedTypes []string

	// ExistingEntities provides context for identity resolution (optional).
	ExistingEntities []ExistingEntityContext
}

// ExtractionPipelineOutput is the result of running the extraction pipeline.
type ExtractionPipelineOutput struct {
	// Entities are the extracted entities with temp_ids.
	Entities []InternalEntity

	// Relationships are the extracted relationships.
	Relationships []ExtractedRelationship
}

// ExtractionPipeline orchestrates entity and relationship extraction using ADK agents.
type ExtractionPipeline struct {
	modelFactory        *adk.ModelFactory
	objectSchemas       map[string]ObjectSchema
	relationshipSchemas map[string]RelationshipSchema
	orphanThreshold     float64
	maxRetries          uint
	log                 *slog.Logger
	traceLogger         TraceLogger
}

// NewExtractionPipeline creates a new extraction pipeline.
func NewExtractionPipeline(cfg ExtractionPipelineConfig) (*ExtractionPipeline, error) {
	if cfg.ModelFactory == nil {
		return nil, fmt.Errorf("model factory is required")
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	orphanThreshold := cfg.OrphanThreshold
	if orphanThreshold <= 0 {
		orphanThreshold = 0.3 // Default 30%
	}

	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	return &ExtractionPipeline{
		modelFactory:        cfg.ModelFactory,
		objectSchemas:       cfg.ObjectSchemas,
		relationshipSchemas: cfg.RelationshipSchemas,
		orphanThreshold:     orphanThreshold,
		maxRetries:          maxRetries,
		log:                 log,
		traceLogger:         cfg.TraceLogger,
	}, nil
}

// Run executes the extraction pipeline on the given input.
func (p *ExtractionPipeline) Run(ctx context.Context, input ExtractionPipelineInput) (*ExtractionPipelineOutput, error) {
	if input.DocumentText == "" {
		return nil, fmt.Errorf("document text is required")
	}

	p.log.Info("starting extraction pipeline",
		slog.Int("document_length", len(input.DocumentText)),
		slog.Int("schema_count", len(input.ObjectSchemas)),
	)

	if p.traceLogger != nil {
		p.traceLogger.LogStageStart("PIPELINE INITIALIZATION")
		p.traceLogger.LogSchemas(input.ObjectSchemas, input.RelationshipSchemas)
		p.traceLogger.LogDocumentText(input.DocumentText)
	}

	// Create the LLM model
	llm, err := p.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	// Create the sequential pipeline agent
	pipelineAgent, err := p.createPipelineAgent(llm)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline agent: %w", err)
	}

	// Create initial state map
	initialState := map[string]any{
		"document_text": input.DocumentText,
	}
	if len(input.ObjectSchemas) > 0 {
		initialState["object_schemas"] = input.ObjectSchemas
	}
	if len(input.RelationshipSchemas) > 0 {
		initialState["relationship_schemas"] = input.RelationshipSchemas
	}
	if len(input.AllowedTypes) > 0 {
		initialState["allowed_types"] = input.AllowedTypes
	}
	if len(input.ExistingEntities) > 0 {
		initialState["existing_entities"] = input.ExistingEntities
	}

	// Create a session with initial state
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "extraction",
		UserID:  "system",
		State:   initialState,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	sess := createResp.Session

	// Create a runner and execute
	r, err := runner.New(runner.Config{
		Agent:          pipelineAgent,
		SessionService: sessionService,
		AppName:        "extraction",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// Run the pipeline with an empty user message (agents use state)
	userMessage := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText("Extract entities and relationships from the document.")},
	}

	var events []*session.Event
	for event, err := range r.Run(ctx, "system", sess.ID(), userMessage, agent.RunConfig{}) {
		if err != nil {
			if p.traceLogger != nil {
				p.traceLogger.LogError("PIPELINE EXECUTION", err)
			}
			return nil, fmt.Errorf("pipeline execution failed: %w", err)
		}
		events = append(events, event)

		if p.traceLogger != nil && event != nil {
			p.logEvent(event)
		}
	}

	p.log.Debug("pipeline completed", slog.Int("event_count", len(events)))

	// Fetch the updated session to get the final state
	getResp, err := sessionService.Get(ctx, &session.GetRequest{
		AppName:   "extraction",
		UserID:    "system",
		SessionID: sess.ID(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Extract results from session state
	result, err := p.extractResults(getResp.Session)
	if err != nil {
		return nil, err
	}

	if p.traceLogger != nil {
		p.traceLogger.LogStageStart("FINAL RESULTS")
		p.traceLogger.LogEntities(result.Entities)
		p.traceLogger.LogRelationships(result.Relationships)
		orphanRate := CalculateOrphanRate(result.Entities, result.Relationships)
		orphanIDs := GetOrphanTempIDs(result.Entities, result.Relationships)
		p.traceLogger.LogQualityCheck(0, orphanRate, p.orphanThreshold, orphanIDs)
	}

	return result, nil
}

func (p *ExtractionPipeline) logEvent(event *session.Event) {
	if event.Content == nil {
		return
	}

	var contentText string
	for _, part := range event.Content.Parts {
		if part.Text != "" {
			contentText += part.Text
		}
	}

	p.traceLogger.LogEvent("ADK_EVENT", event.Author, contentText)
}

// createPipelineAgent creates the sequential pipeline agent.
func (p *ExtractionPipeline) createPipelineAgent(llm model.LLM) (agent.Agent, error) {
	var entityGenerateConfig *genai.GenerateContentConfig
	if len(p.objectSchemas) > 0 {
		entitySchema := BuildEntitySchemaFromTemplatePack(p.objectSchemas)
		entityGenerateConfig = p.modelFactory.ExtractionGenerateConfigWithSchema(entitySchema)
		p.log.Debug("using dynamic entity schema with type constraints",
			slog.Int("type_count", len(p.objectSchemas)))
	} else {
		entityGenerateConfig = p.modelFactory.ExtractionGenerateConfig()
	}

	entityExtractor, err := NewEntityExtractorAgent(EntityExtractorConfig{
		Model:          llm,
		GenerateConfig: entityGenerateConfig,
		OutputKey:      "extracted_entities_raw",
		Logger:         p.log,
		TraceLogger:    p.traceLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create entity extractor: %w", err)
	}

	entityProcessor, err := p.createEntityProcessorAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to create entity processor: %w", err)
	}

	var relationshipGenerateConfig *genai.GenerateContentConfig
	if len(p.relationshipSchemas) > 0 {
		relationshipSchema := BuildRelationshipSchemaFromTemplatePack(p.relationshipSchemas)
		relationshipGenerateConfig = p.modelFactory.ExtractionGenerateConfigWithSchema(relationshipSchema)
		p.log.Debug("using dynamic relationship schema with type constraints",
			slog.Int("type_count", len(p.relationshipSchemas)))
	} else {
		relationshipGenerateConfig = p.modelFactory.ExtractionGenerateConfig()
	}

	relationshipBuilder, err := NewRelationshipBuilderAgent(RelationshipBuilderConfig{
		Model:          llm,
		GenerateConfig: relationshipGenerateConfig,
		OutputKey:      "extracted_relationships",
		Logger:         p.log,
		TraceLogger:    p.traceLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship builder: %w", err)
	}

	qualityChecker, err := NewQualityCheckerAgent(QualityCheckerConfig{
		OrphanThreshold: p.orphanThreshold,
		Logger:          p.log,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create quality checker: %w", err)
	}

	relationshipProcessor, err := p.createRelationshipProcessorAgent()
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship processor: %w", err)
	}

	relationshipWithQuality, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "RelationshipWithQuality",
			Description: "Builds relationships, processes output, and checks quality",
			SubAgents:   []agent.Agent{relationshipBuilder, relationshipProcessor, qualityChecker},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship with quality agent: %w", err)
	}

	relationshipLoop, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:        "RelationshipRetryLoop",
			Description: "Retries relationship building until quality threshold is met",
			SubAgents:   []agent.Agent{relationshipWithQuality},
		},
		MaxIterations: p.maxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create relationship loop agent: %w", err)
	}

	return sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "ExtractionPipeline",
			Description: "Extracts entities and relationships from documents",
			SubAgents:   []agent.Agent{entityExtractor, entityProcessor, relationshipLoop},
		},
	})
}

// createEntityProcessorAgent creates an agent that processes raw entities to add temp_ids.
func (p *ExtractionPipeline) createEntityProcessorAgent() (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        "EntityProcessor",
		Description: "Processes extracted entities to add temp_ids for relationship building",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				_, span := tracing.Start(ctx, "extraction.pipeline.extract_entities")
				defer span.End()

				state := ctx.Session().State()

				// Get raw extracted entities
				rawEntitiesAny, err := state.Get("extracted_entities_raw")
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					yield(nil, fmt.Errorf("no extracted entities found: %w", err))
					return
				}

				p.log.Debug("raw entity output",
					slog.String("type", fmt.Sprintf("%T", rawEntitiesAny)),
				)

				// Parse the raw output
				var entities []ExtractedEntity
				switch v := rawEntitiesAny.(type) {
				case string:
					p.log.Debug("raw entity string",
						slog.Int("length", len(v)),
						slog.String("first_500", v[:min(500, len(v))]),
						slog.String("last_200", v[max(0, len(v)-200):]),
					)
					output, err := ParseEntityExtractionOutput(v)
					if err != nil {
						yield(nil, fmt.Errorf("failed to parse entity output: %w", err))
						return
					}
					entities = output.Entities
				case *EntityExtractionOutput:
					entities = v.Entities
				case map[string]any:
					// Try to extract entities array from map
					if entArr, ok := v["entities"].([]any); ok {
						for _, e := range entArr {
							data, _ := json.Marshal(e)
							var entity ExtractedEntity
							if err := json.Unmarshal(data, &entity); err == nil {
								entities = append(entities, entity)
							}
						}
					}
				default:
					data, _ := json.Marshal(v)
					output, err := ParseEntityExtractionOutput(string(data))
					if err != nil {
						yield(nil, fmt.Errorf("failed to parse entity output from %T: %w", v, err))
						return
					}
					entities = output.Entities
				}

				// Generate temp_ids for entities
				existingIDs := make(map[string]bool)
				internalEntities := make([]InternalEntity, 0, len(entities))

				for _, e := range entities {
					tempID := generateTempID(e.Name, e.Type, existingIDs)
					existingIDs[tempID] = true

					internalEntities = append(internalEntities, InternalEntity{
						TempID:           tempID,
						Name:             e.Name,
						Type:             e.Type,
						Description:      e.Description,
						Properties:       e.Properties,
						Action:           e.Action,
						ExistingEntityID: e.ExistingEntityID,
					})
				}

				p.log.Debug("processed entities",
					slog.Int("count", len(internalEntities)),
				)

				span.SetAttributes(attribute.Int("emergent.extraction.entity_count", len(internalEntities)))

				// Use state.Set() for immediate visibility to subsequent agents
				if err := state.Set("extracted_entities", internalEntities); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					yield(nil, fmt.Errorf("failed to set extracted_entities in state: %w", err))
					return
				}

				// Also use StateDelta for persistence to session storage (needed by extractResults)
				event := session.NewEvent(ctx.InvocationID())
				event.Author = "EntityProcessor"
				event.Content = genai.NewContentFromText(fmt.Sprintf("Processed %d entities with temp_ids", len(internalEntities)), "model")
				event.Actions.StateDelta = map[string]any{
					"extracted_entities": internalEntities,
				}
				span.SetStatus(codes.Ok, "")
				yield(event, nil)
			}
		},
	})
}

// extractResults extracts the final results from session state.
func (p *ExtractionPipeline) extractResults(sess session.Session) (*ExtractionPipelineOutput, error) {
	state := sess.State()

	p.log.Debug("extracting results from session state")

	// Get extracted entities
	var entities []InternalEntity
	if entitiesRaw, err := state.Get("extracted_entities"); err == nil {
		p.log.Debug("found extracted_entities in state", slog.String("type", fmt.Sprintf("%T", entitiesRaw)))
		switch v := entitiesRaw.(type) {
		case []InternalEntity:
			entities = v
		default:
			data, _ := json.Marshal(v)
			p.log.Debug("unmarshaling entities", slog.String("json", string(data)))
			if err := json.Unmarshal(data, &entities); err != nil {
				p.log.Warn("failed to parse entities from state", slog.String("error", err.Error()))
			}
		}
	} else {
		p.log.Warn("extracted_entities not found in state", slog.String("error", err.Error()))
	}

	// Get extracted relationships
	var relationships []ExtractedRelationship
	if relsRaw, err := state.Get("extracted_relationships"); err == nil {
		switch v := relsRaw.(type) {
		case []ExtractedRelationship:
			relationships = v
		case *RelationshipExtractionOutput:
			relationships = v.Relationships
		case string:
			output, err := ParseRelationshipExtractionOutput(v)
			if err == nil {
				relationships = output.Relationships
			}
		default:
			data, _ := json.Marshal(v)
			var output RelationshipExtractionOutput
			if err := json.Unmarshal(data, &output); err == nil {
				relationships = output.Relationships
			}
		}
	}

	p.log.Info("extraction pipeline completed",
		slog.Int("entity_count", len(entities)),
		slog.Int("relationship_count", len(relationships)),
	)

	return &ExtractionPipelineOutput{
		Entities:      entities,
		Relationships: relationships,
	}, nil
}

// generateTempID generates a unique temp_id for an entity.
func generateTempID(name, typeName string, existing map[string]bool) string {
	// Normalize the type and name
	typeSlug := strings.ToLower(typeName)
	typeSlug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(typeSlug, "_")
	if len(typeSlug) > 20 {
		typeSlug = typeSlug[:20]
	}

	nameSlug := strings.ToLower(name)
	nameSlug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(nameSlug, "_")
	if len(nameSlug) > 20 {
		nameSlug = nameSlug[:20]
	}

	baseID := fmt.Sprintf("%s_%s", typeSlug, nameSlug)
	id := baseID
	counter := 1

	for existing[id] {
		id = fmt.Sprintf("%s_%d", baseID, counter)
		counter++
	}

	return id
}

// CalculateOrphanRate calculates the percentage of entities not in any relationship.
func CalculateOrphanRate(entities []InternalEntity, relationships []ExtractedRelationship) float64 {
	if len(entities) == 0 {
		return 0
	}

	connectedIDs := make(map[string]bool)
	for _, rel := range relationships {
		connectedIDs[rel.SourceRef] = true
		connectedIDs[rel.TargetRef] = true
	}

	orphanCount := 0
	for _, e := range entities {
		if !connectedIDs[e.TempID] {
			orphanCount++
		}
	}

	return float64(orphanCount) / float64(len(entities))
}

// GetOrphanTempIDs returns temp_ids of entities without relationships.
func GetOrphanTempIDs(entities []InternalEntity, relationships []ExtractedRelationship) []string {
	connectedIDs := make(map[string]bool)
	for _, rel := range relationships {
		connectedIDs[rel.SourceRef] = true
		connectedIDs[rel.TargetRef] = true
	}

	var orphans []string
	for _, e := range entities {
		if !connectedIDs[e.TempID] {
			orphans = append(orphans, e.TempID)
		}
	}

	return orphans
}

func (p *ExtractionPipeline) createRelationshipProcessorAgent() (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        "RelationshipProcessor",
		Description: "Processes relationship builder output to ensure state visibility for quality checker",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				_, span := tracing.Start(ctx, "extraction.pipeline.extract_relationships")
				defer span.End()

				state := ctx.Session().State()

				relsRaw, err := state.Get("extracted_relationships")
				if err != nil {
					p.log.Debug("no relationships in state yet, checking for raw output")
					span.SetStatus(codes.Ok, "")
					yield(nil, nil)
					return
				}

				var relationships []ExtractedRelationship
				switch v := relsRaw.(type) {
				case []ExtractedRelationship:
					relationships = v
				case *RelationshipExtractionOutput:
					relationships = v.Relationships
				case string:
					output, err := ParseRelationshipExtractionOutput(v)
					if err != nil {
						p.log.Warn("failed to parse relationship output", slog.String("error", err.Error()))
						yield(nil, nil)
						return
					}
					relationships = output.Relationships
				default:
					data, _ := json.Marshal(v)
					var output RelationshipExtractionOutput
					if err := json.Unmarshal(data, &output); err != nil {
						p.log.Warn("failed to unmarshal relationships", slog.String("type", fmt.Sprintf("%T", v)))
						yield(nil, nil)
						return
					}
					relationships = output.Relationships
				}

				p.log.Debug("processed relationships", slog.Int("count", len(relationships)))

				span.SetAttributes(attribute.Int("emergent.extraction.relationship_count", len(relationships)))

				// Use state.Set() for immediate visibility to QualityChecker
				if err := state.Set("extracted_relationships", relationships); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					yield(nil, fmt.Errorf("failed to set extracted_relationships in state: %w", err))
					return
				}

				// Also use StateDelta for persistence to session storage
				event := session.NewEvent(ctx.InvocationID())
				event.Author = "RelationshipProcessor"
				event.Content = genai.NewContentFromText(fmt.Sprintf("Processed %d relationships", len(relationships)), "model")
				event.Actions.StateDelta = map[string]any{
					"extracted_relationships": relationships,
				}
				span.SetStatus(codes.Ok, "")
				yield(event, nil)
			}
		},
	})
}
